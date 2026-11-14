package services

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
)

type fakeStore struct {
	limits []int
	runs   []history.Run
}

func (f *fakeStore) Append(history.Run) error { return nil }

func (f *fakeStore) List(limit int) ([]history.Run, error) {
	f.limits = append(f.limits, limit)
	return f.runs, nil
}

func TestHistoryListLimit(t *testing.T) {
	store := &fakeStore{runs: []history.Run{{ID: "a"}}}
	h := NewHistory(store)
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
}

type fakeMonitor struct{ alerts []health.Alert }

func (f fakeMonitor) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }
func (f fakeMonitor) Alerts() []health.Alert        { return f.alerts }
func (f fakeMonitor) Subscribe() (<-chan []health.Alert, func()) {
	ch := make(chan []health.Alert, 1)
	ch <- f.alerts
	return ch, func() {}
}

func TestHealthService(t *testing.T) {
	alert := health.Alert{PID: 1, Name: "Xcode", CPUPercent: 48, Duration: 12 * time.Minute, Threshold: 40}
	opened := 0
	boom := errors.New("boom")
	h := NewHealth(fakeMonitor{alerts: []health.Alert{alert}}, func(context.Context) error {
		opened++
		return boom
	})
	if got := h.Alerts(); len(got) != 1 || got[0] != alert {
		t.Errorf("Alerts = %+v", got)
	}
	if err := h.OpenSystemMonitor(context.Background()); !errors.Is(err, boom) || opened != 1 {
		t.Errorf("OpenSystemMonitor = %v after %d calls", err, opened)
	}
}
