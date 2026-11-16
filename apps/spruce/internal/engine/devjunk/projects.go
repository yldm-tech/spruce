package devjunk

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"spruce/apps/spruce/internal/engine/gitstate"
)

// maxProjectDepth is how deep below a project root the walk reads folders; build output is found in the folders it reads.
const maxProjectDepth = 6

// walkWorkers bounds how many folders the project walk reads at once.
const walkWorkers = 8

// marker is a bit set of the project files a folder holds.
type marker uint16

const (
	markPackageJSON marker = 1 << iota
	markCargo
	markPom
	markGradle
	markPodfile
	markPackageSwift
	markPython
	markCsproj
	markZig
	markPubspec
	markXcodeProject
)

// markerOf returns the marker an entry of a folder sets.
func markerOf(name string, dir bool) marker {
	if dir {
		if strings.HasSuffix(name, ".xcodeproj") || strings.HasSuffix(name, ".xcworkspace") {
			return markXcodeProject
		}
		return 0
	}
	switch name {
	case "package.json":
		return markPackageJSON
	case "Cargo.toml":
		return markCargo
	case "pom.xml":
		return markPom
	case "build.gradle", "build.gradle.kts", "settings.gradle", "settings.gradle.kts":
		return markGradle
	case "Podfile":
		return markPodfile
	case "Package.swift":
		return markPackageSwift
	case "pyproject.toml", "setup.py", "requirements.txt":
		return markPython
	case "build.zig":
		return markZig
	case "pubspec.yaml":
		return markPubspec
	}
	if strings.HasSuffix(name, ".csproj") {
		return markCsproj
	}
	return 0
}

// artifactKind is one category of build output.
type artifactKind struct {
	id          string
	title       string
	description string
	icon        string
}

// The artifact kinds, in the order the page shows them.
var artifactKinds = []artifactKind{
	{"node-modules", "node_modules", "安装的 npm 依赖，在项目里重新安装（npm install）即可恢复", "node"},
	{"web-build", "前端构建缓存", ".next、.nuxt、.svelte-kit、.turbo、.parcel-cache、.angular/cache、.expo，下次构建时重新生成", "build"},
	{"rust-target", "Rust target", "cargo 的编译输出，下次编译时重新生成", "build"},
	{"maven-target", "Maven target", "Maven 的构建输出，下次构建时重新生成", "build"},
	{"gradle", "Gradle 构建输出", "项目里的 build 和 .gradle 文件夹，下次构建时重新生成", "build"},
	{"cocoapods", "CocoaPods Pods", "pod install 安装的依赖，重新运行 pod install 即可恢复", "package"},
	{"xcode-project", "项目内的 DerivedData", "项目文件夹里的 Xcode 编译输出，下次编译时重新生成", "xcode"},
	{"swiftpm", "SwiftPM .build", "Swift 包的编译输出，下次编译时重新生成", "build"},
	{"python", "Python 缓存", "__pycache__、.pytest_cache、.mypy_cache、.ruff_cache、.tox，运行时重新生成；不包括虚拟环境", "package"},
	{"dotnet", ".NET bin 和 obj", ".NET 项目的编译输出，下次编译时重新生成", "build"},
	{"zig", "Zig 缓存", "zig-cache 和 .zig-cache，下次编译时重新生成", "build"},
	{"dart", "Dart 和 Flutter 构建输出", ".dart_tool 和 build，下次构建时重新生成", "build"},
}

// artifactRule says that a folder named name, next to a project with one of the markers, is build output of kind. sub, when set, is the folder inside it that is the artifact (.angular/cache).
type artifactRule struct {
	name    string
	sub     string
	markers marker
	kind    string
	// inside also matches anywhere inside such a project, not only next to its marker (Python caches sit in every package folder).
	inside bool
	// generated marks folder names that only ever hold generated content, so untracked files inside them do not make the project dirty (gitstate.Artifact.Generated). bin and obj (.NET), build (Gradle, Dart) and Pods are not: people keep their own files there.
	generated bool
}

