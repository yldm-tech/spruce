package devjunk

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/gitstate"
)

// fakeGit answers for each project folder by its base name, records every question and can be changed between scan and clean.
type fakeGit struct {
	mu      sync.Mutex
	answers map[string]gitstate.Result
	errs    map[string]error
	asked   map[string]int
	arts    map[string][]gitstate.Artifact
	repos   map[string]string
}

func newFakeGit() *fakeGit {
	return &fakeGit{answers: map[string]gitstate.Result{}, errs: map[string]error{}, asked: map[string]int{}, arts: map[string][]gitstate.Artifact{}, repos: map[string]string{}}
}

func (g *fakeGit) set(name string, r gitstate.Result, err error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	g.answers[name], g.errs[name] = r, err
}

func (g *fakeGit) check(_ context.Context, repo, project string, arts []gitstate.Artifact) (gitstate.Result, error) {
	g.mu.Lock()
	defer g.mu.Unlock()
	name := filepath.Base(project)
	g.asked[name]++
	g.arts[name] = arts
	g.repos[name] = repo
	return g.answers[name], g.errs[name]
}

func (g *fakeGit) count(name string) int {
	g.mu.Lock()
	defer g.mu.Unlock()
	return g.asked[name]
}

// gitProjects makes, under ws, one idle Node project per name (a repository unless its name is "plain") and a .NET project "dotnet" whose bin and obj are not generated-only folders.
func gitProjects(t *testing.T, ws string, names ...string) {
	t.Helper()
	for _, n := range names {
		writeFile(t, ws, n+"/package.json", 10)
		writeFile(t, ws, n+"/node_modules/dep/index.js", 4000)
		if n != "plain" {
			if err := os.MkdirAll(filepath.Join(ws, n, ".git"), 0o755); err != nil {
				t.Fatal(err)
			}
		}
		age(t, filepath.Join(ws, n), 90*24*time.Hour)
	}
}

// realPath is path with its symlinks resolved.
func realPath(t *testing.T, path string) string {
	t.Helper()
	real, err := filepath.EvalSymlinks(path)
	if err != nil {
		t.Fatal(err)
	}
	return real
}

func itemOf(t *testing.T, res cleanup.ScanResult, catID, project string) cleanup.Item {
	t.Helper()
	cat := categoryByID(res, catID)
	if cat == nil {
		t.Fatalf("no category %s", catID)
	}
	for _, it := range cat.Items {
		if filepath.Base(it.Project) == project {
			return it
		}
	}
	t.Fatalf("no item of %s in %s", project, catID)
	return cleanup.Item{}
}

