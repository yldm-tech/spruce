package fsx

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestWriteFileAtomic(t *testing.T) {
	dir := t.TempDir()
	leftovers := func(t *testing.T, folder string) []string {
		t.Helper()
		matches, err := filepath.Glob(filepath.Join(folder, "*.tmp"))
		if err != nil {
			t.Fatal(err)
		}
		return matches
	}
	for _, tc := range []struct {
		name  string
		path  string
		setup func(t *testing.T, path string)
		data  string
		want  string
		fails bool
	}{
		{name: "new file in a new folder", path: filepath.Join(dir, "new", "state.json"), data: "a", want: "a"},
		{name: "replaces an existing file", path: filepath.Join(dir, "old.json"), setup: func(t *testing.T, p string) {
			if err := os.WriteFile(p, []byte("before"), 0o644); err != nil {
				t.Fatal(err)
			}
		}, data: "after", want: "after"},
		{name: "a folder in the way keeps it", path: filepath.Join(dir, "taken"), setup: func(t *testing.T, p string) {
			if err := os.MkdirAll(filepath.Join(p, "inside"), 0o755); err != nil {
				t.Fatal(err)
			}
		}, data: "x", fails: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			if tc.setup != nil {
				tc.setup(t, tc.path)
			}
			err := WriteFileAtomic(tc.path, []byte(tc.data))
			if got := leftovers(t, filepath.Dir(tc.path)); len(got) != 0 {
				t.Errorf("temporary files left: %v", got)
			}
			if tc.fails {
				if err == nil {
					t.Fatal("no error")
				}
				if info, err := os.Stat(tc.path); err != nil || !info.IsDir() {
					t.Errorf("what was there changed: %v %v", info, err)
				}
				return
			}
			if err != nil {
				t.Fatal(err)
			}
			b, err := os.ReadFile(tc.path)
			if err != nil || string(b) != tc.want {
				t.Errorf("content %q %v, want %q", b, err, tc.want)
			}
			if runtime.GOOS != "windows" {
				if info, _ := os.Stat(tc.path); info.Mode().Perm() != 0o600 {
					t.Errorf("mode %v", info.Mode().Perm())
				}
				if info, _ := os.Stat(filepath.Dir(tc.path)); tc.setup == nil && info.Mode().Perm() != 0o700 {
					t.Errorf("folder mode %v", info.Mode().Perm())
				}
			}
		})
	}
}
