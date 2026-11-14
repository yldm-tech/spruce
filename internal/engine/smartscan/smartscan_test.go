package smartscan

import (
	"context"
	"errors"
	"slices"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// fake is a source scanner that returns what it is given and records the requests it gets.
type fake struct {
	mu      sync.Mutex
	res     cleanup.ScanResult
	err     error
	steps   int
	cleanFn func(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error)
	reqs    []cleanup.CleanRequest
}

func (f *fake) Scan(ctx context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	for i := range f.steps {
		progress(cleanup.Progress{Phase: cleanup.PhaseScanning, Path: "/step", Done: i, Total: f.steps, Bytes: int64(i)})
	}
	if f.err != nil {
		return cleanup.ScanResult{}, f.err
	}
	if err := ctx.Err(); err != nil {
		return cleanup.ScanResult{}, err
	}
	return f.res, nil
}

func (f *fake) Clean(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	f.mu.Lock()
	f.reqs = append(f.reqs, req)
	f.mu.Unlock()
	if f.cleanFn != nil {
		return f.cleanFn(ctx, req, progress)
	}
	res := cleanup.CleanResult{Failed: []cleanup.Failure{}}
	for i, id := range req.CategoryIDs {
		progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, CategoryID: id, Done: i, Total: len(req.ItemIDs), Bytes: int64(i)})
		res.Categories = append(res.Categories, cleanup.CategoryOutcome{CategoryID: id, Title: id, Removed: 1, Freed: 10})
		res.Removed++
		res.Freed += 10
	}
	progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Done: len(req.ItemIDs), Total: len(req.ItemIDs), Bytes: res.Freed})
	return res, nil
}

func item(path string, size int64, selected bool) cleanup.Item {
	return cleanup.Item{ID: cleanup.ItemID(path), Path: path, Size: size, Selected: selected}
}

func result(cats ...cleanup.Category) cleanup.ScanResult {
	return cleanup.NewResult(cats, time.Now())
}

// newScanner returns a Scanner over the fakes, as sources "a", "b", … in order.
func newScanner(t *testing.T, fakes ...*fake) *Scanner {
	t.Helper()
	var sources []Source
	for i, f := range fakes {
		sources = append(sources, Source{ID: string(rune('a' + i)), New: func() cleanup.Scanner { return f }})
	}
	return New(sources, nil)
}

func TestNewBuildsItsOwnScanners(t *testing.T) {
	var built atomic.Int32
	s := New([]Source{{ID: "system-junk", New: func() cleanup.Scanner { built.Add(1); return &fake{} }}}, nil)
	if built.Load() != 1 || len(s.sources) != 1 {
		t.Fatalf("built %d scanners", built.Load())
	}
	for name, sources := range map[string][]Source{
		"repeated":       {{ID: "x", New: func() cleanup.Scanner { return &fake{} }}, {ID: "x", New: func() cleanup.Scanner { return &fake{} }}},
		"empty id":       {{ID: "", New: func() cleanup.Scanner { return &fake{} }}},
		"separator":      {{ID: "a:b", New: func() cleanup.Scanner { return &fake{} }}},
		"no constructor": {{ID: "x"}},
		"nil scanner":    {{ID: "x", New: func() cleanup.Scanner { return nil }}},
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			New(sources, nil)
		}()
	}
}

