package trash

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strconv"
	"sync"
	"syscall"
	"unsafe"

	"github.com/ebitengine/purego"
	"github.com/ebitengine/purego/objc"
	"github.com/yldm-tech/spruce/internal/engine/fsx"
)

// Foundation error codes trashItemAtURL reports for a missing item and a permission problem (NSFileNoSuchFileError, NSFileReadNoPermissionError, NSFileWriteNoPermissionError).
const (
	nsFileNoSuchFile        = 4
	nsFileReadNoPermission  = 257
	nsFileWriteNoPermission = 513
)

const foundationPath = "/System/Library/Frameworks/Foundation.framework/Foundation"

// loadFoundation makes Foundation's classes available to the Objective-C runtime (the app has usually loaded it already).
var loadFoundation = sync.OnceValue(func() error {
	if _, err := purego.Dlopen(foundationPath, purego.RTLD_GLOBAL|purego.RTLD_NOW); err != nil {
		return fmt.Errorf("load Foundation: %w", err)
	}
	return nil
})

var (
	selNew                  = objc.RegisterName("new")
	selDrain                = objc.RegisterName("drain")
	selDefaultManager       = objc.RegisterName("defaultManager")
	selStringWithUTF8       = objc.RegisterName("stringWithUTF8String:")
	selFileURLWithPath      = objc.RegisterName("fileURLWithPath:")
	selTrashItem            = objc.RegisterName("trashItemAtURL:resultingItemURL:error:")
	selPath                 = objc.RegisterName("path")
	selUTF8String           = objc.RegisterName("UTF8String")
	selCode                 = objc.RegisterName("code")
	selDomain               = objc.RegisterName("domain")
	selUserInfo             = objc.RegisterName("userInfo")
	selObjectForKey         = objc.RegisterName("objectForKey:")
	selLocalizedDescription = objc.RegisterName("localizedDescription")
)

// foundationMover calls NSFileManager trashItemAtURL:resultingItemURL:error: through the Objective-C runtime with purego, without cgo: the app builds with CGO_ENABLED=0, as MyGo does.
type foundationMover struct{}

func nsString(s string) objc.ID {
	return objc.ID(objc.GetClass("NSString")).Send(selStringWithUTF8, s)
}

func goString(ns objc.ID) string {
	if ns == 0 {
		return ""
	}
	return objc.Send[string](ns, selUTF8String)
}

func (foundationMover) move(path string) (string, error) {
	if err := loadFoundation(); err != nil {
		return "", err
	}
	// Autorelease pools belong to a thread; everything autoreleased below is drained before the thread is released.
	runtime.LockOSThread()
	defer runtime.UnlockOSThread()
	pool := objc.ID(objc.GetClass("NSAutoreleasePool")).Send(selNew)
	defer pool.Send(selDrain)

	url := objc.ID(objc.GetClass("NSURL")).Send(selFileURLWithPath, nsString(path))
	fm := objc.ID(objc.GetClass("NSFileManager")).Send(selDefaultManager)
	// Out parameters on the heap: Foundation writes an object pointer into each.
	out := new([2]objc.ID)
	if objc.Send[bool](fm, selTrashItem, url, unsafe.Pointer(&out[0]), unsafe.Pointer(&out[1])) {
		if out[0] == 0 {
			return "", nil
		}
		return goString(out[0].Send(selPath)), nil
	}
	nserr := out[1]
	if nserr == 0 {
		return "", errors.New("trashItemAtURL failed without an error")
	}
	code := objc.Send[int](nserr, selCode)
	text := goString(nserr.Send(selLocalizedDescription))
	posix := 0
	if goString(nserr.Send(selDomain)) == "NSPOSIXErrorDomain" {
		posix = code
	} else if u := nserr.Send(selUserInfo).Send(selObjectForKey, nsString("NSUnderlyingError")); u != 0 && goString(u.Send(selDomain)) == "NSPOSIXErrorDomain" {
		posix = objc.Send[int](u, selCode)
	}
	switch {
	case posix != 0:
		return "", fmt.Errorf("%s: %w", text, syscall.Errno(posix))
	case code == nsFileNoSuchFile:
		return "", fmt.Errorf("%s: %w", text, fs.ErrNotExist)
	case code == nsFileReadNoPermission || code == nsFileWriteNoPermission:
		return "", fmt.Errorf("%s: %w", text, fs.ErrPermission)
	}
	return "", errors.New(text + " (NSError " + strconv.Itoa(code) + ")")
}

