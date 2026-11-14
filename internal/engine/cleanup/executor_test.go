package cleanup

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/fsx"
)

// fakeDeleter deletes with os.RemoveAll inside a test's temporary folder and records every call.
type fakeDeleter struct {
	mu      sync.Mutex
	removed []string
	trashed []string
	// fail maps a path to the error its deletion returns.
	fail map[string]error
	// partial maps a path to the failures its permanent deletion reports.
	partial map[string][]fsx.Failure
	// hook, when set, runs before each deletion.
	hook func(path string)
}

func (f *fakeDeleter) Remove(_ context.Context, path string, roots []string) (fsx.Removal, error) {
	if f.hook != nil {
		f.hook(path)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail[path]; err != nil {
		return fsx.Removal{}, err
	}
	if len(roots) == 0 {
		return fsx.Removal{}, fsx.ErrOutsideRoots
	}
	if fl := f.partial[path]; fl != nil {
		return fsx.Removal{Freed: 1, Failures: fl}, nil
	}
	f.removed = append(f.removed, path)
	return fsx.Removal{Freed: 100, Removed: 1}, os.RemoveAll(path)
}

func (f *fakeDeleter) Trash(_ context.Context, path string, _ []string) (Trashed, error) {
	if f.hook != nil {
		f.hook(path)
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if err := f.fail[path]; err != nil {
		return Trashed{}, err
	}
	f.trashed = append(f.trashed, path)
	return Trashed{Path: path + ".trashed", Dev: 7, Ino: uint64(len(f.trashed))}, os.RemoveAll(path)
}

type fixture struct {
	dir  string
	scan ScanResult
	// paths by item name.
	paths map[string]string
}

// newFixture makes caches/{a,b(dir),c} (permanent) and downloads/{x.dmg,y.pkg} (trash) in a temporary folder and a scan of them.
func newFixture(t *testing.T) fixture {
	t.Helper()
	dir := t.TempDir()
	fx := fixture{dir: dir, paths: map[string]string{}}
	mk := func(rel string, isDir bool) Item {
		p := filepath.Join(dir, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if isDir {
			if err := os.MkdirAll(filepath.Join(p, "inner"), 0o755); err != nil {
				t.Fatal(err)
			}
		} else if err := os.WriteFile(p, []byte("data"), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		fx.paths[filepath.Base(rel)] = p
		return NewItem(p, info, int64(len(rel)*10))
	}
	cats := []Category{
		{ID: "caches", Title: "缓存", Kind: KindCache, Deletion: DeletePermanent, Roots: []string{filepath.Join(dir, "caches")},
			Items: []Item{mk("caches/a", false), mk("caches/b", true), mk("caches/c", false)}},
		{ID: "installers", Title: "安装包", Kind: KindInstaller, Deletion: DeleteTrash, Roots: []string{filepath.Join(dir, "downloads")},
			Items: []Item{mk("downloads/x.dmg", false), mk("downloads/y.pkg", false)}},
	}
	fx.scan = NewResult(cats, time.Now().Add(-time.Second))
	return fx
}

func (fx fixture) id(name string) string { return ItemID(fx.paths[name]) }

func TestNewResultSortsAndTotals(t *testing.T) {
	fx := newFixture(t)
	if fx.scan.Items != 5 || fx.scan.Size == 0 || fx.scan.Duration < time.Second {
		t.Errorf("scan %+v", fx.scan)
	}
	c := fx.scan.Categories[0]
	if !slices.IsSortedFunc(c.Items, func(a, b Item) int { return int(b.Size - a.Size) }) {
		t.Errorf("items not largest first: %+v", c.Items)
	}
	if !c.Items[slices.IndexFunc(c.Items, func(it Item) bool { return it.Path == fx.paths["b"] })].Dir {
		t.Error("folder item not marked Dir")
	}
	if r := NewResult(nil, time.Now()); r.Categories == nil {
		t.Error("nil categories")
	}
}

func TestNewResultSettlesDefaultSelection(t *testing.T) {
	item := func(id string, selected bool) Item { return Item{ID: id, Path: "/x/" + id, Selected: selected} }
	res := NewResult([]Category{
		{ID: "whole", Selected: true, Items: []Item{item("a", false), item("b", false)}},
		{ID: "part", Items: []Item{item("c", true), item("d", false)}},
		{ID: "every", Items: []Item{item("e", true)}},
		{ID: "none", Items: []Item{item("f", false)}},
		{ID: "empty", Selected: true},
	}, time.Now())
	want := map[string]bool{"whole": true, "part": false, "every": true, "none": false, "empty": true}
	wantItems := map[string]bool{"a": true, "b": true, "c": true, "d": false, "e": true, "f": false}
	for _, c := range res.Categories {
		if c.Selected != want[c.ID] {
			t.Errorf("%s selected = %v", c.ID, c.Selected)
		}
		for _, it := range c.Items {
			if it.Selected != wantItems[it.ID] {
				t.Errorf("%s/%s selected = %v", c.ID, it.ID, it.Selected)
			}
		}
	}
}

func TestRememberValidates(t *testing.T) {
	e := NewExecutor(&fakeDeleter{})
	bad := []ScanResult{
		{Categories: []Category{{ID: "", Deletion: DeleteTrash}}},
		{Categories: []Category{{ID: "a", Deletion: DeleteTrash}, {ID: "a", Deletion: DeleteTrash}}},
		{Categories: []Category{{ID: "a", Deletion: "shred"}}},
		{Categories: []Category{{ID: "a", Deletion: DeleteTrash, Items: []Item{{ID: "x"}, {ID: "x"}}}}},
	}
	for i, s := range bad {
		if err := e.Remember(s); !errors.Is(err, ErrInvalidScan) {
			t.Errorf("case %d: %v", i, err)
		}
	}
}

func TestCleanRejectsWithoutDeleting(t *testing.T) {
	fx := newFixture(t)
	del := &fakeDeleter{}
	e := NewExecutor(del)
	if _, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}}, nil); !errors.Is(err, ErrNoScan) {
		t.Fatalf("before a scan: %v", err)
	}
	if err := e.Remember(fx.scan); err != nil {
		t.Fatal(err)
	}
	cases := map[string]CleanRequest{
		"unknown category":            {CategoryIDs: []string{"caches", "gone"}},
		"unknown item":                {CategoryIDs: []string{"caches"}, ItemIDs: []string{fx.id("a"), "deadbeef"}},
		"item of an unnamed category": {CategoryIDs: []string{"caches"}, ItemIDs: []string{fx.id("x.dmg")}},
	}
	for name, req := range cases {
		if _, err := e.Clean(t.Context(), req, nil); !errors.Is(err, ErrStale) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if _, err := e.Clean(t.Context(), CleanRequest{}, nil); !errors.Is(err, ErrNothingSelected) {
		t.Errorf("empty request: %v", err)
	}
	if len(del.removed)+len(del.trashed) != 0 {
		t.Fatalf("a rejected request deleted %v %v", del.removed, del.trashed)
	}
}

func TestCleanDeletesSelectionAndReports(t *testing.T) {
	fx := newFixture(t)
	del := &fakeDeleter{}
	e := NewExecutor(del)
	if err := e.Remember(fx.scan); err != nil {
		t.Fatal(err)
	}
	var events []Progress
	res, err := e.Clean(t.Context(), CleanRequest{
		CategoryIDs: []string{"caches", "installers"},
		ItemIDs:     []string{fx.id("a"), fx.id("b"), fx.id("x.dmg")},
	}, func(p Progress) { events = append(events, p) })
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 3 || res.Freed != 200 || res.Trashed != fx.scan.Categories[1].Items[slices.IndexFunc(fx.scan.Categories[1].Items, func(it Item) bool { return it.Path == fx.paths["x.dmg"] })].Size || res.Partial || len(res.Failed) != 0 {
		t.Fatalf("result %+v", res)
	}
	if len(del.removed) != 2 || len(del.trashed) != 1 || del.trashed[0] != fx.paths["x.dmg"] {
		t.Errorf("removed %v trashed %v", del.removed, del.trashed)
	}
	if _, err := os.Stat(fx.paths["c"]); err != nil {
		t.Errorf("an unselected item was deleted: %v", err)
	}
	if len(res.Categories) != 2 || res.Categories[0].Removed != 2 || res.Categories[1].Removed != 1 || res.Categories[0].Title != "缓存" {
		t.Errorf("categories %+v", res.Categories)
	}
	if len(events) != 4 || events[0].Phase != PhaseCleaning || events[3].Done != 3 || events[3].Total != 3 || events[3].Bytes != res.Freed+res.Trashed {
		t.Errorf("progress %+v", events)
	}

	// The deleted items are gone from the remembered scan: naming one again is stale, the rest still cleans.
	if _, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}, ItemIDs: []string{fx.id("a")}}, nil); !errors.Is(err, ErrStale) {
		t.Errorf("cleaning a deleted item again: %v", err)
	}
	res, err = e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}}, nil)
	if err != nil || res.Removed != 1 || del.removed[2] != fx.paths["c"] {
		t.Errorf("second clean %+v, %v", res, err)
	}
}

