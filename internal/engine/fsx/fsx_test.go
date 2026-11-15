package fsx

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync/atomic"
	"testing"
)

// write creates the file at path (and its folders) with n bytes.
func write(t *testing.T, path string, n int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", n)), 0o644); err != nil {
		t.Fatal(err)
	}
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks need Developer Mode or administrator rights here: %v", err)
		}
		t.Fatal(err)
	}
}

func resolve(t *testing.T, p string) string {
	t.Helper()
	r, err := filepath.EvalSymlinks(p)
	if err != nil {
		t.Fatal(err)
	}
	return r
}

func TestDirSizeCountsFilesAndSkipsLinks(t *testing.T) {
	dir := resolve(t, t.TempDir())
	root := filepath.Join(dir, "root")
	write(t, filepath.Join(root, "a.txt"), 1000)
	write(t, filepath.Join(root, "sub", "b.txt"), 3000)
	write(t, filepath.Join(root, "sub", "deeper", "c.txt"), 5000)
	write(t, filepath.Join(root, "skipme", "big.bin"), 100_000)
	outside := filepath.Join(dir, "outside")
	write(t, filepath.Join(outside, "huge.bin"), 200_000)
	symlink(t, outside, filepath.Join(root, "link-to-outside"))

	var calls atomic.Int64
	res, err := DirSize(t.Context(), root, SizeOptions{
		Workers:  2,
		Skip:     func(p string) bool { return filepath.Base(p) == "skipme" },
		Progress: func(SizeProgress) { calls.Add(1) },
	})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 3 || res.Logical != 9000 {
		t.Fatalf("files %d logical %d, want 3 and 9000 (symlink and skipped folder left out): %+v", res.Files, res.Logical, res)
	}
	if res.Dirs != 3 {
		t.Errorf("dirs %d, want 3", res.Dirs)
	}
	if res.Bytes < 9000 {
		t.Errorf("allocated %d below logical size", res.Bytes)
	}
	if calls.Load() == 0 {
		t.Error("progress was never reported")
	}
}

func TestDirSizeHardLinksOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard links are counted per link on Windows")
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "a"), 8192)
	if err := os.Link(filepath.Join(root, "a"), filepath.Join(root, "b")); err != nil {
		t.Fatal(err)
	}
	res, err := DirSize(t.Context(), root, SizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	single, err := DirSize(t.Context(), filepath.Join(root, "a"), SizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Files != 2 || res.Bytes != single.Bytes {
		t.Errorf("two links of one file: %+v, one link alone %d bytes", res, single.Bytes)
	}
}

func TestDirSizeDeniedIsNotFatal(t *testing.T) {
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("needs POSIX permissions and a non-root user")
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "ok.txt"), 10)
	locked := filepath.Join(root, "locked")
	write(t, filepath.Join(locked, "secret"), 10)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	res, err := DirSize(t.Context(), root, SizeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	if res.Denied != 1 || res.Files != 1 {
		t.Errorf("result %+v, want one denied folder and one file", res)
	}
	if _, err := DirSize(t.Context(), locked, SizeOptions{}); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("unreadable root: err %v", err)
	}
}

func TestDirSizeErrorsAndCancel(t *testing.T) {
	if _, err := DirSize(t.Context(), filepath.Join(t.TempDir(), "missing"), SizeOptions{}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing root: %v", err)
	}
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "b"), 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := DirSize(ctx, root, SizeOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled walk: %v", err)
	}
}

// sandbox is a fake home with a cache folder to clean, inside a temporary folder.
type sandbox struct {
	base, home, caches string
	pol                policy
}

func newSandbox(t *testing.T) sandbox {
	t.Helper()
	base := resolve(t, t.TempDir())
	home := filepath.Join(base, "home")
	caches := filepath.Join(home, "Library", "Caches")
	if runtime.GOOS == "windows" {
		caches = filepath.Join(home, "AppData", "Local", "Temp")
	}
	if err := os.MkdirAll(caches, 0o755); err != nil {
		t.Fatal(err)
	}
	return sandbox{base: base, home: home, caches: caches, pol: newPolicy(home)}
}

