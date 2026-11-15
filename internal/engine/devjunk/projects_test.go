package devjunk

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/fsx"
)

// projectTree builds a tree of projects under a new root and returns it.
func projectTree(t *testing.T) string {
	t.Helper()
	root := t.TempDir()
	for _, f := range []string{
		"web/package.json", "web/node_modules/x/package.json", "web/node_modules/x/node_modules/y/index.js",
		"web/.next/cache/f", "web/.angular/cache/f", "web/.turbo/f", "web/src/build/keep.js",
		"rust/Cargo.toml", "rust/target/debug/f",
		"notrust/target/f",
		"java/pom.xml", "java/target/x.jar",
		"android/settings.gradle", "android/build/x", "android/.gradle/x", "android/app/build.gradle", "android/app/build/y",
		"ios/Podfile", "ios/Pods/x", "ios/App.xcodeproj/project.pbxproj", "ios/DerivedData/x",
		"swift/Package.swift", "swift/.build/x",
		"py/pyproject.toml", "py/pkg/sub/__pycache__/m.pyc", "py/.pytest_cache/x", "py/.tox/x", "py/.venv/lib/__pycache__/v.pyc", "py/venv/lib/__pycache__/v.pyc",
		"loose/__pycache__/m.pyc",
		"dotnet/App.csproj", "dotnet/bin/x.dll", "dotnet/obj/x",
		"zig/build.zig", "zig/zig-cache/x", "zig/.zig-cache/x",
		"flutter/pubspec.yaml", "flutter/.dart_tool/x", "flutter/build/x",
		"a/b/c/d/e/f/package.json", "a/b/c/d/e/f/node_modules/x",
		"a/b/c/d/e/f/g/package.json", "a/b/c/d/e/f/g/node_modules/x",
		".hidden/proj/package.json", ".hidden/proj/node_modules/x",
		"excluded/package.json", "excluded/node_modules/x",
		"ignored-one/package.json", "ignored-one/node_modules/x",
		"linked/package.json", "elsewhere/node_modules/x",
	} {
		writeFile(t, root, f, 10)
	}
	if err := os.Symlink(filepath.Join(root, "elsewhere", "node_modules"), filepath.Join(root, "linked", "node_modules")); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(filepath.Join(root, "web"), filepath.Join(root, "web-link")); err != nil {
		t.Fatal(err)
	}
	return root
}

func TestWalkProjectsFindsArtifactsNextToMarkers(t *testing.T) {
	root := projectTree(t)
	found, denied, err := walkProjects(context.Background(), root, []string{filepath.Join(root, "excluded"), "ignored-*"}, fsx.Excluded, nil, nil)
	if err != nil || denied != 0 {
		t.Fatalf("walk = %v, denied %d", err, denied)
	}
	got := map[string]artifact{}
	for _, a := range found {
		rel, _ := filepath.Rel(root, a.path)
		if _, dup := got[rel]; dup {
			t.Errorf("%s found twice", rel)
		}
		got[rel] = a
	}
	want := map[string]struct{ kind, project string }{
		"web/node_modules":         {"node-modules", "web"},
		"web/.next":                {"web-build", "web"},
		"web/.angular/cache":       {"web-build", "web"},
		"web/.turbo":               {"web-build", "web"},
		"rust/target":              {"rust-target", "rust"},
		"java/target":              {"maven-target", "java"},
		"android/build":            {"gradle", "android"},
		"android/.gradle":          {"gradle", "android"},
		"android/app/build":        {"gradle", "android/app"},
		"ios/Pods":                 {"cocoapods", "ios"},
		"ios/DerivedData":          {"xcode-project", "ios"},
		"swift/.build":             {"swiftpm", "swift"},
		"py/pkg/sub/__pycache__":   {"python", "py"},
		"py/.pytest_cache":         {"python", "py"},
		"py/.tox":                  {"python", "py"},
		"dotnet/bin":               {"dotnet", "dotnet"},
		"dotnet/obj":               {"dotnet", "dotnet"},
		"zig/zig-cache":            {"zig", "zig"},
		"zig/.zig-cache":           {"zig", "zig"},
		"flutter/.dart_tool":       {"dart", "flutter"},
		"flutter/build":            {"dart", "flutter"},
		"a/b/c/d/e/f/node_modules": {"node-modules", "a/b/c/d/e/f"},
	}
	for rel, w := range want {
		a, ok := got[filepath.FromSlash(rel)]
		if !ok {
			t.Errorf("%s not found", rel)
			continue
		}
		if a.kind != w.kind || a.project != filepath.Join(root, filepath.FromSlash(w.project)) || a.root != root {
			t.Errorf("%s = kind %s project %s root %s, want %s in %s", rel, a.kind, a.project, a.root, w.kind, w.project)
		}
	}
	for rel := range got {
		if _, ok := want[filepath.ToSlash(rel)]; !ok {
			t.Errorf("%s found but is no build output here (no marker, too deep, hidden, excluded, a symlink or inside a virtualenv)", rel)
		}
	}
}

