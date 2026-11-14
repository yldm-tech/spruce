package uninstall

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"howett.net/plist"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
)

// fixture is a fake machine: two app folders and a home with its Library.
type fixture struct {
	apps, userApps, home, lib string
}

func newFixture(t *testing.T) fixture {
	t.Helper()
	root := t.TempDir()
	// The engine compares resolved paths with fsx, which resolves /var to /private/var.
	if r, err := filepath.EvalSymlinks(root); err == nil {
		root = r
	}
	f := fixture{
		apps:     filepath.Join(root, "Applications"),
		home:     filepath.Join(root, "home"),
		userApps: filepath.Join(root, "home", "Applications"),
	}
	f.lib = filepath.Join(f.home, "Library")
	mkdir(t, f.apps)
	mkdir(t, f.userApps)
	mkdir(t, f.lib)
	return f
}

// bundle writes a fake .app with the given Info.plist values and returns its path.
func (f fixture) bundle(t *testing.T, dir, file string, info map[string]any) string {
	t.Helper()
	p := filepath.Join(dir, file)
	writePlist(t, filepath.Join(p, "Contents", "Info.plist"), info)
	writeFile(t, filepath.Join(p, "Contents", "MacOS", strings.TrimSuffix(file, ".app")), strings.Repeat("x", 4096))
	return p
}

func (f fixture) engine(t *testing.T, cfg Config) *Engine {
	t.Helper()
	cfg.Home = f.home
	cfg.AppDirs = []string{f.apps, f.userApps}
	if cfg.Processes == nil {
		cfg.Processes = noProcesses
	}
	if cfg.Runner == nil {
		cfg.Runner = execxtest.New()
	}
	if cfg.IconCache == "" {
		cfg.IconCache = filepath.Join(t.TempDir(), "icons")
	}
	return New(cfg)
}

