package gitstate

import (
	"bytes"
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/execx"
)

// DefaultTimeout bounds each git run when Checker.Timeout is zero.
const DefaultTimeout = 10 * time.Second

// ErrNoGit is what Check returns, with GitUnknown, when no git program was found.
var ErrNoGit = errors.New("gitstate: git was not found")

// unsetVars are the variables a git run must not inherit: a shell or a git hook may have set them to point at another repository, index or configuration.
var unsetVars = []string{
	"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES",
	"GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM",
	"GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_*", "GIT_CONFIG_VALUE_*", "GIT_CONFIG_PARAMETERS",
}

// Checker asks git about projects. The zero Timeout means DefaultTimeout.
type Checker struct {
	Runner execx.Runner
	// Git is the absolute path of the git program (devjunk's locate finds it); "" makes every answer GitUnknown.
	Git     string
	Timeout time.Duration
}

// Artifact is one build output folder of the project being checked.
type Artifact struct {
	Path string
	// Generated is set for folder names that only ever hold generated content (node_modules, .next, target of Cargo and Maven, .gradle, DerivedData, .build, the Python caches, .tox, zig-cache, .dart_tool, .angular/cache, .expo, .turbo, .parcel-cache, .nuxt, .svelte-kit); untracked entries inside them do not count. bin and obj (.NET), build (Gradle, Dart) and Pods are not Generated: people keep their own files there.
	Generated bool
}

// Result is what git says about one project.
type Result struct {
	State cleanup.GitState
	// Tracked is set when git ls-files lists a file inside one of the artifacts.
	Tracked bool
}

// RepoOf walks up from project to the root of its volume (the first ancestor whose device number differs, or "/") looking for a ".git" folder or file, with os.Lstat only; found false means not a repository, and git is not run. It does not stop at the configured project root, which may itself be inside a repository. A .git that cannot be looked at (anything but "does not exist") counts as found, so a doubt never makes a project "not a repository". The walk goes up the path as written, so project must have its symlinks resolved (filepath.EvalSymlinks): through a link into the middle of a repository the walk would never reach its .git.
func RepoOf(project string) (repo string, found bool) {
	p := filepath.Clean(project)
	var dev uint64
	var known bool
	if info, err := os.Lstat(p); err == nil {
		dev, known = device(info)
	}
	for {
		if _, err := os.Lstat(filepath.Join(p, ".git")); err == nil || !errors.Is(err, os.ErrNotExist) {
			return p, true
		}
		parent := filepath.Dir(p)
		if parent == p {
			return "", false
		}
		if known {
			if info, err := os.Lstat(parent); err == nil {
				if d, ok := device(info); ok && d != dev {
					return "", false
				}
			}
		}
		p = parent
	}
}

// Check runs git for project inside repo: once status limited to the project folder, and once ls-files over the artifacts. Once a .git was found, every failure is GitUnknown, never GitNone; the error says why, for the log. repo, project and the artifacts' paths must have their symlinks resolved, as git writes paths relative to the real top level. It reads the folders between the project and a generated artifact deeper than one level (wrappers).
func (c Checker) Check(ctx context.Context, repo, project string, artifacts []Artifact) (Result, error) {
	unknown := Result{State: cleanup.GitUnknown}
	if c.Git == "" || c.Runner == nil {
		return unknown, ErrNoGit
	}
	projRel, err := relSlash(repo, project)
	if err != nil {
		return unknown, err
	}
	var ignore []string
	for _, a := range artifacts {
		if !a.Generated {
			continue
		}
		rel, err := relSlash(repo, a.Path)
		if err != nil {
			return unknown, err
		}
		ignore = append(ignore, rel)
		// git shows a wholly untracked folder as that folder alone, so an untracked .angular holding only cache appears as ".angular/"; a folder between the project and the artifact that holds nothing but the way to it is generated content too.
		for _, dir := range wrappers(project, a.Path) {
			rel, err := relSlash(repo, dir)
			if err != nil {
				return unknown, err
			}
			ignore = append(ignore, rel)
		}
	}
	out, err := c.run(ctx, project, "status", "--porcelain=v1", "-z", "--untracked-files=normal", "--ignore-submodules=dirty", "--", ".")
	if err != nil {
		return unknown, err
	}
	count, whole := parseStatusZ(out, ignore, projRel)
	if whole {
		return unknown, nil
	}
	res := Result{State: cleanup.GitClean}
	if count > 0 {
		res.State = cleanup.GitDirty
	}
	if len(artifacts) == 0 {
		return res, nil
	}
	args := []string{"ls-files", "-z", "--"}
	for _, a := range artifacts {
		rel, err := filepath.Rel(project, a.Path)
		if err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			return unknown, errors.New("gitstate: build output outside its project: " + a.Path)
		}
		args = append(args, filepath.ToSlash(rel))
	}
	out, err = c.run(ctx, project, args...)
	if err != nil {
		return unknown, err
	}
	res.Tracked = len(bytes.Trim(out, "\x00\n")) > 0
	return res, nil
}