func TestSafeRemoveDeletesInsideRoot(t *testing.T) {
	s := newSandbox(t)
	app := filepath.Join(s.caches, "com.example.app")
	write(t, filepath.Join(app, "x", "y.bin"), 4096)
	write(t, filepath.Join(app, "z.bin"), 2048)
	rm, err := safeRemove(t.Context(), s.pol, app, []string{s.caches})
	if err != nil {
		t.Fatal(err)
	}
	if rm.Removed != 4 || rm.Freed < 6144 || len(rm.Failures) != 0 {
		t.Errorf("removal %+v", rm)
	}
	if _, err := os.Lstat(app); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("folder still there: %v", err)
	}
	if _, err := os.Stat(s.caches); err != nil {
		t.Errorf("the root itself went: %v", err)
	}
	if _, err := safeRemove(t.Context(), s.pol, app, []string{s.caches}); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("second removal: %v", err)
	}
}

func TestSafeRemoveRemovesLinksNotTargets(t *testing.T) {
	s := newSandbox(t)
	keep := filepath.Join(s.base, "keep")
	write(t, filepath.Join(keep, "precious.txt"), 10)
	link := filepath.Join(s.caches, "link")
	symlink(t, keep, link)
	write(t, filepath.Join(s.caches, "dir", "f"), 1)
	symlink(t, keep, filepath.Join(s.caches, "dir", "inner-link"))

	for _, p := range []string{link, filepath.Join(s.caches, "dir")} {
		if _, err := safeRemove(t.Context(), s.pol, p, []string{s.caches}); err != nil {
			t.Fatalf("remove %s: %v", p, err)
		}
	}
	if _, err := os.Stat(filepath.Join(keep, "precious.txt")); err != nil {
		t.Fatalf("symlink target was deleted: %v", err)
	}
	if _, err := os.Lstat(link); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("link still there: %v", err)
	}
}

func TestSafeRemoveRefuses(t *testing.T) {
	s := newSandbox(t)
	outside := filepath.Join(s.base, "outside")
	write(t, filepath.Join(outside, "f"), 1)
	// A folder in the root that is a symlink to the outside: paths through it resolve outside.
	symlink(t, outside, filepath.Join(s.caches, "escape"))
	docs := filepath.Join(s.home, "Documents", "project")
	write(t, filepath.Join(docs, "node_modules", "m.js"), 1)

	cases := []struct {
		name  string
		path  string
		roots []string
		want  error
	}{
		{"relative", "Library/Caches/x", []string{s.caches}, ErrNotAbsolute},
		{"outside", filepath.Join(outside, "f"), []string{s.caches}, ErrOutsideRoots},
		{"the root itself", s.caches, []string{s.caches}, ErrOutsideRoots},
		{"through a symlinked folder", filepath.Join(s.caches, "escape", "f"), []string{s.caches}, ErrOutsideRoots},
		{"home as a root", filepath.Join(s.home, "Library"), []string{s.home}, ErrOutsideRoots},
		{"above home as a root", filepath.Join(s.home, "Library"), []string{s.base}, ErrOutsideRoots},
		{"volume root as a root", filepath.Join(outside, "f"), []string{filepath.VolumeName(s.base) + string(filepath.Separator)}, ErrOutsideRoots},
		{"protected subtree as a root", filepath.Join(docs, "node_modules"), []string{docs}, ErrOutsideRoots},
		{"protected folder", filepath.Join(s.home, "Documents"), []string{filepath.Join(s.home, "Documents")}, ErrOutsideRoots},
	}
	for _, c := range cases {
		if _, err := safeRemove(t.Context(), s.pol, c.path, c.roots); !errors.Is(err, c.want) {
			t.Errorf("%s: err %v, want %v", c.name, err, c.want)
		}
	}
	if _, err := os.Stat(filepath.Join(outside, "f")); err != nil {
		t.Fatalf("a refused removal deleted something: %v", err)
	}
	if _, err := os.Stat(filepath.Join(docs, "node_modules", "m.js")); err != nil {
		t.Fatalf("a refused removal deleted something: %v", err)
	}

	// The Trash may take what permanent deletion refuses inside the protected subtrees, but not the protected folders themselves.
	if _, _, err := s.pol.check(filepath.Join(docs, "node_modules"), []string{docs}, false); err != nil {
		t.Errorf("trash inside Documents: %v", err)
	}
	if _, _, err := s.pol.check(filepath.Join(s.home, "Documents"), []string{s.home}, false); !errors.Is(err, ErrOutsideRoots) {
		t.Errorf("trash Documents itself: %v", err)
	}
	if _, _, err := s.pol.check(filepath.Join(s.home, "Downloads"), []string{filepath.Join(s.home, "Downloads", "..")}, false); err == nil {
		t.Error("trash Downloads itself was accepted")
	}
}

