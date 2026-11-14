package sysinfo

import (
	"context"
	"errors"
	"log/slog"
	"maps"
	"sync"
	"time"
)

// Config tunes the sampler.
type Config struct {
	// Interval is the time between samples; the first comes Interval/2 after sampling starts.
	Interval time.Duration
	// History is how many samples are kept for a page that subscribes later.
	History int
	// BatteryEvery is how often the battery is read; samples in between reuse the last reading.
	BatteryEvery time.Duration
	// TopN is the length of each process list.
	TopN int
}

// DefaultConfig samples every 2 s, keeps 2 minutes of history, reads the battery every 30 s and lists 8 processes.
func DefaultConfig() Config {
	return Config{Interval: 2 * time.Second, History: 60, BatteryEvery: 30 * time.Second, TopN: 8}
}

// Validate reports a configuration the sampler cannot use.
func (c Config) Validate() error {
	switch {
	case c.Interval <= 0:
		return errors.New("sysinfo: Interval must be positive")
	case c.History < 1:
		return errors.New("sysinfo: History must be at least 1")
	case c.BatteryEvery < 0:
		return errors.New("sysinfo: BatteryEvery must not be negative")
	case c.TopN < 1:
		return errors.New("sysinfo: TopN must be at least 1")
	}
	return nil
}

// subscriberBuffer is how many samples a subscriber may fall behind before it loses the oldest.
const subscriberBuffer = 4

// Sampler samples the machine while anyone subscribes.
type Sampler struct {
	cfg Config
	src Source
	log *slog.Logger

	mu      sync.Mutex
	history *ring[Vitals]
	subs    map[chan Vitals]struct{}
	// gen counts sampling runs; a run publishes only while it is the current one, so a run that is still finishing a read after it was stopped never adds to the history of the next.
	gen  uint64
	stop context.CancelFunc
	// The battery reading samples reuse until it is BatteryEvery old.
	battery   *BatteryStats
	batteryAt time.Time
	// failing holds the error text of each metric that failed in the latest reading, so a persistent failure is logged once.
	failing map[string]string
	// runs counts sampling goroutines that have not returned (tests wait on it).
	runs sync.WaitGroup

	infoMu sync.Mutex
	info   *SystemInfo
}

// New returns a Sampler over src with cfg. It takes no samples until the first Subscribe.
func New(cfg Config, src Source, log *slog.Logger) (*Sampler, error) {
	if err := cfg.Validate(); err != nil {
		return nil, err
	}
	if src == nil {
		return nil, errors.New("sysinfo: nil Source")
	}
	if log == nil {
		log = slog.Default()
	}
	return &Sampler{
		cfg:     cfg,
		src:     src,
		log:     log.With("component", "sysinfo"),
		history: newRing[Vitals](cfg.History),
		subs:    make(map[chan Vitals]struct{}),
		failing: make(map[string]string),
	}, nil
}

// Subscribe starts sampling if nobody else listens and returns the samples kept from earlier (oldest first), a channel that receives every later sample, and a function that unsubscribes and closes the channel. Sampling stops when the last subscriber leaves; the history stays for the next one unless it is older than History intervals by then. A subscriber more than a few samples behind loses the oldest ones.
func (s *Sampler) Subscribe() (history []Vitals, samples <-chan Vitals, unsubscribe func()) {
	ch := make(chan Vitals, subscriberBuffer)
	s.mu.Lock()
	if s.stop == nil {
		s.start()
	}
	s.subs[ch] = struct{}{}
	history = s.history.values()
	s.mu.Unlock()

	var once sync.Once
	return history, ch, func() {
		once.Do(func() {
			s.mu.Lock()
			defer s.mu.Unlock()
			delete(s.subs, ch)
			close(ch)
			if len(s.subs) == 0 && s.stop != nil {
				s.stop()
				s.stop = nil
				s.gen++
			}
		})
	}
}

