package health

import (
	"context"
	"errors"
	"math"
	"sync"
	"testing"
	"time"
)

var t0 = time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)

// testConfig samples every 5 s, averages over 30 s, alerts at 40 % sustained for 1 min and stops being hot below 30 %.
func testConfig() Config {
	return Config{
		Interval:   5 * time.Second,
		Window:     30 * time.Second,
		Threshold:  40,
		Hysteresis: 10,
		Sustain:    time.Minute,
		Ignore:     []string{"kernel_task", "System Idle Process"},
	}
}

func newTestMonitor(t *testing.T, cfg Config) *monitor {
	t.Helper()
	if err := cfg.Validate(); err != nil {
		t.Fatal(err)
	}
	return newMonitor(cfg, nil)
}

// sim is a process whose cumulative CPU time grows at a settable percent of one CPU.
type sim struct {
	pid     int32
	name    string
	created time.Time
	cpu     time.Duration
	percent float64
}

func (p *sim) advance(d time.Duration) {
	p.cpu += time.Duration(float64(d) * p.percent / 100)
}

func (p *sim) sample() ProcessSample {
	return ProcessSample{PID: p.pid, Name: p.name, CPUTime: p.cpu, CreateTime: p.created}
}

// clock drives a monitor through snapshots 5 s apart.
type clock struct {
	m   *monitor
	now time.Time
}

func newClock(m *monitor) *clock { return &clock{m: m, now: t0} }

// step advances every running process by one interval and feeds the snapshot.
func (c *clock) step(procs ...*sim) {
	c.now = c.now.Add(c.m.cfg.Interval)
	snap := Snapshot{At: c.now}
	for _, p := range procs {
		p.advance(c.m.cfg.Interval)
		snap.Processes = append(snap.Processes, p.sample())
	}
	c.m.observe(snap)
}

func (c *clock) run(d time.Duration, procs ...*sim) {
	for range int(d / c.m.cfg.Interval) {
		c.step(procs...)
	}
}

func near(a, b float64) bool { return math.Abs(a-b) < 0.01 }

func TestSustainedProcessAlerts(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	xcode := &sim{pid: 100, name: "Xcode", created: t0.Add(-time.Hour), percent: 48}
	idle := &sim{pid: 101, name: "Finder", created: t0.Add(-time.Hour), percent: 1}

	c.step(xcode, idle) // first sample: no rate yet
	hotFrom := c.now.Add(m.cfg.Interval)
	c.run(time.Minute, xcode, idle)
	if got := m.Alerts(); len(got) != 0 {
		t.Fatalf("alert before Sustain: %+v", got)
	}
	c.step(xcode, idle)
	got := m.Alerts()
	if len(got) != 1 {
		t.Fatalf("alerts after Sustain = %+v, want Xcode", got)
	}
	a := got[0]
	if a.PID != 100 || a.Name != "Xcode" || !near(a.CPUPercent, 48) || a.Threshold != 40 {
		t.Errorf("alert %+v", a)
	}
	if !a.Since.Equal(hotFrom) || a.Duration != time.Minute {
		t.Errorf("since %v duration %v, want %v and 1m", a.Since, a.Duration, hotFrom)
	}

	c.run(11*time.Minute, xcode, idle)
	a = m.Alerts()[0]
	if !a.Since.Equal(hotFrom) || a.Duration != 12*time.Minute {
		t.Errorf("after 12 min: since %v duration %v", a.Since, a.Duration)
	}
}

func TestBriefSpikesNeverAlert(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	p := &sim{pid: 7, name: "spiky", created: t0}
	for range 10 {
		p.percent = 300
		c.run(20*time.Second, p)
		p.percent = 0
		c.run(40*time.Second, p)
		if got := m.Alerts(); len(got) != 0 {
			t.Fatalf("spikes alerted: %+v", got)
		}
	}
}

func TestReusedPIDStartsOver(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	old := &sim{pid: 42, name: "old", created: t0.Add(-time.Hour), cpu: time.Hour, percent: 90}
	c.run(50*time.Second, old)

	// The old process exits and a new one gets PID 42 with far less CPU time; keyed by PID alone this would read as a negative rate, and the hot period would carry over.
	fresh := &sim{pid: 42, name: "fresh", created: c.now, percent: 90}
	c.step(fresh)
	if len(m.procs) != 1 {
		t.Fatalf("tracked %d processes, want only the new one", len(m.procs))
	}
	c.run(30*time.Second, fresh)
	if got := m.Alerts(); len(got) != 0 {
		t.Fatalf("hot period carried over to the reused PID: %+v", got)
	}
	c.run(40*time.Second, fresh)
	got := m.Alerts()
	if len(got) != 1 || got[0].Name != "fresh" || !near(got[0].CPUPercent, 90) {
		t.Fatalf("alerts = %+v, want fresh at 90", got)
	}
}

