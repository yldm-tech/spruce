package smartscan

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/fsx"
)

// moving is a fake source's clean that moves every named item to the Trash, failing those in fail.
func moving(fail ...string) func(context.Context, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
	return func(ctx context.Context, req cleanup.CleanRequest, _ func(cleanup.Progress)) (cleanup.CleanResult, error) {
		res := cleanup.CleanResult{Failed: []cleanup.Failure{}}
		for _, id := range req.ItemIDs {
			if slices.Contains(fail, id) {
				res.Failed = append(res.Failed, cleanup.Failure{ItemID: id, CategoryID: req.CategoryIDs[0], Reason: "删除失败"})
				continue
			}
			m := cleanup.Moved{ItemID: id, CategoryID: req.CategoryIDs[0], Path: "/" + id, TrashedPath: "/.Trash/" + id}
			res.Moved = append(res.Moved, m)
			cleanup.OnMoved(ctx)(m)
			res.Removed++
		}
		return res, nil
	}
}

func TestCleanKeepsTheRestForTheNextClean(t *testing.T) {
	a1, a2, a3 := item("/a1", 10, true), item("/a2", 10, true), item("/a3", 10, true)
	a := &fake{res: result(cleanup.Category{ID: "dl", Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{a1, a2, a3}})}
	a.cleanFn = moving(a2.ID)
	s := newScanner(t, a)
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	var seen []cleanup.Moved
	ctx := cleanup.WithOnMoved(t.Context(), func(m cleanup.Moved) { seen = append(seen, m) })
	res, err := s.Clean(ctx, cleanup.CleanRequest{CategoryIDs: []string{"a:dl"}, ItemIDs: []string{a1.ID, a2.ID}, Mode: cleanup.ModeTrashOnly}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if a.reqs[0].Mode != cleanup.ModeTrashOnly {
		t.Errorf("source asked %+v, want the mode passed on", a.reqs[0])
	}
	if len(res.Moved) != 1 || res.Moved[0].CategoryID != "a:dl" || res.Moved[0].ItemID != a1.ID {
		t.Errorf("moved %+v", res.Moved)
	}
	if len(seen) != 1 || seen[0].CategoryID != "a:dl" {
		t.Errorf("callback saw %+v", seen)
	}
	// The cleaned item is gone; the failed one and the one not named stay and clean next time.
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:dl"}, ItemIDs: []string{a1.ID}}, nil); !errors.Is(err, cleanup.ErrStale) {
		t.Errorf("cleaned item named again: %v", err)
	}
	a.cleanFn = moving()
	res, err = s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:dl"}}, nil)
	if err != nil || res.Removed != 2 {
		t.Fatalf("second clean %+v, %v", res, err)
	}
	if got := a.reqs[len(a.reqs)-1].ItemIDs; !slices.Equal(got, []string{a2.ID, a3.ID}) {
		t.Errorf("second clean asked for %v", got)
	}
}

func TestCleanDropsACommandCategoryThatRan(t *testing.T) {
	tool := item("/home/.npm", 300, true)
	a := &fake{res: result(
		cleanup.Category{ID: "npm", Deletion: cleanup.DeletePermanent, Command: "npm cache clean --force", Items: []cleanup.Item{tool}},
		cleanup.Category{ID: "logs", Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{item("/l1", 1, true)}},
	)}
	s := newScanner(t, a)
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:npm"}}, nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:npm"}}, nil); !errors.Is(err, cleanup.ErrStale) {
		t.Errorf("command category named again: %v", err)
	}
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:logs"}}, nil); err != nil {
		t.Errorf("the rest: %v", err)
	}
}

func TestCleanKeepsWhatAnInterruptedCleanDidNotMove(t *testing.T) {
	a1, a2 := item("/a1", 10, true), item("/a2", 10, true)
	a := &fake{res: result(cleanup.Category{ID: "dl", Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{a1, a2}})}
	a.cleanFn = func(ctx context.Context, req cleanup.CleanRequest, p func(cleanup.Progress)) (cleanup.CleanResult, error) {
		res, err := moving()(ctx, cleanup.CleanRequest{CategoryIDs: req.CategoryIDs, ItemIDs: req.ItemIDs[:1]}, p)
		res.Interrupted = true
		return res, err
	}
	s := newScanner(t, a)
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:dl"}}, nil); err != nil {
		t.Fatal(err)
	}
	a.cleanFn = moving()
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:dl"}}, nil); err != nil {
		t.Fatal(err)
	}
	if got := a.reqs[len(a.reqs)-1].ItemIDs; !slices.Equal(got, []string{a2.ID}) {
		t.Errorf("after the interruption asked for %v, want only the item not moved", got)
	}
}

