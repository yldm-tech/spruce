package folderaccess_test

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"

	"spruce/apps/spruce/internal/host/folderaccess"
)

var t0 = time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

func TestPrompting(t *testing.T) {
	if got, ok := folderaccess.PromptingOn("windows", `C:\Users\x`, `C:\Users\x\Desktop`); ok {
		t.Errorf("windows: %q", got)
	}
}

type fixture struct {
	home, data string
	fdaCalls   int
	fda        bool
	now        time.Time
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	base, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &fixture{home: filepath.Join(base, "home"), data: filepath.Join(base, "data", "folder-access.json"), now: t0}
	for _, d := range []string{"Desktop", "Documents/code/app", "Downloads", "Library/Caches", "Library/CloudStorage/Dropbox"} {
		if err := os.MkdirAll(filepath.Join(f.home, d), 0o755); err != nil {
			t.Fatal(err)
		}
	}
	return f
}

func (f *fixture) open() *folderaccess.Store {
	return folderaccess.OpenFor("darwin", f.data, f.home, func() bool { f.fdaCalls++; return f.fda }, func() time.Time { return f.now })
}

func (f *fixture) at(rel string) string { return filepath.Join(f.home, filepath.FromSlash(rel)) }

func TestAllowAndObserve(t *testing.T) {
	f := newFixture(t)
	s := f.open()
	for rel, want := range map[string]bool{
		"":                   true,
		"Library/Caches":     true,
		"Downloads":          false,
		"Downloads/a/b":      false,
		"Desktop":            false,
		"Documents/code/app": false,
	} {
		if got := s.Allow(f.at(rel)); got != want {
			t.Errorf("%q: %v", rel, got)
		}
	}
	if _, err := os.Stat(f.data); !os.IsNotExist(err) {
		t.Errorf("file written before Observe: %v", err)
	}
	// A path in no prompting folder records nothing.
	if err := s.Observe(f.at("Library/Caches")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.data); !os.IsNotExist(err) {
		t.Errorf("file written for a plain folder: %v", err)
	}
	if err := s.Observe(f.at("Downloads/sub")); err != nil {
		t.Fatal(err)
	}
	if !s.Allow(f.at("Downloads/a/b")) || !s.Allow(f.at("downloads")) || s.Allow(f.at("Desktop")) {
		t.Error("after observing Downloads")
	}
	// Another store reads what was recorded.
	again := f.open()
	if !again.Allow(f.at("Downloads/x")) || again.Allow(f.at("Documents")) {
		t.Error("not persisted")
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(f.data)
		if err != nil || info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v, %v", info, err)
		}
	}
	if f.fdaCalls == 0 {
		t.Error("Full Disk Access never asked for a refused folder")
	}
}

func TestFullDiskAccess(t *testing.T) {
	f := newFixture(t)
	f.fda = true
	s := f.open()
	if !s.Allow(f.at("Desktop")) || !s.Allow(f.at("Documents/code")) || !s.Allow("/Volumes/USB/x") {
		t.Error("Full Disk Access does not open the TCC folders")
	}
	// File Provider domains prompt on their own: Full Disk Access does not cover them.
	if s.Allow(f.at("Library/CloudStorage/Dropbox/x")) {
		t.Error("a File Provider domain is read with Full Disk Access alone")
	}
	if err := s.Observe(f.at("Library/CloudStorage/Dropbox/x")); err != nil {
		t.Fatal(err)
	}
	if !s.Allow(f.at("Library/CloudStorage/Dropbox/y")) {
		t.Error("an observed File Provider domain is refused")
	}
	// The check is reused for a minute, then asked again.
	calls := f.fdaCalls
	s.Allow(f.at("Desktop/a"))
	if f.fdaCalls != calls {
		t.Errorf("asked again within a minute: %d", f.fdaCalls)
	}
	f.fda = false
	f.now = t0.Add(time.Minute)
	if s.Allow(f.at("Desktop/a")) || f.fdaCalls != calls+1 {
		t.Errorf("revoked access still used, %d calls", f.fdaCalls)
	}
	// Without a check function nothing prompting is read.
	if folderaccess.OpenFor("darwin", f.data+"2", f.home, nil, time.Now).Allow(f.at("Desktop")) {
		t.Error("nil fda allows Desktop")
	}
}