func TestWalkProjectsHonoursCancel(t *testing.T) {
	root := projectTree(t)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, _, err := walkProjects(ctx, root, nil, fsx.Excluded, nil, nil); err == nil {
		t.Error("a cancelled walk returned no error")
	}
}

func TestWalkProjectsMissingRoot(t *testing.T) {
	found, _, err := walkProjects(context.Background(), filepath.Join(t.TempDir(), "missing"), nil, fsx.Excluded, nil, nil)
	if err != nil || len(found) != 0 {
		t.Errorf("missing root = %v, %v", found, err)
	}
}

func TestProjectActivity(t *testing.T) {
	proj := t.TempDir()
	old := time.Now().Add(-100 * 24 * time.Hour)
	recent := time.Now().Add(-2 * 24 * time.Hour)
	for _, f := range []string{"package.json", "README.md", ".git/HEAD", ".git/index", "node_modules/x", "src/deep.js"} {
		p := writeFile(t, proj, f, 1)
		if err := os.Chtimes(p, old, old); err != nil {
			t.Fatal(err)
		}
	}
	if got := projectActivity(proj, nil); got.Sub(old).Abs() > time.Second {
		t.Errorf("activity = %v, want %v", got, old)
	}
	// A commit (.git/index) counts; a change deep inside the tree does not, since the walk does not read it.
	if err := os.Chtimes(filepath.Join(proj, ".git", "index"), recent, recent); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(filepath.Join(proj, "src", "deep.js"), time.Now(), time.Now()); err != nil {
		t.Fatal(err)
	}
	if got := projectActivity(proj, nil); got.Sub(recent).Abs() > time.Second {
		t.Errorf("activity = %v, want %v", got, recent)
	}
}

// ageAll sets every file and folder under dir to at.
func ageAll(t *testing.T, dir string, at time.Time) {
	t.Helper()
	err := filepath.WalkDir(dir, func(p string, _ fs.DirEntry, err error) error {
		if err != nil {
			return err
		}
		return os.Chtimes(p, at, at)
	})
	if err != nil {
		t.Fatal(err)
	}
}

