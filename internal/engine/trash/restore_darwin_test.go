package trash

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"syscall"
	"testing"
)

// restoreFixture is a fake Trash folder and an allowed root in a temporary directory; the real Trash is never touched.
type restoreFixture struct {
	bin, root string
	r         *darwinRestorer
}

func newRestoreFixture(t *testing.T) *restoreFixture {
	t.Helper()
	base := t.TempDir()
	f := &restoreFixture{bin: filepath.Join(base, "home", ".Trash"), root: filepath.Join(base, "home", "Downloads")}
	for _, d := range []string{f.bin, f.root} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.r = newRestorer(func() []string { return []string{f.bin, filepath.Join(base, "unmounted", ".Trashes", "501")} })
	return f
}

// trash moves path into the fake Trash as Foundation would and returns the request that puts it back.
func (f *restoreFixture) trash(t *testing.T, path string) RestoreRequest {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(f.bin, filepath.Base(path))
	if err := os.Rename(path, to); err != nil {
		t.Fatal(err)
	}
	dev, ino := identify(to)
	return RestoreRequest{TrashedPath: to, Original: path, Root: f.root, Dir: info.IsDir(), Dev: dev, Ino: ino}
}

func TestRestorePutsAFileAndAFolderBack(t *testing.T) {
	f := newRestoreFixture(t)
	file := filepath.Join(f.root, "app.dmg")
	fill(t, file, 100)
	folder := filepath.Join(f.root, "sub", "Thing.app")
	fill(t, filepath.Join(folder, "Contents", "Info.plist"), 10)

	for _, p := range []string{file, folder} {
		req := f.trash(t, p)
		got, err := f.r.Restore(t.Context(), req)
		if err != nil {
			t.Fatalf("restore %s: %v", p, err)
		}
		if got != p {
			t.Errorf("restored to %q, want %q", got, p)
		}
		if _, err := os.Lstat(req.TrashedPath); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s is still in the Trash", req.TrashedPath)
		}
	}
	if _, err := os.Stat(filepath.Join(folder, "Contents", "Info.plist")); err != nil {
		t.Errorf("the folder's contents did not come back: %v", err)
	}
}

func TestRestoreOccupiedKeepsBothOnlyWhenAsked(t *testing.T) {
	f := newRestoreFixture(t)
	p := filepath.Join(f.root, "report.pdf")
	fill(t, p, 10)
	req := f.trash(t, p)
	// Something new took the name since.
	fill(t, p, 20)

	if _, err := f.r.Restore(t.Context(), req); !errors.Is(err, ErrOccupied) {
		t.Fatalf("err %v, want ErrOccupied", err)
	}
	if info, err := os.Stat(p); err != nil || info.Size() != 20 {
		t.Fatalf("the new file was touched: %v %v", info, err)
	}
	// "report 2.pdf" is taken as well: the next free number is used.
	fill(t, filepath.Join(f.root, "report 2.pdf"), 1)
	req.KeepBoth = true
	got, err := f.r.Restore(t.Context(), req)
	if err != nil {
		t.Fatal(err)
	}
	if want := filepath.Join(f.root, "report 3.pdf"); got != want {
		t.Errorf("restored to %q, want %q", got, want)
	}
	if info, err := os.Stat(got); err != nil || info.Size() != 10 {
		t.Errorf("restored file %v %v", info, err)
	}
}

