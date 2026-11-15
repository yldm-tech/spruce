package undo

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"syscall"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/trash"
)

// fixture is a home with its own Trash and a root of items in a temporary folder: HOME points there, so trash.InTrash and trash.SystemRestorer see only that Trash. The real Trash is never touched.
type fixture struct {
	home, bin, root string
	l               *Ledger
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	if runtime.GOOS != "darwin" {
		t.Skip("putting back from the Trash is macOS only")
	}
	base := t.TempDir()
	f := &fixture{home: filepath.Join(base, "home"), root: filepath.Join(base, "data", "Caches")}
	f.bin = filepath.Join(f.home, ".Trash")
	for _, d := range []string{f.bin, f.root} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("HOME", f.home)
	l, err := Open(filepath.Join(base, "data", "undo"))
	if err != nil {
		t.Fatal(err)
	}
	f.l = l
	return f
}

// item makes a file (or a folder, when name ends in "/") under the root.
func (f *fixture) item(t *testing.T, name string) string {
	t.Helper()
	p := filepath.Join(f.root, strings.TrimSuffix(name, "/"))
	if strings.HasSuffix(name, "/") {
		if err := os.MkdirAll(filepath.Join(p, "inside"), 0o755); err != nil {
			t.Fatal(err)
		}
		return p
	}
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// trash moves path into the fixture's Trash as Foundation would and returns the move a clean reports.
func (f *fixture) trash(t *testing.T, path string, kind cleanup.CategoryKind) cleanup.Moved {
	t.Helper()
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	to := filepath.Join(f.bin, filepath.Base(path))
	if err := os.Rename(path, to); err != nil {
		t.Fatal(err)
	}
	st, err := os.Lstat(to)
	if err != nil {
		t.Fatal(err)
	}
	sys := st.Sys().(*syscall.Stat_t)
	return cleanup.Moved{ItemID: cleanup.ItemID(path), CategoryID: "caches", Kind: kind, Path: path, TrashedPath: to, Root: f.root,
		Size: 4, Dir: info.IsDir(), Dev: uint64(sys.Dev), Ino: sys.Ino}
}

func TestRecordKeepsOnlyWhatCanBePutBack(t *testing.T) {
	f := newFixture(t)
	good := f.trash(t, f.item(t, "a"), cleanup.KindCache)
	elsewhere := good
	elsewhere.Path, elsewhere.TrashedPath = filepath.Join(f.root, "b"), filepath.Join(f.home, "Library", "Mobile Documents", ".Trash", "b")
	unknown := good
	unknown.Path, unknown.TrashedPath = filepath.Join(f.root, "c"), ""
	noIdentity := good
	noIdentity.Path, noIdentity.TrashedPath, noIdentity.Ino = filepath.Join(f.root, "d"), filepath.Join(f.bin, "d"), 0
	isRoot := good
	isRoot.Path, isRoot.TrashedPath = f.root, filepath.Join(f.bin, "Caches")
	moves := []cleanup.Moved{good, elsewhere, unknown, noIdentity, isRoot}

	if items, bytes := Kept(moves); items != 1 || bytes != 4 {
		t.Errorf("Kept = %d, %d", items, bytes)
	}
	kept, err := f.l.Record("20261114T010412Z-3f9a", time.Now(), moves)
	if err != nil || kept != 1 {
		t.Fatalf("kept %d, err %v", kept, err)
	}
	e, err := f.l.Load("20261114T010412Z-3f9a")
	if err != nil {
		t.Fatal(err)
	}
	if len(e.Moves) != 1 || e.Moves[0].Path != good.Path || e.Version != Version || e.Partial {
		t.Errorf("entry %+v", e)
	}
	info, err := os.Stat(f.l.entryPath("20261114T010412Z-3f9a"))
	if err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("entry file %v %v", info, err)
	}
	if info, err := os.Stat(f.l.dir); err != nil || info.Mode().Perm() != 0o700 {
		t.Errorf("ledger folder %v %v", info, err)
	}

	// Nothing to keep writes nothing.
	if kept, err := f.l.Record("20261114T010413Z-0000", time.Now(), []cleanup.Moved{unknown}); kept != 0 || err != nil {
		t.Errorf("kept %d, err %v", kept, err)
	}
	if _, err := f.l.Load("20261114T010413Z-0000"); !errors.Is(err, ErrNoEntry) {
		t.Errorf("an entry was written: %v", err)
	}
}

