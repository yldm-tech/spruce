package datadir

import (
	"os"
	"path/filepath"
	"testing"
)

func TestInCreatesTheFolder(t *testing.T) {
	base := t.TempDir()
	dir, err := in(base)
	if err != nil {
		t.Fatal(err)
	}
	if dir != filepath.Join(base, "Spruce") {
		t.Errorf("dir = %q", dir)
	}
	if fi, err := os.Stat(dir); err != nil || !fi.IsDir() {
		t.Fatalf("folder not created: %v", err)
	}
	// A second call finds it.
	if again, err := in(base); err != nil || again != dir {
		t.Errorf("second call = %q, %v", again, err)
	}
}