func TestCleanSkipsChangedAndGoneItems(t *testing.T) {
	fx := newFixture(t)
	del := &fakeDeleter{}
	e := NewExecutor(del)
	if err := e.Remember(fx.scan); err != nil {
		t.Fatal(err)
	}
	// b was a folder and is now a file; a vanished; c became a symlink to something precious.
	if err := os.RemoveAll(fx.paths["b"]); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fx.paths["b"], []byte("x"), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fx.paths["a"]); err != nil {
		t.Fatal(err)
	}
	precious := filepath.Join(fx.dir, "precious")
	if err := os.Mkdir(precious, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fx.paths["c"]); err != nil {
		t.Fatal(err)
	}
	symlinked := true
	if err := os.Symlink(precious, fx.paths["c"]); err != nil {
		if runtime.GOOS != "windows" {
			t.Fatal(err)
		}
		symlinked = false
		if err := os.WriteFile(fx.paths["c"], []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	res, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	wantFailed := 2
	if !symlinked {
		wantFailed = 1
	}
	if res.Skipped != 1 || len(res.Failed) != wantFailed || !res.Partial || res.Failed[0].Reason != "扫描后发生了变化，已跳过" {
		t.Fatalf("result %+v", res)
	}
	if symlinked && len(del.removed) != 0 {
		t.Errorf("deleted %v", del.removed)
	}
	if _, err := os.Stat(precious); err != nil {
		t.Errorf("the symlink target went: %v", err)
	}
}

func TestCleanReportsFailures(t *testing.T) {
	fx := newFixture(t)
	del := &fakeDeleter{
		fail:    map[string]error{fx.paths["a"]: fsx.ErrProtected, fx.paths["x.dmg"]: fs.ErrPermission},
		partial: map[string][]fsx.Failure{fx.paths["b"]: {{Path: "inner", Err: errors.New("busy"), InUse: true}}},
	}
	e := NewExecutor(del)
	if err := e.Remember(fx.scan); err != nil {
		t.Fatal(err)
	}
	res, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches", "installers"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]Failure{}
	for _, f := range res.Failed {
		reasons[filepath.Base(f.Path)] = f
	}
	if reasons["a"].Reason != "位于受保护的位置，已跳过" || reasons["x.dmg"].Reason != "没有权限删除" ||
		reasons["b"].Reason != "有 1 个文件未能删除，可能正在使用" || !reasons["b"].InUse || reasons["b"].Detail == "" {
		t.Errorf("failures %+v", res.Failed)
	}
	if res.Removed != 2 || !res.Partial || res.Categories[0].Failed != 2 || res.Categories[1].Failed != 1 {
		t.Errorf("result %+v", res)
	}
}

func TestCleanInterruptedAndBusy(t *testing.T) {
	fx := newFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	entered := make(chan struct{})
	release := make(chan struct{})
	var once sync.Once
	del := &fakeDeleter{hook: func(string) {
		once.Do(func() {
			close(entered)
			<-release
			cancel()
		})
	}}
	e := NewExecutor(del)
	if err := e.Remember(fx.scan); err != nil {
		t.Fatal(err)
	}
	done := make(chan CleanResult)
	go func() {
		res, err := e.Clean(ctx, CleanRequest{CategoryIDs: []string{"caches", "installers"}}, nil)
		if err != nil {
			t.Error(err)
		}
		done <- res
	}()
	<-entered
	if _, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}}, nil); !errors.Is(err, ErrBusy) {
		t.Errorf("concurrent clean: %v", err)
	}
	close(release)
	res := <-done
	if !res.Interrupted || !res.Partial || res.Removed != 1 {
		t.Errorf("result %+v", res)
	}
}