func TestRecordCapsAndRefusesBadRunIDs(t *testing.T) {
	f := newFixture(t)
	moves := make([]cleanup.Moved, MaxMoves+1)
	for i := range moves {
		name := fmt.Sprintf("f%d", i)
		moves[i] = cleanup.Moved{Path: filepath.Join(f.root, name), TrashedPath: filepath.Join(f.bin, name), Root: f.root, Ino: uint64(i + 1), Size: 1}
	}
	kept, err := f.l.Record("big", time.Now(), moves)
	if err != nil || kept != MaxMoves {
		t.Fatalf("kept %d, err %v", kept, err)
	}
	if e, err := f.l.Load("big"); err != nil || !e.Partial || len(e.Moves) != MaxMoves {
		t.Errorf("partial %v, %d moves, err %v", e.Partial, len(e.Moves), err)
	}
	for _, id := range []string{"", "../x", "a/b", "a.b", strings.Repeat("a", 65)} {
		if _, err := f.l.Record(id, time.Now(), moves[:1]); !errors.Is(err, ErrBadRunID) {
			t.Errorf("Record(%q) err %v", id, err)
		}
		if _, err := f.l.Begin(JournalMeta{RunID: id}); !errors.Is(err, ErrBadRunID) {
			t.Errorf("Begin(%q) err %v", id, err)
		}
	}
}

func TestRestorePutsBackAndRemembers(t *testing.T) {
	f := newFixture(t)
	a := f.trash(t, f.item(t, "a/"), cleanup.KindCache)
	b := f.trash(t, f.item(t, "sub/b.log"), cleanup.KindLog)
	if _, err := f.l.Record("run1", time.Now(), []cleanup.Moved{a, b}); err != nil {
		t.Fatal(err)
	}
	if items, bytes, ok := f.l.Restorable("run1", time.Now()); !ok || items != 2 || bytes != 8 {
		t.Errorf("Restorable = %d, %d, %v", items, bytes, ok)
	}
	if n := f.l.Pending(time.Now()); n != 2 {
		t.Errorf("Pending = %d", n)
	}
	outs, err := f.l.Restore(t.Context(), "run1", trash.SystemRestorer())
	if err != nil {
		t.Fatal(err)
	}
	if len(outs) != 2 {
		t.Fatalf("outcomes %+v", outs)
	}
	for _, o := range outs {
		if o.Err != nil || o.Reason != "" || o.To != o.Move.Path || !o.Move.Restored {
			t.Errorf("outcome %+v", o)
		}
		if _, err := os.Lstat(o.Move.Path); err != nil {
			t.Errorf("%s did not come back: %v", o.Move.Path, err)
		}
	}
	if _, _, ok := f.l.Restorable("run1", time.Now()); ok {
		t.Error("the run is still restorable")
	}
	if n := f.l.Pending(time.Now()); n != 0 {
		t.Errorf("Pending = %d", n)
	}
	e, _ := f.l.Load("run1")
	if !e.Moves[0].Restored || e.Moves[0].RestoredTo != a.Path {
		t.Errorf("entry not rewritten: %+v", e.Moves[0])
	}
	restored := f.l.Restored(time.Now())
	for _, m := range []cleanup.Moved{a, b} {
		if _, ok := restored[cleanup.ItemID(m.Path)]; !ok {
			t.Errorf("%s is not in the restored set", m.Path)
		}
	}
	data, err := os.ReadFile(filepath.Join(f.l.dir, restoredFile))
	if err != nil || strings.Contains(string(data), f.root) {
		t.Errorf("restored.json holds paths or is missing: %s %v", data, err)
	}
	// A second restore has nothing left to do.
	if outs, err := f.l.Restore(t.Context(), "run1", trash.SystemRestorer()); err != nil || len(outs) != 0 {
		t.Errorf("second restore %+v %v", outs, err)
	}
}

