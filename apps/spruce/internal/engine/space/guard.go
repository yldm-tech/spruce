package space

import (
	"errors"
	"fmt"
	"path/filepath"

	"spruce/apps/spruce/internal/engine/fsx"
)

// Reasons Guard.Check refuses a path. Match them with errors.Is.
var (
	// ErrOutside: the path is neither inside the home folder nor inside the scanned folder (a whole startup disk counts only for its applications folder on macOS).
	ErrOutside = errors.New("space: outside the home folder and the scanned folder")
	// ErrTopLevel: the path is directly in the home folder or in a volume root, where fsx accepts no allowed root to vouch for it.
	ErrTopLevel = errors.New("space: directly in the home folder or a volume root")
	// ErrSystem: the path is in a system location outside the home folder.
	ErrSystem = errors.New("space: system location")
	// ErrProtected: the path is a folder fsx never removes (Documents, Library, Applications …).
	ErrProtected = errors.New("space: protected folder")
	// ErrExcluded: the path matches the exclusions of the settings.
	ErrExcluded = errors.New("space: excluded in the settings")
)

// Guard decides whether the page may move a path of a scan to the Trash. Moving to the Trash can be undone, so it asks less than permanent deletion, but it still never touches what is not the user's to move.
type Guard struct {
	// Home is the user's home folder.
	Home string
	// Target is the folder that was scanned.
	Target string
	// Exclusions are settings.Cleanup.Exclusions (fsx.Excluded).
	Exclusions []string
	// System lists folders outside the home folder at or under which nothing is moved (SystemLocations).
	System []string
	// StartupRoot is the root of the startup volume ("/" on macOS, the system drive on Windows). A scan of it does not make the whole disk movable: only the home folder and Apps are.
	StartupRoot string
	// Apps is the folder whose applications a scan of StartupRoot may move to the Trash (/Applications on macOS, none on Windows).
	Apps string
}

// NewGuard returns the Guard of this platform for a scan of target.
func NewGuard(home, target string, exclusions []string) Guard {
	return Guard{
		Home:        home,
		Target:      target,
		Exclusions:  exclusions,
		System:      SystemLocations(),
		StartupRoot: startupRoot(),
		Apps:        appsFolder(),
	}
}

// Check reports why path must not be moved to the Trash, or nil. On top of fsx.CheckTrashable, which judges path against its own parent folder as the allowed root, it requires path to be strictly inside the home folder or the scanned folder, refuses system locations outside the home folder and anything excluded in the settings.
func (g Guard) Check(path string) error {
	if !filepath.IsAbs(path) {
		return fmt.Errorf("%w: %q", ErrOutside, path)
	}
	clean := filepath.Clean(path)
	if fsx.Excluded(clean, g.Exclusions) {
		return fmt.Errorf("%w: %q", ErrExcluded, path)
	}
	inHome := strictlyWithin(clean, g.Home)
	wholeDisk := g.StartupRoot != "" && samePath(filepath.Clean(g.Target), g.StartupRoot)
	inTarget := !wholeDisk && strictlyWithin(clean, g.Target)
	inApps := wholeDisk && strictlyWithin(clean, g.Apps)
	if !inHome && !inTarget && !inApps {
		return fmt.Errorf("%w: %q", ErrOutside, path)
	}
	parent := filepath.Dir(clean)
	if samePath(parent, g.Home) || filepath.Dir(parent) == parent || isVolumeRoot(parent) {
		return fmt.Errorf("%w: %q", ErrTopLevel, path)
	}
	if !inHome {
		for _, s := range g.System {
			if within(clean, s) {
				return fmt.Errorf("%w: %q", ErrSystem, path)
			}
		}
		// Outside the home folder the stricter rules of permanent deletion apply: nothing in the operating system's subtrees (fsx refuses them as roots, too).
		if err := fsx.CheckRemovable(clean, []string{parent}); errors.Is(err, fsx.ErrProtected) || errors.Is(err, fsx.ErrOutsideRoots) {
			return fmt.Errorf("%w: %q", ErrSystem, path)
		}
	}
	if err := fsx.CheckTrashable(clean, []string{parent}); err != nil {
		if errors.Is(err, fsx.ErrProtected) {
			return fmt.Errorf("%w: %q", ErrProtected, path)
		}
		if errors.Is(err, fsx.ErrOutsideRoots) {
			return fmt.Errorf("%w: %q", ErrTopLevel, path)
		}
		return err
	}
	return nil
}
