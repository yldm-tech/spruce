//go:build !darwin

package undo

import "os"

// syncJournal hands what was written to f to the drive.
func syncJournal(f *os.File) error { return f.Sync() }