// start begins a sampling run; s.mu is held.
func (s *Sampler) start() {
	if last, ok := s.history.last(); ok && time.Since(last.At) > time.Duration(s.cfg.History)*s.cfg.Interval {
		// Every kept sample is older than the window the page draws; it would only draw a stale line.
		s.history.reset()
	}
	s.gen++
	ctx, cancel := context.WithCancel(context.Background())
	s.stop = cancel
	s.runs.Add(1)
	go s.run(ctx, s.gen)
}

// run reads a baseline, then a sample Interval/2 later and every Interval after that, until ctx is done.
func (s *Sampler) run(ctx context.Context, gen uint64) {
	defer s.runs.Done()
	prev := s.src.Read(ctx)
	s.noteErrors(prev.Errors)
	timer := time.NewTimer(s.cfg.Interval / 2)
	defer timer.Stop()
	for {
		select {
		case <-ctx.Done():
			return
		case <-timer.C:
		}
		// Reset before reading, so the cadence does not drift by the time a reading takes.
		timer.Reset(s.cfg.Interval)
		cur := s.src.Read(ctx)
		if ctx.Err() != nil {
			return
		}
		v := derive(&prev, cur, s.cfg.TopN)
		errs := cur.Errors
		v.Battery, errs = s.readBattery(ctx, cur.At, errs)
		s.noteErrors(errs)
		prev = cur
		s.publish(gen, v)
	}
}

// readBattery returns the battery reading for a sample taken at now, reading it again when the cached one is BatteryEvery old; a failure is added to errs.
func (s *Sampler) readBattery(ctx context.Context, now time.Time, errs map[string]error) (*BatteryStats, map[string]error) {
	s.mu.Lock()
	cached, at := s.battery, s.batteryAt
	s.mu.Unlock()
	if !at.IsZero() && now.Sub(at) < s.cfg.BatteryEvery && now.Sub(at) >= 0 {
		if cached == nil {
			// Still failing as of the last read: keep it logged as failing.
			errs = withError(errs, MetricBattery, errBatteryStale)
		}
		return cached, errs
	}
	b, err := s.src.Battery(ctx)
	var out *BatteryStats
	if err != nil {
		errs = withError(errs, MetricBattery, err)
	} else {
		out = &b
	}
	s.mu.Lock()
	s.battery, s.batteryAt = out, now
	s.mu.Unlock()
	return out, errs
}

// errBatteryStale stands in for the last battery error between reads, so noteErrors neither forgets nor re-logs it.
var errBatteryStale = errors.New("sysinfo: battery failed at its last read")

func withError(errs map[string]error, metric string, err error) map[string]error {
	out := make(map[string]error, len(errs)+1)
	maps.Copy(out, errs)
	out[metric] = err
	return out
}

// noteErrors logs each metric that starts failing, or fails differently, and forgets the ones that recovered.
func (s *Sampler) noteErrors(errs map[string]error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	for metric := range s.failing {
		if errs[metric] == nil {
			delete(s.failing, metric)
		}
	}
	for metric, err := range errs {
		if err == nil || errors.Is(err, errBatteryStale) {
			continue
		}
		if text := err.Error(); s.failing[metric] != text {
			s.failing[metric] = text
			s.log.Warn("cannot read metric", "metric", metric, "err", err)
		}
	}
}

// publish keeps v and sends it to every subscriber, if run gen is still the current one.
func (s *Sampler) publish(gen uint64, v Vitals) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if gen != s.gen {
		return
	}
	s.history.push(v)
	for ch := range s.subs {
		offer(ch, v)
	}
}

// offer sends v, dropping the oldest buffered sample when ch is full. Only publish sends, under s.mu, so the send after dropping cannot block.
func offer(ch chan Vitals, v Vitals) {
	select {
	case ch <- v:
		return
	default:
	}
	select {
	case <-ch:
	default:
	}
	ch <- v
}

// Info returns the static facts, read on the first call that is not cancelled and cached after it.
func (s *Sampler) Info(ctx context.Context) SystemInfo {
	s.infoMu.Lock()
	defer s.infoMu.Unlock()
	if s.info != nil {
		return *s.info
	}
	info := s.src.Info(ctx)
	if ctx.Err() == nil {
		s.info = &info
	}
	return info
}
