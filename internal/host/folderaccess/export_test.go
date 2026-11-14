package folderaccess

import "time"

// OpenFor is Open for the platform goos with the clock now, so the macOS rules are tested on any machine.
func OpenFor(goos, path, home string, fda func() bool, now func() time.Time) *Store {
	s := Open(path, home, fda)
	s.goos, s.now = goos, now
	return s
}

// PromptingOn is Prompting for the platform goos.
func PromptingOn(goos, home, path string) (string, bool) {
	return prompting(goos, []string{home}, path)
}
