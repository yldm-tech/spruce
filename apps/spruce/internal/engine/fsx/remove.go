package fsx

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
)

// Removal is what SafeRemove did.
type Removal struct {
	// Freed is the space given back: allocated bytes on macOS (a hard-linked file frees nothing until its last link goes), file sizes on Windows.
	Freed int64
	// Removed counts the files, links and folders deleted.
	Removed int
	// Failures lists every entry below the path that could not be deleted; their folders are left in place.
	Failures []Failure
}

// Failure is one entry SafeRemove could not delete.
type Failure struct {
	Path string
	Err  error
	// InUse is set when another program holds the file open (Windows sharing or lock violation).
	InUse bool
}

func (f Failure) Error() string { return f.Path + ": " + f.Err.Error() }

// SafeRemove permanently deletes path if CheckRemovable accepts it. The error is non-nil when path was refused (ErrNotAbsolute, ErrOutsideRoots, ErrProtected), does not exist (fs.ErrNotExist), or ctx was cancelled part way, in which case the Removal so far comes with it. Entries below path that could not be deleted are in Removal.Failures and do not make the error non-nil.
func SafeRemove(ctx context.Context, path string, allowedRoots []string) (Removal, error) {
	return safeRemove(ctx, systemPolicy(), path, allowedRoots)
}

func safeRemove(ctx context.Context, p policy, path string, roots []string) (Removal, error) {
	resolved, root, err := p.check(path, roots, true)
	if err != nil {
		return Removal{}, err
	}
	rel, err := filepath.Rel(root, resolved)
	if err != nil {
		return Removal{}, fmt.Errorf("fsx: %w", err)
	}
	r, err := os.OpenRoot(root)
	if err != nil {
		return Removal{}, fmt.Errorf("fsx: open root %q: %w", root, err)
	}
	defer r.Close()
	if _, err := r.Lstat(rel); err != nil {
		return Removal{}, fmt.Errorf("fsx: %w", err)
	}
	rm := &remover{ctx: ctx, root: r, base: root}
	rm.remove(rel)
	if err := ctx.Err(); err != nil {
		return rm.out, err
	}
	return rm.out, nil
}

type remover struct {
	ctx  context.Context
	root *os.Root
	base string
	out  Removal
}

func (rm *remover) fail(rel string, err error) {
	rm.out.Failures = append(rm.out.Failures, Failure{Path: filepath.Join(rm.base, rel), Err: err, InUse: inUse(err)})
}

// remove deletes rel (relative to the root) depth first and reports whether it is gone. A folder whose contents could not all be deleted stays.
func (rm *remover) remove(rel string) bool {
	if rm.ctx.Err() != nil {
		return false
	}
	info, err := rm.root.Lstat(rel)
	if errors.Is(err, fs.ErrNotExist) {
		return true
	}
	if err != nil {
		rm.fail(rel, err)
		return false
	}
	if info.Mode().Type() == fs.ModeDir {
		names, err := rm.names(rel)
		if err != nil {
			rm.fail(rel, err)
			return false
		}
		all := true
		for _, name := range names {
			if !rm.remove(filepath.Join(rel, name)) {
				all = false
			}
		}
		if !all {
			return false
		}
	}
	// A symlink, junction or other special entry is removed itself; os.Root never lets the removal leave the root.
	if err := rm.root.Remove(rel); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return true
		}
		rm.fail(rel, err)
		return false
	}
	if info.Mode().Type() == 0 {
		if alloc, _, linked := usage(info); !linked {
			rm.out.Freed += alloc
		}
	}
	rm.out.Removed++
	return true
}

// names lists a folder's entries before any of them is deleted, so deleting does not disturb the directory read.
func (rm *remover) names(rel string) ([]string, error) {
	f, err := rm.root.Open(rel)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Readdirnames(-1)
}
