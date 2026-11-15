package sysinfo

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"strings"
	"sync"
	"testing"
	"time"
)

// fakeSource counts reads and returns readings whose counters grow by a fixed amount per read, with a fake clock.
type fakeSource struct {
	mu        sync.Mutex
	reads     int
	batteries int
	infos     int
	// at is the time of the next reading; it advances by step per read.
	at   time.Time
	step time.Duration
	// failNetwork makes every reading miss the network.
	failNetwork bool
	batteryErr  error
	// ctxs are the contexts of the reads, to check that stopping cancels them.
	ctxs []context.Context
}

func newFakeSource() *fakeSource {
	return &fakeSource{at: time.Now(), step: 2 * time.Second}
}

func (f *fakeSource) Read(ctx context.Context) Readings {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.reads++
	f.ctxs = append(f.ctxs, ctx)
	n := uint64(f.reads)
	r := Readings{
		At:      f.at,
		CPU:     []CPUTime{{Busy: float64(n), Total: float64(4 * n)}},
		Memory:  &MemoryStats{Total: 100, Used: 50, Percent: 50},
		DiskIO:  map[string]DiskCount{"disk0": {Read: 1000 * n, Written: 0}},
		Network: map[string]NetCount{"en0": {Received: 2000 * n, Sent: 0}},
		Errors:  map[string]error{},
	}
	if f.failNetwork {
		r.Network = nil
		r.Errors[MetricNetwork] = errors.New("netstat failed")
	}
	f.at = f.at.Add(f.step)
	return r
}

func (f *fakeSource) Battery(context.Context) (BatteryStats, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.batteries++
	if f.batteryErr != nil {
		return BatteryStats{}, f.batteryErr
	}
	return BatteryStats{Present: true, Percent: 80, State: BatteryDischarging}, nil
}

func (f *fakeSource) Info(ctx context.Context) SystemInfo {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.infos++
	return SystemInfo{OSName: "macOS", LogicalCores: 8}
}

func (f *fakeSource) counts() (reads, batteries int) {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.reads, f.batteries
}

