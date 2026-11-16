package devjunk

import (
	"path/filepath"
	"testing"
	"time"
)

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