func TestAppsListsBundles(t *testing.T) {
	f := newFixture(t)
	foo := f.bundle(t, f.apps, "Foo.app", map[string]any{
		"CFBundleIdentifier": "com.example.foo", "CFBundleDisplayName": "Foo Pro", "CFBundleName": "Foo",
		"CFBundleShortVersionString": "2.1", "CFBundleVersion": "210", "CFBundleIconFile": "AppIcon",
	})
	writeFile(t, filepath.Join(foo, "Contents", "Resources", "AppIcon.icns"), "icns")
	writeFile(t, filepath.Join(foo, "Contents", "_MASReceipt", "receipt"), "receipt")
	// A vendor folder with the app one level down, and a bundle nested in a bundle that must not be found.
	nested := f.bundle(t, filepath.Join(f.apps, "Vendor"), "Bar.app", map[string]any{"CFBundleIdentifier": "com.example.bar", "CFBundleName": "Bar", "CFBundleVersion": "7"})
	f.bundle(t, filepath.Join(nested, "Contents", "Helpers"), "BarHelper.app", map[string]any{"CFBundleIdentifier": "com.example.bar.helper"})
	f.bundle(t, filepath.Join(f.apps, "Vendor", "Deeper"), "Deep.app", map[string]any{"CFBundleIdentifier": "com.example.deep"})
	// No Info.plist at all: listed under its file name.
	mkdir(t, filepath.Join(f.userApps, "Plain.app", "Contents"))
	// A broken Info.plist: listed under its file name.
	writeFile(t, filepath.Join(f.userApps, "Broken.app", "Contents", "Info.plist"), "<plist><dict><key>CFBundleIdentifier")
	// Left out: Apple's apps, Spruce, symlinks, hidden bundles and plain files.
	f.bundle(t, f.apps, "Safari.app", map[string]any{"CFBundleIdentifier": "com.apple.Safari"})
	f.bundle(t, f.apps, "Spruce.app", map[string]any{"CFBundleIdentifier": SelfBundleID})
	f.bundle(t, f.apps, "Spruce Dev.app", map[string]any{"CFBundleIdentifier": SelfBundleID + ".dev"})
	f.bundle(t, f.apps, ".Hidden.app", map[string]any{"CFBundleIdentifier": "com.example.hidden"})
	if err := os.Symlink(foo, filepath.Join(f.userApps, "Link.app")); err != nil {
		t.Fatal(err)
	}
	writeFile(t, filepath.Join(f.apps, "File.app"), "not a bundle")
	// The bundle the running executable is in, whatever its ID.
	self := f.bundle(t, f.apps, "Other.app", map[string]any{"CFBundleIdentifier": "com.example.other"})

	running := filepath.Join(nested, "Contents", "MacOS", "Bar")
	e := f.engine(t, Config{
		Self:      filepath.Join(self, "Contents", "MacOS", "Other"),
		Processes: func(context.Context) ([]string, error) { return []string{running, "/usr/bin/true"}, nil },
	})
	apps, err := e.Apps(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, a := range apps {
		names = append(names, a.Name)
	}
	if want := []string{"Bar", "Broken", "Foo Pro", "Plain"}; !slices.Equal(names, want) {
		t.Fatalf("names = %v, want %v", names, want)
	}
	bar, broken, fooApp := apps[0], apps[1], apps[2]
	if fooApp.BundleID != "com.example.foo" || fooApp.Version != "2.1" || !fooApp.AppStore || fooApp.Running || fooApp.Path != foo {
		t.Errorf("Foo = %+v", fooApp)
	}
	if fooApp.iconFile != filepath.Join(foo, "Contents", "Resources", "AppIcon.icns") {
		t.Errorf("Foo icon = %q", fooApp.iconFile)
	}
	if !slices.Equal(fooApp.names, []string{"Foo Pro", "Foo"}) {
		t.Errorf("Foo names = %v", fooApp.names)
	}
	if bar.Version != "7" || bar.AppStore || !bar.Running || bar.iconFile != "" {
		t.Errorf("Bar = %+v", bar)
	}
	if broken.BundleID != "" || broken.Path != filepath.Join(f.userApps, "Broken.app") {
		t.Errorf("Broken = %+v", broken)
	}
	if fooApp.ID == bar.ID || fooApp.ID == "" {
		t.Error("IDs must be set and distinct")
	}
}

func TestAppsHonoursExclusions(t *testing.T) {
	f := newFixture(t)
	f.bundle(t, f.apps, "Keep.app", map[string]any{"CFBundleIdentifier": "com.example.keep"})
	skip := f.bundle(t, f.apps, "Skip.app", map[string]any{"CFBundleIdentifier": "com.example.skip"})
	e := f.engine(t, Config{Exclusions: func() []string { return []string{skip} }})
	apps, err := e.Apps(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if len(apps) != 1 || apps[0].Name != "Keep" {
		t.Errorf("apps = %+v", apps)
	}
}

func TestAppsFailsOnlyWhenNoFolderCanBeRead(t *testing.T) {
	e := New(Config{AppDirs: []string{filepath.Join(t.TempDir(), "missing")}, Processes: noProcesses})
	apps, err := e.Apps(t.Context())
	if err != nil || len(apps) != 0 {
		t.Errorf("missing folders: %v, %v", apps, err)
	}
}

func TestParseMdls(t *testing.T) {
	used := time.Date(2026, 10, 13, 4, 38, 32, 0, time.UTC)
	out := "com.apple.application-bundle\x002026-10-13 04:38:32 +0000\x00com.apple.application-bundle\x00(null)\x00(null)\x00(null)\x00com.apple.application-bundle\x00garbage"
	got, ok := parseMdls(out, 4)
	if !ok {
		t.Fatal("not parsed")
	}
	if got[0].Usage != UsageUsed || !got[0].LastUsed.Equal(used) {
		t.Errorf("used: %+v", got[0])
	}
	if got[1].Usage != UsageNever || got[2].Usage != UsageUnknown || got[3].Usage != UsageUnknown {
		t.Errorf("never/unknown/garbage: %+v", got[1:])
	}
	if _, ok := parseMdls("com.apple.application-bundle\x00(null)", 2); ok {
		t.Error("a short answer must not be lined up")
	}
}

func TestDetailsStreamsUsageIconAndSize(t *testing.T) {
	f := newFixture(t)
	foo := f.bundle(t, f.apps, "Foo.app", map[string]any{"CFBundleIdentifier": "com.example.foo", "CFBundleIconFile": "Foo.icns"})
	icns := filepath.Join(foo, "Contents", "Resources", "Foo.icns")
	writeFile(t, icns, "icns")
	bar := f.bundle(t, f.apps, "Bar.app", map[string]any{"CFBundleIdentifier": "com.example.bar"})
	gone := f.bundle(t, f.apps, "Gone.app", map[string]any{"CFBundleIdentifier": "com.example.gone"})

	runner := execxtest.New()
	args := []string{"-raw", "-name", "kMDItemContentType", "-name", "kMDItemLastUsedDate"}
	// The batch answer is short (mdls skipped a bundle), so each bundle is asked again on its own.
	runner.On("/usr/bin/mdls", append(args, bar, foo, gone)...).Return("com.apple.application-bundle\x00(null)", 0)
	runner.On("/usr/bin/mdls", append(args, bar)...).Return("com.apple.application-bundle\x00(null)", 0)
	runner.On("/usr/bin/mdls", append(args, foo)...).Return("com.apple.application-bundle\x002026-10-01 02:45:19 +0000", 0)
	runner.On("/usr/bin/mdls", append(args, gone)...).Return("", 1)
	png := "\x89PNG\r\n\x1a\nfake"
	sipsCalls := 0
	sips := func(_ context.Context, cmd execx.Command) (execx.Output, error) {
		sipsCalls++
		if cmd.Args[len(cmd.Args)-3] != icns {
			t.Errorf("sips converts %q", cmd.Args[len(cmd.Args)-3])
		}
		return execx.Output{}, os.WriteFile(cmd.Args[len(cmd.Args)-1], []byte(png), 0o600)
	}
	cache := filepath.Join(t.TempDir(), "icons")
	mkdir(t, cache)
	stale := filepath.Join(cache, "0123.png")
	writeFile(t, stale, png)

	e := f.engine(t, Config{Runner: &sipsRouter{Fake: runner, sips: sips}, IconCache: cache})
	apps, err := e.Apps(t.Context())
	if err != nil || len(apps) != 3 {
		t.Fatalf("apps: %v %v", apps, err)
	}
	// Gone vanishes after it was listed.
	if err := os.RemoveAll(gone); err != nil {
		t.Fatal(err)
	}
	measure := func() map[string][]Details {
		got := make(map[string][]Details)
		if err := e.Details(t.Context(), func(d Details) { got[d.ID] = append(got[d.ID], d) }); err != nil {
			t.Fatal(err)
		}
		return got
	}
	got := measure()
	byName := func(name string) []Details {
		for _, a := range apps {
			if a.Name == name {
				return got[a.ID]
			}
		}
		return nil
	}
	fooDetails, barDetails, goneDetails := byName("Foo"), byName("Bar"), byName("Gone")
	if !hasUsage(fooDetails, UsageUsed) || !hasUsage(barDetails, UsageNever) || !hasUsage(goneDetails, UsageUnknown) {
		t.Errorf("usage: foo %+v bar %+v gone %+v", fooDetails, barDetails, goneDetails)
	}
	if icon := iconOf(fooDetails); icon != dataURL([]byte(png)) {
		t.Errorf("Foo icon = %q", icon)
	}
	if iconOf(barDetails) != "" {
		t.Error("an app without an .icns gets no icon")
	}
	if s, ok := sizeOf(fooDetails); !ok || s <= 0 {
		t.Errorf("Foo size = %d, %v", s, ok)
	}
	if _, ok := sizeOf(goneDetails); ok {
		t.Error("a vanished bundle has no size")
	}
	if exists(stale) {
		t.Error("the icon of an app no longer installed stays in the cache")
	}
	// The second pass reads the icon from the cache.
	got = measure()
	if iconOf(byName("Foo")) == "" || sipsCalls != 1 {
		t.Errorf("cached icon: %d sips runs", sipsCalls)
	}
}

// sipsRouter answers sips with fn whatever its output path, and everything else with the Fake.
type sipsRouter struct {
	*execxtest.Fake
	sips func(context.Context, execx.Command) (execx.Output, error)
}

func (r *sipsRouter) Run(ctx context.Context, cmd execx.Command) (execx.Output, error) {
	if cmd.Name == "/usr/bin/sips" {
		return r.sips(ctx, cmd)
	}
	return r.Fake.Run(ctx, cmd)
}

func hasUsage(ds []Details, u Usage) bool {
	return slices.ContainsFunc(ds, func(d Details) bool { return d.Usage == u })
}

func iconOf(ds []Details) string {
	for _, d := range ds {
		if d.Icon != "" {
			return d.Icon
		}
	}
	return ""
}

func sizeOf(ds []Details) (int64, bool) {
	for _, d := range ds {
		if d.HasSize {
			return d.Size, true
		}
	}
	return 0, false
}

func TestInfoPlistBinary(t *testing.T) {
	dir := t.TempDir()
	b, err := plist.Marshal(map[string]any{"CFBundleIdentifier": "com.example.bin", "CFBundleVersion": 12}, plist.BinaryFormat)
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(dir, "Info.plist")
	writeFile(t, p, string(b))
	got := readInfoPlist(p)
	if got["CFBundleIdentifier"] != "com.example.bin" {
		t.Errorf("binary plist: %v", got)
	}
	if _, ok := got["CFBundleVersion"]; ok {
		t.Error("a number is not read as a string")
	}
}