// TestProjectActivityInsideARepository: a package of a monorepo and a git worktree have no .git folder of their own; the repository's git folder still tells when someone last committed or checked out.
func TestProjectActivityInsideARepository(t *testing.T) {
	old := time.Now().Add(-100 * 24 * time.Hour)
	recent := time.Now().Add(-2 * 24 * time.Hour)
	near := func(got, want time.Time) bool { return got.Sub(want).Abs() <= time.Second }

	mono, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	for _, f := range []string{"package.json", ".git/HEAD", ".git/index", ".git/logs/HEAD", "packages/web/package.json", "packages/web/node_modules/x", "packages/web/src/app.js"} {
		writeFile(t, mono, f, 1)
	}
	ageAll(t, mono, old)
	pkg := filepath.Join(mono, "packages", "web")
	if got := projectActivity(pkg, nil); !near(got, old) {
		t.Errorf("idle monorepo package: activity %v, want %v", got, old)
	}
	if err := os.Chtimes(filepath.Join(mono, ".git", "logs", "HEAD"), recent, recent); err != nil {
		t.Fatal(err)
	}
	if got := projectActivity(pkg, nil); !near(got, recent) {
		t.Errorf("monorepo package after a commit: activity %v, want %v", got, recent)
	}

	// A worktree's .git is a file naming its git folder inside the main repository.
	wt, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	gitdir := filepath.Join(mono, ".git", "worktrees", "feature")
	writeFile(t, gitdir, "HEAD", 1)
	writeFile(t, gitdir, "index", 1)
	writeFile(t, wt, "Cargo.toml", 1)
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+gitdir+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	ageAll(t, wt, old)
	ageAll(t, gitdir, old)
	if got := projectActivity(wt, nil); !near(got, old) {
		t.Errorf("idle worktree: activity %v, want %v", got, old)
	}
	if err := os.Chtimes(filepath.Join(gitdir, "index"), recent, recent); err != nil {
		t.Fatal(err)
	}
	if got := projectActivity(wt, nil); !near(got, recent) {
		t.Errorf("worktree after staging: activity %v, want %v", got, recent)
	}
	// A relative gitdir line is relative to the worktree.
	rel, err := filepath.Rel(wt, gitdir)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(wt, ".git"), []byte("gitdir: "+rel+"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if dir, ok := gitDir(wt); !ok || dir != gitdir {
		t.Errorf("relative gitdir %q %v, want %q", dir, ok, gitdir)
	}
}

// TestProjectActivityOutsideARepository: without git, an edit one level inside a top-level folder counts; build output and dependencies do not.
func TestProjectActivityOutsideARepository(t *testing.T) {
	proj := t.TempDir()
	old := time.Now().Add(-100 * 24 * time.Hour)
	recent := time.Now().Add(-2 * 24 * time.Hour)
	for _, f := range []string{"package.json", "src/app.js", "node_modules/x/index.js", ".next/cache"} {
		writeFile(t, proj, f, 1)
	}
	ageAll(t, proj, old)
	for _, p := range []string{filepath.Join(proj, "node_modules", "x"), filepath.Join(proj, ".next", "cache")} {
		if err := os.Chtimes(p, time.Now(), time.Now()); err != nil {
			t.Fatal(err)
		}
	}
	if got := projectActivity(proj, nil); got.Sub(old).Abs() > time.Second {
		t.Errorf("activity %v, want %v: build output counted", got, old)
	}
	if err := os.Chtimes(filepath.Join(proj, "src", "app.js"), recent, recent); err != nil {
		t.Fatal(err)
	}
	if got := projectActivity(proj, nil); got.Sub(recent).Abs() > time.Second {
		t.Errorf("activity %v, want %v after an edit in src", got, recent)
	}
}

func TestMatchRule(t *testing.T) {
	if r, ok := matchRule("target", markCargo|markPom, false); !ok || r.kind != "rust-target" {
		t.Errorf("target with Cargo.toml and pom.xml = %+v", r)
	}
	if _, ok := matchRule("build", markPackageJSON, false); ok {
		t.Error("build next to package.json alone is source, not output")
	}
	if _, ok := matchRule(".tox", 0, true); ok {
		t.Error(".tox only counts next to the Python marker")
	}
	if r, ok := matchRule("__pycache__", 0, true); !ok || r.kind != "python" {
		t.Error("__pycache__ inside a Python project not matched")
	}
	if markerOf("App.csproj", false) != markCsproj || markerOf("App.xcworkspace", true) != markXcodeProject || markerOf("App.xcodeproj", false) != 0 {
		t.Error("markerOf is wrong")
	}
}
