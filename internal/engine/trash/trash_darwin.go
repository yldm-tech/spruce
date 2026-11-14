package trash

/*
#cgo CFLAGS: -x objective-c -fobjc-arc
#cgo LDFLAGS: -framework Foundation
#include <stdlib.h>
#import <Foundation/Foundation.h>

// spruce_trash moves path to the Trash. It returns 0 on success with *dest set to the item's new path, or the NSError code with *msg set to its description and *posix to the underlying POSIX errno (0 if none). The caller frees *dest and *msg.
static long spruce_trash(const char *path, char **dest, char **msg, int *posix) {
	@autoreleasepool {
		NSURL *url = [NSURL fileURLWithPath:[NSString stringWithUTF8String:path]];
		NSURL *result = nil;
		NSError *error = nil;
		*dest = NULL;
		*msg = NULL;
		*posix = 0;
		if ([[NSFileManager defaultManager] trashItemAtURL:url resultingItemURL:&result error:&error]) {
			if (result != nil && result.path != nil) {
				*dest = strdup(result.path.fileSystemRepresentation);
			}
			return 0;
		}
		if (error == nil) {
			*msg = strdup("unknown error");
			return -1;
		}
		NSError *underlying = error.userInfo[NSUnderlyingErrorKey];
		if (underlying != nil && [underlying.domain isEqualToString:NSPOSIXErrorDomain]) {
			*posix = (int)underlying.code;
		} else if ([error.domain isEqualToString:NSPOSIXErrorDomain]) {
			*posix = (int)error.code;
		}
		*msg = strdup(error.localizedDescription.UTF8String ?: "unknown error");
		long code = (long)error.code;
		return code == 0 ? -1 : code;
	}
}
*/
import "C"

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"unsafe"

	"github.com/yldm-tech/spruce/internal/engine/fsx"
)

// Foundation error codes trashItemAtURL reports for a missing item and a permission problem (NSFileNoSuchFileError, NSFileWriteNoPermissionError, NSFileReadNoPermissionError).
const (
	nsFileNoSuchFile        = 4
	nsFileReadNoPermission  = 257
	nsFileWriteNoPermission = 513
)

type foundationMover struct{}

func (foundationMover) move(path string) (string, error) {
	cpath := C.CString(path)
	defer C.free(unsafe.Pointer(cpath))
	var dest, msg *C.char
	var posix C.int
	code := C.spruce_trash(cpath, &dest, &msg, &posix)
	defer C.free(unsafe.Pointer(dest))
	defer C.free(unsafe.Pointer(msg))
	if code == 0 {
		if dest == nil {
			return "", nil
		}
		return C.GoString(dest), nil
	}
	text := C.GoString(msg)
	switch {
	case posix != 0:
		return "", fmt.Errorf("%s: %w", text, syscall.Errno(posix))
	case code == nsFileNoSuchFile:
		return "", fmt.Errorf("%s: %w", text, fs.ErrNotExist)
	case code == nsFileReadNoPermission || code == nsFileWriteNoPermission:
		return "", fmt.Errorf("%s: %w", text, fs.ErrPermission)
	}
	return "", errors.New(text + " (NSError " + strconv.Itoa(int(code)) + ")")
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

func (b *darwinBin) Size(ctx context.Context) (Usage, error) {
	var u Usage
	for i, dir := range b.dirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if errNotExist(err) {
				continue
			}
			if i == 0 && errors.Is(err, fs.ErrPermission) {
				return Usage{}, ErrNoAccess
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

func (b *darwinBin) Empty(ctx context.Context) (Usage, error) {
	return emptyDirs(ctx, b.dirs())
}

// emptyDirs deletes the contents of each Trash folder for good, keeping the folders, and returns what it freed. Items that could not be deleted are reported in the error.
func emptyDirs(ctx context.Context, dirs []string) (Usage, error) {
	var freed Usage
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
