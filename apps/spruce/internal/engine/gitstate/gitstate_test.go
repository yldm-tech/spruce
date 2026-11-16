package gitstate

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/execx/execxtest"
)

const git = "/opt/homebrew/bin/git"

func mkdir(t *testing.T, parts ...string) string {
	t.Helper()
	p := filepath.Join(parts...)
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

func write(t *testing.T, path, data string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(data), 0o644); err != nil {
		t.Fatal(err)
	}
}

// statusArgs and lsArgs are the argument lists Check runs for project.
func statusArgs(project string) []string {
	return []string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.quotepath=false", "-C", project,
		"status", "--porcelain=v1", "-z", "--untracked-files=normal", "--ignore-submodules=dirty", "--", "."}
}

func lsArgs(project string, rels ...string) []string {
	return append([]string{"--no-optional-locks", "-c", "core.fsmonitor=false", "-c", "core.quotepath=false", "-C", project, "ls-files", "-z", "--"}, rels...)
}

func TestParseStatusZ(t *testing.T) {
	cases := []struct {
		name    string
		out     string
		ignore  []string
		project string
		count   int
		whole   bool
	}{
		{"nothing", "", nil, "web", 0, false},
		{"modified", " M web/a.go\x00", nil, "web", 1, false},
		{"several", " M web/a.go\x00?? web/new.txt\x00D  web/old.txt\x00", nil, "web", 3, false},
		{"rename skips its source", "R  web/new.go\x00web/old.go\x00 M web/b.go\x00", nil, "web", 2, false},
		{"copy skips its source", "C  web/c.go\x00web/a.go\x00", nil, "web", 1, false},
		{"untracked in generated output does not count", "?? web/node_modules/\x00?? web/.next/cache/x\x00", []string{"web/node_modules", "web/.next"}, "web", 0, false},
		{"untracked in bin counts", "?? web/bin/x.dll\x00", []string{"web/node_modules"}, "web", 1, false},
		{"staged deletion in generated output counts", "D  web/node_modules/a.js\x00?? web/node_modules/\x00", []string{"web/node_modules"}, "web", 1, false},
		{"tracked change in generated output counts", " M web/node_modules/a.js\x00", []string{"web/node_modules"}, "web", 1, false},
		{"ignore is a folder, not a prefix", "?? web/node_modules2/x\x00", []string{"web/node_modules"}, "web", 1, false},
		{"whole project untracked", "?? web/\x00", nil, "web", 1, true},
		{"an ancestor untracked", "?? Workspace/\x00", nil, "Workspace/web", 1, true},
		{"a sibling with a common prefix", "?? Work/\x00", nil, "Workspace/web", 1, false},
		{"untracked project with more records", "?? web/\x00 M other\x00", nil, "web", 2, false},
		{"repository root is never whole", "?? x/\x00", nil, "", 1, false},
		{"tracked change is not untracked", " M web\x00", nil, "web", 1, false},
	}
	for _, c := range cases {
		count, whole := parseStatusZ([]byte(c.out), c.ignore, c.project)
		if count != c.count || whole != c.whole {
			t.Errorf("%s: count %d whole %v, want %d %v", c.name, count, whole, c.count, c.whole)
		}
	}
}

func TestRepoOf(t *testing.T) {
	repo := t.TempDir()
	mkdir(t, repo, ".git")
	// The configured project root (ws) lies inside the repository: the walk goes past it.
	project := mkdir(t, repo, "ws", "proj")
	if got, ok := RepoOf(project); !ok || got != repo {
		t.Errorf("RepoOf(project) = %q %v, want %q", got, ok, repo)
	}
	// A linked work tree has a .git file.
	linked := t.TempDir()
	write(t, filepath.Join(linked, ".git"), "gitdir: /nowhere/.git/worktrees/x\n")
	sub := mkdir(t, linked, "app")
	if got, ok := RepoOf(sub); !ok || got != linked {
		t.Errorf("RepoOf with a .git file = %q %v", got, ok)
	}
	// The nearest .git wins.
	inner := mkdir(t, repo, "ws", "proj", "vendor", "lib")
	mkdir(t, inner, ".git")
	if got, _ := RepoOf(inner); got != inner {
		t.Errorf("nested repository = %q, want %q", got, inner)
	}
	plain := mkdir(t, t.TempDir(), "plain", "proj")
	if got, ok := RepoOf(plain); ok {
		t.Errorf("RepoOf outside any repository = %q, want not found", got)
	}
}

