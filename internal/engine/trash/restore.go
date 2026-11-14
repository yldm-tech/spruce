package trash

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"strconv"
	"strings"
)

// Errors of Restorer.Restore. Match them with errors.Is; the returned error names the paths.
var (
	// ErrRestoreUnsupported is returned where Spruce cannot put items back (Windows: the shell does not say where an item went in the Recycle Bin).
	ErrRestoreUnsupported = errors.New("trash: restoring is not supported on this platform")
	// ErrGone is returned when the item is no longer in the Trash (emptied, put back by hand, or its volume is not mounted). It matches fs.ErrNotExist.
	ErrGone = fmt.Errorf("trash: the item is no longer in the Trash: %w", fs.ErrNotExist)
	// ErrOccupied is returned when something is at the original location and KeepBoth was not asked for (or every other name is taken too).
	ErrOccupied = errors.New("trash: something is at the original location")
	// ErrParentGone is returned when a folder between the allowed root and the original location is gone or is no longer a real folder; Restore never creates folders.
	ErrParentGone = errors.New("trash: the original folder no longer exists")
	// ErrReplaced is returned when the item in the Trash is not the one moved there: another type, a symlink, or another device or inode.
	ErrReplaced = errors.New("trash: the item in the Trash is not the one moved there")
)

// maxKeepBoth is the highest number KeepBoth tries: "<stem> 2<ext>" to "<stem> 99<ext>".
const maxKeepBoth = 99

// RestoreRequest is one item to put back.
type RestoreRequest struct {
	// TrashedPath is where the item is in the Trash; Original is where it was.
	TrashedPath, Original string
	// Root is the allowed root the item was inside; Original must be strictly inside it and Root must still exist.
	Root string
	// Dir says whether the item is a folder.
	Dir bool
	// Dev and Ino identify the item in the Trash as it was right after the move.
	Dev, Ino uint64
	// KeepBoth restores under "<stem> 2<ext>" … "<stem> 99<ext>" when Original is taken; otherwise ErrOccupied.
	KeepBoth bool
}

// Restorer puts items back from the Trash.
type Restorer interface {
	// Restore moves the item back and returns where it went.
	Restore(ctx context.Context, req RestoreRequest) (string, error)
}

// SystemRestorer returns the Restorer of the user's Trash: renaming out of ~/.Trash and the volumes' .Trashes folders on macOS; one that always answers ErrRestoreUnsupported on Windows.
func SystemRestorer() Restorer { return systemRestorer() }

// CanRestore reports whether Restore works on this platform: true on macOS, false on Windows.
func CanRestore() bool { return canRestore }

// InTrash reports whether path lies directly inside one of the Trash folders Restore accepts (macOS: ~/.Trash and /Volumes/*/.Trashes/<uid>); false on Windows. A move the system put elsewhere (iCloud Drive, a File Provider's own trash) is not restorable by Spruce.
func InTrash(path string) bool { return inTrash(path) }

// directlyIn reports whether path is a direct child of one of dirs, comparing cleaned absolute paths.
func directlyIn(path string, dirs []string) bool {
	if path == "" || !filepath.IsAbs(path) {
		return false
	}
	parent := filepath.Dir(filepath.Clean(path))
	for _, d := range dirs {
		if d != "" && filepath.Clean(d) == parent {
			return true
		}
	}
	return false
}

// keepBothName is the n-th other name for original in the same folder, as Finder names copies: "report 2.pdf", "Photos 3"; a name that starts with its only dot keeps it whole (".env 2").
func keepBothName(original string, n int) string {
	dir, base := filepath.Split(original)
	ext := filepath.Ext(base)
	stem := strings.TrimSuffix(base, ext)
	if stem == "" {
		stem, ext = base, ""
	}
	return filepath.Join(dir, stem+" "+strconv.Itoa(n)+ext)
}
