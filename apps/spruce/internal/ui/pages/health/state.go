package health

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/engine/sysinfo"
	"spruce/apps/spruce/internal/ui/async"
)

// sampleEvery bounds how often samples reach the page: the sampler sends one every two seconds, but its history (up to HistoryLength samples) arrives at once and is drawn in one go.
const sampleEvery = 100 * time.Millisecond

// State is the 健康监测 page's controller: the system's static facts and the samples of the last two minutes. Make it in the page's component (NewState); sampling stops when the page goes, as the page's scope ends the watch. Its methods run on the engine goroutine.
type State struct {
	cx  *core.Scope
	ctx context.Context
	svc Service

	// Samples are the samples so far, oldest first: the history the sampler kept, then one every two seconds. Each value is a new slice.
	Samples *core.Signal[[]sysinfo.Vitals]
	// Info is the system's static facts, nil until read.
	Info *core.Signal[*sysinfo.SystemInfo]
	// Failed is set when the watch ended while the page was still there; Retry starts it again.
	Failed *core.Signal[bool]

	// ring is Samples' value, read and written only on the engine goroutine.
	ring []sysinfo.Vitals
	// watch counts the watches, so an old one that ends does not count as a failure of a newer one.
	watch int
}

// NewState makes the controller in cx, the page's scope, reads the static facts and starts watching.
func NewState(cx *core.Scope, svc Service) *State {
	s := &State{cx: cx, ctx: cx.Context(), svc: svc,
		Samples: core.NewSignalFunc[[]sysinfo.Vitals](cx, nil, nil),
		Info:    core.NewSignalFunc[*sysinfo.SystemInfo](cx, nil, nil),
		Failed:  core.NewSignal(cx, false),
	}
	async.Do(cx, s.ctx, func(ctx context.Context) (sysinfo.SystemInfo, error) { return svc.Info(ctx), nil },
		func(info sysinfo.SystemInfo, _ error) { s.Info.Set(&info) })
	s.start()
	return s
}

// Latest returns the newest sample, nil before the first; reading it subscribes.
func (s *State) Latest() *sysinfo.Vitals {
	samples := s.Samples.Get()
	if len(samples) == 0 {
		return nil
	}
	return &samples[len(samples)-1]
}

// Retry watches again after a failure, keeping the samples already drawn.
func (s *State) Retry() {
	if !s.Failed.GetUntracked() {
		return
	}
	s.Failed.Set(false)
	s.start()
}

// start watches the sampler until the page goes. The samples go through a batch, so the history arrives in one update; the ring is kept here on the engine goroutine, never on the watch's.
func (s *State) start() {
	s.watch++
	watch := s.watch
	samples := async.NewBatch(s.cx, sampleEvery, func(vs []sysinfo.Vitals) {
		ring := s.ring
		for _, v := range vs {
			ring = AppendSample(ring, v)
		}
		if len(ring) == len(s.ring) && (len(ring) == 0 || &ring[0] == &s.ring[0]) {
			return
		}
		s.ring = ring
		s.Samples.Set(ring)
	})
	async.Do(s.cx, s.ctx, func(ctx context.Context) (struct{}, error) {
		defer samples.Flush()
		s.svc.Watch(ctx, func(v sysinfo.Vitals) error {
			samples.Send(v)
			return ctx.Err()
		})
		return struct{}{}, nil
	}, func(struct{}, error) {
		// The page's scope ending cancels the watch and drops this callback; a watch that ends while the page is there failed.
		if watch == s.watch && s.ctx.Err() == nil {
			s.Failed.Set(true)
		}
	})
}