func TestRestoreReportsEachFailure(t *testing.T) {
	f := newFixture(t)
	gone := f.trash(t, f.item(t, "gone"), cleanup.KindCache)
	cache := f.trash(t, f.item(t, "cache"), cleanup.KindCache)
	mine := f.trash(t, f.item(t, "mine.txt"), "")
	installer := f.trash(t, f.item(t, "app.dmg"), cleanup.KindInstaller)
	parent := f.trash(t, f.item(t, "folder/x"), cleanup.KindLog)
	ok := f.trash(t, f.item(t, "ok"), cleanup.KindCache)
	if _, err := f.l.Record("run2", time.Now(), []cleanup.Moved{gone, cache, mine, installer, parent, ok}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone.TrashedPath); err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{cache.Path, mine.Path, installer.Path} {
		f.item(t, filepath.Base(p))
	}
	if err := os.Remove(filepath.Join(f.root, "folder")); err != nil {
		t.Fatal(err)
	}
	outs, err := f.l.Restore(t.Context(), "run2", trash.SystemRestorer())
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, o := range outs {
		reasons[filepath.Base(o.Move.Path)] = o.Reason
	}
	want := map[string]string{
		"gone":     "已从废纸篓清除，无法放回",
		"cache":    "原位置已有新生成的内容，未放回",
		"mine.txt": "原位置已有同名项目",
		"app.dmg":  "",
		"x":        "原来的文件夹已不存在",
		"ok":       "",
	}
	for k, v := range want {
		if reasons[k] != v {
			t.Errorf("%s: reason %q, want %q", k, reasons[k], v)
		}
	}
	// The installer is a file of the user's: it came back beside the new one.
	if _, err := os.Stat(filepath.Join(f.root, "app 2.dmg")); err != nil {
		t.Errorf("the installer did not come back as app 2.dmg: %v", err)
	}
	// Both its old path and the one it came back to are in the restored set, so an automatic rule skips the copy under its new name.
	restored := f.l.Restored(time.Now())
	for _, p := range []string{installer.Path, filepath.Join(f.root, "app 2.dmg")} {
		if _, ok := restored[cleanup.ItemID(p)]; !ok {
			t.Errorf("%s is not in the restored set", p)
		}
	}
	// Every item was tried: 放回原处 no longer offers the run, though the failures stay in the entry, marked.
	if items, _, ok := f.l.Restorable("run2", time.Now()); ok || items != 0 {
		t.Errorf("Restorable %d %v after a restore that tried everything", items, ok)
	}
	if n := f.l.Pending(time.Now()); n != 0 {
		t.Errorf("Pending = %d", n)
	}
	e, err := f.l.Load("run2")
	if err != nil {
		t.Fatal(err)
	}
	tried := 0
	for _, m := range e.Moves {
		if m.Tried == m.Restored {
			t.Errorf("move %s: restored %v, tried %v", m.Path, m.Restored, m.Tried)
		}
		if m.Tried {
			tried++
		}
	}
	if tried != 4 {
		t.Errorf("%d moves marked tried, want 4", tried)
	}
	// Asked again, Restore tries nothing again, even once a move could go back: nothing is retried.
	if err := os.Remove(cache.Path); err != nil {
		t.Fatal(err)
	}
	outs, err = f.l.Restore(t.Context(), "run2", trash.SystemRestorer())
	if err != nil || len(outs) != 0 {
		t.Fatalf("second restore %d outcomes, %v", len(outs), err)
	}
	if again, _ := f.l.Load("run2"); slicesContainsRestored(again, cache.Path) || !slices.EqualFunc(again.Moves, e.Moves, func(a, b Move) bool { return a.Path == b.Path && a.Restored == b.Restored && a.Tried == b.Tried }) {
		t.Error("a second restore changed the entry")
	}
	if _, err := os.Lstat(cache.Path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the tried cache was put back: %v", err)
	}
}

