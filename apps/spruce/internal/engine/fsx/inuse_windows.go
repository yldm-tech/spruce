package fsx

import (
	"errors"

	"golang.org/x/sys/windows"
)

// inUse reports whether err means another program holds the file open without allowing deletion.
func inUse(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION) || errors.Is(err, windows.ERROR_USER_MAPPED_FILE)
}
