package trash

import (
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// renameMover moves into a folder of the test, as Foundation moves into ~/.Trash.
type renameMover struct{ bin string }

func (m renameMover) move(path string) (string, error) {
	dest := filepath.Join(m.bin, filepath.Base(path))
	return dest, os.Rename(path, dest)
}

// TestMoveToTrashIdentifiesTheTrashedItem: each outcome carries the device and inode of the item where it went, and none when the system did not say where.
func TestMoveToTrashIdentifiesTheTrashedItem(t *testing.T) {
	dir := t.TempDir()
	bin := filepath.Join(dir, "bin")
	fill(t, filepath.Join(dir, "a.dmg"), 10)
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	b := &darwinBin{mover: renameMover{bin: bin}, dirs: func() []string { return []string{bin} }}
	out, err := b.MoveToTrash(t.Context(), []string{filepath.Join(dir, "a.dmg")})
	if err != nil || out[0].Err != nil {
		t.Fatal(err, out)
	}
	info, err := os.Lstat(filepath.Join(bin, "a.dmg"))
	if err != nil {
		t.Fatal(err)
	}
	st := info.Sys().(*syscall.Stat_t)
	if out[0].Dev != uint64(st.Dev) || out[0].Ino != st.Ino || out[0].Ino == 0 {
		t.Errorf("outcome %+v, want dev %d ino %d", out[0], st.Dev, st.Ino)
	}

	// The fake mover's path does not exist: no identity.
	b = &darwinBin{mover: &fakeMover{}, dirs: func() []string { return nil }}
	out, _ = b.MoveToTrash(t.Context(), []string{filepath.Join(dir, "gone")})
	if out[0].Err != nil || out[0].Dev != 0 || out[0].Ino != 0 {
		t.Errorf("outcome %+v", out[0])
	}
}
