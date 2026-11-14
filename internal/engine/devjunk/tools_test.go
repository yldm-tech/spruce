package devjunk

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/devcaches"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
)

// lookPathOf finds exactly the names and paths in found.
func lookPathOf(found map[string]string) func(string) (string, error) {
	return func(name string) (string, error) {
		if p, ok := found[name]; ok {
			return p, nil
		}
		return "", exec.ErrNotFound
	}
}

func envOf(vars map[string]string) func(string) string {
	return func(k string) string { return vars[k] }
}

func toolByID(t *testing.T, ts []*tool, id string) *tool {
	t.Helper()
	for _, x := range ts {
		if x.id == id {
			return x
		}
	}
	t.Fatalf("no tool %q in %d tools", id, len(ts))
	return nil
}

func hasTool(ts []*tool, id string) bool {
	return slices.ContainsFunc(ts, func(x *tool) bool { return x.id == id })
}

func TestEnvIsAbs(t *testing.T) {
	win := env{goos: "windows"}
	for p, want := range map[string]bool{`C:\Users\a`: true, `d:/x`: true, `\\server\share`: true, `Users\a`: false, `C:`: false, `/usr`: false, `1:\x`: false} {
		if got := win.isAbs(p); got != want {
			t.Errorf("windows isAbs(%q) = %v, want %v", p, got, want)
		}
	}
	mac := env{goos: "darwin"}
	if !mac.isAbs("/Users/a") || mac.isAbs(`C:\x`) {
		t.Error("darwin isAbs is wrong")
	}
}

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