func TestPendingAfterAnUnfinishedScan(t *testing.T) {
	a := &fake{res: result(cleanup.Category{ID: "dl", Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{item("/a1", 10, true)}})}
	b := &fake{res: result()}
	s := newScanner(t, a, b)
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if s.Pending() {
		t.Fatal("pending after a complete scan")
	}

	// A stopped scan: the sources may have remembered new scans the merged one does not match.
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := s.Scan(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("stopped scan: %v", err)
	}
	if !s.Pending() {
		t.Error("not pending after a stopped scan")
	}
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:dl"}}, nil); !errors.Is(err, cleanup.ErrStale) {
		t.Errorf("clean after a stopped scan: %v", err)
	}
	if len(a.reqs) != 0 {
		t.Errorf("a source was asked to clean: %+v", a.reqs)
	}

	// A scan with a failed source does not finish either.
	b.err = errors.New("boom")
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if !s.Pending() {
		t.Error("not pending after a scan with a failed source")
	}
	b.err = nil
	if _, err := s.Scan(t.Context(), nil); err != nil || s.Pending() {
		t.Errorf("complete scan again: pending %v, %v", s.Pending(), err)
	}
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:dl"}}, nil); err != nil {
		t.Errorf("clean after a complete scan: %v", err)
	}
}

// executorSource is a source on a real cleanup.Executor, so the merged scan is checked against what the Executor still remembers.
type executorSource struct {
	exec *cleanup.Executor
	res  cleanup.ScanResult
}

func (s *executorSource) Scan(context.Context, func(cleanup.Progress)) (cleanup.ScanResult, error) {
	return s.res, s.exec.Remember(s.res)
}

func (s *executorSource) Clean(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	return s.exec.Clean(ctx, req, progress)
}

// cancellingDeleter deletes files for good inside a test folder and calls after once the first deletion is done.
type cancellingDeleter struct {
	removed []string
	after   func()
}

func (d *cancellingDeleter) Remove(_ context.Context, path string, _ []string) (fsx.Removal, error) {
	if err := os.Remove(path); err != nil {
		return fsx.Removal{}, err
	}
	d.removed = append(d.removed, path)
	if len(d.removed) == 1 && d.after != nil {
		d.after()
	}
	return fsx.Removal{Freed: 1, Removed: 1}, nil
}

func (d *cancellingDeleter) Trash(context.Context, string, []string) (cleanup.Trashed, error) {
	return cleanup.Trashed{}, errors.New("no Trash in this test")
}

// TestCleanAfterAnInterruptedPermanentClean: a clean interrupted after its first permanent deletion leaves the merged scan with exactly what the source's Executor still remembers, so the next clean of the same category deletes the rest instead of failing as stale.
func TestCleanAfterAnInterruptedPermanentClean(t *testing.T) {
	dir := t.TempDir()
	var items []cleanup.Item
	for _, name := range []string{"one", "two", "three"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		items = append(items, cleanup.NewItem(p, info, 100))
	}
	ctx, cancel := context.WithCancel(t.Context())
	del := &cancellingDeleter{after: cancel}
	src := &executorSource{exec: cleanup.NewExecutor(del), res: result(cleanup.Category{ID: "logs", Kind: cleanup.KindLog, Deletion: cleanup.DeletePermanent, Roots: []string{dir}, Items: items})}
	s := New([]Source{{ID: "a", New: func() cleanup.Scanner { return src }}}, nil)
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	res, err := s.Clean(ctx, cleanup.CleanRequest{CategoryIDs: []string{"a:logs"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !res.Interrupted || res.Removed != 1 || len(del.removed) != 1 {
		t.Fatalf("first clean %+v, removed %v", res, del.removed)
	}
	if !slices.Equal(res.Handled, []string{cleanup.ItemID(del.removed[0])}) {
		t.Errorf("handled %v, want the deleted item", res.Handled)
	}
	res, err = s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:logs"}}, nil)
	if err != nil {
		t.Fatalf("second clean: %v", err)
	}
	if res.Removed != 2 || len(res.Failed) != 0 || len(del.removed) != 3 {
		t.Errorf("second clean %+v, removed %v", res, del.removed)
	}
}

// TestCleanRefusesAnUnknownMode: a mode that is neither the default nor trash-only is refused before any source is asked.
func TestCleanRefusesAnUnknownMode(t *testing.T) {
	a := &fake{res: result(cleanup.Category{ID: "dl", Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{item("/a1", 10, true)}})}
	s := newScanner(t, a)
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:dl"}, Mode: "shred"}, nil); !errors.Is(err, cleanup.ErrBadMode) {
		t.Errorf("unknown mode: %v", err)
	}
	if len(a.reqs) != 0 {
		t.Errorf("a source was asked to clean: %+v", a.reqs)
	}
}