func TestCheckStates(t *testing.T) {
	repo := t.TempDir()
	mkdir(t, repo, ".git")
	project := mkdir(t, repo, "ws", "web")
	nm := filepath.Join(project, "node_modules")
	bin := filepath.Join(project, "bin")
	arts := []Artifact{{Path: nm, Generated: true}, {Path: bin}}
	cases := []struct {
		name   string
		status string
		ls     string
		want   Result
	}{
		{"clean", "", "", Result{State: cleanup.GitClean}},
		{"dirty", " M ws/web/src/a.js\x00", "", Result{State: cleanup.GitDirty}},
		{"untracked in node_modules does not count", "?? ws/web/node_modules/\x00", "", Result{State: cleanup.GitClean}},
		{"untracked in bin counts", "?? ws/web/bin/mine.txt\x00", "", Result{State: cleanup.GitDirty}},
		{"tracked file in bin", "", "bin/App.dll\x00", Result{State: cleanup.GitClean, Tracked: true}},
		{"dirty and tracked", " M ws/web/x\x00", "bin/App.dll\x00", Result{State: cleanup.GitDirty, Tracked: true}},
		{"whole project untracked in an enclosing repository", "?? ws/\x00", "", Result{State: cleanup.GitUnknown}},
	}
	for _, c := range cases {
		f := execxtest.New()
		f.On(git, statusArgs(project)...).Return(c.status, 0)
		f.On(git, lsArgs(project, "node_modules", "bin")...).Return(c.ls, 0)
		got, err := Checker{Runner: f, Git: git}.Check(context.Background(), repo, project, arts)
		if err != nil || got != c.want {
			t.Errorf("%s: %+v, %v, want %+v", c.name, got, err, c.want)
		}
	}
}

// TestCheckFailuresAreUnknown: once a .git was found, git failing in any way is GitUnknown, never GitNone.
func TestCheckFailuresAreUnknown(t *testing.T) {
	repo := t.TempDir()
	// A linked work tree whose main repository was moved away: git says "not a git repository" and exits 128.
	write(t, filepath.Join(repo, ".git"), "gitdir: /moved/away/.git/worktrees/web\n")
	project := mkdir(t, repo, "web")
	arts := []Artifact{{Path: filepath.Join(project, "node_modules"), Generated: true}}
	if r, ok := RepoOf(project); !ok || r != repo {
		t.Fatalf("RepoOf = %q %v", r, ok)
	}
	failures := map[string]func(e *execxtest.Expectation){
		"exit 128": func(e *execxtest.Expectation) {
			e.ReturnOutput(execx.Output{ExitCode: 128, Stderr: []byte("fatal: not a git repository: /moved/away/.git/worktrees/web")})
		},
		"dubious ownership": func(e *execxtest.Expectation) {
			e.ReturnOutput(execx.Output{ExitCode: 128, Stderr: []byte("fatal: detected dubious ownership in repository")})
		},
		"other exit":    func(e *execxtest.Expectation) { e.Return("", 1) },
		"timeout":       func(e *execxtest.Expectation) { e.Fail(fmt.Errorf("git: %w", context.DeadlineExceeded)) },
		"not installed": func(e *execxtest.Expectation) { e.Fail(os.ErrNotExist) },
	}
	for name, fail := range failures {
		f := execxtest.New()
		fail(f.On(git, statusArgs(project)...))
		got, err := Checker{Runner: f, Git: git}.Check(context.Background(), repo, project, arts)
		if got.State != cleanup.GitUnknown || got.Tracked || err == nil {
			t.Errorf("%s: %+v, %v, want GitUnknown with the error", name, got, err)
		}
	}
	// ls-files failing after a clean status is unknown as well.
	f := execxtest.New()
	f.On(git, statusArgs(project)...).Return("", 0)
	f.On(git, lsArgs(project, "node_modules")...).Return("", 128)
	if got, _ := (Checker{Runner: f, Git: git}).Check(context.Background(), repo, project, arts); got.State != cleanup.GitUnknown {
		t.Errorf("ls-files failure: %+v", got)
	}
	// No git program: unknown, and nothing runs.
	f = execxtest.New()
	got, err := Checker{Runner: f, Git: ""}.Check(context.Background(), repo, project, arts)
	if got.State != cleanup.GitUnknown || !errors.Is(err, ErrNoGit) || len(f.Calls()) != 0 {
		t.Errorf("no git: %+v, %v, calls %v", got, err, f.Calls())
	}
}

