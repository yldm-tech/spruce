package services

import (
	"context"
	"os"
	"os/exec"
	"path/filepath"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/devjunk"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

func TestDevJunkDetails(t *testing.T) {
	home := t.TempDir()
	runner := execxtest.New()
	runner.On("/opt/homebrew/bin/npm", "cache", "clean", "--force").Return("", 0)
	c := devjunk.New(devjunk.Options{
		Home: home, Runner: runner, GOOS: "darwin",
		Settings: func() settings.CleanupSettings { return settings.CleanupSettings{} },
		Getenv:   func(string) string { return "" },
		LookPath: func(name string) (string, error) {
			if name == "/opt/homebrew/bin/npm" {
				return name, nil
			}
			return "", exec.ErrNotFound
		},
		VSCodeRunning: func(context.Context) bool { return false },
	})
	svc := NewDevJunk(c)

	before := svc.Details()
	if before.DefaultItems == nil || before.Commands == nil || len(before.DefaultItems)+len(before.Commands) != 0 {
		t.Fatalf("details before a scan = %+v, want empty lists", before)
	}

	if err := os.MkdirAll(filepath.Join(home, ".npm", "_cacache", "content-v2"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(home, ".npm", "_cacache", "content-v2", "x"), make([]byte, 100), 0o644); err != nil {
		t.Fatal(err)
	}
	res, err := c.Scan(context.Background(), nil)
	if err != nil {
		t.Fatal(err)
	}
	d := svc.Details()
	if len(d.Commands) != 1 || d.Commands[0] != (DevJunkCommand{CategoryID: "tool-npm", Command: "npm cache clean --force"}) {
		t.Errorf("commands = %+v", d.Commands)
	}
	if len(res.Categories) != 1 || len(d.DefaultItems) != 1 || d.DefaultItems[0] != res.Categories[0].Items[0].ID {
		t.Errorf("default items = %v for %+v", d.DefaultItems, res.Categories)
	}
}
