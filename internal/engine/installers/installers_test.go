package installers

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/fsx"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

// now is the clock of every test; files are dated relative to it.
var now = time.Now()

const day = 24 * time.Hour

// fakeDeleter moves to a pretend Trash (os.RemoveAll after the real root check) and refuses permanent deletion, which this page must never ask for.
type fakeDeleter struct {
	mu      sync.Mutex
	trashed []string
}

func (d *fakeDeleter) Remove(context.Context, string, []string) (fsx.Removal, error) {
	return fsx.Removal{}, errors.New("installers must never delete permanently")
}

func (d *fakeDeleter) Trash(_ context.Context, path string, roots []string) error {
	if err := fsx.CheckTrashable(path, roots); err != nil {
		return err
	}
	d.mu.Lock()
	d.trashed = append(d.trashed, filepath.Base(path))
	d.mu.Unlock()
	return os.RemoveAll(path)
}

func write(t *testing.T, base, rel string, size int64, age time.Duration) string {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("installer"), 0o644); err != nil {
		t.Fatal(err)
	}
	if size > 0 {
		// Sparse: the logical size is what classify reads.
		if err := os.Truncate(p, size); err != nil {
			t.Fatal(err)
		}
	}
	setAge(t, p, age)
	return p
}

func setAge(t *testing.T, p string, age time.Duration) {
	t.Helper()
	ts := now.Add(-age)
	if err := os.Chtimes(p, ts, ts); err != nil {
		t.Fatal(err)
	}
}

type fixture struct {
	home       string
	downloads  string
	desktop    string
	del        *fakeDeleter
	env        Env
	roots      []string
	exclusions []string
}

