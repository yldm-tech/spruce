package trash

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func fill(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", n)), 0o644); err != nil {
		t.Fatal(err)
	}
}

// TestFakeTrashFolders measures and empties Trash folders made in a temporary directory; the real Trash is never touched.
func TestFakeTrashFolders(t *testing.T) {
	base := t.TempDir()
	home := filepath.Join(base, "home", ".Trash")
	vol := filepath.Join(base, "vol", ".Trashes", "501")
	fill(t, filepath.Join(home, "old.dmg"), 5000)
	fill(t, filepath.Join(home, "folder", "a.txt"), 3000)
	fill(t, filepath.Join(home, ".DS_Store"), 10)
	fill(t, filepath.Join(vol, "movie.mov"), 7000)
	missing := filepath.Join(base, "unmounted", ".Trashes", "501")
	b := &darwinBin{mover: &fakeMover{}, dirs: func() []string { return []string{home, vol, missing} }}

	u, err := b.Size(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if u.Items != 3 || u.Bytes < 15000 {
		t.Fatalf("usage %+v, want 3 items and at least 15000 bytes", u)
	}
	freed, err := b.Empty(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if freed.Items != 3 || freed.Bytes < 15000 {
		t.Errorf("freed %+v", freed)
	}
	for _, dir := range []string{home, vol} {
		entries, err := os.ReadDir(dir)
		if err != nil {
			t.Fatalf("the Trash folder itself went: %v", err)
		}
		if len(entries) != 0 {
			t.Errorf("%s still holds %v", dir, entries)
		}
	}
}

func TestUnreadableTrash(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	home := filepath.Join(t.TempDir(), ".Trash")
	fill(t, filepath.Join(home, "x"), 1)
	if err := os.Chmod(home, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(home, 0o755) })
	b := &darwinBin{mover: &fakeMover{}, dirs: func() []string { return []string{home} }}
	if _, err := b.Size(t.Context()); !errors.Is(err, ErrNoAccess) || !errors.Is(err, fs.ErrPermission) {
		t.Errorf("Size err %v", err)
	}
	if _, err := b.Empty(t.Context()); !errors.Is(err, ErrNoAccess) {
		t.Errorf("Empty err %v", err)
	}
}

// TestFoundationMoverMissingPath runs the real NSFileManager call on a path that does not exist, which exercises the Objective-C bridge without moving anything to the Trash.
func TestFoundationMoverMissingPath(t *testing.T) {
	_, err := foundationMover{}.move(filepath.Join(t.TempDir(), "does-not-exist"))
	if !errors.Is(err, fs.ErrNotExist) {
		t.Fatalf("err %v, want fs.ErrNotExist", err)
	}
}
