package health

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// New returns a Monitor sampling src with cfg (validated with cfg.Validate). It does nothing until Run.
func New(cfg Config, src ProcessSource) (Monitor, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errors.New("health: nil ProcessSource")
	}
	return newMonitor(cfg, src), nil
}

// procKey tells a reused PID from the process that had it before.
type procKey struct {
	pid     int32
	created int64 // CreateTime in Unix nanoseconds
}

type cpuSample struct {
	at  time.Time
	cpu time.Duration
}

// procState is the history of one process inside the window.
type procState struct {
	// samples are oldest first and never older than Window before the newest one.
	samples []cpuSample
	// hotSince is the sample at which the current hot period began; zero while the process is not hot.
	hotSince time.Time
}

type monitor struct {
	cfg    Config
	src    ProcessSource
	log    *slog.Logger
	ignore map[string]struct{}
	// self is the app's own PID, which never alerts.
	self int32

	running atomic.Bool

	// Owned by the sampling goroutine.
	procs   map[procKey]*procState
	lastAt  time.Time
	failing bool

	mu     sync.Mutex
	alerts []Alert
	sig    string
	subs   map[chan []Alert]struct{}
}

func newMonitor(cfg Config, src ProcessSource) *monitor {
	ignore := make(map[string]struct{}, len(cfg.Ignore))
	for _, n := range cfg.Ignore {
		ignore[n] = struct{}{}
	}
	return &monitor{
		cfg:    cfg,
		src:    src,
		log:    slog.Default().With("component", "health"),
		ignore: ignore,
		self:   int32(os.Getpid()),
		procs:  make(map[procKey]*procState),
		alerts: []Alert{},
		subs:   make(map[chan []Alert]struct{}),
	}
}

// Run samples immediately and then every Interval until ctx is done. Only one Run may be active at a time.
func (m *monitor) Run(ctx context.Context) error {
	if !m.running.CompareAndSwap(false, true) {
		return errors.New("health: monitor is already running")
	}
	defer m.running.Store(false)

	ticker := time.NewTicker(m.cfg.Interval)
	defer ticker.Stop()
	for {
		m.sample(ctx)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-ticker.C:
		}
	}
}

// sample takes one snapshot and folds it in. A failed snapshot is logged once per run of failures and skipped.
func (m *monitor) sample(ctx context.Context) {
	snap, err := m.src.Sample(ctx)
	if err != nil {
		if ctx.Err() == nil && !m.failing {
			m.log.Warn("process sample failed", "err", err)
		}
		m.failing = true
		return
	}
	m.failing = false
	m.observe(snap)
}

// observe folds one snapshot into the per-process history and publishes the resulting alerts.
func (m *monitor) observe(snap Snapshot) {
	if !m.lastAt.IsZero() && !snap.At.After(m.lastAt) {
		m.log.Debug("dropping snapshot not later than the previous one", "at", snap.At, "previous", m.lastAt)
		return
	}
	m.lastAt = snap.At

	seen := make(map[procKey]struct{}, len(snap.Processes))
	var alerts []Alert
	for _, ps := range snap.Processes {
		if ps.PID == m.self {
			continue
		}
		if _, ok := m.ignore[ps.Name]; ok {
			continue
		}
		key := procKey{pid: ps.PID, created: ps.CreateTime.UnixNano()}
		if _, dup := seen[key]; dup {
			continue
		}
		seen[key] = struct{}{}

		st := m.procs[key]
		if st == nil {
			st = &procState{}
			m.procs[key] = st
		}
		pct, ok := st.add(snap.At, ps.CPUTime, m.cfg.Window)
		if !ok {
			st.hotSince = time.Time{}
			continue
		}
		bar := m.cfg.Threshold
		if !st.hotSince.IsZero() {
			bar -= m.cfg.Hysteresis
		}
		if pct < bar {
			st.hotSince = time.Time{}
			continue
		}
		if st.hotSince.IsZero() {
			st.hotSince = snap.At
		}
		if d := snap.At.Sub(st.hotSince); d >= m.cfg.Sustain {
			alerts = append(alerts, Alert{
				PID:        ps.PID,
				Name:       ps.Name,
				CPUPercent: pct,
				Since:      st.hotSince,
				Duration:   d,
				Threshold:  m.cfg.Threshold,
			})
		}
	}
	for key := range m.procs {
		if _, ok := seen[key]; !ok {
			delete(m.procs, key)
		}
	}
	slices.SortFunc(alerts, func(a, b Alert) int {
		if c := cmp.Compare(b.CPUPercent, a.CPUPercent); c != 0 {
			return c
		}
		return cmp.Compare(a.PID, b.PID)
	})
	m.publish(alerts)
}

// add records a sample, drops the ones that fell out of the window and returns the average CPU percent (of one logical CPU) across the window; ok is false until there are two samples.
func (st *procState) add(at time.Time, cpu time.Duration, window time.Duration) (pct float64, ok bool) {
	if n := len(st.samples); n > 0 && cpu < st.samples[n-1].cpu {
		// Cumulative CPU time never decreases for one process; start over rather than report a negative rate.
		st.samples = st.samples[:0]
	}
	st.samples = append(st.samples, cpuSample{at: at, cpu: cpu})
	drop := 0
	for drop < len(st.samples)-1 && at.Sub(st.samples[drop].at) > window {
		drop++
	}
	if drop > 0 {
		st.samples = append(st.samples[:0], st.samples[drop:]...)
	}
	if len(st.samples) < 2 {
		return 0, false
	}
	first, last := st.samples[0], st.samples[len(st.samples)-1]
	return float64(last.cpu-first.cpu) / float64(last.at.Sub(first.at)) * 100, true
}

// publish stores alerts and sends them to every subscriber when they differ in a way the banner shows.
func (m *monitor) publish(alerts []Alert) {
	if alerts == nil {
		alerts = []Alert{}
	}
	sig := signature(alerts)
	m.mu.Lock()
	defer m.mu.Unlock()
	m.alerts = alerts
	if sig == m.sig {
		return
	}
	m.sig = sig
	for ch := range m.subs {
		offer(ch, slices.Clone(alerts))
	}
}

// signature captures what the banner shows of an alert list: which processes, their rounded percent and whole minutes.
func signature(alerts []Alert) string {
	var b strings.Builder
	for _, a := range alerts {
		fmt.Fprintf(&b, "%d/%d/%s/%d/%d;", a.PID, a.Since.UnixNano(), a.Name, int64(math.Round(a.CPUPercent)), int64(a.Duration/time.Minute))
	}
	return b.String()
}

// offer replaces whatever ch holds with v. Only publishers send, under m.mu, so the send after draining cannot block.
func offer(ch chan []Alert, v []Alert) {
	select {
	case <-ch:
	default:
	}
	ch <- v
}

// Alerts returns the current alerts, most CPU first.
func (m *monitor) Alerts() []Alert {
	m.mu.Lock()
	defer m.mu.Unlock()
	return slices.Clone(m.alerts)
}

// Subscribe returns a channel holding the current alert list and then every changed list, and a function that unsubscribes and closes the channel.
func (m *monitor) Subscribe() (<-chan []Alert, func()) {
	ch := make(chan []Alert, 1)
	m.mu.Lock()
	m.subs[ch] = struct{}{}
	ch <- slices.Clone(m.alerts)
	m.mu.Unlock()

	var once sync.Once
	return ch, func() {
		once.Do(func() {
			m.mu.Lock()
			delete(m.subs, ch)
			close(ch)
			m.mu.Unlock()
		})
	}
}