var artifactRules = []artifactRule{
	{name: "node_modules", markers: markPackageJSON, kind: "node-modules", generated: true},
	{name: ".next", markers: markPackageJSON, kind: "web-build", generated: true},
	{name: ".nuxt", markers: markPackageJSON, kind: "web-build", generated: true},
	{name: ".svelte-kit", markers: markPackageJSON, kind: "web-build", generated: true},
	{name: ".turbo", markers: markPackageJSON, kind: "web-build", generated: true},
	{name: ".parcel-cache", markers: markPackageJSON, kind: "web-build", generated: true},
	{name: ".angular", sub: "cache", markers: markPackageJSON, kind: "web-build", generated: true},
	{name: ".expo", markers: markPackageJSON, kind: "web-build", generated: true},
	{name: "target", markers: markCargo, kind: "rust-target", generated: true},
	{name: "target", markers: markPom, kind: "maven-target", generated: true},
	{name: "build", markers: markGradle, kind: "gradle"},
	{name: ".gradle", markers: markGradle, kind: "gradle", generated: true},
	{name: "Pods", markers: markPodfile, kind: "cocoapods"},
	{name: "DerivedData", markers: markXcodeProject, kind: "xcode-project", generated: true},
	{name: ".build", markers: markPackageSwift, kind: "swiftpm", generated: true},
	{name: "__pycache__", markers: markPython, kind: "python", inside: true, generated: true},
	{name: ".pytest_cache", markers: markPython, kind: "python", inside: true, generated: true},
	{name: ".mypy_cache", markers: markPython, kind: "python", inside: true, generated: true},
	{name: ".ruff_cache", markers: markPython, kind: "python", inside: true, generated: true},
	{name: ".tox", markers: markPython, kind: "python", generated: true},
	{name: "bin", markers: markCsproj, kind: "dotnet"},
	{name: "obj", markers: markCsproj, kind: "dotnet"},
	{name: "zig-cache", markers: markZig, kind: "zig", generated: true},
	{name: ".zig-cache", markers: markZig, kind: "zig", generated: true},
	{name: ".dart_tool", markers: markPubspec, kind: "dart", generated: true},
	{name: "build", markers: markPubspec, kind: "dart"},
}

// neverDescend are folders the walk does not enter even when they are no build output here: dependency trees and virtual environments, which hold other projects' files, not the user's.
var neverDescend = map[string]bool{"node_modules": true, "venv": true, ".venv": true}

// artifact is one build output folder the walk found.
type artifact struct {
	path string
	kind string
	// project is the project folder it belongs to; root the project root it was found under.
	project string
	root    string
	info    fs.FileInfo
	// active is the project's last activity.
	active time.Time
	// generated is the rule's generated flag.
	generated bool
}

// projectWalk finds build output under one project root.
type projectWalk struct {
	ctx        context.Context
	root       string
	exclusions []string
	excluded   func(path string, exclusions []string) bool
	mayRead    func(path string) bool
	onDir      func(path string)

	sem chan struct{}
	wg  sync.WaitGroup

	mu       sync.Mutex
	found    []artifact
	denied   int64
	activity map[string]time.Time
	firstErr error
}

// walkProjects walks root and returns the build output it found and how many folders could not be read for lack of permission. mayRead, when not nil, is asked before each folder is read, the root included; a folder it refuses is skipped silently. onDir is called with each folder read (from several goroutines).
func walkProjects(ctx context.Context, root string, exclusions []string, excluded func(string, []string) bool, mayRead func(string) bool, onDir func(string)) ([]artifact, int64, error) {
	w := &projectWalk{
		ctx: ctx, root: root, exclusions: exclusions, excluded: excluded, mayRead: mayRead, onDir: onDir,
		sem: make(chan struct{}, walkWorkers), activity: map[string]time.Time{},
	}
	w.visit(root, 0, "")
	w.wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, w.denied, err
	}
	return w.found, w.denied, w.firstErr
}

// visit reads dir (depth below the root), records the build output next to its markers and walks the other sub-folders. pyProject is the nearest Python project folder above dir, or "".
func (w *projectWalk) visit(dir string, depth int, pyProject string) {
	if w.ctx.Err() != nil {
		return
	}
	if w.mayRead != nil && !w.mayRead(dir) {
		return
	}
	des, err := os.ReadDir(dir)
	if err != nil {
		w.mu.Lock()
		if errors.Is(err, fs.ErrPermission) {
			w.denied++
		} else if depth == 0 && !errors.Is(err, fs.ErrNotExist) {
			w.firstErr = err
		}
		w.mu.Unlock()
		return
	}
	if w.onDir != nil {
		w.onDir(dir)
	}
	var marks marker
	for _, de := range des {
		marks |= markerOf(de.Name(), de.IsDir())
	}
	if marks&markPython != 0 {
		pyProject = dir
	}
	for _, de := range des {
		// Only real folders: a symlink (or a Windows junction, which ReadDir reports as irregular) is neither followed nor offered.
		if de.Type() != fs.ModeDir {
			continue
		}
		name := de.Name()
		path := filepath.Join(dir, name)
		if w.excluded(path, w.exclusions) {
			continue
		}
		if rule, ok := matchRule(name, marks, pyProject != ""); ok {
			project := dir
			if marks&rule.markers == 0 {
				project = pyProject
			}
			w.record(rule, path, project, des)
			continue
		}
		if strings.HasPrefix(name, ".") || neverDescend[name] || depth >= maxProjectDepth {
			continue
		}
		w.spawn(path, depth+1, pyProject)
	}
}

// matchRule finds the rule for a sub-folder name of a folder with marks; inPython is set inside a Python project.
func matchRule(name string, marks marker, inPython bool) (artifactRule, bool) {
	for _, r := range artifactRules {
		if r.name != name {
			continue
		}
		if marks&r.markers != 0 || (r.inside && inPython) {
			return r, true
		}
	}
	return artifactRule{}, false
}

