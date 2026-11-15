package cleanup

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/fsx"
)

// cleanFixture remembers fx.scan, changed by edit first, in a new Executor over a fakeDeleter.
func cleanFixture(t *testing.T, fx fixture, opts ExecutorOptions, edit func(*ScanResult)) (*Executor, *fakeDeleter) {
	t.Helper()
	scan := fx.scan
	scan.Categories = slices.Clone(scan.Categories)
	for i := range scan.Categories {
		scan.Categories[i].Items = slices.Clone(scan.Categories[i].Items)
	}
	if edit != nil {
		edit(&scan)
	}
	del := &fakeDeleter{}
	e := NewExecutorWith(del, opts)
	if err := e.Remember(scan); err != nil {
		t.Fatal(err)
	}
	return e, del
}

// item returns a pointer to the item called name in scan.
func (fx fixture) item(scan *ScanResult, name string) *Item {
	for i := range scan.Categories {
		for j := range scan.Categories[i].Items {
			if scan.Categories[i].Items[j].Path == fx.paths[name] {
				return &scan.Categories[i].Items[j]
			}
		}
	}
	panic("no item " + name)
}

func failureOf(res CleanResult, path string) (Failure, bool) {
	for _, f := range res.Failed {
		if f.Path == path {
			return f, true
		}
	}
	return Failure{}, false
}

func TestCleanRefusesHeldItems(t *testing.T) {
	fx := newFixture(t)
	e, del := cleanFixture(t, fx, ExecutorOptions{}, func(s *ScanResult) { fx.item(s, "a").Hold = "项目有未提交的改动" })
	res, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	f, ok := failureOf(res, fx.paths["a"])
	if !ok || f.Reason != "项目有未提交的改动" || res.Removed != 2 || !res.Partial {
		t.Fatalf("result %+v", res)
	}
	if slices.Contains(del.removed, fx.paths["a"]) {
		t.Error("a held item was deleted")
	}
	if _, err := os.Stat(fx.paths["a"]); err != nil {
		t.Errorf("held item gone: %v", err)
	}
	// It stays in the remembered scan, so naming it again is not stale.
	if _, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}, ItemIDs: []string{fx.id("a")}}, nil); err != nil {
		t.Errorf("held item no longer remembered: %v", err)
	}
}

func TestCleanRechecksExclusionsAddedAfterTheScan(t *testing.T) {
	fx := newFixture(t)
	var exclusions []string
	e, del := cleanFixture(t, fx, ExecutorOptions{Exclusions: func() []string { return exclusions }}, nil)
	exclusions = []string{fx.paths["b"], "*.dmg"}
	res, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches", "installers"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"b", "x.dmg"} {
		if f, ok := failureOf(res, fx.paths[name]); !ok || f.Reason != "已在设置中排除，已跳过" {
			t.Errorf("%s: %+v", name, res.Failed)
		}
	}
	if slices.Contains(del.removed, fx.paths["b"]) || slices.Contains(del.trashed, fx.paths["x.dmg"]) {
		t.Errorf("excluded items deleted: %v %v", del.removed, del.trashed)
	}
	if res.Removed != 3 {
		t.Errorf("removed %d, want a, c and y.pkg", res.Removed)
	}
}

// TestCleanRefusesAFolderHoldingAnExclusion: an exclusion added inside a folder item after the scan keeps the whole folder, as the scans do.
func TestCleanRefusesAFolderHoldingAnExclusion(t *testing.T) {
	fx := newFixture(t)
	var exclusions []string
	e, del := cleanFixture(t, fx, ExecutorOptions{Exclusions: func() []string { return exclusions }}, nil)
	exclusions = []string{filepath.Join(fx.paths["b"], "inner"), filepath.Join(fx.dir, "caches", "a", "*")}
	res, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := failureOf(res, fx.paths["b"]); !ok || f.Reason != "已在设置中排除，已跳过" {
		t.Errorf("folder holding an exclusion: %+v", res.Failed)
	}
	if slices.Contains(del.removed, fx.paths["b"]) {
		t.Error("a folder holding an exclusion was deleted")
	}
	// A file is never refused for an exclusion below its path, nor for a pattern.
	if res.Removed != 2 || !slices.Contains(del.removed, fx.paths["a"]) {
		t.Errorf("removed %d %v, want a and c", res.Removed, del.removed)
	}
}