func testSampler(t *testing.T, src Source, log *slog.Logger) *Sampler {
	t.Helper()
	cfg := Config{Interval: 5 * time.Millisecond, History: 5, BatteryEvery: time.Minute, TopN: 3}
	s, err := New(cfg, src, log)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func receive(t *testing.T, ch <-chan Vitals) Vitals {
	t.Helper()
	select {
	case v, ok := <-ch:
		if !ok {
			t.Fatal("channel closed")
		}
		return v
	case <-time.After(10 * time.Second):
		t.Fatal("no sample")
	}
	return Vitals{}
}

// waitStopped waits until every sampling goroutine has returned.
func waitStopped(t *testing.T, s *Sampler) {
	t.Helper()
	done := make(chan struct{})
	go func() {
		s.runs.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(10 * time.Second):
		t.Fatal("sampling did not stop")
	}
}

func TestSamplerLifecycle(t *testing.T) {
	src := newFakeSource()
	s := testSampler(t, src, nil)
	if reads, _ := src.counts(); reads != 0 {
		t.Fatal("New read the machine")
	}

	history, ch, unsubscribe := s.Subscribe()
	if len(history) != 0 {
		t.Fatalf("history before any sample: %d", len(history))
	}
	// The first sample already has rates: the run reads a baseline first.
	v := receive(t, ch)
	if v.CPU == nil || v.CPU.Percent != 25 || v.DiskIO == nil || v.DiskIO.ReadPerSec != 500 || v.Network == nil || v.Network.DownloadPerSec != 1000 {
		t.Fatalf("first sample %+v", v)
	}
	if v.Battery == nil || !v.Battery.Present {
		t.Fatalf("battery %+v", v.Battery)
	}

	// A second subscriber shares the run and gets the history so far.
	history2, ch2, unsubscribe2 := s.Subscribe()
	if len(history2) == 0 || !history2[0].At.Equal(v.At) {
		t.Fatalf("second subscriber's history %d", len(history2))
	}
	receive(t, ch2)
	unsubscribe2()
	unsubscribe2() // idempotent
	if _, ok := <-ch2; ok {
		t.Fatal("channel open after unsubscribe")
	}
	// Still sampling for the first subscriber.
	for range 6 {
		receive(t, ch)
	}

	unsubscribe()
	waitStopped(t, s)
	reads, _ := src.counts()
	src.mu.Lock()
	for _, ctx := range src.ctxs {
		if ctx.Err() == nil {
			t.Error("a read's context was not cancelled when sampling stopped")
		}
	}
	src.mu.Unlock()

	// History is bounded by Config.History and survives the stop.
	s.mu.Lock()
	kept := s.history.len()
	s.mu.Unlock()
	if kept != 5 {
		t.Fatalf("kept %d samples, want 5", kept)
	}

	time.Sleep(20 * time.Millisecond)
	if after, _ := src.counts(); after != reads {
		t.Fatalf("read %d more times after the last unsubscribe", after-reads)
	}

	// Coming back shows the history at once and starts sampling again.
	history3, ch3, unsubscribe3 := s.Subscribe()
	defer unsubscribe3()
	if len(history3) != 5 {
		t.Fatalf("history on return: %d", len(history3))
	}
	for i := 1; i < len(history3); i++ {
		if !history3[i].At.After(history3[i-1].At) {
			t.Fatal("history not oldest first")
		}
	}
	next := receive(t, ch3)
	if !next.At.After(history3[len(history3)-1].At) {
		t.Fatal("new sample older than the history")
	}
}

func TestSamplerDropsStaleHistory(t *testing.T) {
	src := newFakeSource()
	s := testSampler(t, src, nil)
	_, ch, unsubscribe := s.Subscribe()
	receive(t, ch)
	unsubscribe()
	waitStopped(t, s)

	// Pretend the kept samples are from long ago (more than History intervals).
	s.mu.Lock()
	vals := s.history.values()
	s.history.reset()
	for _, v := range vals {
		v.At = v.At.Add(-time.Hour)
		s.history.push(v)
	}
	s.mu.Unlock()

	history, _, unsubscribe := s.Subscribe()
	defer unsubscribe()
	if len(history) != 0 {
		t.Fatalf("stale history returned: %d samples", len(history))
	}
}

func TestSamplerLateRunDoesNotPublish(t *testing.T) {
	src := newFakeSource()
	s := testSampler(t, src, nil)
	_, _, unsubscribe := s.Subscribe()
	s.mu.Lock()
	oldGen := s.gen
	s.mu.Unlock()
	unsubscribe()
	waitStopped(t, s)

	_, ch, unsubscribe := s.Subscribe()
	defer unsubscribe()
	// A sample from the stopped run that arrives now (it was still reading when it was stopped) is dropped.
	marker := Vitals{At: t0}
	s.publish(oldGen, marker)
	s.mu.Lock()
	kept := s.history.values()
	s.mu.Unlock()
	for _, v := range append(kept, drain(ch)...) {
		if v.At.Equal(t0) {
			t.Fatal("a stopped run published")
		}
	}
}

func drain(ch <-chan Vitals) []Vitals {
	var out []Vitals
	for {
		select {
		case v := <-ch:
			out = append(out, v)
		default:
			return out
		}
	}
}

func TestSamplerBatteryCached(t *testing.T) {
	src := newFakeSource()
	s := testSampler(t, src, nil)
	_, ch, unsubscribe := s.Subscribe()
	for range 5 {
		if v := receive(t, ch); v.Battery == nil || v.Battery.Percent != 80 {
			t.Fatalf("battery %+v", v.Battery)
		}
	}
	unsubscribe()
	waitStopped(t, s)
	// The fake clock advances 2 s per reading and BatteryEvery is a minute: one battery read for all five samples.
	if _, batteries := src.counts(); batteries != 1 {
		t.Fatalf("battery read %d times", batteries)
	}

	// With a 10 s step, the reading is refreshed every 6 samples or so.
	src.mu.Lock()
	src.step = 10 * time.Second
	src.mu.Unlock()
	_, ch, unsubscribe = s.Subscribe()
	for range 14 {
		receive(t, ch)
	}
	unsubscribe()
	waitStopped(t, s)
	if _, batteries := src.counts(); batteries < 3 || batteries > 4 {
		t.Fatalf("battery read %d times in total, want 3 or 4", batteries)
	}
}

func TestSamplerMetricErrorsAreIndependentAndLoggedOnce(t *testing.T) {
	src := newFakeSource()
	src.failNetwork = true
	src.batteryErr = errors.New("ioreg failed")
	var buf bytes.Buffer
	var mu sync.Mutex
	log := slog.New(slog.NewTextHandler(&lockedWriter{w: &buf, mu: &mu}, nil))
	s := testSampler(t, src, log)
	_, ch, unsubscribe := s.Subscribe()
	for range 5 {
		v := receive(t, ch)
		if v.Network != nil || v.Battery != nil {
			t.Fatalf("failed metrics present: %+v", v)
		}
		if v.CPU == nil || v.Memory == nil || v.DiskIO == nil {
			t.Fatalf("working metrics missing: %+v", v)
		}
	}
	unsubscribe()
	waitStopped(t, s)
	mu.Lock()
	out := buf.String()
	mu.Unlock()
	if n := strings.Count(out, "metric=network"); n != 1 {
		t.Errorf("network failure logged %d times:\n%s", n, out)
	}
	if n := strings.Count(out, "metric=battery"); n != 1 {
		t.Errorf("battery failure logged %d times:\n%s", n, out)
	}
}

type lockedWriter struct {
	w  *bytes.Buffer
	mu *sync.Mutex
}

func (l *lockedWriter) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.w.Write(p)
}

