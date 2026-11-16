package instance

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestLockFile(t *testing.T) {
	path := filepath.Join(t.TempDir(), "data", "clean.lock")
	release, held, err := LockFile(path)
	if err != nil || !held {
		t.Fatalf("first LockFile = %v, %v", held, err)
	}
	info, err := os.Stat(path)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v", info.Mode().Perm())
		}
		if dir, _ := os.Stat(filepath.Dir(path)); dir.Mode().Perm() != 0o700 {
			t.Errorf("folder mode %v", dir.Mode().Perm())
		}
	}
	// A second open file of the same process is refused, as another process would be.
	if r2, held2, err := LockFile(path); err != nil || held2 || r2 != nil {
		t.Fatalf("second LockFile = %v, %v", held2, err)
	}
	if err := release(); err != nil {
		t.Fatal(err)
	}
	if err := release(); err != nil {
		t.Errorf("second release: %v", err)
	}
	r3, held3, err := LockFile(path)
	if err != nil || !held3 {
		t.Fatalf("LockFile after release = %v, %v", held3, err)
	}
	_ = r3()
}

func TestLockFileError(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs a folder that cannot be written")
	}
	dir := filepath.Join(t.TempDir(), "ro")
	if err := os.Mkdir(dir, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(dir, 0o700) })
	if _, held, err := LockFile(filepath.Join(dir, "clean.lock")); err == nil || held {
		t.Errorf("LockFile in a read-only folder = %v, %v; want an error", held, err)
	}
}
