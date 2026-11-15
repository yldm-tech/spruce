package growth

import (
	"cmp"
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/fsx"
)

// ErrBudget is what Take returns when the snapshot took longer than TakeOptions.Budget; nothing of it should be saved.
var ErrBudget = errors.New("growth: the snapshot took longer than its budget")

// The defaults of TakeOptions.
const (
	defaultWorkers = 2
	defaultBudget  = 10 * time.Minute
)

// Take measures watches with fsx.DirSize (allocated blocks on macOS, hard links once, no symlinks followed, no other volumes entered): the total of each, and each child of those with Children. A watch that does not exist, or is not a folder, is left out; one the read gate refuses (MayRead on the location or on any of its children, or an app-data watch without Full Disk Access) is kept as Skipped, without sizes; what cannot be read for lack of permission counts in Denied and asks nothing. Past the budget it returns ErrBudget, when ctx ends ctx's error, and nothing else.
func Take(ctx context.Context, watches []Watch, opts TakeOptions) (Snapshot, error) {
	if opts.Workers <= 0 {
		opts.Workers = defaultWorkers
	}
	if opts.Budget <= 0 {
		opts.Budget = defaultBudget
	}
	run, cancel := context.WithTimeoutCause(ctx, opts.Budget, ErrBudget)
	defer cancel()
	snap := Snapshot{Version: Version, At: time.Now(), Locations: []Location{}}
	usageAt := ""
	for _, w := range watches {
		loc, ok := measure(run, w, opts)
		if err := run.Err(); err != nil {
			if ctx.Err() == nil && errors.Is(context.Cause(run), ErrBudget) {
				return Snapshot{}, ErrBudget
			}
			return Snapshot{}, ctx.Err()
		}
		if !ok {
			continue
		}
		if usageAt == "" && !loc.Skipped {
			usageAt = loc.Path
		}
		snap.Locations = append(snap.Locations, loc)
	}
	if opts.Usage != nil && usageAt != "" {
		if total, free, err := opts.Usage(usageAt); err == nil {
			snap.Total, snap.Free = total, free
		}
	}
	return snap, nil
}

// measure returns w's location, and false when it does not exist or is not a folder.
func measure(ctx context.Context, w Watch, opts TakeOptions) (Location, bool) {
	path := filepath.Clean(w.Path)
	loc := Location{ID: w.ID, Path: path}
	if AppDataWatch(w) && (opts.FullDiskAccess == nil || !opts.FullDiskAccess()) {
		loc.Skipped = true
		return loc, true
	}
	if opts.MayRead != nil && !opts.MayRead(path) {
		loc.Skipped = true
		return loc, true
	}
	info, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrPermission):
		loc.Denied = 1
		return loc, true
	case err != nil, !info.IsDir():
		return Location{}, false
	}
	sizeOpts := fsx.SizeOptions{Workers: opts.Workers}
	if !w.Children {
		r, err := fsx.DirSize(ctx, path, sizeOpts)
		loc.Bytes, loc.Denied = r.Bytes, r.Denied
		if err != nil && ctx.Err() == nil {
			loc.Denied++
		}
		return loc, true
	}
	entries, err := os.ReadDir(path)
	if err != nil {
		loc.Denied = 1
		return loc, true
	}
	rootDev, checkDev := deviceOf(info)
	var children []Entry
	for _, e := range entries {
		if ctx.Err() != nil {
			return loc, true
		}
		child := filepath.Join(path, e.Name())
		if opts.MayRead != nil && !opts.MayRead(child) {
			// A location with a child left unread would not compare with one read whole.
			return Location{ID: w.ID, Path: path, Skipped: true}, true
		}
		switch e.Type() {
		case 0:
		case fs.ModeDir:
			if checkDev {
				ci, err := e.Info()
				if err != nil {
					continue
				}
				if dev, ok := deviceOf(ci); ok && dev != rootDev {
					continue
				}
			}
		default:
			// Symlinks, Windows reparse points, devices and sockets are not measured, as fsx.DirSize does.
			continue
		}
		r, err := fsx.DirSize(ctx, child, sizeOpts)
		loc.Bytes += r.Bytes
		loc.Denied += r.Denied
		if err != nil && ctx.Err() == nil && errors.Is(err, fs.ErrPermission) {
			loc.Denied++
		}
		if r.Bytes >= ChildMin {
			children = append(children, Entry{Name: e.Name(), Bytes: r.Bytes})
		}
	}
	slices.SortFunc(children, func(a, b Entry) int {
		if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
			return c
		}
		return cmp.Compare(a.Name, b.Name)
	})
	if len(children) > MaxChildren {
		children = children[:MaxChildren]
	}
	listed := int64(0)
	for _, c := range children {
		listed += c.Bytes
	}
	loc.Children, loc.Rest = children, loc.Bytes-listed
	return loc, true
}