func TestCleanTrashOnly(t *testing.T) {
	fx := newFixture(t)
	e, del := cleanFixture(t, fx, ExecutorOptions{}, func(s *ScanResult) {
		s.Categories = append(s.Categories,
			Category{ID: "tool", Title: "npm", Kind: KindDevCache, Deletion: DeletePermanent, Command: "npm cache clean --force",
				Items: []Item{{ID: "tool-item", Path: filepath.Join(fx.dir, "npm")}}},
			Category{ID: "bin", Title: "废纸篓", Kind: KindTrash, Deletion: DeletePermanent,
				Items: []Item{{ID: "bin-item", Path: filepath.Join(fx.dir, ".Trash")}}})
	})
	res, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches", "installers", "tool", "bin"}, Mode: ModeTrashOnly}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(del.removed) != 0 {
		t.Fatalf("trash-only deleted for good: %v", del.removed)
	}
	if len(del.trashed) != 5 {
		t.Errorf("trashed %v, want every cache and installer", del.trashed)
	}
	reasons := map[string]string{}
	for _, f := range res.Failed {
		reasons[f.ItemID] = f.Reason
	}
	if reasons["tool-item"] != ReasonNoCommand || reasons["bin-item"] != ReasonNoEmptyTrash() || len(res.Failed) != 2 {
		t.Errorf("failures %+v", res.Failed)
	}
	if len(res.Moved) != 5 || res.Freed != 0 || res.Trashed == 0 {
		t.Errorf("result %+v", res)
	}
	if _, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}, Mode: "shred"}, nil); !errors.Is(err, ErrBadMode) {
		t.Errorf("unknown mode: %v", err)
	}
}

func TestCleanReportsMovesBeforeProgress(t *testing.T) {
	fx := newFixture(t)
	e, _ := cleanFixture(t, fx, ExecutorOptions{}, nil)
	var events []string
	ctx := WithOnMoved(t.Context(), func(m Moved) { events = append(events, "moved:"+filepath.Base(m.Path)) })
	res, err := e.Clean(ctx, CleanRequest{CategoryIDs: []string{"caches", "installers"}}, func(p Progress) {
		events = append(events, "progress:"+filepath.Base(p.Path))
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Moved) != 2 {
		t.Fatalf("moved %+v", res.Moved)
	}
	m := res.Moved[slices.IndexFunc(res.Moved, func(m Moved) bool { return m.Path == fx.paths["x.dmg"] })]
	want := Moved{ItemID: fx.id("x.dmg"), CategoryID: "installers", Kind: KindInstaller, Path: fx.paths["x.dmg"], TrashedPath: fx.paths["x.dmg"] + ".trashed",
		Root: filepath.Join(fx.dir, "downloads"), Size: fx.item(&fx.scan, "x.dmg").Size, Dev: 7, Ino: m.Ino}
	if m != want || m.Ino == 0 {
		t.Errorf("moved %+v, want %+v", m, want)
	}
	// Each move is reported once, right after it happened and before the progress of the next item.
	var moved []int
	for i, ev := range events {
		if ev == "moved:x.dmg" || ev == "moved:y.pkg" {
			moved = append(moved, i)
		}
	}
	if len(moved) != 2 || events[moved[0]+1] != "progress:y.pkg" || events[moved[1]+1] != "progress:." {
		t.Errorf("events %v", events)
	}
	// Permanent deletions are not moves.
	if slices.ContainsFunc(res.Moved, func(m Moved) bool { return m.CategoryID == "caches" }) {
		t.Errorf("a deletion counted as a move: %+v", res.Moved)
	}
}

func TestCleanSkipsReplacedItems(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("os.Lstat reports no inode on Windows")
	}
	fx := newFixture(t)
	e, del := cleanFixture(t, fx, ExecutorOptions{}, nil)
	// a is deleted and made again under the same name: another inode, the same type.
	tmp := fx.paths["a"] + ".new"
	if err := os.WriteFile(tmp, []byte("data"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Rename(tmp, fx.paths["a"]); err != nil {
		t.Fatal(err)
	}
	res, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f, ok := failureOf(res, fx.paths["a"]); !ok || f.Reason != "扫描后发生了变化，已跳过" {
		t.Errorf("failures %+v", res.Failed)
	}
	if slices.Contains(del.removed, fx.paths["a"]) {
		t.Error("a replaced item was deleted")
	}
}

func TestCleanSkipsTrashFilesWhoseContentChanged(t *testing.T) {
	fx := newFixture(t)
	e, del := cleanFixture(t, fx, ExecutorOptions{}, nil)
	// x.dmg was written again in place (same inode, new time); y.pkg grew. c, a cache, also changed time, which caches may.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(fx.paths["x.dmg"], later, later); err != nil {
		t.Fatal(err)
	}
	f, err := os.OpenFile(fx.paths["y.pkg"], os.O_APPEND|os.O_WRONLY, 0)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := f.WriteString("more"); err != nil {
		t.Fatal(err)
	}
	if err := f.Close(); err != nil {
		t.Fatal(err)
	}
	if err := os.Chtimes(fx.paths["c"], later, later); err != nil {
		t.Fatal(err)
	}
	res, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches", "installers"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"x.dmg", "y.pkg"} {
		if f, ok := failureOf(res, fx.paths[name]); !ok || f.Reason != "扫描后发生了变化，已跳过" {
			t.Errorf("%s: %+v", name, res.Failed)
		}
	}
	if len(del.trashed) != 0 || !slices.Contains(del.removed, fx.paths["c"]) {
		t.Errorf("trashed %v removed %v", del.trashed, del.removed)
	}
}