// slicesContainsRestored reports whether e holds the move of path as put back, and not as tried.
func slicesContainsRestored(e Entry, path string) bool {
	for _, m := range e.Moves {
		if m.Path == path {
			return m.Restored && !m.Tried
		}
	}
	return false
}

// cancelAfter is a Restorer that puts back through trash.SystemRestorer and cancels its context after n items, as quitting Spruce does mid-restore.
type cancelAfter struct {
	n      int
	cancel func()
}

func (c *cancelAfter) Restore(ctx context.Context, req trash.RestoreRequest) (string, error) {
	to, err := trash.SystemRestorer().Restore(ctx, req)
	if c.n--; c.n == 0 {
		c.cancel()
	}
	return to, err
}

func TestRestoreStoppedPartWayStaysOffered(t *testing.T) {
	f := newFixture(t)
	gone := f.trash(t, f.item(t, "gone"), cleanup.KindCache)
	ok := f.trash(t, f.item(t, "ok"), cleanup.KindCache)
	later := f.trash(t, f.item(t, "later"), cleanup.KindCache)
	if _, err := f.l.Record("run3", time.Now(), []cleanup.Moved{gone, ok, later}); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(gone.TrashedPath); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	outs, err := f.l.Restore(ctx, "run3", &cancelAfter{n: 2, cancel: cancel})
	if !errors.Is(err, context.Canceled) || len(outs) != 2 {
		t.Fatalf("outcomes %+v, err %v", outs, err)
	}
	// The item it did not get to is still offered; the one it tried in vain is not.
	if items, _, ok := f.l.Restorable("run3", time.Now()); !ok || items != 1 {
		t.Errorf("Restorable %d %v, want the untried item", items, ok)
	}
	e, _ := f.l.Load("run3")
	for _, m := range e.Moves {
		want := Move{Moved: m.Moved}
		switch m.Path {
		case gone.Path:
			want.Tried = true
		case ok.Path:
			want.Restored, want.RestoredTo = true, ok.Path
		}
		if m != want {
			t.Errorf("move %+v, want %+v", m, want)
		}
	}
}

