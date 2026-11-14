package fsx

import (
	"os"
	"path/filepath"
	"strings"
	"sync"
)

var homeDir = sync.OnceValue(func() string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	return home
})

// Excluded reports whether path is one of exclusions or inside one; see the package documentation for the forms an exclusion takes. Comparison ignores case on both platforms, so an exclusion never misses because of how it was typed.
func Excluded(path string, exclusions []string) bool {
	return excluded(path, exclusions, homeDir())
}

func excluded(path string, exclusions []string, home string) bool {
	if path == "" {
		return false
	}
	path = filepath.Clean(path)
	lower := strings.ToLower(path)
	for _, ex := range exclusions {
		ex = strings.TrimSpace(ex)
		if ex == "" {
			continue
		}
		if home != "" && (ex == "~" || strings.HasPrefix(ex, "~/") || strings.HasPrefix(ex, `~\`)) {
			ex = filepath.Join(home, ex[1:])
		}
		if filepath.IsAbs(ex) && !strings.ContainsAny(ex, "*?[") {
			if hasPathPrefix(path, filepath.Clean(ex), true) {
				return true
			}
			continue
		}
		pattern := strings.ToLower(filepath.Clean(ex))
		if !strings.ContainsAny(pattern, `/\`) {
			for _, part := range strings.FieldsFunc(lower, func(r rune) bool { return r == '/' || r == '\\' }) {
				if ok, _ := filepath.Match(pattern, part); ok {
					return true
				}
			}
			continue
		}
		for p := lower; ; p = filepath.Dir(p) {
			if ok, _ := filepath.Match(pattern, p); ok {
				return true
			}
			if filepath.Dir(p) == p {
				break
			}
		}
	}
	return false
}