func TestHysteresis(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	p := &sim{pid: 9, name: "build", created: t0, percent: 50}
	c.step(p)
	c.run(2*time.Minute, p)
	if len(m.Alerts()) != 1 {
		t.Fatal("no alert at 50 %")
	}
	since := m.Alerts()[0].Since

	// Below Threshold but above Threshold - Hysteresis: still hot, same period.
	p.percent = 35
	c.run(time.Minute, p)
	got := m.Alerts()
	if len(got) != 1 || !got[0].Since.Equal(since) || !near(got[0].CPUPercent, 35) {
		t.Fatalf("alert at 35 %% = %+v, want the same period kept", got)
	}

	// Below Threshold - Hysteresis: clears as soon as the window average gets there.
	p.percent = 20
	c.step(p)
	if len(m.Alerts()) != 1 {
		t.Fatal("cleared while the window average was still above 30 %")
	}
	c.run(20*time.Second, p)
	if got := m.Alerts(); len(got) != 0 {
		t.Fatalf("still alerting at 20 %%: %+v", got)
	}

	// Back to 35: not hot again, because it must reach Threshold first.
	p.percent = 35
	c.run(3*time.Minute, p)
	if got := m.Alerts(); len(got) != 0 {
		t.Fatalf("re-alerted below Threshold: %+v", got)
	}
}

func TestIgnoredAndOwnProcessNeverAlert(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	kernel := &sim{pid: 0, name: "kernel_task", created: t0, percent: 400}
	self := &sim{pid: m.self, name: "Spruce", created: t0, percent: 400}
	c.step(kernel, self)
	c.run(5*time.Minute, kernel, self)
	if got := m.Alerts(); len(got) != 0 {
		t.Fatalf("alerts %+v", got)
	}
	if len(m.procs) != 0 {
		t.Errorf("ignored processes are tracked: %d", len(m.procs))
	}
}

func TestExitClearsAndForgets(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	p := &sim{pid: 5, name: "runaway", created: t0, percent: 100}
	c.step(p)
	c.run(2*time.Minute, p)
	if len(m.Alerts()) != 1 {
		t.Fatal("no alert")
	}
	c.step()
	if got := m.Alerts(); len(got) != 0 || len(m.procs) != 0 {
		t.Fatalf("after exit: alerts %+v, tracked %d", got, len(m.procs))
	}
}

func TestMemoryBoundedByWindow(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	p := &sim{pid: 5, name: "p", created: t0, percent: 10}
	c.run(time.Hour, p)
	want := int(m.cfg.Window/m.cfg.Interval) + 1
	if n := len(m.procs[procKey{5, t0.UnixNano()}].samples); n != want {
		t.Errorf("kept %d samples, want %d", n, want)
	}
}

func TestGapLongerThanWindowStartsOver(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	p := &sim{pid: 5, name: "p", created: t0, percent: 100}
	c.step(p)
	c.run(50*time.Second, p)
	// The machine sleeps for ten minutes: CPU time does not grow.
	c.now = c.now.Add(10 * time.Minute)
	c.step(p)
	st := m.procs[procKey{5, t0.UnixNano()}]
	if len(st.samples) != 1 || !st.hotSince.IsZero() {
		t.Errorf("after the gap: %d samples, hotSince %v", len(st.samples), st.hotSince)
	}
}

func TestStaleSnapshotDropped(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	p := &sim{pid: 5, name: "p", created: t0}
	m.observe(Snapshot{At: t0, Processes: []ProcessSample{p.sample()}})
	m.observe(Snapshot{At: t0, Processes: []ProcessSample{p.sample()}})
	m.observe(Snapshot{At: t0.Add(-time.Second), Processes: []ProcessSample{p.sample()}})
	if n := len(m.procs[procKey{5, t0.UnixNano()}].samples); n != 1 {
		t.Errorf("kept %d samples, want 1", n)
	}
}

func TestMultiCorePercent(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	p := &sim{pid: 3, name: "chrome", created: t0, percent: 250}
	c.step(p)
	c.run(2*time.Minute, p)
	got := m.Alerts()
	if len(got) != 1 || !near(got[0].CPUPercent, 250) || got[0].Threshold != 40 {
		t.Fatalf("alerts %+v, want 250 %% of one CPU with threshold 40", got)
	}
}

func TestAlertsSortedByCPU(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	a := &sim{pid: 1, name: "a", created: t0, percent: 60}
	b := &sim{pid: 2, name: "b", created: t0, percent: 150}
	c.step(a, b)
	c.run(2*time.Minute, a, b)
	got := m.Alerts()
	if len(got) != 2 || got[0].Name != "b" || got[1].Name != "a" {
		t.Fatalf("alerts %+v", got)
	}
}

