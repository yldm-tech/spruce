package devcaches

import (
	"strings"
	"testing"
)

func TestOwned(t *testing.T) {
	for _, name := range []string{"Homebrew", "homebrew", "GO-BUILD", "golangci-lint", "Mozilla.sccache"} {
		if !Owned(name) {
			t.Errorf("Owned(%q) = false", name)
		}
	}
	for _, name := range []string{"com.apple.Safari", "Google", "com.microsoft.VSCode", "com.microsoft.VSCode.ShipIt", "", "go-build/x"} {
		if Owned(name) {
			t.Errorf("Owned(%q) = true", name)
		}
	}
}

func TestNamesAreUniqueAndPlain(t *testing.T) {
	seen := map[string]bool{}
	for _, n := range MacOS() {
		key := strings.ToLower(n)
		if seen[key] || n == "" || strings.ContainsAny(n, `/\`) {
			t.Errorf("bad or repeated name %q", n)
		}
		seen[key] = true
	}
}
