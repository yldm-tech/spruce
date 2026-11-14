package health

import (
	"context"
	"testing"
	"time"
)

func TestTuneAppliesFromNextSample(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	node := &sim{pid: 200, name: "node", created: t0.Add(-time.Hour), percent: 30}
	c.run(3*time.Minute, node)
	if len(m.Alerts()) != 0 {
		t.Fatal("30 % alerted at a 40 % threshold")
	}
	// The user lowers the threshold to 25 % and the sustain to 30 s.
	if err := m.Tune(25, 30*time.Second, true); err != nil {
		t.Fatal(err)
	}
	c.run(30*time.Second, node)
	c.step(node)
	got := m.Alerts()
	if len(got) != 1 || got[0].Threshold != 25 {
		t.Fatalf("alerts after tuning %+v", got)
	}

	for _, bad := range []struct {
		threshold float64
		sustain   time.Duration
	}{{10, time.Minute}, {5, time.Minute}, {40, -time.Second}} {
		if err := m.Tune(bad.threshold, bad.sustain, true); err == nil {
			t.Errorf("Tune(%v, %v) accepted", bad.threshold, bad.sustain)
		}
	}
}

func TestTuneDisableClearsAlerts(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	calls := 0
	m.src = fakeSource{next: func(context.Context) (Snapshot, error) {
		calls++
		return Snapshot{}, nil
	}}
	c := newClock(m)
	hot := &sim{pid: 300, name: "hot", created: t0, percent: 90}
	c.run(2*time.Minute, hot)
	if len(m.Alerts()) != 1 {
		t.Fatal("no alert to clear")
	}
	if err := m.Tune(40, time.Minute, false); err != nil {
		t.Fatal(err)
	}
	m.sample(t.Context())
	if len(m.Alerts()) != 0 || calls != 0 || len(m.procs) != 0 {
		t.Errorf("disabled monitor: alerts %v, %d samples, %d processes remembered", m.Alerts(), calls, len(m.procs))
	}
	if err := m.Tune(40, time.Minute, true); err != nil {
		t.Fatal(err)
	}
	m.sample(t.Context())
	if calls != 1 {
		t.Errorf("enabled monitor took %d samples", calls)
	}
}
