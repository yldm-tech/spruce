package devjunk

import (
	"context"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"spruce/apps/spruce/internal/engine/execx/execxtest"
)

func TestCatalogDarwin(t *testing.T) {
	home := "/Users/u"
	e := env{goos: "darwin", home: home, getenv: envOf(map[string]string{"PATH": "/usr/bin:/bin", "CARGO_HOME": "/opt/cargo"})}
	l := located{
		bins:      map[string]string{"brew": "/opt/homebrew/bin/brew", "npm": "/opt/homebrew/bin/npm", "go": "/usr/local/go/bin/go"},
		goCache:   "/Users/u/Library/Caches/go-build",
		brewCache: "/Users/u/Library/Caches/Homebrew",
		xcrun:     xcrunPath,
	}
	ts := catalog(e, l)

	for id, want := range map[string]string{
		"xcode-derived-data": "/Users/u/Library/Developer/Xcode/DerivedData",
		"xcode-archives":     "/Users/u/Library/Developer/Xcode/Archives",
		"simulator-caches":   "/Users/u/Library/Developer/CoreSimulator/Caches",
		"xcode-simulators":   "/Users/u/Library/Developer/CoreSimulator/Devices",
		"xcode-cache":        "/Users/u/Library/Caches/com.apple.dt.Xcode",
		"homebrew":           "/Users/u/Library/Caches/Homebrew",
		"npm":                "/Users/u/.npm/_cacache",
		"yarn":               "/Users/u/Library/Caches/Yarn",
		"bun":                "/Users/u/.bun/install/cache",
		"node-gyp":           "/Users/u/Library/Caches/node-gyp",
		"playwright":         "/Users/u/Library/Caches/ms-playwright",
		"typescript":         "/Users/u/Library/Caches/typescript",
		"deno":               "/Users/u/Library/Caches/deno",
		"go-build":           "/Users/u/Library/Caches/go-build",
		"cargo":              "/opt/cargo/registry/cache",
		"pip":                "/Users/u/Library/Caches/pip",
		"uv":                 "/Users/u/.cache/uv",
		"gradle-caches":      "/Users/u/.gradle/caches",
		"maven":              "/Users/u/.m2/repository",
		"cocoapods":          "/Users/u/Library/Caches/CocoaPods",
		"swiftpm":            "/Users/u/Library/Caches/org.swift.swiftpm",
		"vscode":             "/Users/u/Library/Application Support/Code/Cache",
		"bazel":              "/Users/u/Library/Caches/bazel",
		"pnpm-cache":         "/Users/u/Library/Caches/pnpm",
		"go-tools":           "/Users/u/Library/Caches/gopls",
		"rust-tools":         "/Users/u/Library/Caches/Mozilla.sccache",
		"poetry":             "/Users/u/Library/Caches/pypoetry/cache",
		"mise":               "/Users/u/Library/Caches/mise",
	} {
		if got := toolByID(t, ts, id).dirs[0].path; got != want {
			t.Errorf("%s: dir %q, want %q", id, got, want)
		}
	}
	for _, id := range []string{"nuget-cache", "nuget-packages"} {
		if hasTool(ts, id) {
			t.Errorf("darwin catalog has the Windows-only %s", id)
		}
	}

	brew := toolByID(t, ts, "homebrew")
	if brew.command == nil || brew.command.Name != "/opt/homebrew/bin/brew" || !slices.Equal(brew.command.Args, []string{"cleanup", "-s", "--prune=all"}) {
		t.Errorf("homebrew command = %+v", brew.command)
	}
	if !slices.Contains(brew.command.Env, "PATH=/opt/homebrew/bin:/usr/bin:/bin") {
		t.Errorf("homebrew command env = %v, want its folder first on PATH", brew.command.Env)
	}
	goBuild := toolByID(t, ts, "go-build")
	if goBuild.command == nil || !slices.Equal(goBuild.command.Args, []string{"clean", "-cache"}) || !slices.Contains(goBuild.command.Env, "GOTOOLCHAIN=local") {
		t.Errorf("go-build command = %+v", goBuild.command)
	}
	if sim := toolByID(t, ts, "xcode-simulators"); sim.command == nil || sim.command.Name != xcrunPath || !slices.Equal(sim.command.Args, []string{"simctl", "delete", "unavailable"}) {
		t.Errorf("simulators command = %+v", sim.command)
	}
	if p := toolByID(t, ts, "pip"); p.command != nil {
		t.Errorf("pip has a command without pip3: %+v", p.command)
	}
	if p := toolByID(t, ts, "pnpm"); p.command != nil || !p.commandOnly {
		t.Errorf("pnpm without pnpm = %+v", p)
	}

	// Default selection: risky caches start unchecked.
	for id, want := range map[string]bool{
		"xcode-derived-data": true, "xcode-archives": false, "playwright": false, "cypress": false,
		"go-build": true, "go-mod": false, "gradle-caches": true, "gradle-wrapper": false, "maven": false, "vscode": true,
	} {
		if got := toolByID(t, ts, id).selected; got != want {
			t.Errorf("%s selected = %v, want %v", id, got, want)
		}
	}
	if a := toolByID(t, ts, "xcode-archives"); !a.trash || a.warning == "" {
		t.Errorf("archives must go to the Trash with a warning: %+v", a)
	}
	cargo := toolByID(t, ts, "cargo")
	if len(cargo.dirs) != 3 || cargo.dirs[0].off || cargo.dirs[1].off || !cargo.dirs[2].off {
		t.Errorf("cargo dirs = %+v, want git checkouts off", cargo.dirs)
	}
	if poetry := toolByID(t, ts, "poetry"); len(poetry.dirs) != 3 || poetry.dirs[0].off || poetry.dirs[1].off || !poetry.dirs[2].off || filepath.Base(poetry.dirs[2].path) != "virtualenvs" {
		t.Errorf("poetry dirs = %+v, want the virtualenvs off", poetry.dirs)
	}
	if uv := toolByID(t, ts, "uv"); len(uv.dirs) != 2 || uv.dirs[1].path != "/Users/u/Library/Caches/uv" {
		t.Errorf("uv dirs = %+v, want the old Library/Caches folder too", uv.dirs)
	}

	running := catalog(e, located{bins: map[string]string{}, vscodeRunning: true})
	if v := toolByID(t, running, "vscode"); v.selected || v.warning == "" {
		t.Errorf("vscode while running = %+v, want unchecked with a warning", v)
	}
}

