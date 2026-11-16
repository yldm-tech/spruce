package app

import (
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/history"
)

type fakeStore struct {
	mu       sync.Mutex
	limits   []int
	runs     []history.Run
	appended []history.Run
	cutoffs  []time.Time
	cleared  int
}

func (f *fakeStore) Append(r history.Run) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.appended = append(f.appended, r)
	return nil
}

func (f *fakeStore) Prune(cutoff time.Time) (int, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cutoffs = append(f.cutoffs, cutoff)
	return 0, nil
}

func (f *fakeStore) Clear() error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cleared++
	return nil
}

func (f *fakeStore) List(limit int) ([]history.Run, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.limits = append(f.limits, limit)
	return f.runs, nil
}

func TestHistoryListLimit(t *testing.T) {
	store := &fakeStore{runs: []history.Run{{ID: "a"}}}
	h := NewHistory(store, nil, slog.New(slog.DiscardHandler))
	for _, bad := range []int{-1, 0, history.MaxRuns + 1} {
		if _, err := h.List(bad); err == nil {
			t.Errorf("List(%d) accepted", bad)
		}
	}
	for _, ok := range []int{1, history.MaxRuns} {
		runs, err := h.List(ok)
		if err != nil || len(runs) != 1 {
			t.Errorf("List(%d) = %v, %v", ok, runs, err)
		}
	}
	if len(store.limits) != 2 || store.limits[0] != 1 || store.limits[1] != history.MaxRuns {
		t.Errorf("store saw limits %v", store.limits)
	}
	if err := h.Clear(); err != nil || store.cleared != 1 {
		t.Errorf("Clear = %v, store cleared %d times", err, store.cleared)
	}
}

func TestHistorySubscribe(t *testing.T) {
	if _, _, ok := NewHistory(&fakeStore{}, nil, slog.New(slog.DiscardHandler)).Subscribe(); ok {
		t.Error("a store that announces nothing offered a subscription")
	}
	store, err := history.Open(filepath.Join(t.TempDir(), "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	runs, unsubscribe, ok := NewHistory(store, nil, slog.New(slog.DiscardHandler)).Subscribe()
	if !ok {
		t.Fatal("the store history.Open returns offered no subscription")
	}
	defer unsubscribe()
	start := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	if err := store.Append(history.Run{ID: "run-1", Kind: history.KindSystemJunk, StartedAt: start, EndedAt: start.Add(time.Second)}); err != nil {
		t.Fatal(err)
	}
	select {
	case r := <-runs:
		if r.ID != "run-1" {
			t.Errorf("got %+v", r)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the appended run did not arrive")
	}
}
