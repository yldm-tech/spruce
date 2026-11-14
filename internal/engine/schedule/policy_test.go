package schedule_test

import (
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/schedule"
)

var t0 = time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

func TestDefaultPolicy(t *testing.T) {
	p := schedule.DefaultPolicy()
	if p.Interval != 6*time.Hour || p.LaunchDelay != 2*time.Minute || p.Retry != 15*time.Minute || p.Tick != 5*time.Minute ||
		p.CPUWindow != 10*time.Second || p.MaxCPUPercent != 40 || p.Watch != time.Minute || p.DiskEvery != 15*time.Minute {
		t.Errorf("policy %+v", p)
	}
}

func TestDueAndNext(t *testing.T) {
	p := schedule.DefaultPolicy()
	launched := t0
	for name, tc := range map[string]struct {
		now, last time.Time
		due       bool
		next      time.Time
	}{
		"never checked, right after launch":    {now: t0.Add(time.Minute), due: false, next: t0.Add(2 * time.Minute)},
		"never checked, launch delay over":     {now: t0.Add(2 * time.Minute), due: true, next: t0.Add(2 * time.Minute)},
		"old cache waits for the launch delay": {now: t0.Add(time.Minute), last: t0.Add(-10 * time.Hour), due: false, next: t0.Add(2 * time.Minute)},
		"old cache after the launch delay":     {now: t0.Add(3 * time.Minute), last: t0.Add(-10 * time.Hour), due: true, next: t0.Add(2 * time.Minute)},
		"checked an hour ago":                  {now: t0.Add(time.Hour), last: t0, due: false, next: t0.Add(6 * time.Hour)},
		"interval just over":                   {now: t0.Add(6 * time.Hour), last: t0, due: true, next: t0.Add(6 * time.Hour)},
		"last check in the future":             {now: t0.Add(time.Hour), last: t0.Add(48 * time.Hour), due: true, next: t0.Add(2 * time.Minute)},
	} {
		t.Run(name, func(t *testing.T) {
			if got := schedule.Due(p, tc.now, tc.last, launched); got != tc.due {
				t.Errorf("Due %v, want %v", got, tc.due)
			}
			if got := schedule.Next(p, tc.now, tc.last, launched); !got.Equal(tc.next) {
				t.Errorf("Next %v, want %v", got, tc.next)
			}
		})
	}
}
