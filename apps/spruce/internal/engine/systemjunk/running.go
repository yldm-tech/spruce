package systemjunk

import "strings"

// Running is the set of running apps, in lower case: bundle IDs and app names on macOS, executable names (chrome.exe) on Windows.
type Running map[string]bool

// NewRunning returns the set of names, lower-cased.
func NewRunning(names ...string) Running {
	r := make(Running, len(names))
	for _, n := range names {
		if n = strings.TrimSpace(n); n != "" {
			r[strings.ToLower(n)] = true
		}
	}
	return r
}

// owns reports whether a cache folder named name belongs to a running app: the name is a running app's bundle ID or name, or starts with a running bundle ID and a dot (com.tinyspeck.slackmacgap.ShipIt).
func (r Running) owns(name string) bool {
	lower := strings.ToLower(name)
	if r[lower] {
		return true
	}
	// Only prefixes that are bundle IDs themselves (with a dot) count, so an app named "Google" does not own "Google.Drive".
	for i := range len(lower) {
		if lower[i] == '.' && strings.Contains(lower[:i], ".") && r[lower[:i]] {
			return true
		}
	}
	return false
}

// anyOf reports whether any of keys is running.
func (r Running) anyOf(keys []string) bool {
	for _, k := range keys {
		if r[strings.ToLower(k)] {
			return true
		}
	}
	return false
}