// TestSystemDeleterPermanent runs the real fsx.SafeRemove inside a temporary folder; no category uses the Trash.
func TestSystemDeleterPermanent(t *testing.T) {
	fx := newFixture(t)
	e := NewExecutor(nil)
	scan := fx.scan
	scan.Categories = scan.Categories[:1]
	if err := e.Remember(scan); err != nil {
		t.Fatal(err)
	}
	res, err := e.Clean(t.Context(), CleanRequest{CategoryIDs: []string{"caches"}}, nil)
	if err != nil || res.Removed != 3 || res.Freed == 0 || res.Partial {
		t.Fatalf("result %+v, %v", res, err)
	}
	if _, err := os.Stat(filepath.Join(fx.dir, "caches")); err != nil {
		t.Errorf("root removed: %v", err)
	}
}

func TestThrottle(t *testing.T) {
	var got []Progress
	fn := Throttle(func(p Progress) { got = append(got, p) }, time.Hour)
	for i := range 10 {
		fn(Progress{Done: i, Total: 10})
	}
	fn(Progress{Done: 10, Total: 10})
	if len(got) != 2 || got[0].Done != 0 || got[1].Done != 10 {
		t.Errorf("passed %+v", got)
	}
	Throttle(nil, time.Second)(Progress{})
}