func TestRestoreRefusals(t *testing.T) {
	cases := []struct {
		name string
		// change alters the fixture after the move and returns the request to try.
		change func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest
		want   error
	}{
		{"emptied from the Trash", func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest {
			if err := os.Remove(req.TrashedPath); err != nil {
				t.Fatal(err)
			}
			return req
		}, ErrGone},
		{"its volume is not mounted", func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest {
			req.TrashedPath = filepath.Join(filepath.Dir(filepath.Dir(f.bin)), "unmounted", ".Trashes", "501", "a.dmg")
			return req
		}, ErrGone},
		{"not in a Trash folder", func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest {
			other := filepath.Join(f.root, "elsewhere.dmg")
			fill(t, other, 1)
			req.TrashedPath = other
			return req
		}, ErrGone},
		{"the parent folder is gone", func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest {
			if err := os.RemoveAll(filepath.Join(f.root, "sub")); err != nil {
				t.Fatal(err)
			}
			return req
		}, ErrParentGone},
		{"the parent folder became a symlink", func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest {
			sub := filepath.Join(f.root, "sub")
			elsewhere := t.TempDir()
			if err := os.RemoveAll(sub); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink(elsewhere, sub); err != nil {
				t.Fatal(err)
			}
			return req
		}, ErrParentGone},
		{"the root is gone", func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest {
			if err := os.RemoveAll(f.root); err != nil {
				t.Fatal(err)
			}
			return req
		}, ErrParentGone},
		{"a folder where a file was", func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest {
			if err := os.Remove(req.TrashedPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Mkdir(req.TrashedPath, 0o755); err != nil {
				t.Fatal(err)
			}
			return req
		}, ErrReplaced},
		{"a symlink in the Trash", func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest {
			if err := os.Remove(req.TrashedPath); err != nil {
				t.Fatal(err)
			}
			if err := os.Symlink("/etc/hosts", req.TrashedPath); err != nil {
				t.Fatal(err)
			}
			return req
		}, ErrReplaced},
		{"another file of the same name", func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest {
			if err := os.Remove(req.TrashedPath); err != nil {
				t.Fatal(err)
			}
			fill(t, req.TrashedPath, 3)
			return req
		}, ErrReplaced},
		{"no identity was recorded", func(t *testing.T, f *restoreFixture, req RestoreRequest) RestoreRequest {
			req.Dev, req.Ino = 0, 0
			return req
		}, ErrReplaced},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := newRestoreFixture(t)
			p := filepath.Join(f.root, "sub", "a.dmg")
			fill(t, p, 10)
			req := c.change(t, f, f.trash(t, p))
			if _, err := f.r.Restore(t.Context(), req); !errors.Is(err, c.want) {
				t.Fatalf("err %v, want %v", err, c.want)
			}
			if _, err := os.Lstat(p); err == nil {
				t.Error("something was put at the original location")
			}
		})
	}
}

func TestRestoreRefusesOutsideTheRoot(t *testing.T) {
	f := newRestoreFixture(t)
	p := filepath.Join(f.root, "a.dmg")
	fill(t, p, 10)
	req := f.trash(t, p)
	for _, original := range []string{f.root, filepath.Join(filepath.Dir(f.root), "a.dmg"), filepath.Join(f.root, "..", "x", "a.dmg")} {
		r := req
		r.Original = original
		if _, err := f.r.Restore(t.Context(), r); err == nil {
			t.Errorf("restored to %q outside the root", original)
		}
	}
	if _, err := os.Lstat(req.TrashedPath); err != nil {
		t.Errorf("the item left the Trash: %v", err)
	}
}

func TestRestoreReportsAPermissionProblem(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root ignores folder permissions")
	}
	f := newRestoreFixture(t)
	p := filepath.Join(f.root, "locked", "a.dmg")
	fill(t, p, 10)
	req := f.trash(t, p)
	locked := filepath.Dir(p)
	if err := os.Chmod(locked, 0o500); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { os.Chmod(locked, 0o755) })
	_, err := f.r.Restore(t.Context(), req)
	if !errors.Is(err, fs.ErrPermission) {
		t.Fatalf("err %v, want a permission error", err)
	}
	var errno syscall.Errno
	if !errors.As(err, &errno) {
		t.Errorf("err %v does not carry the system's error", err)
	}
}

func TestRestoreStopsOnACancelledContext(t *testing.T) {
	f := newRestoreFixture(t)
	p := filepath.Join(f.root, "a.dmg")
	fill(t, p, 10)
	req := f.trash(t, p)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.r.Restore(ctx, req); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if _, err := os.Lstat(req.TrashedPath); err != nil {
		t.Error("the item left the Trash")
	}
}

func TestInTrashOnlyAcceptsTheTrashFolders(t *testing.T) {
	home := t.TempDir()
	t.Setenv("HOME", home)
	if !InTrash(filepath.Join(home, ".Trash", "a.dmg")) {
		t.Error("an item in ~/.Trash is not in the Trash")
	}
	for _, p := range []string{filepath.Join(home, "Library", "Mobile Documents", ".Trash", "a"), filepath.Join(home, ".Trash"), ""} {
		if InTrash(p) {
			t.Errorf("InTrash(%q)", p)
		}
	}
	if !CanRestore() {
		t.Error("CanRestore is false on macOS")
	}
}
