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
}

var artifactRules = []artifactRule{
	{name: "node_modules", markers: markPackageJSON, kind: "node-modules"},
	{name: ".next", markers: markPackageJSON, kind: "web-build"},
	{name: ".nuxt", markers: markPackageJSON, kind: "web-build"},
	{name: ".svelte-kit", markers: markPackageJSON, kind: "web-build"},
	{name: ".turbo", markers: markPackageJSON, kind: "web-build"},
	{name: ".parcel-cache", markers: markPackageJSON, kind: "web-build"},
	{name: ".angular", sub: "cache", markers: markPackageJSON, kind: "web-build"},
	{name: ".expo", markers: markPackageJSON, kind: "web-build"},
	{name: "target", markers: markCargo, kind: "rust-target"},
	{name: "target", markers: markPom, kind: "maven-target"},
	{name: "build", markers: markGradle, kind: "gradle"},
	{name: ".gradle", markers: markGradle, kind: "gradle"},
	{name: "Pods", markers: markPodfile, kind: "cocoapods"},
	{name: "DerivedData", markers: markXcodeProject, kind: "xcode-project"},
	{name: ".build", markers: markPackageSwift, kind: "swiftpm"},
	{name: "__pycache__", markers: markPython, kind: "python", inside: true},
	{name: ".pytest_cache", markers: markPython, kind: "python", inside: true},
	{name: ".mypy_cache", markers: markPython, kind: "python", inside: true},
	{name: ".ruff_cache", markers: markPython, kind: "python", inside: true},
	{name: ".tox", markers: markPython, kind: "python"},
	{name: "bin", markers: markCsproj, kind: "dotnet"},
	{name: "obj", markers: markCsproj, kind: "dotnet"},
	{name: "zig-cache", markers: markZig, kind: "zig"},
	{name: ".zig-cache", markers: markZig, kind: "zig"},
	{name: ".dart_tool", markers: markPubspec, kind: "dart"},
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
}

// projectWalk finds build output under one project root.
type projectWalk struct {
	ctx        context.Context
	root       string
	exclusions []string
	excluded   func(path string, exclusions []string) bool
	onDir      func(path string)

	sem chan struct{}
	wg  sync.WaitGroup

	mu       sync.Mutex
	found    []artifact
	denied   int64
	activity map[string]time.Time
	firstErr error
}

// walkProjects walks root and returns the build output it found and how many folders could not be read for lack of permission. onDir is called with each folder read (from several goroutines).
func walkProjects(ctx context.Context, root string, exclusions []string, excluded func(string, []string) bool, onDir func(string)) ([]artifact, int64, error) {
	w := &projectWalk{
		ctx: ctx, root: root, exclusions: exclusions, excluded: excluded, onDir: onDir,
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
	w.found = append(w.found, artifact{path: path, kind: rule.kind, project: project, root: w.root, info: info, active: active})
	w.mu.Unlock()
}

// lastActivity is the newest modification time of project's top-level files and of .git/HEAD and .git/index, computed once per project. entries are the project folder's entries when the caller has them.
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

// projectActivity reads the times lastActivity combines; entries nil reads the folder.
func projectActivity(project string, entries []fs.DirEntry) time.Time {
	if entries == nil {
		entries, _ = os.ReadDir(project)
	}
	var newest time.Time
	for _, de := range entries {
		if !de.Type().IsRegular() {
			continue
		}
		if info, err := de.Info(); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	for _, name := range []string{"HEAD", "index"} {
		if info, err := os.Lstat(filepath.Join(project, ".git", name)); err == nil && info.ModTime().After(newest) {
			newest = info.ModTime()
		}
	}
	return newest
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