// TestScanMarksGitStates: only clean projects and projects in no repository start checked; a dirty project and build output holding tracked files are held; an unknown state is noted, unchecked and not held.
func TestScanMarksGitStates(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ws := filepath.Join(f.home, "Workspace")
	gitProjects(t, ws, "clean", "dirty", "unknown", "plain", "tracked", "failing", "cleanerr", "dirtyerr")
	writeFile(t, ws, "dotnet/App.csproj", 10)
	writeFile(t, ws, "dotnet/bin/App.dll", 3000)
	writeFile(t, ws, "dotnet/obj/x", 3000)
	if err := os.MkdirAll(filepath.Join(ws, "dotnet", ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	age(t, filepath.Join(ws, "dotnet"), 90*24*time.Hour)
	g := newFakeGit()
	g.set("clean", gitstate.Result{State: cleanup.GitClean}, nil)
	g.set("dirty", gitstate.Result{State: cleanup.GitDirty}, nil)
	g.set("unknown", gitstate.Result{State: cleanup.GitUnknown}, nil)
	g.set("tracked", gitstate.Result{State: cleanup.GitClean, Tracked: true}, nil)
	g.set("failing", gitstate.Result{}, errors.New("exit status 128"))
	// An answer that comes with an error is never clean, but a known reason to hold still holds.
	g.set("cleanerr", gitstate.Result{State: cleanup.GitClean}, errors.New("killed"))
	g.set("dirtyerr", gitstate.Result{State: cleanup.GitDirty}, errors.New("ls-files failed"))
	g.set("dotnet", gitstate.Result{State: cleanup.GitClean}, nil)
	f.opts.Git = g.check
	f.cfg.DevProjectRoots = []string{ws}
	res := f.scan(t)

	cases := []struct {
		name     string
		git      cleanup.GitState
		selected bool
		hold     string
		note     string
	}{
		{"clean", cleanup.GitClean, true, "", "clean"},
		{"plain", cleanup.GitNone, true, "", "plain"},
		{"dirty", cleanup.GitDirty, false, "项目有未提交的改动", "dirty · 有未提交的改动"},
		{"tracked", cleanup.GitClean, false, "构建产物文件夹里有纳入版本控制的文件", "tracked"},
		{"unknown", cleanup.GitUnknown, false, "", "unknown · 无法确定 Git 状态"},
		{"failing", cleanup.GitUnknown, false, "", "failing · 无法确定 Git 状态"},
		{"cleanerr", cleanup.GitUnknown, false, "", "cleanerr · 无法确定 Git 状态"},
		{"dirtyerr", cleanup.GitDirty, false, "项目有未提交的改动", "dirtyerr · 有未提交的改动"},
	}
	for _, c := range cases {
		it := itemOf(t, res, "artifact-node-modules", c.name)
		if it.Git != c.git || it.Selected != c.selected || it.Hold != c.hold || it.Note != c.note || it.Project != filepath.Join(ws, c.name) {
			t.Errorf("%s: git %q selected %v hold %q note %q project %q", c.name, it.Git, it.Selected, it.Hold, it.Note, it.Project)
		}
		if !it.Newest.IsZero() {
			t.Errorf("%s: build output has Newest %v", c.name, it.Newest)
		}
	}
	if g.count("plain") != 0 {
		t.Error("git was asked about a project in no repository")
	}
	// git is asked about real paths (the temporary folder itself lies behind a symlink on macOS).
	realWS := realPath(t, ws)
	if g.repos["clean"] != filepath.Join(realWS, "clean") {
		t.Errorf("repo of clean = %q", g.repos["clean"])
	}
	if arts := g.arts["clean"]; len(arts) != 1 || !arts[0].Generated || arts[0].Path != filepath.Join(realWS, "clean", "node_modules") {
		t.Errorf("artifacts of clean = %+v", arts)
	}
	arts := g.arts["dotnet"]
	if len(arts) != 2 || arts[0].Generated || arts[1].Generated {
		t.Errorf("artifacts of dotnet = %+v, want bin and obj, not generated-only", arts)
	}
	if g.count("dotnet") != 1 {
		t.Errorf("git asked %d times about one project with two build output folders", g.count("dotnet"))
	}
	// A recently active clean project stays unchecked.
	writeFile(t, ws, "clean/README.md", 1)
	res = f.scan(t)
	if it := itemOf(t, res, "artifact-node-modules", "clean"); it.Selected {
		t.Error("recently active project checked")
	}
}

// TestCleanRechecksGit: between scan and clean a project became dirty or got tracked files in its build output: the clean refuses it with the reason and deletes nothing of it; an unknown state goes ahead in a manual clean, which may pick it by hand.
func TestCleanRechecksGit(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ws := filepath.Join(f.home, "Workspace")
	gitProjects(t, ws, "a", "b", "c")
	g := newFakeGit()
	for _, n := range []string{"a", "b"} {
		g.set(n, gitstate.Result{State: cleanup.GitClean}, nil)
	}
	g.set("c", gitstate.Result{State: cleanup.GitUnknown}, nil)
	f.opts.Git = g.check
	f.cfg.DevProjectRoots = []string{ws}
	res := f.scan(t)
	a, b, c := itemOf(t, res, "artifact-node-modules", "a"), itemOf(t, res, "artifact-node-modules", "b"), itemOf(t, res, "artifact-node-modules", "c")
	if c.Selected || c.Hold != "" {
		t.Fatalf("unknown project %+v", c)
	}
	g.set("a", gitstate.Result{State: cleanup.GitDirty}, nil)
	g.set("b", gitstate.Result{State: cleanup.GitClean, Tracked: true}, nil)
	g.set("c", gitstate.Result{}, context.DeadlineExceeded)
	out, err := f.cleaner.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{"artifact-node-modules"}, ItemIDs: []string{a.ID, b.ID, c.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, fl := range out.Failed {
		reasons[fl.ItemID] = fl.Reason
	}
	if reasons[a.ID] != "项目有未提交的改动，已跳过" || reasons[b.ID] != "构建产物文件夹里有纳入版本控制的文件，已跳过" || len(out.Failed) != 2 {
		t.Errorf("failures %+v", out.Failed)
	}
	if !exists(a.Path) || !exists(b.Path) || exists(c.Path) {
		t.Errorf("after the clean: a %v b %v c %v, want only c gone", exists(a.Path), exists(b.Path), exists(c.Path))
	}
	if out.Removed != 1 {
		t.Errorf("removed %d", out.Removed)
	}
}

// TestCleanTrashOnlyNeedsConfirmedGit: an automatic clean takes build output only when git confirms the project clean, or it is in no repository; a git timeout at clean time skips a project the scan found clean, and nothing is deleted for good.
func TestCleanTrashOnlyNeedsConfirmedGit(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ws := filepath.Join(f.home, "Workspace")
	gitProjects(t, ws, "clean", "slow", "plain", "two")
	writeFile(t, ws, "two/.next/cache/x", 2000)
	g := newFakeGit()
	for _, n := range []string{"clean", "slow", "two"} {
		g.set(n, gitstate.Result{State: cleanup.GitClean}, nil)
	}
	f.opts.Git = g.check
	f.cfg.DevProjectRoots = []string{ws}
	res := f.scan(t)
	var req cleanup.CleanRequest
	req.Mode = cleanup.ModeTrashOnly
	for _, c := range res.Categories {
		req.CategoryIDs = append(req.CategoryIDs, c.ID)
		for _, it := range c.Items {
			if it.Project != "" {
				req.ItemIDs = append(req.ItemIDs, it.ID)
			}
		}
	}
	g.set("slow", gitstate.Result{State: cleanup.GitUnknown}, context.DeadlineExceeded)
	before := g.count("two")
	out, err := f.cleaner.Clean(context.Background(), req, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(f.del.removed) != 0 {
		t.Errorf("trash-only deleted for good: %v", f.del.removed)
	}
	slow := itemOf(t, res, "artifact-node-modules", "slow")
	if len(out.Failed) != 1 || out.Failed[0].ItemID != slow.ID || out.Failed[0].Reason != "无法确认 Git 状态，自动清理已跳过" {
		t.Errorf("failures %+v", out.Failed)
	}
	for _, n := range []string{"clean", "plain", "two"} {
		if _, ok := f.del.trashed[itemOf(t, res, "artifact-node-modules", n).Path]; !ok {
			t.Errorf("%s not moved to the Trash: %v", n, f.del.trashed)
		}
	}
	if _, ok := f.del.trashed[filepath.Join(ws, "two", ".next")]; !ok {
		t.Errorf(".next of two not moved: %v", f.del.trashed)
	}
	if got := g.count("two") - before; got != 1 {
		t.Errorf("git asked %d times about one project during one clean", got)
	}
	// The next clean asks again.
	f.del.trashed = map[string][]string{}
	if _, err := f.cleaner.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{"artifact-node-modules"}, ItemIDs: []string{slow.ID}, Mode: cleanup.ModeTrashOnly}, nil); err != nil {
		t.Fatal(err)
	}
	if g.count("slow") < 3 {
		t.Errorf("slow asked %d times over a scan and two cleans", g.count("slow"))
	}
}

// TestHeldItemsAreRefused: build output of a dirty project is refused even when a request names it.
func TestHeldItemsAreRefused(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ws := filepath.Join(f.home, "Workspace")
	gitProjects(t, ws, "dirty")
	g := newFakeGit()
	g.set("dirty", gitstate.Result{State: cleanup.GitDirty}, nil)
	f.opts.Git = g.check
	f.cfg.DevProjectRoots = []string{ws}
	res := f.scan(t)
	it := itemOf(t, res, "artifact-node-modules", "dirty")
	g.set("dirty", gitstate.Result{State: cleanup.GitClean}, nil)
	out, err := f.cleaner.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{"artifact-node-modules"}, ItemIDs: []string{it.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Failed) != 1 || out.Failed[0].Reason != "项目有未提交的改动" || !exists(it.Path) {
		t.Errorf("clean of a held item: %+v", out)
	}
}

// TestScanAsksTheLocatedGit: without Options.Git the scan asks the git it located (here the Command Line Tools' git, as /usr/bin/git is a stub) through the Runner, without the inherited GIT_DIR family.
func TestScanAsksTheLocatedGit(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ws := filepath.Join(f.home, "Workspace")
	gitProjects(t, ws, "web")
	devDir := filepath.Join(f.home, "CommandLineTools")
	gitBin := writeFile(t, devDir, "usr/bin/git", 1)
	f.opts.LookPath = lookPathOf(map[string]string{"git": "/usr/bin/git"})
	f.runner.On(xcodeSelectPath, "-p").Return(devDir+"\n", 0)
	project := filepath.Join(realPath(t, ws), "web")
	f.runner.On(gitBin, "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.quotepath=false", "-C", project,
		"status", "--porcelain=v1", "-z", "--untracked-files=normal", "--ignore-submodules=dirty", "--", ".").Return(" M src/index.js\x00", 0)
	f.runner.On(gitBin, "--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.quotepath=false", "-C", project, "ls-files", "-z", "--", "node_modules").Return("", 0)
	f.cfg.DevProjectRoots = []string{ws}
	res := f.scan(t)
	if it := itemOf(t, res, "artifact-node-modules", "web"); it.Git != cleanup.GitDirty || it.Hold == "" {
		t.Errorf("item %+v", it)
	}
	var ran bool
	for _, c := range f.runner.Calls() {
		if c.Name == gitBin {
			ran = true
			if !slices.Contains(c.Unset, "GIT_DIR") || !slices.Contains(c.Env, "GIT_TERMINAL_PROMPT=0") {
				t.Errorf("git ran as %+v", c)
			}
		}
	}
	if !ran {
		t.Error("the located git did not run")
	}
}

// TestMayReadSkipsFolders: a folder the read gate refuses is neither read nor counted as denied.
func TestMayReadSkipsFolders(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ws := filepath.Join(f.home, "Workspace")
	docs := filepath.Join(f.home, "Documents")
	gitProjects(t, ws, "plain")
	writeFile(t, docs, "app/package.json", 10)
	writeFile(t, docs, "app/node_modules/x", 1000)
	var asked []string
	var mu sync.Mutex
	f.opts.MayRead = func(p string) bool {
		mu.Lock()
		asked = append(asked, p)
		mu.Unlock()
		return !strings.HasPrefix(p, docs)
	}
	f.cfg.DevProjectRoots = []string{ws, docs}
	res := f.scan(t)
	nm := categoryByID(res, "artifact-node-modules")
	if nm == nil || len(nm.Items) != 1 || filepath.Base(nm.Items[0].Project) != "plain" || res.Denied != 0 {
		t.Fatalf("result %+v", res)
	}
	if !slices.Contains(asked, docs) || !slices.Contains(asked, ws) || !slices.Contains(asked, filepath.Join(ws, "plain")) {
		t.Errorf("asked %v", asked)
	}
	for _, p := range asked {
		if strings.HasPrefix(p, filepath.Join(docs, "app")) {
			t.Errorf("asked about %s inside a refused root", p)
		}
	}
}

func TestLocateGit(t *testing.T) {
	home := t.TempDir()
	e := env{goos: "darwin", home: home, getenv: envOf(nil)}
	// Homebrew's git wins; xcode-select is not asked.
	f := execxtest.New()
	l := locate(context.Background(), e, f, lookPathOf(map[string]string{"/opt/homebrew/bin/git": "/opt/homebrew/bin/git", "git": "/usr/bin/git"}), "", true)
	if l.bins["git"] != "/opt/homebrew/bin/git" || len(f.Calls()) != 0 {
		t.Errorf("git = %q, calls %v", l.bins["git"], f.Calls())
	}
	// Only the /usr/bin stub: the selected developer folder's git.
	dev := filepath.Join(home, "Xcode.app", "Contents", "Developer")
	gitBin := writeFile(t, dev, "usr/bin/git", 1)
	f = execxtest.New()
	f.On(xcodeSelectPath, "-p").Return(dev+"\n", 0)
	if l := locate(context.Background(), e, f, lookPathOf(map[string]string{"git": "/usr/bin/git"}), "", true); l.bins["git"] != gitBin {
		t.Errorf("git = %q, want %q", l.bins["git"], gitBin)
	}
	// No developer tools at all: no git, and never the stub.
	f = execxtest.New()
	f.On(xcodeSelectPath, "-p").Return("", 2)
	if l := locate(context.Background(), e, f, lookPathOf(map[string]string{"git": "/usr/bin/git"}), "", true); l.bins["git"] != "" {
		t.Errorf("git = %q without developer tools", l.bins["git"])
	}
	// Not wanted: not looked for.
	f = execxtest.New()
	if l := locate(context.Background(), e, f, lookPathOf(map[string]string{"/opt/homebrew/bin/git": "/opt/homebrew/bin/git"}), "", false); l.bins["git"] != "" || len(f.Calls()) != 0 {
		t.Errorf("git looked for when not wanted: %q %v", l.bins["git"], f.Calls())
	}
	// Windows: PATH first, then Git for Windows in Program Files.
	w := env{goos: "windows", home: `C:\Users\u`, getenv: envOf(map[string]string{"ProgramFiles": `C:\Program Files`})}
	pf := filepath.Join(`C:\Program Files`, "Git", "cmd", "git.exe")
	if l := locate(context.Background(), w, execxtest.New(), lookPathOf(map[string]string{pf: pf}), "", true); l.bins["git"] != pf {
		t.Errorf("windows git = %q", l.bins["git"])
	}
	if l := locate(context.Background(), w, execxtest.New(), lookPathOf(map[string]string{"git": `C:\Tools\git.exe`, pf: pf}), "", true); l.bins["git"] != `C:\Tools\git.exe` {
		t.Errorf("windows git on PATH = %q", l.bins["git"])
	}
}

func TestGeneratedRules(t *testing.T) {
	want := map[string]bool{
		"node_modules": true, ".next": true, ".nuxt": true, ".svelte-kit": true, ".turbo": true, ".parcel-cache": true, ".angular": true, ".expo": true,
		".gradle": true, "DerivedData": true, ".build": true, "__pycache__": true, ".pytest_cache": true, ".mypy_cache": true, ".ruff_cache": true,
		".tox": true, "zig-cache": true, ".zig-cache": true, ".dart_tool": true, "bin": false, "obj": false, "Pods": false,
	}
	for _, r := range artifactRules {
		switch r.name {
		case "target":
			if !r.generated {
				t.Errorf("target of %s is not generated", r.kind)
			}
		case "build":
			if r.generated {
				t.Errorf("build of %s is generated: people keep files there", r.kind)
			}
		default:
			if g, ok := want[r.name]; !ok || g != r.generated {
				t.Errorf("%s generated = %v", r.name, r.generated)
			}
		}
	}
}

func TestToolIDs(t *testing.T) {
	ids := ToolIDs()
	for _, id := range []string{"xcode-derived-data", "xcode-device-support", "xcode-simulators", "homebrew", "npm", "pnpm", "go-build", "pip", "nuget-cache", "nuget-packages", "jetbrains", "vscode"} {
		if !slices.Contains(ids, id) {
			t.Errorf("ToolIDs lacks %s", id)
		}
	}
	seen := map[string]bool{}
	for _, id := range ids {
		if seen[id] {
			t.Errorf("%s twice", id)
		}
		seen[id] = true
	}
	// Every tool category a scan makes is one of them.
	f := newFixture(t, "darwin", nil)
	writeFile(t, f.home, "Library/Developer/Xcode/DerivedData/App/x", 1000)
	writeFile(t, f.home, ".npm/_cacache/x", 1000)
	for _, c := range f.scan(t).Categories {
		if id, ok := strings.CutPrefix(c.ID, "tool-"); !ok || !seen[id] {
			t.Errorf("category %s is no tool of ToolIDs", c.ID)
		}
	}
}

func TestGroupProjects(t *testing.T) {
	now := time.Now()
	item := func(project, path, note string, size int64, mod time.Time, git cleanup.GitState, hold string) cleanup.Item {
		return cleanup.Item{ID: cleanup.ItemID(path), Path: path, Size: size, ModTime: mod, Note: note, Project: project, Git: git, Hold: hold}
	}
	res := cleanup.ScanResult{Categories: []cleanup.Category{
		{ID: "dev-junk:tool-npm", Items: []cleanup.Item{{ID: "npm", Path: "/u/.npm", Size: 1 << 30}}},
		{ID: "dev-junk:artifact-node-modules", Items: []cleanup.Item{
			item("/ws/web", "/ws/web/node_modules", "web · 最近 30 天内有改动", 500, now.Add(-2*time.Hour), cleanup.GitClean, ""),
			item("/ws/apps/api", "/ws/apps/api/node_modules", "apps/api · 有未提交的改动", 300, now.Add(-48*time.Hour), cleanup.GitDirty, "项目有未提交的改动"),
		}},
		{ID: "dev-junk:artifact-web-build", Items: []cleanup.Item{
			item("/ws/web", "/ws/web/.next", "web · 最近 30 天内有改动", 700, now.Add(-2*time.Hour), cleanup.GitClean, ""),
			item("/ws/web", "/ws/web/.angular/cache", "web · 最近 30 天内有改动", 10, now.Add(-2*time.Hour), cleanup.GitClean, ""),
		}},
		{ID: "dev-junk:artifact-rust-target-trash", Items: []cleanup.Item{
			item("/docs/r", "/docs/r/target", "", 300, now.Add(-100*24*time.Hour), cleanup.GitNone, ""),
		}},
	}}
	got := GroupProjects(res)
	if len(got) != 3 {
		t.Fatalf("projects %+v", got)
	}
	web, docs, api := got[0], got[1], got[2]
	if web.Path != "/ws/web" || web.ID != cleanup.ItemID("/ws/web") || web.Label != "web" || web.Size != 1210 || web.Git != cleanup.GitClean || web.Hold != "" {
		t.Errorf("web %+v", web)
	}
	if !slices.Equal(web.Kinds, []string{".next", "node_modules", ".angular/cache"}) {
		t.Errorf("kinds %v", web.Kinds)
	}
	if len(web.Items) != 3 || web.Items[0] != (ProjectItem{CategoryID: "dev-junk:artifact-node-modules", ItemID: cleanup.ItemID("/ws/web/node_modules")}) {
		t.Errorf("items %+v", web.Items)
	}
	if !web.LastActive.Equal(now.Add(-2 * time.Hour)) {
		t.Errorf("last active %v", web.LastActive)
	}
	// Equal sizes sort by path; a missing note falls back to the folder name.
	if docs.Path != "/docs/r" || api.Path != "/ws/apps/api" {
		t.Errorf("order %s, %s", docs.Path, api.Path)
	}
	if docs.Label != "r" || docs.Git != cleanup.GitNone || api.Label != "apps/api" || api.Hold != "项目有未提交的改动" || api.Git != cleanup.GitDirty {
		t.Errorf("projects %+v %+v", docs, api)
	}
	if len(GroupProjects(cleanup.ScanResult{})) != 0 {
		t.Error("projects of an empty scan")
	}
}

// TestGroupProjectsOfAScan: the projects of a real scan carry the label the scan gave them.
func TestGroupProjectsOfAScan(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ws := filepath.Join(f.home, "Workspace")
	gitProjects(t, ws, "plain")
	writeFile(t, ws, "group/sub/package.json", 10)
	writeFile(t, ws, "group/sub/node_modules/x", 9000)
	f.cfg.DevProjectRoots = []string{ws}
	got := GroupProjects(f.scan(t))
	if len(got) != 2 || got[0].Label != filepath.Join("group", "sub") || got[1].Label != "plain" || got[1].Git != cleanup.GitNone {
		t.Errorf("projects %+v", got)
	}
}

// TestSymlinkedRootFindsTheRepository: a project root that is a symlink into the middle of a repository still finds that repository's .git, so a dirty project behind the link is held instead of being taken as in no repository, and git is asked about the real paths.
func TestSymlinkedRootFindsTheRepository(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	mono := filepath.Join(f.home, "repos", "mono")
	if err := os.MkdirAll(filepath.Join(mono, ".git"), 0o755); err != nil {
		t.Fatal(err)
	}
	writeFile(t, mono, "packages/app/package.json", 10)
	writeFile(t, mono, "packages/app/node_modules/dep/index.js", 4000)
	age(t, filepath.Join(mono, "packages", "app"), 90*24*time.Hour)
	link := filepath.Join(f.home, "pkgs")
	if err := os.Symlink(filepath.Join(mono, "packages"), link); err != nil {
		t.Fatal(err)
	}
	g := newFakeGit()
	g.set("app", gitstate.Result{State: cleanup.GitDirty}, nil)
	f.opts.Git = g.check
	f.cfg.DevProjectRoots = []string{link}
	res := f.scan(t)
	it := itemOf(t, res, "artifact-node-modules", "app")
	if it.Git != cleanup.GitDirty || it.Hold != "项目有未提交的改动" || it.Selected {
		t.Errorf("project behind a symlinked root: git %q hold %q selected %v", it.Git, it.Hold, it.Selected)
	}
	if it.Project != filepath.Join(link, "app") {
		t.Errorf("project %q, want the path as configured", it.Project)
	}
	realMono := realPath(t, mono)
	if g.repos["app"] != realMono {
		t.Errorf("repo %q, want %q", g.repos["app"], realMono)
	}
	if arts := g.arts["app"]; len(arts) != 1 || arts[0].Path != filepath.Join(realMono, "packages", "app", "node_modules") {
		t.Errorf("artifacts %+v", arts)
	}
	// A project folder that cannot be resolved is in doubt, never "not a repository".
	if got := askGit(context.Background(), g.check, filepath.Join(link, "gone"), nil, slog.New(slog.DiscardHandler)); got.State != cleanup.GitUnknown {
		t.Errorf("unresolvable project: %+v", got)
	}
}

// TestCleanRechecksAfterARescan: a scan that finishes during a clean and no longer lists a build output folder (here refused by the read gate) does not make the clean skip that folder's git re-check.
func TestCleanRechecksAfterARescan(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ws := filepath.Join(f.home, "Workspace")
	gitProjects(t, ws, "web")
	var hide atomic.Bool
	f.opts.MayRead = func(p string) bool { return !hide.Load() || p != filepath.Join(ws, "web") }
	g := newFakeGit()
	g.set("web", gitstate.Result{State: cleanup.GitClean}, nil)
	f.opts.Git = g.check
	f.cfg.DevProjectRoots = []string{ws}
	res := f.scan(t)
	it := itemOf(t, res, "artifact-node-modules", "web")
	c := f.cleaner
	var once sync.Once
	out, err := c.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{"artifact-node-modules"}, ItemIDs: []string{it.ID}}, func(cleanup.Progress) {
		once.Do(func() {
			hide.Store(true)
			again, err := c.Scan(context.Background(), nil)
			if err != nil {
				t.Errorf("scan during the clean: %v", err)
			}
			if categoryByID(again, "artifact-node-modules") != nil {
				t.Errorf("the second scan still lists the build output")
			}
			g.set("web", gitstate.Result{State: cleanup.GitDirty}, nil)
		})
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(out.Failed) != 1 || out.Failed[0].Reason != "项目有未提交的改动，已跳过" || !exists(it.Path) {
		t.Errorf("clean after a rescan: %+v, exists %v", out, exists(it.Path))
	}
}