func newFixture(t *testing.T, windows bool) *fixture {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fx := &fixture{home: home, downloads: filepath.Join(home, "Downloads"), desktop: filepath.Join(home, "Desktop"), del: &fakeDeleter{}}
	fx.roots = []string{fx.downloads, fx.desktop}
	for _, d := range fx.roots {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	fx.env = Env{
		Home:      home,
		Downloads: fx.downloads,
		Settings: func() settings.AppSettings {
			return settings.AppSettings{Cleanup: settings.CleanupSettings{InstallerRoots: fx.roots, Exclusions: fx.exclusions}}
		},
		Deleter: fx.del,
		Now:     func() time.Time { return now },
		windows: windows,
		arrived: func(info fs.FileInfo) time.Time { return info.ModTime() },
	}
	return fx
}

func (fx *fixture) scan(t *testing.T) (*Cleaner, cleanup.ScanResult) {
	t.Helper()
	c := newCleaner(fx.env)
	res, err := c.Scan(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return c, res
}

// contents maps each category ID to its items' paths relative to home, in scan order (largest first, then as found).
func (fx *fixture) contents(t *testing.T, res cleanup.ScanResult) map[string][]string {
	t.Helper()
	out := map[string][]string{}
	for _, c := range res.Categories {
		var rels []string
		for _, it := range c.Items {
			r, err := filepath.Rel(fx.home, it.Path)
			if err != nil {
				t.Fatal(err)
			}
			rels = append(rels, filepath.ToSlash(r))
		}
		slices.Sort(rels)
		out[c.ID] = rels
	}
	return out
}

func category(t *testing.T, res cleanup.ScanResult, id string) cleanup.Category {
	t.Helper()
	for _, c := range res.Categories {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no category %q", id)
	return cleanup.Category{}
}

func TestScanDarwin(t *testing.T) {
	fx := newFixture(t, false)
	old := 30 * day
	for _, rel := range []string{
		"Downloads/old.dmg", "Downloads/mounted.dmg", "Downloads/flat.pkg", "Downloads/Xcode_16.xip", "Downloads/ubuntu-24.04.iso",
		"Downloads/sub/inner.dmg", "Downloads/sub/deeper/too-deep.dmg", "Downloads/Some.app/Contents/x.dmg",
		"Downloads/.hidden.dmg", "Downloads/.cache/hidden-dir.dmg", "Downloads/readme.txt", "Downloads/excluded.dmg",
		"Desktop/desk.pkg",
	} {
		write(t, fx.home, rel, 0, old)
	}
	write(t, fx.home, "Downloads/new.dmg", 0, 2*day)
	write(t, fx.home, "Downloads/VSCode-darwin-universal.zip", 11_000_000, old)
	write(t, fx.home, "Downloads/holiday photos.zip", 11_000_000, old)
	write(t, fx.home, "Downloads/tool-1.2.zip", 1000, old)
	write(t, fx.home, "Downloads/Legacy.pkg/Contents/Archive.pax", 0, old)
	setAge(t, filepath.Join(fx.home, "Downloads/Legacy.pkg"), old)
	if err := os.Symlink(filepath.Join(fx.home, "Downloads/old.dmg"), filepath.Join(fx.home, "Downloads/link.dmg")); err != nil {
		t.Fatal(err)
	}
	mounted := filepath.Join(fx.home, "Downloads/mounted.dmg")
	fx.env.Mounted = func(context.Context) ([]string, error) { return []string{mounted}, nil }
	fx.exclusions = []string{filepath.Join(fx.home, "Downloads/excluded.dmg")}
	// The same folder twice, once spelled with a trailing separator: searched once.
	fx.roots = append(fx.roots, fx.downloads+string(filepath.Separator), "relative/ignored")

	_, res := fx.scan(t)
	got := fx.contents(t, res)
	want := map[string][]string{
		catDiskImages:   {"Downloads/old.dmg", "Downloads/sub/inner.dmg"},
		catPackages:     {"Desktop/desk.pkg", "Downloads/Legacy.pkg", "Downloads/flat.pkg"},
		catArchives:     {"Downloads/VSCode-darwin-universal.zip", "Downloads/Xcode_16.xip"},
		catSystemImages: {"Downloads/ubuntu-24.04.iso"},
		catRecent:       {"Downloads/mounted.dmg", "Downloads/new.dmg"},
	}
	for id, w := range want {
		if !slices.Equal(got[id], w) {
			t.Errorf("%s: %v, want %v", id, got[id], w)
		}
	}
	if len(got) != len(want) {
		t.Errorf("categories %v", got)
	}
	for _, c := range res.Categories {
		if c.Deletion != cleanup.DeleteTrash || c.Kind != cleanup.KindInstaller || c.Selected != (c.ID != catRecent) {
			t.Errorf("%s %+v", c.ID, c)
		}
	}
	recent := category(t, res, catRecent)
	notes := map[string]string{}
	for _, it := range recent.Items {
		notes[filepath.Base(it.Path)] = it.Note
	}
	if notes["mounted.dmg"] != "磁盘映像 · 已挂载" || notes["new.dmg"] != "磁盘映像" {
		t.Errorf("notes %v", notes)
	}
	legacy := category(t, res, catPackages)
	for _, it := range legacy.Items {
		if filepath.Base(it.Path) == "Legacy.pkg" && !it.Dir {
			t.Error("bundle package not marked as a folder")
		}
		if !it.ModTime.Before(now.Add(-7 * day)) {
			t.Errorf("%s dated %v", it.Path, it.ModTime)
		}
	}
}

func TestScanWindows(t *testing.T) {
	fx := newFixture(t, true)
	old := 30 * day
	for _, rel := range []string{
		"Downloads/rustup-init.exe", "Downloads/tools/portable.exe", "Downloads/tools/App-Setup-1.0.exe",
		"Downloads/x.msi", "Downloads/y.msixbundle", "Downloads/z.appx", "Downloads/win11.iso", "Downloads/a.dmg", "Downloads/b.pkg",
		"Desktop/game.exe", "Desktop/Firefox Installer.exe", "Desktop/c.msi",
	} {
		write(t, fx.home, rel, 0, old)
	}
	_, res := fx.scan(t)
	got := fx.contents(t, res)
	want := map[string][]string{
		catPackages: {
			"Desktop/Firefox Installer.exe", "Desktop/c.msi",
			"Downloads/rustup-init.exe", "Downloads/tools/App-Setup-1.0.exe", "Downloads/x.msi", "Downloads/y.msixbundle", "Downloads/z.appx",
		},
		catSystemImages: {"Downloads/win11.iso"},
	}
	if len(got) != len(want) || !slices.Equal(got[catPackages], want[catPackages]) || !slices.Equal(got[catSystemImages], want[catSystemImages]) {
		t.Errorf("got %v, want %v", got, want)
	}
	if desc := category(t, res, catPackages).Description; desc == "" || desc[0] != '.' {
		t.Errorf("description %q", desc)
	}
}

func TestScanProgressAndDenied(t *testing.T) {
	fx := newFixture(t, false)
	write(t, fx.home, "Downloads/a.dmg", 0, 30*day)
	locked := filepath.Join(fx.home, "Downloads", "locked")
	if err := os.Mkdir(locked, 0o000); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	fx.roots = append(fx.roots, filepath.Join(fx.home, "missing"))
	var events []cleanup.Progress
	c := newCleaner(fx.env)
	res, err := c.Scan(t.Context(), func(p cleanup.Progress) { events = append(events, p) })
	if err != nil {
		t.Fatal(err)
	}
	last := events[len(events)-1]
	if last.Done != 3 || last.Total != 3 || last.Bytes == 0 || last.Phase != cleanup.PhaseScanning {
		t.Errorf("progress %+v", events)
	}
	if os.Geteuid() != 0 && res.Denied != 1 {
		t.Errorf("denied %d", res.Denied)
	}
}

func TestMountedErrorIsNotFatal(t *testing.T) {
	fx := newFixture(t, false)
	write(t, fx.home, "Downloads/a.dmg", 0, 30*day)
	fx.env.Mounted = func(context.Context) ([]string, error) { return nil, errors.New("hdiutil failed") }
	_, res := fx.scan(t)
	if got := fx.contents(t, res)[catDiskImages]; !slices.Equal(got, []string{"Downloads/a.dmg"}) {
		t.Errorf("disk images %v", got)
	}
}

func TestScanCancelled(t *testing.T) {
	fx := newFixture(t, false)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := newCleaner(fx.env).Scan(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("err %v", err)
	}
}

func TestCleanMovesToTrash(t *testing.T) {
	fx := newFixture(t, false)
	write(t, fx.home, "Downloads/a.dmg", 0, 30*day)
	write(t, fx.home, "Desktop/b.pkg", 0, 30*day)
	write(t, fx.home, "Downloads/new.dmg", 0, day)
	c, res := fx.scan(t)
	pkg := category(t, res, catPackages).Items[0]
	out, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catDiskImages, catPackages}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	slices.Sort(fx.del.trashed)
	if !slices.Equal(fx.del.trashed, []string{"a.dmg", "b.pkg"}) || out.Removed != 2 || out.Trashed == 0 || out.Freed != 0 || out.Partial {
		t.Errorf("trashed %v, result %+v", fx.del.trashed, out)
	}
	if _, err := os.Stat(filepath.Join(fx.home, "Downloads/new.dmg")); err != nil {
		t.Errorf("an unselected installer went: %v", err)
	}
	if _, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catPackages}, ItemIDs: []string{pkg.ID}}, nil); !errors.Is(err, cleanup.ErrStale) {
		t.Errorf("cleaning a trashed item again: %v", err)
	}
}

