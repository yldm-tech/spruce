package fsx

import (
	"errors"
	"io/fs"
	"path/filepath"
	"testing"

	"spruce/apps/spruce/internal/engine/execx/execxtest"
)

func TestRevealOpensFinder(t *testing.T) {
	dir := t.TempDir()
	file := filepath.Join(dir, "a b.txt")
	write(t, file, 1)
	f := execxtest.New()
	f.On("/usr/bin/open", "-R", file)
	if err := Reveal(t.Context(), f, file); err != nil {
		t.Fatal(err)
	}
	f.AssertCalled(t, "/usr/bin/open", "-R", file)

	if err := Reveal(t.Context(), f, filepath.Join(dir, "missing")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing path: %v", err)
	}
	if err := Reveal(t.Context(), f, "relative"); !errors.Is(err, ErrNotAbsolute) {
		t.Errorf("relative path: %v", err)
	}
	if n := len(f.Calls()); n != 1 {
		t.Errorf("%d commands ran, want 1", n)
	}
}