// darwinBin is the user's Trash: trashing goes through Foundation, measuring and emptying through the Trash folders.
type darwinBin struct {
	mover mover
	// dirs returns the Trash folders: ~/.Trash and the user's .Trashes folder on each mounted volume.
	dirs func() []string
}

func systemBin() Bin {
	return &darwinBin{mover: foundationMover{}, dirs: trashDirs}
}

func trashDirs() []string {
	var dirs []string
	if home, err := os.UserHomeDir(); err == nil {
		dirs = append(dirs, filepath.Join(home, ".Trash"))
	}
	uid := strconv.Itoa(os.Getuid())
	vols, _ := os.ReadDir("/Volumes")
	for _, v := range vols {
		// The startup volume appears in /Volumes as a symlink to /; its Trash is ~/.Trash.
		if v.Type()&fs.ModeSymlink != 0 || !v.IsDir() {
			continue
		}
		dir := filepath.Join("/Volumes", v.Name(), ".Trashes", uid)
		if info, err := os.Lstat(dir); err == nil && info.IsDir() {
			dirs = append(dirs, dir)
		}
	}
	return dirs
}

func (b *darwinBin) MoveToTrash(ctx context.Context, paths []string) ([]Outcome, error) {
	return moveAll(ctx, b.mover, paths)
}

func (b *darwinBin) Size(ctx context.Context) (TrashUsage, error) {
	var u TrashUsage
	for i, dir := range b.dirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errNotExist(err) {
				continue
			}
			if i == 0 && errors.Is(err, fs.ErrPermission) {
				return TrashUsage{}, ErrNoAccess
			}
			return u, fmt.Errorf("trash: read %q: %w", dir, err)
		}
		for _, e := range entries {
			if e.Name() == ".DS_Store" {
				continue
			}
			u.Items++
		}
		res, err := fsx.DirSize(ctx, dir, fsx.SizeOptions{})
		if err != nil {
			return u, fmt.Errorf("trash: measure %q: %w", dir, err)
		}
		u.Bytes += res.Bytes
	}
	return u, nil
}

func (b *darwinBin) Empty(ctx context.Context) (TrashUsage, error) {
	return emptyDirs(ctx, b.dirs())
}

// emptyDirs deletes the contents of each Trash folder for good, keeping the folders, and returns what it freed. Items that could not be deleted are reported in the error.
func emptyDirs(ctx context.Context, dirs []string) (TrashUsage, error) {
	var freed TrashUsage
	var errs []error
	for i, dir := range dirs {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errNotExist(err) {
				continue
			}
			if i == 0 && errors.Is(err, fs.ErrPermission) {
				return freed, ErrNoAccess
			}
			errs = append(errs, fmt.Errorf("trash: read %q: %w", dir, err))
			continue
		}
		for _, e := range entries {
			rm, err := fsx.SafeRemove(ctx, filepath.Join(dir, e.Name()), []string{dir})
			freed.Bytes += rm.Freed
			if err != nil {
				if ctx.Err() != nil {
					return freed, ctx.Err()
				}
				errs = append(errs, err)
				continue
			}
			if len(rm.Failures) > 0 {
				errs = append(errs, fmt.Errorf("trash: %d entries of %q could not be deleted, first %w", len(rm.Failures), e.Name(), rm.Failures[0]))
				continue
			}
			if e.Name() != ".DS_Store" {
				freed.Items++
			}
		}
	}
	return freed, errors.Join(errs...)
}

// errNotExist reports whether err means the folder is not there (a volume without a Trash yet).
func errNotExist(err error) bool { return errors.Is(err, fs.ErrNotExist) }