func TestScanMergesSources(t *testing.T) {
	shared := item("/home/Library/Caches/shared", 50, true)
	a := &fake{steps: 3, res: result(
		cleanup.Category{ID: "caches", Title: "应用缓存", Deletion: cleanup.DeletePermanent, Selected: true, Roots: []string{"/home/Library/Caches"}, Items: []cleanup.Item{item("/home/Library/Caches/x", 100, false), shared}},
		cleanup.Category{ID: "trash", Title: "废纸篓", Deletion: cleanup.DeletePermanent, Warning: "需要权限", Items: []cleanup.Item{}},
	)}
	a.res.Denied = 2
	b := &fake{steps: 2, res: result(
		cleanup.Category{ID: "tools", Title: "工具缓存", Deletion: cleanup.DeletePermanent, Command: "npm cache clean --force", Items: []cleanup.Item{item("/home/.npm", 300, true), item("/home/.npm-old", 30, false)}},
		cleanup.Category{ID: "dup", Title: "重复", Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{shared}},
	)}
	b.res.Denied = 1
	s := newScanner(t, a, b)

	var (
		events  []cleanup.Progress
		running atomic.Int32
	)
	res, err := s.Scan(t.Context(), func(p cleanup.Progress) {
		if running.Add(1) != 1 {
			t.Error("progress called concurrently")
		}
		events = append(events, p)
		running.Add(-1)
	})
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, c := range res.Categories {
		ids = append(ids, c.ID)
		if c.Roots != nil {
			t.Errorf("%s carries roots %v", c.ID, c.Roots)
		}
	}
	if want := []string{"a:caches", "a:trash", "b:tools"}; !slices.Equal(ids, want) {
		t.Fatalf("categories %v, want %v (the duplicate dropped)", ids, want)
	}
	caches, trash, tools := res.Categories[0], res.Categories[1], res.Categories[2]
	if caches.Source != "a" || !caches.Selected || caches.Title != "应用缓存" || caches.Deletion != cleanup.DeletePermanent {
		t.Errorf("caches = %+v", caches)
	}
	if trash.Warning != "需要权限" || len(trash.Items) != 0 {
		t.Errorf("an item-less category with a warning is kept: %+v", trash)
	}
	if tools.Source != "b" || tools.Selected || tools.Command != "npm cache clean --force" || !tools.Items[0].Selected || tools.Items[1].Selected {
		t.Errorf("tools lost its defaults: %+v", tools)
	}
	if res.Items != 4 || res.Size != 480 || res.Denied != 3 {
		t.Errorf("totals = %d items, %d bytes, %d denied", res.Items, res.Size, res.Denied)
	}
	wantSources := []cleanup.SourceState{
		{ID: "a", Status: cleanup.SourceDone, Bytes: 150, Items: 2, Denied: 2},
		{ID: "b", Status: cleanup.SourceDone, Bytes: 380, Items: 3, Denied: 1},
	}
	if !slices.Equal(res.Sources, wantSources) {
		t.Errorf("sources = %+v", res.Sources)
	}

	first, last := events[0], events[len(events)-1]
	if len(first.Sources) != 2 || first.Sources[0].Status != cleanup.SourceScanning || first.Done != 0 || first.Total != 2*permille {
		t.Errorf("first event = %+v", first)
	}
	if last.Done != last.Total || last.Sources[0].Status != cleanup.SourceDone || last.Sources[1].Status != cleanup.SourceDone {
		t.Errorf("last event = %+v", last)
	}
	for _, e := range events[:len(events)-1] {
		if e.Done >= e.Total || e.Phase != cleanup.PhaseScanning {
			t.Errorf("an event before the end looks complete: %+v", e)
		}
	}
}

func TestScanKeepsGoingWhenASourceFails(t *testing.T) {
	a := &fake{err: cleanup.NewUserError("需要「完全磁盘访问权限」", errors.New("EPERM"))}
	b := &fake{res: result(cleanup.Category{ID: "x", Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{item("/d/x.dmg", 5, true)}})}
	c := &fake{err: errors.New("walk: boom")}
	res, err := newScanner(t, a, b, c).Scan(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Categories) != 1 || res.Categories[0].ID != "b:x" {
		t.Errorf("categories = %+v", res.Categories)
	}
	if st := res.Sources[0]; st.Status != cleanup.SourceFailed || st.Error != "需要「完全磁盘访问权限」" {
		t.Errorf("a = %+v", st)
	}
	if st := res.Sources[2]; st.Status != cleanup.SourceFailed || st.Error != scanFailed {
		t.Errorf("c = %+v", st)
	}

	if _, err := newScanner(t, a, c).Scan(t.Context(), nil); err == nil || err != a.err {
		t.Errorf("every source failed: %v, want the first source's error", err)
	}
}

func TestScanCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s := newScanner(t, &fake{res: result()})
	if _, err := s.Scan(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled scan: %v", err)
	}
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:x"}}, nil); !errors.Is(err, cleanup.ErrNoScan) {
		t.Errorf("clean after a cancelled scan: %v", err)
	}
}

func TestCleanDispatchesToTheSources(t *testing.T) {
	shared := item("/s", 50, true)
	a := &fake{res: result(
		cleanup.Category{ID: "caches", Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{item("/a1", 100, true), item("/a2", 100, true)}},
		cleanup.Category{ID: "logs", Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{item("/l1", 10, true), shared}},
	)}
	b := &fake{res: result(
		cleanup.Category{ID: "dmg", Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{item("/b1", 7, true), shared}},
	)}
	s := newScanner(t, a, b)
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}

	var events []cleanup.Progress
	res, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"b:dmg", "a:caches", "a:logs"}}, func(p cleanup.Progress) { events = append(events, p) })
	if err != nil {
		t.Fatal(err)
	}
	// Items go in scan order, largest first within a category.
	wantA := cleanup.CleanRequest{CategoryIDs: []string{"caches", "logs"}, ItemIDs: []string{cleanup.ItemID("/a1"), cleanup.ItemID("/a2"), shared.ID, cleanup.ItemID("/l1")}}
	if len(a.reqs) != 1 || !slices.Equal(a.reqs[0].CategoryIDs, wantA.CategoryIDs) || !slices.Equal(a.reqs[0].ItemIDs, wantA.ItemIDs) {
		t.Errorf("a got %+v, want %+v", a.reqs, wantA)
	}
	// The item b shares with a is a's: b is asked only for its own.
	if len(b.reqs) != 1 || !slices.Equal(b.reqs[0].ItemIDs, []string{cleanup.ItemID("/b1")}) {
		t.Errorf("b got %+v", b.reqs)
	}
	var outs []string
	for _, c := range res.Categories {
		outs = append(outs, c.Source+" "+c.CategoryID)
	}
	if want := []string{"a a:caches", "a a:logs", "b b:dmg"}; !slices.Equal(outs, want) {
		t.Errorf("outcomes %v, want %v", outs, want)
	}
	if res.Removed != 3 || res.Freed != 30 || res.Partial || res.Interrupted {
		t.Errorf("result = %+v", res)
	}
	last := events[len(events)-1]
	if last.Total != 5 || last.Done != 5 || last.Bytes != 30 {
		t.Errorf("last progress = %+v", last)
	}
	for _, e := range events {
		if e.Total != 5 || e.Done > 5 || (e.CategoryID != "" && e.CategoryID[1] != ':') {
			t.Errorf("progress %+v", e)
		}
	}
	// A merged scan serves one clean.
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:caches"}}, nil); !errors.Is(err, cleanup.ErrNoScan) {
		t.Errorf("second clean: %v", err)
	}
}

