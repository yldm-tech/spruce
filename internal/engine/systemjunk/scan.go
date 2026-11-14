package systemjunk

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/fsx"
)

// spec is one category of a platform's plan: the category as the page sees it (with its Roots), an optional sibling for items whose app is running, and how to find its candidates.
type spec struct {
	cat cleanup.Category
	// busy, when set, receives the candidates marked busy; it starts unchecked.
	busy    *cleanup.Category
	collect func(ctx context.Context, sc *scanCtx) []candidate
}

// candidate is a file or folder a spec found, before it is measured.
type candidate struct {
	path string
	// info is from os.Lstat.
	info fs.FileInfo
	note string
	// busy marks an item whose app is running.
	busy bool
	// size is filled in by measure.
	size int64
}

// scanCtx is the state the collectors of one scan share.
type scanCtx struct {
	env     Env
	running Running
	now     time.Time
	// denied counts folders that could not be read for lack of permission.
	denied atomic.Int64
	report func(path string)
}

// entry is one child of a folder: a plain file or folder, never a symlink.
type entry struct {
	name string
	path string
	info fs.FileInfo
}

// entries lists the plain files and folders in dir, each with its Lstat info; symlinks, junctions and devices are left out, so nothing is ever followed. A missing folder lists nothing; an unreadable one counts as denied unless quiet.
func (sc *scanCtx) entries(dir string, quiet bool) []entry {
	sc.report(dir)
	des, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) && !quiet {
			sc.denied.Add(1)
		}
		return nil
	}
	out := make([]entry, 0, len(des))
	for _, de := range des {
		if t := de.Type(); t != 0 && t != fs.ModeDir {
			continue
		}
		info, err := de.Info()
		if err != nil {
			continue // gone since the folder was read
		}
		out = append(out, entry{name: de.Name(), path: filepath.Join(dir, de.Name()), info: info})
	}
	return out
}

// lstatDir returns the Lstat info of path when it is a plain folder.
func lstatDir(path string) (fs.FileInfo, bool) {
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() || info.Mode().Type() != fs.ModeDir {
		return nil, false
	}
	return info, true
}

// newest is the latest modification time of e and, for a folder, of its direct children: a folder's own time changes only when entries are added or removed, so a program writing into an existing file inside shows only on that file. Deeper changes are not seen; the categories that use it delete only what fsx.SafeRemove can, and on Windows files still held open are skipped.
func newest(e entry) time.Time {
	t := e.info.ModTime()
	if !e.info.IsDir() {
		return t
	}
	des, err := os.ReadDir(e.path)
	if err != nil {
		return t
	}
	for _, de := range des {
		if info, err := de.Info(); err == nil && info.ModTime().After(t) {
			t = info.ModTime()
		}
	}
	return t
}

// children offers every entry of each of dirs that keep accepts (nil keeps all), with note.
func children(dirs []string, note string, keep func(sc *scanCtx, e entry) bool) func(context.Context, *scanCtx) []candidate {
	return func(ctx context.Context, sc *scanCtx) []candidate {
		var out []candidate
		for _, dir := range dirs {
			if ctx.Err() != nil {
				return out
			}
			for _, e := range sc.entries(dir, false) {
				if keep == nil || keep(sc, e) {
					out = append(out, candidate{path: e.path, info: e.info, note: note})
				}
			}
		}
		return out
	}
}

// notHidden accepts entries whose names do not start with a dot (.DS_Store, .localized).
func notHidden(_ *scanCtx, e entry) bool { return !strings.HasPrefix(e.name, ".") }

// olderThan accepts entries not modified (newest) within d.
func olderThan(d time.Duration) func(*scanCtx, entry) bool {
	return func(sc *scanCtx, e entry) bool { return sc.now.Sub(newest(e)) >= d }
}

// incomplete offers the entries directly in dir whose extension is one of exts (lower case), untouched for d. Safari's .download is a folder, the others are files.
func incomplete(dir string, exts []string, d time.Duration) func(context.Context, *scanCtx) []candidate {
	return func(_ context.Context, sc *scanCtx) []candidate {
		var out []candidate
		for _, e := range sc.entries(dir, false) {
			ext := strings.ToLower(filepath.Ext(e.name))
			if ext == "" || strings.HasPrefix(e.name, ".") {
				continue
			}
			for _, want := range exts {
				if ext == want && sc.now.Sub(newest(e)) >= d {
					out = append(out, candidate{path: e.path, info: e.info})
					break
				}
			}
		}
		return out
	}
}

