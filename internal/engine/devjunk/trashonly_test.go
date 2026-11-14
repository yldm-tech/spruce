package devjunk

import (
	"context"
	"path/filepath"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// TestCleanTrashOnlyRefusesCommands: an automatic clean never runs a tool's command (one failure for its category) and moves file items to the Trash instead of deleting them.
func TestCleanTrashOnlyRefusesCommands(t *testing.T) {
	f := newFixture(t, "darwin", map[string]string{"/opt/homebrew/bin/npm": "/opt/homebrew/bin/npm"})
	writeFile(t, f.home, "Library/Developer/Xcode/DerivedData/App-abc/Build/x", 4000)
	writeFile(t, f.home, ".npm/_cacache/content-v2/x", 6000)
	f.runner.On("/opt/homebrew/bin/npm", "cache", "clean", "--force").Return("", 0)
	res := f.scan(t)
	derived, npm := categoryByID(res, "tool-xcode-derived-data"), categoryByID(res, "tool-npm")
	if derived == nil || npm == nil || npm.Command == "" {
		t.Fatalf("derived %+v npm %+v", derived, npm)
	}
	// Tool caches carry the newest time inside them.
	if derived.Items[0].Newest.IsZero() || npm.Items[0].Newest.IsZero() || time.Since(derived.Items[0].Newest) > time.Hour {
		t.Errorf("Newest %v, %v", derived.Items[0].Newest, npm.Items[0].Newest)
	}

	out, err := f.cleaner.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{"tool-xcode-derived-data", "tool-npm"}, Mode: cleanup.ModeTrashOnly}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f.runner.AssertNotCalled(t, "/opt/homebrew/bin/npm")
	if len(f.del.removed) != 0 {
		t.Errorf("trash-only deleted for good: %v", f.del.removed)
	}
	if _, ok := f.del.trashed[derived.Items[0].Path]; !ok {
		t.Errorf("derived data not moved to the Trash: %v", f.del.trashed)
	}
	if len(out.Failed) != 1 || out.Failed[0].CategoryID != "tool-npm" || out.Failed[0].Reason != cleanup.ReasonNoCommand || !out.Partial {
		t.Errorf("failures %+v", out.Failed)
	}
	if len(out.Moved) != 1 || out.Moved[0].CategoryID != "tool-xcode-derived-data" || out.Moved[0].Root != filepath.Join(f.home, "Library", "Developer", "Xcode", "DerivedData") {
		t.Errorf("moved %+v", out.Moved)
	}
	// Only the command alone: one failure, no error.
	out, err = f.cleaner.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{"tool-npm"}, Mode: cleanup.ModeTrashOnly}, nil)
	if err != nil || len(out.Failed) != 1 || out.Removed != 0 {
		t.Errorf("command alone: %+v, %v", out, err)
	}
	f.runner.AssertNotCalled(t, "/opt/homebrew/bin/npm")
}

// TestBuildOutputHasNoNewest: build output's age is its project's activity (ModTime); Newest stays zero.
func TestBuildOutputHasNoNewest(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ws := filepath.Join(f.home, "Workspace")
	writeFile(t, ws, "old/package.json", 10)
	writeFile(t, ws, "old/node_modules/dep/index.js", 8000)
	age(t, filepath.Join(ws, "old"), 90*24*time.Hour)
	f.cfg.DevProjectRoots = []string{ws}
	res := f.scan(t)
	nm := categoryByID(res, "artifact-node-modules")
	if nm == nil || len(nm.Items) != 1 {
		t.Fatalf("node_modules %+v", nm)
	}
	if !nm.Items[0].Newest.IsZero() || time.Since(nm.Items[0].ModTime) < 80*24*time.Hour {
		t.Errorf("item %+v, want no Newest and the project's activity as ModTime", nm.Items[0])
	}
}