func TestClassify(t *testing.T) {
	const big = minArchiveSize
	cases := []struct {
		f       file
		windows bool
		want    kind
	}{
		{file{name: "Foo.dmg"}, false, kindDiskImage},
		{file{name: "Foo.DMG"}, false, kindDiskImage},
		{file{name: "Foo.dmg", dir: true}, false, kindNone},
		{file{name: "Foo.pkg"}, false, kindPackage},
		{file{name: "Foo.mpkg", dir: true}, false, kindPackage},
		{file{name: "ubuntu.iso"}, false, kindSystemImage},
		{file{name: "Xcode.xip"}, false, kindArchive},
		{file{name: "VSCode-darwin-universal.zip", size: big}, false, kindArchive},
		{file{name: "VSCode-darwin-universal.zip", size: big - 1}, false, kindNone},
		{file{name: "photos.zip", size: big}, false, kindNone},
		{file{name: "setup.exe"}, false, kindNone},
		{file{name: "tool.exe", topOfDownloads: true}, true, kindPackage},
		{file{name: "tool.exe"}, true, kindNone},
		{file{name: "tool-setup.exe"}, true, kindPackage},
		{file{name: "x.msi"}, true, kindPackage},
		{file{name: "x.MSIX"}, true, kindPackage},
		{file{name: "x.appxbundle"}, true, kindPackage},
		{file{name: "x.iso"}, true, kindSystemImage},
		{file{name: "x.dmg"}, true, kindNone},
		{file{name: "x.zip", size: big}, true, kindNone},
		{file{name: "x.msi", dir: true}, true, kindNone},
	}
	for _, tc := range cases {
		if got := classify(tc.f, tc.windows); got != tc.want {
			t.Errorf("classify(%+v, windows=%v) = %v, want %v", tc.f, tc.windows, got, tc.want)
		}
	}
}

func TestInstallerLike(t *testing.T) {
	for name, want := range map[string]bool{
		"VSCode-darwin-universal.zip":  true,
		"ChromeSetup.exe":              true,
		"Docker Desktop Installer.exe": true,
		"node-v22.11.0.pkg":            true,
		"ffmpeg_arm64.zip":             true,
		"python-3.13-amd64.exe":        true,
		"app_x86_64.zip":               true,
		"Firefox 132.0.dmg":            true,
		"tool-v2.zip":                  true,
		"holiday photos.zip":           false,
		"machine-learning.zip":         false,
		"universe.zip":                 false,
		"report final.zip":             false,
	} {
		if got := installerLike(name); got != want {
			t.Errorf("installerLike(%q) = %v", name, got)
		}
	}
}

func TestDescendable(t *testing.T) {
	for name, want := range map[string]bool{
		"tools": true, "node-v22.1": true, "My Stuff": true,
		"Some.app": false, "Photos.photoslibrary": false, ".git": false, "Project.xcodeproj": false,
	} {
		if got := descendable(name); got != want {
			t.Errorf("descendable(%q) = %v", name, got)
		}
	}
}

func TestBusy(t *testing.T) {
	fx := newFixture(t, false)
	entered := make(chan struct{})
	release := make(chan struct{})
	fx.env.Mounted = func(context.Context) ([]string, error) {
		close(entered)
		<-release
		return nil, nil
	}
	c := newCleaner(fx.env)
	done := make(chan error)
	go func() {
		_, err := c.Scan(t.Context(), nil)
		done <- err
	}()
	<-entered
	if _, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catDiskImages}}, nil); !errors.Is(err, ErrBusy) {
		t.Errorf("clean during scan: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