func TestSamplerSlowSubscriberKeepsLatest(t *testing.T) {
	src := newFakeSource()
	s := testSampler(t, src, nil)
	_, ch, unsubscribe := s.Subscribe()
	defer unsubscribe()
	// Never reading while many samples are published must not block sampling.
	deadline := time.Now().Add(10 * time.Second)
	for {
		if reads, _ := src.counts(); reads > 3*subscriberBuffer {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("sampling blocked on a slow subscriber")
		}
		time.Sleep(time.Millisecond)
	}
	// Sampling goes on while the test reads, so draining until empty can pick up a sample published after the check; read only what was buffered at this moment.
	n := len(ch)
	got := make([]Vitals, 0, n)
	for range n {
		got = append(got, <-ch)
	}
	if len(got) == 0 || len(got) > subscriberBuffer {
		t.Fatalf("buffered %d samples", len(got))
	}
	for i := 1; i < len(got); i++ {
		if !got[i].At.After(got[i-1].At) {
			t.Fatal("buffered samples out of order")
		}
	}
}

func TestSamplerInfoCached(t *testing.T) {
	src := newFakeSource()
	s := testSampler(t, src, nil)
	cancelled, cancel := context.WithCancel(context.Background())
	cancel()
	s.Info(cancelled) // not cached: it may be incomplete
	for range 3 {
		if got := s.Info(context.Background()); got.OSName != "macOS" || got.LogicalCores != 8 {
			t.Fatalf("info %+v", got)
		}
	}
	src.mu.Lock()
	defer src.mu.Unlock()
	if src.infos != 2 {
		t.Fatalf("Info read %d times, want 2", src.infos)
	}
}

func TestNewValidates(t *testing.T) {
	if err := DefaultConfig().Validate(); err != nil {
		t.Fatal(err)
	}
	for i, mutate := range []func(*Config){
		func(c *Config) { c.Interval = 0 },
		func(c *Config) { c.History = 0 },
		func(c *Config) { c.BatteryEvery = -1 },
		func(c *Config) { c.TopN = 0 },
	} {
		cfg := DefaultConfig()
		mutate(&cfg)
		if _, err := New(cfg, newFakeSource(), nil); err == nil {
			t.Errorf("case %d accepted", i)
		}
	}
	if _, err := New(DefaultConfig(), nil, nil); err == nil {
		t.Error("nil source accepted")
	}
}