func TestSymlinksIntoPromptingFolders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	f := newFixture(t)
	if err := os.Symlink(f.at("Documents/code"), f.at("code")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink("Documents/missing", f.at("relative")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.at("loop-b"), f.at("loop-a")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.at("loop-a"), f.at("loop-b")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(f.at("Library/Caches"), f.at("caches")); err != nil {
		t.Fatal(err)
	}
	s := f.open()
	if s.Allow(f.at("code/app")) || s.Allow(f.at("code")) {
		t.Error("a symlink into Documents is read")
	}
	// The walk stops at the prompting folder: a target that does not exist inside it is never looked up.
	if folder, ok, err := s.PromptingFor(f.at("relative/x")); err != nil || !ok || folder != f.at("Documents") {
		t.Errorf("relative link: %q %v %v", folder, ok, err)
	}
	if s.Allow(f.at("loop-a/x")) {
		t.Error("a symlink loop is read")
	}
	if _, _, err := s.PromptingFor(f.at("loop-a")); err == nil {
		t.Error("no error for a loop")
	}
	if !s.Allow(f.at("caches/x")) || !s.Allow(f.at("missing/x")) {
		t.Error("plain folders refused")
	}
	if err := s.Observe(f.at("Documents")); err != nil {
		t.Fatal(err)
	}
	if !s.Allow(f.at("code/app")) {
		t.Error("observed Documents still refused through the link")
	}

	// Observe resolves a path as Allow does: a foreground read of a project root reached through a link opens that root to background reads.
	o := folderaccess.OpenFor("darwin", f.data+"-observe", f.home, nil, time.Now)
	if err := o.Observe(f.at("loop-a/x")); err == nil {
		t.Error("no error observing through a loop")
	}
	if err := o.Observe(f.at("caches/x")); err != nil {
		t.Fatal(err)
	}
	if o.Allow(f.at("code/app")) {
		t.Error("allowed before the link was observed")
	}
	if err := o.Observe(f.at("code/app")); err != nil {
		t.Fatal(err)
	}
	if !o.Allow(f.at("code/app")) || !o.Allow(f.at("Documents/other")) || o.Allow(f.at("Desktop")) {
		t.Error("observing through the link did not record Documents alone")
	}
	if !folderaccess.OpenFor("darwin", f.data+"-observe", f.home, nil, time.Now).Allow(f.at("code/app")) {
		t.Error("the link's folder was not saved")
	}
}

func TestHomeThroughASymlink(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("symlinks need privileges on Windows")
	}
	f := newFixture(t)
	alias := filepath.Join(filepath.Dir(f.home), "alias")
	if err := os.Symlink(f.home, alias); err != nil {
		t.Fatal(err)
	}
	s := folderaccess.OpenFor("darwin", f.data, alias, nil, time.Now)
	if s.Allow(filepath.Join(f.home, "Downloads")) || s.Allow(filepath.Join(alias, "Downloads")) {
		t.Error("Downloads read through either spelling of home")
	}
}

func TestBadFileAndOtherPlatforms(t *testing.T) {
	f := newFixture(t)
	if err := os.MkdirAll(filepath.Dir(f.data), 0o700); err != nil {
		t.Fatal(err)
	}
	for _, body := range []string{"{", `{"version":2,"folders":["` + f.at("Desktop") + `"]}`} {
		if err := os.WriteFile(f.data, []byte(body), 0o600); err != nil {
			t.Fatal(err)
		}
		if f.open().Allow(f.at("Desktop")) {
			t.Errorf("%s: Desktop allowed", body)
		}
	}
	w := folderaccess.OpenFor("windows", f.data, f.home, nil, time.Now)
	if !w.Allow(f.at("Desktop")) || !w.Allow(`C:\Users\x\Documents`) {
		t.Error("Windows refuses a read")
	}
	if err := w.Observe(f.at("Desktop")); err != nil {
		t.Error(err)
	}
}
