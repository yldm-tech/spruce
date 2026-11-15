//go:build !windows

package cli

// AttachConsole does nothing outside Windows: the executable is a console program for the shell that starts it.
func AttachConsole() {}
