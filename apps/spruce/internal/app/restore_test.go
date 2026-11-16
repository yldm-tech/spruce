package app

import (
	"errors"
	"log/slog"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/schedule"
	"spruce/apps/spruce/internal/engine/undo"
)

// TestInterruptedCleanRecordedWithoutUndo: a move the system does not say the place of in the Trash (as every move to the Windows Recycle Bin) cannot be put back, yet the clean journals it and what it deleted for good, so one Spruce did not finish still gets its history run, with nothing offered to put back; on every platform.
func TestInterruptedCleanRecordedWithoutUndo(t *testing.T) {
	dir := t.TempDir()
	t.Setenv("HOME", filepath.Join(dir, "home"))
	l, err := undo.Open(filepath.Join(dir, "data", "undo"))
	if err != nil {
		t.Fatal(err)
	}
	root := filepath.Join(dir, "home", "AppData", "Local", "Temp")
	moved := cleanup.Moved{ItemID: "a", CategoryID: "system-junk:temp", Kind: cleanup.KindCache, Path: filepath.Join(root, "a"), Size: 300, Dir: true}
	removed := cleanup.Removed{ItemID: "b", Path: filepath.Join(root, "b"), Freed: 100}
	const id = "20261114T090000Z-0004"
	ctx, u := beginUndo(t.Context(), l, slog.New(slog.DiscardHandler), undo.JournalMeta{RunID: id, Kind: history.KindSystemJunk, Started: time.Now()})
	if u.journal == nil {
		t.Fatal("the clean is not journaled")
	}
	cleanup.OnMoved(ctx)(moved)
	cleanup.OnRemoved(ctx)(removed)
	u.journal.Close()

	// Spruce ended here; the next launch records the run.
	next, _ := undo.Open(filepath.Join(dir, "data", "undo"))
	store := &fakeStore{}
	RecoverInterrupted(t.Context(), next, store, schedule.NewActivity(nil), slog.New(slog.DiscardHandler))
	if len(store.appended) != 1 {
		t.Fatalf("history %+v", store.appended)
	}
	run := store.appended[0]
	if run.ID != id || run.Kind != history.KindSystemJunk || run.Summary == nil || run.Summary.Items != 2 || run.Summary.Trashed != 300 ||
		run.Summary.Freed != 100 || run.Undo != nil {
		t.Errorf("run %+v %+v", run, run.Summary)
	}
	if !slices.Contains(run.Details, "已删除 1 项") || !slices.Contains(run.Details, "Spruce 在清理时退出，这条记录是下次打开时补上的") {
		t.Errorf("details %q", run.Details)
	}
	if _, err := next.Load(id); !errors.Is(err, undo.ErrNoEntry) {
		t.Errorf("an entry to put back: %v", err)
	}

	// A clean that ends keeps nothing to put back and leaves no journal behind.
	ctx, u = beginUndo(t.Context(), next, slog.New(slog.DiscardHandler), undo.JournalMeta{RunID: "20261114T090000Z-0005", Kind: history.KindSystemJunk, Started: time.Now()})
	cleanup.OnMoved(ctx)(moved)
	if info := u.finish(time.Now(), []cleanup.Moved{moved}); info != nil {
		t.Errorf("undo %+v", info)
	}
	if recs, err := next.Recover(); err != nil || len(recs) != 0 {
		t.Errorf("left %+v %v", recs, err)
	}
}
