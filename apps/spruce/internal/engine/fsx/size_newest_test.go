package fsx

import (
	"os"
	"path/filepath"
	"testing"
	"time"
)

// TestDirSizeNewest finds the latest modification time anywhere under the root, deep files and folders included, and leaves out what Skip and symlinks hide.
func TestDirSizeNewest(t *testing.T) {
	root := filepath.Join(resolve(t, t.TempDir()), "root")
	base := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	files := map[string]time.Duration{
		"a.txt":            0,
		"sub/b.txt":        time.Hour,
		"sub/deep/c.txt":   3 * time.Hour,
		"skipme/later.txt": 9 * time.Hour,
	}
	for rel, off := range files {
		p := filepath.Join(root, filepath.FromSlash(rel))
		write(t, p, 10)
		if err := os.Chtimes(p, base.Add(off), base.Add(off)); err != nil {
			t.Fatal(err)
		}
	}
	for _, d := range []string{"skipme", "sub/deep", "sub", "."} {
		p := filepath.Join(root, filepath.FromSlash(d))
		if err := os.Chtimes(p, base, base); err != nil {
			t.Fatal(err)
		}
	}
	res, err := DirSize(t.Context(), root, SizeOptions{Skip: func(p string) bool { return filepath.Base(p) == "skipme" }})
	if err != nil {
		t.Fatal(err)
	}
	if want := base.Add(3 * time.Hour); !res.Newest.Equal(want) {
		t.Errorf("Newest %v, want %v (the deep file; the skipped folder left out)", res.Newest, want)
	}

	// A folder's own time counts: a file deleted inside leaves only the folder's change.
	later := base.Add(5 * time.Hour)
	if err := os.Chtimes(filepath.Join(root, "sub", "deep"), later, later); err != nil {
		t.Fatal(err)
	}
	res, err = DirSize(t.Context(), root, SizeOptions{Skip: func(p string) bool { return filepath.Base(p) == "skipme" }})
	if err != nil {
		t.Fatal(err)
	}
	if !res.Newest.Equal(later) {
		t.Errorf("Newest %v, want the folder's %v", res.Newest, later)
	}

	// A single file is its own newest time.
	one := filepath.Join(root, "a.txt")
	res, err = DirSize(t.Context(), one, SizeOptions{})
	if err != nil || !res.Newest.Equal(base) {
		t.Errorf("file Newest %v, %v; want %v", res.Newest, err, base)
	}
}