func TestLocateDarwin(t *testing.T) {
	home := t.TempDir()
	devices := filepath.Join(home, "Devices")
	if err := os.Mkdir(devices, 0o755); err != nil {
		t.Fatal(err)
	}
	devDir := filepath.Join(home, "Xcode.app", "Contents", "Developer")
	if err := os.MkdirAll(filepath.Join(devDir, "usr", "bin"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(devDir, "usr", "bin", "simctl"), nil, 0o755); err != nil {
		t.Fatal(err)
	}
	e := env{goos: "darwin", home: home, getenv: envOf(map[string]string{"PATH": "/usr/bin:/bin"})}
	look := lookPathOf(map[string]string{
		"/opt/homebrew/bin/go":   "/opt/homebrew/bin/go",
		"/opt/homebrew/bin/brew": "/opt/homebrew/bin/brew",
		"pip3":                   "/usr/bin/pip3", // the Command Line Tools stub, never used
		filepath.Join(home, "Library", "pnpm", "pnpm"): filepath.Join(home, "Library", "pnpm", "pnpm"),
	})
	f := execxtest.New()
	f.On("/opt/homebrew/bin/go", "env", "GOCACHE", "GOMODCACHE").Return("/c/go-build\n/c/mod\n", 0)
	f.On(filepath.Join(home, "Library", "pnpm", "pnpm"), "store", "path").Return("/c/pnpm/store/v10\n", 0)
	f.On("/opt/homebrew/bin/brew", "--cache").Return("Warning: something\n", 0)
	f.On(xcodeSelectPath, "-p").Return(devDir+"\n", 0)

	l := locate(context.Background(), e, f, look, devices, false)
	if l.bins["go"] != "/opt/homebrew/bin/go" || l.bins["brew"] != "/opt/homebrew/bin/brew" {
		t.Errorf("bins = %v", l.bins)
	}
	if _, ok := l.bins["pip3"]; ok {
		t.Errorf("pip3 in /usr/bin was accepted: %v", l.bins)
	}
	if _, ok := l.bins["npm"]; ok {
		t.Errorf("npm found without being installed: %v", l.bins)
	}
	if l.goCache != "/c/go-build" || l.goModCache != "/c/mod" || l.pnpmStore != "/c/pnpm/store/v10" {
		t.Errorf("located = %+v", l)
	}
	if l.brewCache != "" {
		t.Errorf("brew --cache printing a warning gave %q, want the default", l.brewCache)
	}
	if l.xcrun != xcrunPath {
		t.Errorf("xcrun = %q with simctl present", l.xcrun)
	}
	for _, c := range f.Calls() {
		if c.Timeout != queryTimeout {
			t.Errorf("%s ran with timeout %v", c, c.Timeout)
		}
	}

	// Without Xcode's simctl (only the Command Line Tools) the simulators are left alone, and without a devices folder xcode-select is not even asked.
	if err := os.Remove(filepath.Join(devDir, "usr", "bin", "simctl")); err != nil {
		t.Fatal(err)
	}
	if l := locate(context.Background(), e, f, look, devices, false); l.xcrun != "" {
		t.Errorf("xcrun = %q without simctl", l.xcrun)
	}
	g := execxtest.New()
	locate(context.Background(), e, g, lookPathOf(nil), filepath.Join(home, "missing"), false)
	g.AssertNotCalled(t, xcodeSelectPath)
	if len(g.Calls()) != 0 {
		t.Errorf("locate ran %v with no tools installed", g.Calls())
	}
}
