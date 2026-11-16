package fsx

import (
	"errors"
	"syscall"
)

// inUse reports whether err means another program holds the file. macOS deletes open files, so only a busy mount point qualifies.
func inUse(err error) bool {
	return errors.Is(err, syscall.EBUSY)
}