func TestCleanShowsUserErrorReasons(t *testing.T) {
	fx := newFixture(t)
	e, del := cleanFixture(t, fx, ExecutorOptions{}, nil)
	del.fail = map[string]error{fx.paths["a"]: NewUserError("项目有未提交的改动，已跳过", fsx.ErrProtected)}
	res, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}, ItemIDs: []string{fx.id("a")}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Failed) != 1 || res.Failed[0].Reason != "项目有未提交的改动，已跳过" {
		t.Errorf("failures %+v", res.Failed)
	}
}

func TestDrop(t *testing.T) {
	fx := newFixture(t)
	before := fx.scan
	out := Drop(fx.scan, []string{fx.id("a"), fx.id("x.dmg"), fx.id("y.pkg"), "unknown"})
	if len(out.Categories) != 2 || out.Categories[1].ID != "installers" || len(out.Categories[1].Items) != 0 || out.Categories[1].Items == nil {
		t.Fatalf("categories %+v", out.Categories)
	}
	var want int64
	for _, it := range out.Categories[0].Items {
		want += it.Size
	}
	if out.Categories[0].Size != want || out.Size != want || out.Items != 2 || out.Categories[1].Size != 0 {
		t.Errorf("sizes %+v", out)
	}
	if len(before.Categories[0].Items) != 3 || fx.scan.Items != 5 {
		t.Error("Drop changed its argument")
	}
}

// TestCleanListsHandledItems: Handled is exactly what the Executor dropped from its scan (deleted, moved, already gone), also when the clean is interrupted, so a caller keeping a copy of the scan can drop the same items; a failed item and the items not reached stay and clean next time.
func TestCleanListsHandledItems(t *testing.T) {
	fx := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	calls := 0
	del := &fakeDeleter{fail: map[string]error{fx.paths["a"]: errors.New("boom")}, hook: func(string) {
		calls++
		if calls == 2 {
			cancel()
		}
	}}
	e := NewExecutor(del)
	if err := e.Remember(fx.scan); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fx.paths["c"]); err != nil {
		t.Fatal(err)
	}
	res, err := e.Clean(ctx, CleanRequest{CategoryIDs: []string{"caches", "installers"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Interrupted {
		t.Fatalf("result %+v, want interrupted", res)
	}
	want := map[string]bool{}
	for _, p := range append(slices.Clone(del.removed), del.trashed...) {
		want[ItemID(p)] = true
	}
	all := []string{fx.id("a"), fx.id("b"), fx.id("c"), fx.id("x.dmg"), fx.id("y.pkg")}
	// c was gone before the clean: handled only if the clean reached it.
	reachedC := res.Skipped == 1
	if reachedC {
		want[fx.id("c")] = true
	}
	got := map[string]bool{}
	for _, id := range res.Handled {
		got[id] = true
	}
	if len(got) != len(want) || len(res.Handled) != len(want) {
		t.Fatalf("handled %v, want %v", res.Handled, want)
	}
	for id := range want {
		if !got[id] {
			t.Errorf("handled %v misses %s", res.Handled, id)
		}
	}
	if got[fx.id("a")] {
		t.Error("the failed item is listed as handled")
	}
	var rest []string
	for _, id := range all {
		if !got[id] {
			rest = append(rest, id)
		}
	}
	if len(rest) < 2 {
		t.Fatalf("rest %v, want the failed item and at least one not reached", rest)
	}
	// Every handled item is stale now, every other one still cleans.
	for _, id := range res.Handled {
		if _, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches", "installers"}, ItemIDs: []string{id}}, nil); !errors.Is(err, ErrStale) {
			t.Errorf("handled %s named again: %v", id, err)
		}
	}
	delete(del.fail, fx.paths["a"])
	del.hook = nil
	res, err = e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches", "installers"}, ItemIDs: rest}, nil)
	if err != nil || res.Interrupted || len(res.Failed) != 0 || len(res.Handled) != len(rest) {
		t.Errorf("second clean %+v, %v", res, err)
	}
}