func TestSafeRemoveCancelled(t *testing.T) {
	s := newSandbox(t)
	app := filepath.Join(s.caches, "app")
	write(t, filepath.Join(app, "f"), 1)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := safeRemove(ctx, s.pol, app, []string{s.caches}); !errors.Is(err, context.Canceled) {
		t.Errorf("err %v", err)
	}
	if _, err := os.Stat(filepath.Join(app, "f")); err != nil {
		t.Errorf("a cancelled removal deleted: %v", err)
	}
}

func TestExcludedInside(t *testing.T) {
	home := filepath.FromSlash("/Users/me")
	if runtime.GOOS == "windows" {
		home = `C:\Users\me`
	}
	j := func(parts ...string) string { return filepath.Join(append([]string{home}, parts...)...) }
	ex := []string{"~/Library/Caches/com.foo/keep", j("Projects", "app"), "node_modules", filepath.Join(home, "Logs", "*.log"), " "}
	cases := map[string]bool{
		j("Library", "Caches", "com.foo"):         true,
		j("Library", "Caches", "COM.FOO"):         true,
		j("Library"):                              true,
		j("Library", "Caches", "com.foo", "keep"): false,
		j("Library", "Caches", "com.foobar"):      false,
		j("Projects", "app"):                      false,
		j("Projects", "app", "node_modules"):      false,
		j("Logs"):                                 false,
		"":                                        false,
	}
	for p, want := range cases {
		if got := excludedInside(p, ex, home); got != want {
			t.Errorf("excludedInside(%q) = %v, want %v", p, got, want)
		}
	}
}

func TestExcluded(t *testing.T) {
	home := filepath.FromSlash("/Users/me")
	if runtime.GOOS == "windows" {
		home = `C:\Users\me`
	}
	j := func(parts ...string) string { return filepath.Join(append([]string{home}, parts...)...) }
	ex := []string{j("Library", "Caches", "Keep"), "~/Projects/important", "node_modules", "*.photoslibrary", filepath.Join(home, "Logs", "*.log"), " "}
	cases := map[string]bool{
		j("Library", "Caches", "Keep"):             true,
		j("Library", "Caches", "keep", "x"):        true,
		j("Library", "Caches", "Keeper"):           false,
		j("Projects", "important", "a"):            true,
		j("src", "app", "node_modules", "react"):   true,
		j("Pictures", "Photos.photoslibrary", "x"): true,
		j("Logs", "app.log"):                       true,
		j("Logs", "app.txt"):                       false,
		j("Downloads"):                             false,
		"":                                         false,
	}
	for p, want := range cases {
		if got := excluded(p, ex, home); got != want {
			t.Errorf("excluded(%q) = %v, want %v", p, got, want)
		}
	}
}
