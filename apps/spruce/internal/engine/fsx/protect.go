package fsx

import (
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"sync"
)

// Errors of CheckRemovable, CheckTrashable and SafeRemove. Match them with errors.Is; the returned error names the path.
var (
	// ErrNotAbsolute is returned for a relative path.
	ErrNotAbsolute = errors.New("fsx: path is not absolute")
	// ErrOutsideRoots is returned for a path that is not strictly inside any usable allowed root.
	ErrOutsideRoots = errors.New("fsx: path is outside the allowed roots")
	// ErrProtected is returned for a protected folder, or a path inside a protected subtree (SafeRemove only).
	ErrProtected = errors.New("fsx: path is protected")
)

// policy is the protection of one platform and user.
type policy struct {
	// exact are folders that are never deleted themselves.
	exact []string
	// subtree are folders at or under which nothing is deleted permanently.
	subtree []string
	// volumeRoot reports whether a cleaned absolute path is the root of a volume.
	volumeRoot func(string) bool
	// home is the user's home folder ("" if unknown).
	home string
}

var systemPolicy = sync.OnceValue(func() policy {
	home, err := os.UserHomeDir()
	if err != nil {
		home = ""
	}
	return newPolicy(home)
})

// newPolicy builds the protection of this platform for home, resolved through symlinks so it compares with resolved paths.
func newPolicy(home string) policy {
	if home != "" {
		if r, err := filepath.EvalSymlinks(home); err == nil {
			home = r
		}
	}
	return platformPolicy(home)
}

// CheckRemovable reports why path must not be deleted permanently, or nil.
func CheckRemovable(path string, allowedRoots []string) error {
	_, _, err := systemPolicy().check(path, allowedRoots, true)
	return err
}

// CheckTrashable reports why path must not be moved to the Trash, or nil. It differs from CheckRemovable only in allowing the protected subtrees.
func CheckTrashable(path string, allowedRoots []string) error {
	_, _, err := systemPolicy().check(path, allowedRoots, false)
	return err
}

// check resolves path and returns it with the allowed root that contains it.
func (p policy) check(path string, roots []string, permanent bool) (resolved, root string, err error) {
	if !filepath.IsAbs(path) {
		return "", "", fmt.Errorf("%w: %q", ErrNotAbsolute, path)
	}
	clean := filepath.Clean(path)
	// The parent is resolved, the leaf is not: a symlink is deleted itself, never what it points to.
	parent, err := filepath.EvalSymlinks(filepath.Dir(clean))
	if err != nil {
		return "", "", fmt.Errorf("fsx: resolve %q: %w", path, err)
	}
	resolved = filepath.Join(parent, filepath.Base(clean))
	for _, r := range roots {
		rr, ok := p.usableRoot(r, permanent)
		if ok && hasPathPrefix(resolved, rr, foldRoots) && !samePath(resolved, rr, foldRoots) {
			root = rr
			break
		}
	}
	switch {
	case root == "":
		return "", "", fmt.Errorf("%w: %q", ErrOutsideRoots, path)
	case p.isExact(resolved):
		return "", "", fmt.Errorf("%w: %q", ErrProtected, path)
	case permanent && p.inSubtree(resolved):
		return "", "", fmt.Errorf("%w: %q is inside a protected folder", ErrProtected, path)
	}
	return resolved, root, nil
}

// usableRoot resolves an allowed root; a relative or missing root, a volume root, the home folder or a folder above it, and (for permanent deletion) anything inside a protected subtree are not usable.
func (p policy) usableRoot(r string, permanent bool) (string, bool) {
	if !filepath.IsAbs(r) {
		return "", false
	}
	rr, err := filepath.EvalSymlinks(filepath.Clean(r))
	if err != nil {
		return "", false
	}
	// The home folder or any folder above it would make the whole home deletable.
	if p.volumeRoot(rr) || (p.home != "" && hasPathPrefix(p.home, rr, true)) || (permanent && p.inSubtree(rr)) {
		return "", false
	}
	return rr, true
}

func (p policy) isExact(path string) bool {
	if p.volumeRoot(path) || (p.home != "" && hasPathPrefix(p.home, path, true)) {
		return true
	}
	for _, e := range p.exact {
		if samePath(path, e, true) {
			return true
		}
	}
	return false
}

func (p policy) inSubtree(path string) bool {
	for _, s := range p.subtree {
		if hasPathPrefix(path, s, true) {
			return true
		}
	}
	return false
}

// samePath compares two cleaned paths, ignoring case when fold is set.
func samePath(a, b string, fold bool) bool {
	if fold {
		return strings.EqualFold(a, b)
	}
	return a == b
}

// hasPathPrefix reports whether p is base or inside it, by whole path components.
func hasPathPrefix(p, base string, fold bool) bool {
	if samePath(p, base, fold) {
		return true
	}
	if !strings.HasSuffix(base, string(filepath.Separator)) {
		base += string(filepath.Separator)
	}
	return len(p) > len(base) && samePath(p[:len(base)], base, fold)
}

// joinAll joins each of rel onto base.
func joinAll(base string, rel ...string) []string {
	out := make([]string, 0, len(rel))
	for _, r := range rel {
		out = append(out, filepath.Join(base, r))
	}
	return out
}