// reporter serializes progress events: the Scanner contract says they never arrive concurrently.
type reporter struct {
	mu    sync.Mutex
	fn    func(cleanup.Progress)
	p     cleanup.Progress
	every time.Duration
	last  time.Time
}

func newReporter(fn func(cleanup.Progress)) *reporter {
	if fn == nil {
		fn = func(cleanup.Progress) {}
	}
	return &reporter{fn: fn, p: cleanup.Progress{Phase: cleanup.PhaseScanning}, every: 50 * time.Millisecond}
}

// path reports the folder being read, at most every r.every.
func (r *reporter) path(p string) {
	r.mu.Lock()
	defer r.mu.Unlock()
	if now := time.Now(); now.Sub(r.last) >= r.every {
		r.last = now
		r.p.Path = p
		r.fn(r.p)
	}
}

func (r *reporter) setTotal(n int) {
	r.mu.Lock()
	r.p.Total = n
	r.mu.Unlock()
}

// step reports one measured item.
func (r *reporter) step(path string, size int64) {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.p.Done++
	r.p.Bytes += size
	r.p.Path = path
	r.last = time.Now()
	r.fn(r.p)
}

// done reports the end of the scan.
func (r *reporter) done() {
	r.mu.Lock()
	defer r.mu.Unlock()
	r.p.Done = r.p.Total
	r.p.Path = ""
	r.fn(r.p)
}

// measure sizes every candidate with fsx.DirSize, measureWorkers at a time, and returns the ones to offer, per spec. A candidate that is excluded, contains an excluded path, or cannot be read is dropped. Only a cancelled ctx is an error.
func measure(ctx context.Context, sc *scanCtx, cands [][]candidate, exclusions []string, rep *reporter) ([][]candidate, error) {
	type job struct{ spec, idx int }
	var jobs []job
	for i, list := range cands {
		for j := range list {
			jobs = append(jobs, job{i, j})
		}
	}
	rep.setTotal(len(jobs))
	keep := make([][]bool, len(cands))
	for i, list := range cands {
		keep[i] = make([]bool, len(list))
	}

	queue := make(chan job)
	var wg sync.WaitGroup
	for range measureWorkers {
		wg.Go(func() {
			for j := range queue {
				c := &cands[j.spec][j.idx]
				size, ok := measureOne(ctx, sc, c.path, exclusions, rep)
				c.size = size
				keep[j.spec][j.idx] = ok
				rep.step(c.path, size)
			}
		})
	}
feed:
	for _, j := range jobs {
		select {
		case queue <- j:
		case <-ctx.Done():
			break feed
		}
	}
	close(queue)
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	out := make([][]candidate, len(cands))
	for i, list := range cands {
		for j, c := range list {
			if keep[i][j] {
				out[i] = append(out[i], c)
			}
		}
	}
	return out, nil
}

// measureOne returns the size of path and whether to offer it.
func measureOne(ctx context.Context, sc *scanCtx, path string, exclusions []string, rep *reporter) (int64, bool) {
	if ctx.Err() != nil {
		return 0, false
	}
	if len(exclusions) > 0 && fsx.Excluded(path, exclusions) {
		return 0, false
	}
	var hit atomic.Bool
	opts := fsx.SizeOptions{
		Workers:       sizeWorkers,
		ProgressEvery: 150 * time.Millisecond,
		Progress:      func(p fsx.SizeProgress) { rep.path(p.Path) },
	}
	if len(exclusions) > 0 {
		// Deleting the item would delete everything in it, so an excluded path inside keeps the whole item.
		opts.Skip = func(p string) bool {
			if fsx.Excluded(p, exclusions) {
				hit.Store(true)
				return true
			}
			return false
		}
	}
	res, err := fsx.DirSize(ctx, path, opts)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			sc.denied.Add(1)
		}
		return 0, false
	}
	if hit.Load() {
		return 0, false
	}
	return res.Bytes, true
}
