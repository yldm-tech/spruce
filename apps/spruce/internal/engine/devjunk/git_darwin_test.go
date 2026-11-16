package devjunk

import (
	"context"
	"path/filepath"
	"testing"

	"spruce/apps/spruce/internal/engine/execx/execxtest"
)

func TestLocateGitDarwin(t *testing.T) {
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
}
