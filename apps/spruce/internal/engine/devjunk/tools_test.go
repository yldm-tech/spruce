package devjunk

import (
	"context"
	"os/exec"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"spruce/apps/spruce/internal/engine/devcaches"
	"spruce/apps/spruce/internal/engine/execx/execxtest"
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