func TestRestoreRefusesExpiredAndBusy(t *testing.T) {
	f := newFixture(t)
	m := f.trash(t, f.item(t, "a"), cleanup.KindCache)
	if _, err := f.l.Record("old", time.Now().Add(-Window-time.Hour), []cleanup.Moved{m}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.l.Restore(t.Context(), "old", trash.SystemRestorer()); !errors.Is(err, ErrNoEntry) {
		t.Errorf("err %v", err)
	}
	if _, _, ok := f.l.Restorable("old", time.Now()); ok {
		t.Error("an expired entry is restorable")
	}
	if _, err := f.l.Restore(t.Context(), "never", trash.SystemRestorer()); !errors.Is(err, ErrNoEntry) {
		t.Errorf("err %v", err)
	}
	f.l.restoring["old"] = true
	if _, err := f.l.Restore(t.Context(), "old", trash.SystemRestorer()); !errors.Is(err, ErrRestoring) {
		t.Errorf("err %v", err)
	}
}

func TestJournalRecoverAfterACrash(t *testing.T) {
	f := newFixture(t)
	started := time.Date(2026, 11, 14, 1, 4, 12, 0, time.UTC)
	j, err := f.l.Begin(JournalMeta{RunID: "crashed", Kind: history.KindAuto, Rule: "app-caches", Started: started})
	if err != nil {
		t.Fatal(err)
	}
	a := f.trash(t, f.item(t, "a"), cleanup.KindCache)
	notRestorable := cleanup.Moved{Path: filepath.Join(f.root, "x"), Root: f.root, Size: 7}
	for _, m := range []cleanup.Moved{a, notRestorable} {
		if err := j.Add(m); err != nil {
			t.Fatal(err)
		}
	}
	deleted := cleanup.Removed{ItemID: "gone", Path: filepath.Join(f.root, "gone"), Freed: 9}
	if err := j.AddRemoved(deleted); err != nil {
		t.Fatal(err)
	}
	// Spruce ends mid-write: the last line is cut short.
	if err := j.Close(); err != nil {
		t.Fatal(err)
	}
	jf, err := os.OpenFile(f.l.journalPath("crashed"), os.O_WRONLY|os.O_APPEND, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := jf.WriteString(`{"itemId":"abc","path":"/half`); err != nil {
		t.Fatal(err)
	}
	if err := jf.Close(); err != nil {
		t.Fatal(err)
	}
	if info, err := os.Stat(f.l.journalPath("crashed")); err != nil || info.Mode().Perm() != 0o600 {
		t.Errorf("journal %v %v", info, err)
	}

	// The same ledger leaves its own running journal alone.
	if recs, err := f.l.Recover(); err != nil || len(recs) != 0 {
		t.Fatalf("recovered a running journal: %+v %v", recs, err)
	}
	// The next launch opens a new ledger on the folder.
	next, err := Open(f.l.dir)
	if err != nil {
		t.Fatal(err)
	}
	recs, err := next.Recover()
	if err != nil {
		t.Fatal(err)
	}
	if len(recs) != 1 {
		t.Fatalf("recovered %+v", recs)
	}
	r := recs[0]
	if r.Meta.RunID != "crashed" || r.Meta.Kind != history.KindAuto || r.Meta.Rule != "app-caches" || !r.Meta.Started.Equal(started) {
		t.Errorf("meta %+v", r.Meta)
	}
	if len(r.Moves) != 2 || r.Kept != 1 || r.Ended.IsZero() {
		t.Errorf("moves %d kept %d ended %v", len(r.Moves), r.Kept, r.Ended)
	}
	if len(r.Removed) != 1 || r.Removed[0] != deleted {
		t.Errorf("removed %+v, want %+v", r.Removed, deleted)
	}
	if _, err := os.Stat(next.journalPath("crashed")); !os.IsNotExist(err) {
		t.Errorf("the journal is still there: %v", err)
	}
	if _, _, ok := next.Restorable("crashed", time.Now()); !ok {
		t.Error("the recovered move is not restorable")
	}
	if recs, err := next.Recover(); err != nil || len(recs) != 0 {
		t.Errorf("a second Recover found %+v %v", recs, err)
	}
}

func TestRecordRemovesTheJournal(t *testing.T) {
	f := newFixture(t)
	j, err := f.l.Begin(JournalMeta{RunID: "r", Kind: history.KindSmartScan, Started: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	m := f.trash(t, f.item(t, "a"), cleanup.KindCache)
	if err := j.Add(m); err != nil {
		t.Fatal(err)
	}
	j.Close()
	if _, err := f.l.Record("r", time.Now(), []cleanup.Moved{m}); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(f.l.journalPath("r")); !os.IsNotExist(err) {
		t.Errorf("journal left: %v", err)
	}
	// A journal whose first line is unreadable is dropped.
	bad := f.l.journalPath("bad")
	if err := os.WriteFile(bad, []byte("not json\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if recs, err := f.l.Recover(); err != nil || len(recs) != 0 {
		t.Errorf("recovered %+v %v", recs, err)
	}
	if _, err := os.Stat(bad); !os.IsNotExist(err) {
		t.Errorf("bad journal left: %v", err)
	}
}

func TestPruneByAgeHistoryAndCount(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	m := cleanup.Moved{Path: filepath.Join(f.root, "a"), TrashedPath: filepath.Join(f.bin, "a"), Root: f.root, Ino: 1, Size: 1}
	record := func(id string, at time.Time) {
		t.Helper()
		if _, err := f.l.Record(id, at, []cleanup.Moved{m}); err != nil {
			t.Fatal(err)
		}
	}
	record("expired", now.Add(-Window-time.Minute))
	record("forgotten", now.Add(-time.Hour))
	// Entries whose run the history lacks go once their file is older than orphanGrace.
	old := now.Add(-orphanGrace - time.Minute)
	for _, id := range []string{"expired", "forgotten"} {
		if err := os.Chtimes(f.l.entryPath(id), old, old); err != nil {
			t.Fatal(err)
		}
	}
	for i := range MaxEntries + 1 {
		record(fmt.Sprintf("n%03d", i), now.Add(-time.Duration(MaxEntries+1-i)*time.Second))
	}
	j, err := f.l.Begin(JournalMeta{RunID: "running", Started: now})
	if err != nil {
		t.Fatal(err)
	}
	defer j.Close()
	dropped, err := f.l.Prune(now, func(id string) bool { return id != "forgotten" })
	if err != nil {
		t.Fatal(err)
	}
	if dropped != 3 {
		t.Errorf("dropped %d, want expired, forgotten and the oldest beyond %d", dropped, MaxEntries)
	}
	for _, id := range []string{"expired", "forgotten", "n000"} {
		if _, err := f.l.Load(id); !errors.Is(err, ErrNoEntry) {
			t.Errorf("%s survived", id)
		}
	}
	if _, err := f.l.Load("n001"); err != nil {
		t.Errorf("n001 was dropped: %v", err)
	}
	if _, err := os.Stat(f.l.journalPath("running")); err != nil {
		t.Errorf("Prune touched a running journal: %v", err)
	}
}

func TestPruneSparesAnEntryJustWritten(t *testing.T) {
	f := newFixture(t)
	now := time.Now()
	m := cleanup.Moved{Path: filepath.Join(f.root, "a"), TrashedPath: filepath.Join(f.bin, "a"), Root: f.root, Ino: 1, Size: 1}
	// Recover writes the entry of a clean that ended hours ago just before it appends the history run; a prune in between, whose list of runs predates that run, must not drop it.
	if _, err := f.l.Record("recovered", now.Add(-3*time.Hour), []cleanup.Moved{m}); err != nil {
		t.Fatal(err)
	}
	orphan := func(string) bool { return false }
	if n, err := f.l.Prune(now, orphan); err != nil || n != 0 {
		t.Fatalf("dropped %d %v", n, err)
	}
	if _, err := f.l.Load("recovered"); err != nil {
		t.Fatalf("the entry just written was dropped: %v", err)
	}
	if n, err := f.l.Prune(now.Add(orphanGrace+time.Minute), orphan); err != nil || n != 1 {
		t.Errorf("an orphan past the grace: dropped %d %v", n, err)
	}
}

func TestClearKeepsTheRestoredSet(t *testing.T) {
	f := newFixture(t)
	a := f.trash(t, f.item(t, "a"), cleanup.KindCache)
	b := f.trash(t, f.item(t, "b"), cleanup.KindCache)
	if _, err := f.l.Record("one", time.Now(), []cleanup.Moved{a}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.l.Record("two", time.Now(), []cleanup.Moved{b}); err != nil {
		t.Fatal(err)
	}
	if _, err := f.l.Restore(t.Context(), "one", trash.SystemRestorer()); err != nil {
		t.Fatal(err)
	}
	// A journal left by a Spruce that ended goes; one this ledger is still writing stays, and its clean records it when it ends.
	earlier, err := Open(f.l.dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := earlier.Begin(JournalMeta{RunID: "three", Started: time.Now()}); err != nil {
		t.Fatal(err)
	}
	running, err := f.l.Begin(JournalMeta{RunID: "four", Started: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()
	if err := f.l.Clear(); err != nil {
		t.Fatal(err)
	}
	names, _ := os.ReadDir(f.l.dir)
	var got []string
	for _, n := range names {
		got = append(got, n.Name())
	}
	if want := []string{"four" + journalExt, restoredFile}; !slices.Equal(got, want) {
		t.Errorf("left %v, want %v", got, want)
	}
	if err := running.Add(a); err != nil {
		t.Fatal(err)
	}
	if kept, err := f.l.Record("four", time.Now(), nil); err != nil || kept != 0 {
		t.Errorf("Record after Clear: %d %v", kept, err)
	}
	if _, err := os.Stat(f.l.journalPath("four")); !os.IsNotExist(err) {
		t.Errorf("the finished journal is still there: %v", err)
	}
	if _, ok := f.l.Restored(time.Now())[cleanup.ItemID(a.Path)]; !ok {
		t.Error("the restored set was cleared")
	}
	if n := f.l.Pending(time.Now()); n != 0 {
		t.Errorf("Pending = %d", n)
	}
}