func TestCleanRejectsWithoutCleaning(t *testing.T) {
	a := &fake{res: result(cleanup.Category{ID: "caches", Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{item("/a1", 1, true)}}, cleanup.Category{ID: "logs", Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{item("/l1", 1, true)}})}
	s := newScanner(t, a)
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:caches"}}, nil); !errors.Is(err, cleanup.ErrNoScan) {
		t.Errorf("before a scan: %v", err)
	}
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	for name, req := range map[string]cleanup.CleanRequest{
		"unknown category":           {CategoryIDs: []string{"a:caches", "a:nope"}},
		"unprefixed category":        {CategoryIDs: []string{"caches"}},
		"item of another category":   {CategoryIDs: []string{"a:caches"}, ItemIDs: []string{cleanup.ItemID("/l1")}},
		"item of no category at all": {CategoryIDs: []string{"a:caches"}, ItemIDs: []string{"feedfacefeedface"}},
	} {
		if _, err := s.Clean(t.Context(), req, nil); !errors.Is(err, cleanup.ErrStale) {
			t.Errorf("%s: %v, want ErrStale", name, err)
		}
	}
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{}, nil); !errors.Is(err, cleanup.ErrNothingSelected) {
		t.Errorf("empty request: %v", err)
	}
	if len(a.reqs) != 0 {
		t.Errorf("a source was asked to clean: %+v", a.reqs)
	}
	// The scan is still there for a valid request, which names only the picked item.
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:caches", "a:logs"}, ItemIDs: []string{cleanup.ItemID("/l1")}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(a.reqs) != 1 || !slices.Equal(a.reqs[0].CategoryIDs, []string{"logs"}) {
		t.Errorf("a got %+v", a.reqs)
	}
}

func TestCleanStopsBetweenSources(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	a := &fake{res: result(cleanup.Category{ID: "caches", Title: "缓存", Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{item("/a1", 1, true)}})}
	a.cleanFn = func(context.Context, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
		cancel()
		return cleanup.CleanResult{Removed: 1, Freed: 1, Categories: []cleanup.CategoryOutcome{{CategoryID: "caches", Title: "缓存", Removed: 1, Freed: 1}}}, nil
	}
	b := &fake{res: result(cleanup.Category{ID: "dmg", Title: "磁盘映像", Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{item("/b1", 1, true)}})}
	s := newScanner(t, a, b)
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	res, err := s.Clean(ctx, cleanup.CleanRequest{CategoryIDs: []string{"a:caches", "b:dmg"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(b.reqs) != 0 {
		t.Error("the second source cleaned after the stop")
	}
	if !res.Interrupted || !res.Partial || res.Removed != 1 || len(res.Categories) != 2 || res.Categories[1] != (cleanup.CategoryOutcome{CategoryID: "b:dmg", Title: "磁盘映像", Source: "b"}) {
		t.Errorf("result = %+v", res)
	}
}

func TestCleanReportsALaterSourceThatRefuses(t *testing.T) {
	a := &fake{res: result(cleanup.Category{ID: "caches", Title: "缓存", Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{item("/a1", 1, true)}})}
	b := &fake{res: result(cleanup.Category{ID: "dmg", Title: "磁盘映像", Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{item("/b1", 1, true), item("/b2", 1, true)}})}
	b.cleanFn = func(context.Context, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
		return cleanup.CleanResult{}, cleanup.ErrStale
	}
	s := newScanner(t, a, b)
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	res, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"a:caches", "b:dmg"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.Removed != 1 || !res.Partial || len(res.Failed) != 1 || res.Failed[0].CategoryID != "b:dmg" || res.Categories[1].Failed != 2 {
		t.Errorf("result = %+v", res)
	}

	// The first source refusing fails the clean as a whole.
	if _, err := s.Scan(t.Context(), nil); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{"b:dmg"}}, nil); !errors.Is(err, cleanup.ErrStale) {
		t.Errorf("first source refused: %v", err)
	}
}