func TestCatalogWindows(t *testing.T) {
	home := `C:\Users\u`
	local := `C:\Users\u\AppData\Local`
	roaming := `D:\Roaming`
	e := env{goos: "windows", home: home, getenv: envOf(map[string]string{
		"LOCALAPPDATA": local, "APPDATA": roaming, "USERPROFILE": home, "GRADLE_USER_HOME": "relative", "PATH": `C:\Windows`,
	})}
	l := located{bins: map[string]string{"npm": `C:\Program Files\nodejs\npm.cmd`, "pip": `C:\Python\Scripts\pip.exe`}}
	ts := catalog(e, l)
	for id, want := range map[string]string{
		"npm":            filepath.Join(local, "npm-cache", "_cacache"),
		"yarn":           filepath.Join(local, "Yarn", "Cache"),
		"bun":            filepath.Join(home, ".bun", "install", "cache"),
		"node-gyp":       filepath.Join(local, "node-gyp", "Cache"),
		"playwright":     filepath.Join(local, "ms-playwright"),
		"cypress":        filepath.Join(local, "Cypress", "Cache"),
		"electron":       filepath.Join(local, "electron", "Cache"),
		"typescript":     filepath.Join(local, "Microsoft", "TypeScript"),
		"deno":           filepath.Join(local, "deno"),
		"go-build":       filepath.Join(local, "go-build"),
		"cargo":          filepath.Join(home, ".cargo", "registry", "cache"),
		"pip":            filepath.Join(local, "pip", "Cache"),
		"uv":             filepath.Join(local, "uv", "cache"),
		"gradle-caches":  filepath.Join(home, ".gradle", "caches"), // a relative GRADLE_USER_HOME is ignored
		"maven":          filepath.Join(home, ".m2", "repository"),
		"vscode":         filepath.Join(roaming, "Code", "Cache"),
		"nuget-cache":    filepath.Join(local, "NuGet", "v3-cache"),
		"nuget-packages": filepath.Join(home, ".nuget", "packages"),
		"pnpm-cache":     filepath.Join(local, "pnpm-cache"),
		"go-tools":       filepath.Join(local, "gopls"),
		"rust-tools":     filepath.Join(local, "Mozilla", "sccache"),
		"poetry":         filepath.Join(local, "pypoetry", "Cache", "cache"),
	} {
		if got := toolByID(t, ts, id).dirs[0].path; got != want {
			t.Errorf("%s: dir %q, want %q", id, got, want)
		}
	}
	for _, id := range []string{"xcode-derived-data", "xcode-simulators", "homebrew", "cocoapods", "swiftpm", "bazel", "mise"} {
		if hasTool(ts, id) {
			t.Errorf("windows catalog has the macOS-only %s", id)
		}
	}
	npm := toolByID(t, ts, "npm")
	if npm.command == nil || npm.command.Name != `C:\Program Files\nodejs\npm.cmd` || !slices.Equal(npm.command.Args, []string{"cache", "clean", "--force"}) {
		t.Fatalf("npm command = %+v", npm.command)
	}
	if !slices.Contains(npm.command.Env, `PATH=C:\Program Files\nodejs;C:\Windows`) {
		t.Errorf("npm env = %v, want its folder first on PATH with ; between", npm.command.Env)
	}
	if p := toolByID(t, ts, "pip"); p.command == nil || p.command.Name != `C:\Python\Scripts\pip.exe` || !slices.Equal(p.command.Args, []string{"cache", "purge"}) {
		t.Errorf("pip command = %+v", p.command)
	}
	if !toolByID(t, ts, "nuget-cache").selected || toolByID(t, ts, "nuget-packages").selected {
		t.Error("NuGet caches must start checked and the global packages unchecked")
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

func TestLocateWindows(t *testing.T) {
	e := env{goos: "windows", home: `C:\Users\u`, getenv: envOf(map[string]string{"ProgramFiles": `C:\Program Files`, "LOCALAPPDATA": `C:\Users\u\AppData\Local`})}
	goExe := filepath.Join(`C:\Program Files`, "Go", "bin", "go.exe")
	look := lookPathOf(map[string]string{
		goExe:  goExe,
		"pnpm": `C:\Users\u\AppData\Roaming\npm\pnpm.cmd`,
		"pip":  `C:\Python312\Scripts\pip.exe`,
		"brew": `C:\brew.exe`, // not a Windows tool: never looked up
	})
	f := execxtest.New()
	f.On(goExe, "env", "GOCACHE", "GOMODCACHE").Return("C:\\Users\\u\\AppData\\Local\\go-build\r\nC:\\Users\\u\\go\\pkg\\mod\r\n", 0)
	f.On(`C:\Users\u\AppData\Roaming\npm\pnpm.cmd`, "store", "path").Fail(exec.ErrNotFound)
	l := locate(context.Background(), e, f, look, "", false)
	if l.bins["go"] != goExe || l.bins["pip"] != `C:\Python312\Scripts\pip.exe` || l.bins["brew"] != "" {
		t.Errorf("bins = %v", l.bins)
	}
	if l.goCache != filepath.Clean(`C:\Users\u\AppData\Local\go-build`) || l.goModCache != filepath.Clean(`C:\Users\u\go\pkg\mod`) {
		t.Errorf("go env = %q, %q", l.goCache, l.goModCache)
	}
	if l.pnpmStore != "" || l.xcrun != "" {
		t.Errorf("located = %+v", l)
	}
	f.AssertNotCalled(t, xcodeSelectPath)
}

func TestCommandLabel(t *testing.T) {
	ts := catalog(env{goos: "windows", home: `C:\u`, getenv: envOf(nil)}, located{bins: map[string]string{"npm": `C:\node\npm.cmd`}})
	if got := commandLabel(*toolByID(t, ts, "npm").command); got != "npm cache clean --force" {
		t.Errorf("label = %q", got)
	}
}

// TestCatalogClaimsTheDevCaches checks the split of ~/Library/Caches with 系统垃圾 (internal/engine/devcaches): every folder of the macOS catalog there is named in devcaches, so 系统垃圾 does not offer it too, and every name in devcaches is offered here, so nothing falls between the two pages.
func TestCatalogClaimsTheDevCaches(t *testing.T) {
	home := "/Users/u"
	caches := filepath.Join(home, "Library", "Caches")
	e := env{goos: "darwin", home: home, getenv: envOf(nil)}
	reached := map[string]bool{}
	for _, l := range []located{{bins: map[string]string{}}, {bins: map[string]string{}, brewCache: filepath.Join(caches, "Homebrew"), goCache: filepath.Join(caches, "go-build")}} {
		for _, tl := range catalog(e, l) {
			for _, d := range tl.dirs {
				rel, err := filepath.Rel(caches, d.path)
				if err != nil || strings.HasPrefix(rel, "..") {
					continue
				}
				name := strings.Split(rel, string(filepath.Separator))[0]
				if !devcaches.Owned(name) {
					t.Errorf("%s offers %s, which devcaches does not name: 系统垃圾 would offer it too", tl.id, d.path)
				}
				reached[strings.ToLower(name)] = true
			}
		}
	}
	// Tools that list their entries themselves, by the folder they read.
	reached["jetbrains"] = true
	for _, name := range devcaches.MacOS() {
		if !reached[strings.ToLower(name)] {
			t.Errorf("devcaches names %s but no tool offers it: neither page would", name)
		}
	}
}