// record adds the artifact at path (or its sub-folder) of project. siblings are the entries of path's folder when it is the project folder.
func (w *projectWalk) record(rule artifactRule, path, project string, siblings []fs.DirEntry) {
	if filepath.Dir(path) != project {
		siblings = nil
	}
	if rule.sub != "" {
		path = filepath.Join(path, rule.sub)
		if w.excluded(path, w.exclusions) {
			return
		}
	}
	info, err := os.Lstat(path)
	if err != nil || !info.IsDir() {
		return
	}
	active := w.lastActivity(project, siblings)
	w.mu.Lock()
	w.found = append(w.found, artifact{path: path, kind: rule.kind, project: project, root: w.root, info: info, active: active, generated: rule.generated})
	w.mu.Unlock()
}

// lastActivity is the project's last activity (projectActivity), computed once per project. entries are the project folder's entries when the caller has them.
func (w *projectWalk) lastActivity(project string, entries []fs.DirEntry) time.Time {
	w.mu.Lock()
	t, ok := w.activity[project]
	w.mu.Unlock()
	if ok {
		return t
	}
	t = projectActivity(project, entries)
	w.mu.Lock()
	w.activity[project] = t
	w.mu.Unlock()
	return t
}

// maxActivityFolders bounds how many top-level folders of a project outside a repository projectActivity reads.
const maxActivityFolders = 64

// projectActivity is the newest modification time of what shows that someone works on project, without running git: its top-level files, and in the repository holding it (the project itself, a monorepo above it, or a worktree whose .git file names its git folder) the git folder's index, HEAD and logs/HEAD, which every commit, checkout and staged change touches; outside a repository, the files and folders one level below its top-level folders instead, build output and dependency folders left out. Any activity in the repository keeps all its projects active, which errs on the safe side. entries nil reads the folder.
func projectActivity(project string, entries []fs.DirEntry) time.Time {
	if entries == nil {
		entries, _ = os.ReadDir(project)
	}
	var newest time.Time
	see := func(t time.Time) {
		if t.After(newest) {
			newest = t
		}
	}
	for _, de := range entries {
		if !de.Type().IsRegular() {
			continue
		}
		if info, err := de.Info(); err == nil {
			see(info.ModTime())
		}
	}
	if repo, ok := gitstate.RepoOf(project); ok {
		if dir, ok := gitDir(repo); ok {
			for _, name := range []string{"HEAD", "index", filepath.Join("logs", "HEAD")} {
				if info, err := os.Lstat(filepath.Join(dir, name)); err == nil {
					see(info.ModTime())
				}
			}
		}
		return newest
	}
	read := 0
	for _, de := range entries {
		if !de.IsDir() || skipActivity(de.Name()) {
			continue
		}
		if read == maxActivityFolders {
			break
		}
		read++
		dir := filepath.Join(project, de.Name())
		if info, err := os.Lstat(dir); err == nil {
			see(info.ModTime())
		}
		inner, _ := os.ReadDir(dir)
		for _, e := range inner {
			if (!e.Type().IsRegular() && !e.IsDir()) || (e.IsDir() && skipActivity(e.Name())) {
				continue
			}
			if info, err := e.Info(); err == nil {
				see(info.ModTime())
			}
		}
	}
	return newest
}

// skipActivity reports the folder names whose times say nothing about the user's work: build output, dependency trees and .git.
func skipActivity(name string) bool {
	if name == ".git" || neverDescend[name] {
		return true
	}
	for _, r := range artifactRules {
		if r.name == name {
			return true
		}
	}
	return false
}

// maxGitFile bounds what gitDir reads of a .git file, which holds one short line.
const maxGitFile = 4 << 10

// gitDir returns the git folder of the repository at repo: repo/.git when it is a folder, or the folder the "gitdir:" line of a .git file names (a worktree's <main>/.git/worktrees/<name>, a submodule's folder), relative to repo when it is not absolute.
func gitDir(repo string) (string, bool) {
	dotgit := filepath.Join(repo, ".git")
	info, err := os.Lstat(dotgit)
	switch {
	case err != nil:
		return "", false
	case info.IsDir():
		return dotgit, true
	case !info.Mode().IsRegular() || info.Size() > maxGitFile:
		return "", false
	}
	b, err := os.ReadFile(dotgit)
	if err != nil {
		return "", false
	}
	line, _, _ := strings.Cut(string(b), "\n")
	dir, ok := strings.CutPrefix(strings.TrimSpace(line), "gitdir:")
	dir = strings.TrimSpace(dir)
	if !ok || dir == "" {
		return "", false
	}
	if !filepath.IsAbs(dir) {
		dir = filepath.Join(repo, dir)
	}
	return filepath.Clean(dir), true
}

// spawn walks dir on a spare worker, or inline when every worker is busy, so goroutines stay bounded without a queue.
func (w *projectWalk) spawn(dir string, depth int, pyProject string) {
	select {
	case w.sem <- struct{}{}:
		w.wg.Go(func() {
			defer func() { <-w.sem }()
			w.visit(dir, depth, pyProject)
		})
	default:
		w.visit(dir, depth, pyProject)
	}
}
