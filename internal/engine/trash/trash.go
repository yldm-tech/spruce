package trash

import (
	"context"
	"fmt"
	"io/fs"
	"path/filepath"
)

// ErrNoAccess is returned (wrapped) when the Trash cannot be read or emptied for lack of permission (Full Disk Access on macOS). It matches fs.ErrPermission.
var ErrNoAccess = fmt.Errorf("trash: no access to the Trash: %w", fs.ErrPermission)

// Outcome is what happened to one path given to MoveToTrash.
type Outcome struct {
	// Path is the path as given.
	Path string
	// TrashedPath is where the item now is, when the system says (macOS); empty otherwise.
	TrashedPath string
	// Dev and Ino identify the item at TrashedPath (macOS: uint64(Stat_t.Dev) and Stat_t.Ino of an os.Lstat right after the move); zero otherwise or when it could not be read.
	Dev, Ino uint64
	// Err is nil when the item is in the Trash. A missing item matches fs.ErrNotExist, a batch cut short by ctx matches its error.
	Err error
}

// TrashUsage is how much the Trash holds.
type TrashUsage struct {
	Bytes int64 `json:"bytes"`
	// Items counts the top-level items.
	Items int64 `json:"items"`
}

// Bin is a Trash: the system's (System) or a fake in tests.
type Bin interface {
	MoveToTrash(ctx context.Context, paths []string) ([]Outcome, error)
	Size(ctx context.Context) (TrashUsage, error)
	Empty(ctx context.Context) (TrashUsage, error)
}

// mover moves one absolute, cleaned path to the Trash and returns where it went.
type mover interface {
	move(path string) (string, error)
}

// System returns the Trash (macOS) or Recycle Bin (Windows) of the current user.
func System() Bin { return systemBin() }

// MoveToTrash moves paths to the system Trash.
func MoveToTrash(ctx context.Context, paths []string) ([]Outcome, error) {
	return System().MoveToTrash(ctx, paths)
}

// TrashSize returns how much the system Trash holds.
func TrashSize(ctx context.Context) (TrashUsage, error) { return System().Size(ctx) }

// Empty empties the system Trash for good.
func Empty(ctx context.Context) (TrashUsage, error) { return System().Empty(ctx) }

// moveAll moves each path with m, one at a time, checking ctx between them.
func moveAll(ctx context.Context, m mover, paths []string) ([]Outcome, error) {
	out := make([]Outcome, len(paths))
	for i, p := range paths {
		out[i].Path = p
		if err := ctx.Err(); err != nil {
			for j := i; j < len(paths); j++ {
				out[j] = Outcome{Path: paths[j], Err: err}
			}
			return out, err
		}
		if !filepath.IsAbs(p) {
			out[i].Err = fmt.Errorf("trash: path is not absolute: %q", p)
			continue
		}
		dest, err := m.move(filepath.Clean(p))
		if err != nil {
			out[i].Err = fmt.Errorf("trash: move %q: %w", p, err)
			continue
		}
		out[i].TrashedPath = dest
	}
	return out, nil
}

// Failed returns the outcomes that did not reach the Trash.
func Failed(outcomes []Outcome) []Outcome {
	var bad []Outcome
	for _, o := range outcomes {
		if o.Err != nil {
			bad = append(bad, o)
		}
	}
	return bad
}