// run runs git in dir with args after the options every run takes.
func (c Checker) run(ctx context.Context, dir string, args ...string) ([]byte, error) {
	timeout := c.Timeout
	if timeout <= 0 {
		timeout = DefaultTimeout
	}
	cmd := execx.Command{
		Name:    c.Git,
		Args:    append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.quotepath=false", "-C", dir}, args...),
		Env:     []string{"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"},
		Unset:   unsetVars,
		Timeout: timeout,
	}
	out, err := c.Runner.Run(ctx, cmd)
	return out.Stdout, err
}

// wrappers are the folders between project and the artifact at path, from the artifact's parent upwards, while each holds nothing but the next folder on the way to the artifact; a folder that cannot be read, or holds anything else, ends the list.
func wrappers(project, path string) []string {
	var out []string
	child := path
	for dir := filepath.Dir(path); dir != project && dir != child; dir = filepath.Dir(dir) {
		if rel, err := filepath.Rel(project, dir); err != nil || rel == "." || rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			break
		}
		des, err := os.ReadDir(dir)
		if err != nil || len(des) != 1 || des[0].Name() != filepath.Base(child) {
			break
		}
		out = append(out, dir)
		child = dir
	}
	return out
}

// relSlash is path relative to base with forward slashes, as git writes paths; "" for base itself.
func relSlash(base, path string) (string, error) {
	rel, err := filepath.Rel(base, path)
	if err != nil {
		return "", err
	}
	if rel == ".." || strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		return "", errors.New("gitstate: " + path + " is outside " + base)
	}
	if rel == "." {
		return "", nil
	}
	return filepath.ToSlash(rel), nil
}

// parseStatusZ counts the records of `git status --porcelain=v1 -z` (paths relative to the repository root) except untracked ones under ignore; a rename or copy record's extra NUL field is skipped; whole reports whether the only record is the untracked project folder itself or one of its ancestors. project is the project folder relative to the repository root, "" for the root itself. Pure.
func parseStatusZ(out []byte, ignore []string, project string) (count int, whole bool) {
	fields := strings.Split(string(out), "\x00")
	records := 0
	var only string
	for i := 0; i < len(fields); i++ {
		rec := fields[i]
		if len(rec) < 4 || rec[2] != ' ' {
			continue
		}
		x, y, path := rec[0], rec[1], rec[3:]
		if x == 'R' || x == 'C' || y == 'R' || y == 'C' {
			i++
		}
		records++
		if records == 1 {
			only = rec
		}
		// Only untracked entries inside generated output are ignored; a staged or tracked change there (git rm -r --cached node_modules) still counts.
		if untracked := x == '?' && y == '?'; !untracked || !under(path, ignore) {
			count++
		}
	}
	if records == 1 && project != "" && strings.HasPrefix(only, "?? ") {
		dir := strings.TrimSuffix(only[3:], "/")
		whole = dir == project || strings.HasPrefix(project, dir+"/")
	}
	return count, whole
}

// under reports whether path (a file or, ending in "/", a folder) is one of folders or lies inside one.
func under(path string, folders []string) bool {
	path = strings.TrimSuffix(path, "/")
	for _, f := range folders {
		if f != "" && (path == f || strings.HasPrefix(path, f+"/")) {
			return true
		}
	}
	return false
}
