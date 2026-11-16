package undo

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"
)

func TestTrimRestored(t *testing.T) {
	now := time.Date(2026, 11, 14, 0, 0, 0, 0, time.UTC)
	items := map[string]time.Time{"old": now.Add(-restoredFor - time.Hour), "new": now.Add(-time.Hour)}
	for i := range maxRestored + 10 {
		items[fmt.Sprintf("id%05d", i)] = now.Add(-time.Duration(i) * time.Minute)
	}
	got := trimRestored(items, now)
	if len(got) != maxRestored {
		t.Fatalf("%d IDs, want %d", len(got), maxRestored)
	}
	if _, ok := got["old"]; ok {
		t.Error("an expired ID survived")
	}
	if _, ok := got["id00000"]; !ok {
		t.Error("the newest ID was dropped")
	}
	if _, ok := got[fmt.Sprintf("id%05d", maxRestored+9)]; ok {
		t.Error("the oldest ID survived the cap")
	}
}

func TestOpenNeedsAnAbsoluteFolderAndDoesNoIO(t *testing.T) {
	if _, err := Open("relative/undo"); err == nil {
		t.Error("a relative folder was accepted")
	}
	dir := filepath.Join(t.TempDir(), "undo")
	l, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Error("Open made the folder")
	}
	if n := l.Pending(time.Now()); n != 0 {
		t.Errorf("Pending = %d", n)
	}
	if len(l.Restored(time.Now())) != 0 {
		t.Error("Restored is not empty")
	}
	if err := l.Clear(); err != nil {
		t.Errorf("Clear of a missing folder: %v", err)
	}
}
