package history

import (
	"os"
	"reflect"
	"strings"
	"testing"
)

// TestV2FieldsRoundTrip: the fields of automatic runs, undo and restores are written and read back, and a line from before them reads with them empty.
func TestV2FieldsRoundTrip(t *testing.T) {
	s, path := open(t)
	auto := Run{ID: "auto-1", Kind: KindAuto, StartedAt: base, EndedAt: base, Rule: "app-caches",
		Summary: &Totals{Trashed: 4200, Items: 3}, Undo: &UndoInfo{Items: 3, Bytes: 4200}, Details: []string{"旧的应用缓存：3 项"}}
	restore := Run{ID: "restore-1", Kind: KindRestore, StartedAt: base, EndedAt: base, RestoreOf: "auto-1", Summary: &Totals{Items: 3}}
	for _, r := range []Run{auto, restore} {
		if err := s.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	// A line an older build wrote, without the new fields.
	f, err := os.OpenFile(path, os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	old := `{"id":"old-1","kind":"systemJunk","startedAt":"2026-11-14T00:00:00Z","endedAt":"2026-11-14T00:00:00Z","results":null,"summary":{"freed":10,"items":1,"failed":0}}`
	if _, err := f.WriteString(old + "\n"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	s2, err := Open(path)
	if err != nil {
		t.Fatal(err)
	}
	runs, err := s2.List(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(runs) != 3 || runs[0].ID != "old-1" || runs[0].Undo != nil || runs[0].Rule != "" || runs[0].RestoreOf != "" {
		t.Fatalf("runs %+v", runs)
	}
	if !reflect.DeepEqual(runs[1].RestoreOf, "auto-1") || runs[1].Kind != KindRestore {
		t.Errorf("restore %+v", runs[1])
	}
	if got := runs[2]; got.Rule != "app-caches" || got.Undo == nil || *got.Undo != (UndoInfo{Items: 3, Bytes: 4200}) {
		t.Errorf("auto %+v", got)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), `"undo":{"items":3,"bytes":4200}`) || !strings.Contains(string(data), `"restoreOf":"auto-1"`) {
		t.Errorf("file %s", data)
	}
}
