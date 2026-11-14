package smartscan

import (
	"context"
	"errors"
	"slices"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
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