func TestSubscribe(t *testing.T) {
	m := newTestMonitor(t, testConfig())
	c := newClock(m)
	ch, unsubscribe := m.Subscribe()
	if got := <-ch; got == nil || len(got) != 0 {
		t.Fatalf("initial list %#v, want empty non-nil", got)
	}

	p := &sim{pid: 5, name: "p", created: t0, percent: 100}
	c.step(p)
	c.run(65*time.Second, p)
	got := <-ch
	if len(got) != 1 || got[0].Duration != time.Minute {
		t.Fatalf("first alert list %+v", got)
	}

	// Within the same minute and rounded percent nothing is sent.
	c.step(p)
	select {
	case got := <-ch:
		t.Fatalf("unexpected list %+v", got)
	default:
	}

	// A slow subscriber only sees the latest list sent (when the minute turned to 4), while Alerts has the latest sample.
	c.run(3*time.Minute, p)
	if got := <-ch; got[0].Duration != 4*time.Minute {
		t.Fatalf("latest list %+v", got)
	}
	if got := m.Alerts(); got[0].Duration != 4*time.Minute+5*time.Second {
		t.Fatalf("Alerts %+v", got)
	}
	select {
	case got := <-ch:
		t.Fatalf("intermediate list delivered: %+v", got)
	default:
	}

	unsubscribe()
	unsubscribe()
	if _, ok := <-ch; ok {
		t.Fatal("channel open after unsubscribe")
	}
	c.run(time.Minute, p) // publishing after unsubscribe must not panic
}

func TestValidate(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatalf("DefaultConfig: %v", err)
	}
	bad := []func(*Config){
		func(c *Config) { c.Interval = 0 },
		func(c *Config) { c.Window = c.Interval },
		func(c *Config) { c.Threshold = 0 },
		func(c *Config) { c.Hysteresis = -1 },
		func(c *Config) { c.Hysteresis = c.Threshold },
		func(c *Config) { c.Sustain = -time.Second },
	}
	for i, mutate := range bad {
		cfg := DefaultConfig()
		mutate(&cfg)
		if cfg.Validate() == nil {
			t.Errorf("case %d: %+v validated", i, cfg)
		}
		if _, err := New(cfg, fakeSource{}); err == nil {
			t.Errorf("case %d: New accepted an invalid config", i)
		}
	}
	if _, err := New(DefaultConfig(), nil); err == nil {
		t.Error("New accepted a nil source")
	}
}

// fakeSource returns whatever next returns.
type fakeSource struct {
	next func(ctx context.Context) (Snapshot, error)
}

func (f fakeSource) Sample(ctx context.Context) (Snapshot, error) { return f.next(ctx) }

func TestRunSamplesUntilCancelled(t *testing.T) {
	cfg := testConfig()
	cfg.Interval = time.Millisecond
	cfg.Window = 4 * time.Millisecond
	cfg.Sustain = 10 * time.Millisecond

	var (
		mu    sync.Mutex
		calls int
	)
	failed := errors.New("transient")
	src := fakeSource{next: func(ctx context.Context) (Snapshot, error) {
		mu.Lock()
		defer mu.Unlock()
		calls++
		if calls == 3 {
			return Snapshot{}, failed // a failed sample is skipped, not fatal
		}
		// Synthetic time: 1 ms per call, the process burning one full CPU.
		at := t0.Add(time.Duration(calls) * time.Millisecond)
		return Snapshot{At: at, Processes: []ProcessSample{{PID: 77, Name: "busy", CPUTime: time.Duration(calls) * time.Millisecond, CreateTime: t0}}}, nil
	}}
	mon, err := New(cfg, src)
	if err != nil {
		t.Fatal(err)
	}
	ch, unsubscribe := mon.Subscribe()
	defer unsubscribe()
	<-ch

	ctx, cancel := context.WithCancel(context.Background())
	done := make(chan error, 1)
	go func() { done <- mon.Run(ctx) }()

	timeout := time.After(10 * time.Second)
	for alerted := false; !alerted; {
		select {
		case got := <-ch:
			alerted = len(got) == 1 && got[0].Name == "busy"
		case <-timeout:
			t.Fatal("no alert from Run")
		}
	}
	if err := mon.Run(ctx); err == nil || errors.Is(err, context.Canceled) {
		t.Errorf("second concurrent Run = %v, want already running", err)
	}
	cancel()
	if err := <-done; !errors.Is(err, context.Canceled) {
		t.Errorf("Run = %v, want context.Canceled", err)
	}
}
