package app

import (
	"context"
	"errors"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/health"
)

type fakeMonitor struct{ alerts []health.Alert }

func (f fakeMonitor) Tune(float64, time.Duration, bool) error { return nil }

func (f fakeMonitor) Run(ctx context.Context) error { <-ctx.Done(); return ctx.Err() }

func (f fakeMonitor) Alerts() []health.Alert { return f.alerts }

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
