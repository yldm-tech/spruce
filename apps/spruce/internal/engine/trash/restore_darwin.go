package trash

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"

	"golang.org/x/sys/unix"

	"spruce/apps/spruce/internal/engine/fsx"
)

const canRestore = true

// maxKeepBoth is the highest number KeepBoth tries: "<stem> 2<ext>" to "<stem> 99<ext>".
const maxKeepBoth = 99

// darwinRestorer renames items out of the Trash folders dirs returns. It never asks Finder (an AppleScript "put back" would raise the Automation permission prompt): reading and renaming a known name inside ~/.Trash needs no Full Disk Access.
type darwinRestorer struct {
	dirs func() []string
	// check reports why path must not be put back inside roots (fsx.CheckTrashable: absolute, strictly inside a usable root, not a protected folder).
	check func(path string, roots []string) error
}

// newRestorer returns a Restorer over the Trash folders dirs returns; tests pass folders of their own.
func newRestorer(dirs func() []string) *darwinRestorer {
	return &darwinRestorer{dirs: dirs, check: fsx.CheckTrashable}
}

func systemRestorer() Restorer { return newRestorer(trashDirs) }

func inTrash(path string) bool { return directlyIn(path, trashDirs()) }

func inTrashNow() func(string) bool {
	dirs := trashDirs()
	return func(path string) bool { return directlyIn(path, dirs) }
}

func (r *darwinRestorer) Restore(ctx context.Context, req RestoreRequest) (string, error) {
	if err := ctx.Err(); err != nil {
		return "", err
	}
	if !filepath.IsAbs(req.TrashedPath) || !filepath.IsAbs(req.Original) || !filepath.IsAbs(req.Root) {
		return "", fmt.Errorf("trash: restore %q: paths must be absolute", req.Original)
	}
	from, original, root := filepath.Clean(req.TrashedPath), filepath.Clean(req.Original), filepath.Clean(req.Root)
	// A Trash folder that is not there now (an unmounted volume) holds nothing to put back.
	if !directlyIn(from, r.dirs()) {
		return "", fmt.Errorf("%w: %q is not in a Trash folder", ErrGone, from)
	}
	if err := r.checkTrashed(from, req); err != nil {
		return "", err
	}
	if err := checkParents(root, original); err != nil {
		return "", err
	}
	if err := r.check(original, []string{root}); err != nil {
		return "", fmt.Errorf("trash: restore %q: %w", original, err)
	}
	dest := original
	for n := 2; ; n++ {
		err := unix.RenamexNp(from, dest, unix.RENAME_EXCL)
		switch {
		case err == nil:
			return dest, nil
		case errors.Is(err, unix.EEXIST):
			if !req.KeepBoth || n > maxKeepBoth {
				return "", fmt.Errorf("%w: %q", ErrOccupied, original)
			}
			dest = keepBothName(original, n)
			continue
		case errors.Is(err, unix.ENOENT):
			// The source vanished or a folder above the destination did, since the checks.
			if _, serr := os.Lstat(from); errors.Is(serr, fs.ErrNotExist) {
				return "", fmt.Errorf("%w: %q", ErrGone, from)
			}
			return "", fmt.Errorf("%w: %q", ErrParentGone, filepath.Dir(original))
		}
		return "", fmt.Errorf("trash: put back %q: %w", original, err)
	}
}

// checkTrashed makes sure the item at from is the one the move left there: there, not a symlink, of the same type, with the same device and inode.
func (r *darwinRestorer) checkTrashed(from string, req RestoreRequest) error {
	info, err := os.Lstat(from)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return fmt.Errorf("%w: %q", ErrGone, from)
	case err != nil:
		return fmt.Errorf("trash: read %q: %w", from, err)
	case info.Mode()&fs.ModeSymlink != 0, info.IsDir() != req.Dir:
		return fmt.Errorf("%w: %q changed type", ErrReplaced, from)
	}
	st, ok := info.Sys().(*syscall.Stat_t)
	// Without the identity recorded at the move nothing proves it is the same item.
	if !ok || req.Ino == 0 || uint64(st.Dev) != req.Dev || st.Ino != req.Ino {
		return fmt.Errorf("%w: %q has another identity", ErrReplaced, from)
	}
	return nil
}

// checkParents makes sure root and every folder from it down to original's parent is a real folder (not a symlink), without creating any; original must lie strictly inside root.
func checkParents(root, original string) error {
	rel, err := filepath.Rel(root, original)
	if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) || filepath.IsAbs(rel) {
		return fmt.Errorf("trash: restore %q: %w", original, fsx.ErrOutsideRoots)
	}
	dir := root
	parts := strings.Split(filepath.Dir(rel), string(filepath.Separator))
	if filepath.Dir(rel) == "." {
		parts = nil
	}
	for i := -1; i < len(parts); i++ {
		if i >= 0 {
			dir = filepath.Join(dir, parts[i])
		}
		info, err := os.Lstat(dir)
		switch {
		case errors.Is(err, fs.ErrNotExist):
			return fmt.Errorf("%w: %q", ErrParentGone, dir)
		case err != nil:
			return fmt.Errorf("trash: read %q: %w", dir, err)
		case !info.IsDir() || info.Mode()&fs.ModeSymlink != 0:
			return fmt.Errorf("%w: %q is not a real folder", ErrParentGone, dir)
		}
	}
	return nil
}