// TestCheckCommandSetup: every run drops the inherited GIT_DIR family, sets the prompt, lock and locale variables and is bounded by the timeout.
func TestCheckCommandSetup(t *testing.T) {
	repo := t.TempDir()
	mkdir(t, repo, ".git")
	project := mkdir(t, repo, "app")
	f := execxtest.New()
	f.On(git, statusArgs(project)...).Return("", 0)
	f.On(git, lsArgs(project, "target")...).Return("", 0)
	if _, err := (Checker{Runner: f, Git: git}).Check(context.Background(), repo, project, []Artifact{{Path: filepath.Join(project, "target"), Generated: true}}); err != nil {
		t.Fatal(err)
	}
	calls := f.Calls()
	if len(calls) != 2 {
		t.Fatalf("calls %v", calls)
	}
	wantUnset := []string{"GIT_DIR", "GIT_WORK_TREE", "GIT_INDEX_FILE", "GIT_OBJECT_DIRECTORY", "GIT_ALTERNATE_OBJECT_DIRECTORIES", "GIT_COMMON_DIR", "GIT_NAMESPACE", "GIT_CEILING_DIRECTORIES", "GIT_DISCOVERY_ACROSS_FILESYSTEM", "GIT_CONFIG", "GIT_CONFIG_GLOBAL", "GIT_CONFIG_SYSTEM", "GIT_CONFIG_COUNT", "GIT_CONFIG_KEY_*", "GIT_CONFIG_VALUE_*", "GIT_CONFIG_PARAMETERS"}
	for _, c := range calls {
		for _, name := range wantUnset {
			if !slices.Contains(c.Unset, name) {
				t.Errorf("%s does not unset %s", c, name)
			}
		}
		for _, kv := range []string{"GIT_TERMINAL_PROMPT=0", "GIT_OPTIONAL_LOCKS=0", "LC_ALL=C"} {
			if !slices.Contains(c.Env, kv) {
				t.Errorf("%s lacks %s", c, kv)
			}
		}
		if c.Timeout != DefaultTimeout {
			t.Errorf("%s timeout %v", c, c.Timeout)
		}
	}
	// No artifacts: status only.
	f = execxtest.New()
	f.On(git, statusArgs(project)...).Return("", 0)
	if got, err := (Checker{Runner: f, Git: git, Timeout: 3}).Check(context.Background(), repo, project, nil); err != nil || got.State != cleanup.GitClean || len(f.Calls()) != 1 || f.Calls()[0].Timeout != 3 {
		t.Errorf("without artifacts: %+v %v %v", got, err, f.Calls())
	}
}

// TestCheckGeneratedWrappers: git shows a wholly untracked .angular as ".angular/"; holding only cache it is generated content, holding anything else it counts.
func TestCheckGeneratedWrappers(t *testing.T) {
	repo := t.TempDir()
	mkdir(t, repo, ".git")
	project := mkdir(t, repo, "web")
	cache := mkdir(t, project, ".angular", "cache")
	write(t, filepath.Join(cache, "x"), "1")
	arts := []Artifact{{Path: cache, Generated: true}}
	check := func() Result {
		t.Helper()
		f := execxtest.New()
		f.On(git, statusArgs(project)...).Return("?? web/.angular/\x00", 0)
		f.On(git, lsArgs(project, ".angular/cache")...).Return("", 0)
		got, err := Checker{Runner: f, Git: git}.Check(context.Background(), repo, project, arts)
		if err != nil {
			t.Fatal(err)
		}
		return got
	}
	if got := check(); got.State != cleanup.GitClean {
		t.Errorf(".angular holding only cache: %+v", got)
	}
	write(t, filepath.Join(project, ".angular", "notes.txt"), "mine")
	if got := check(); got.State != cleanup.GitDirty {
		t.Errorf(".angular holding a file of its own: %+v", got)
	}
}

// TestCheckStagedDeletionInGeneratedOutput: node_modules removed from the index but not yet committed is an uncommitted change, though ls-files no longer lists it.
func TestCheckStagedDeletionInGeneratedOutput(t *testing.T) {
	repo := t.TempDir()
	mkdir(t, repo, ".git")
	project := mkdir(t, repo, "web")
	nm := filepath.Join(project, "node_modules")
	f := execxtest.New()
	f.On(git, statusArgs(project)...).Return("D  web/node_modules/a.js\x00?? web/node_modules/\x00", 0)
	f.On(git, lsArgs(project, "node_modules")...).Return("", 0)
	got, err := Checker{Runner: f, Git: git}.Check(context.Background(), repo, project, []Artifact{{Path: nm, Generated: true}})
	if err != nil || got.State != cleanup.GitDirty || got.Tracked {
		t.Errorf("staged deletion: %+v, %v", got, err)
	}
}
