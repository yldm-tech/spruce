package store

import (
	"context"
	"errors"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/growth"
	"github.com/yldm-tech/spruce/internal/ui/async"
)

// OverviewService is 概览's controller: *app.Overview.
type OverviewService interface {
	State() app.OverviewState
	Subscribe() (<-chan app.OverviewState, func())
	Notices() (<-chan app.Notice, func())
	CheckNow() error
	StopCheck()
	Clean(ctx context.Context, req cleanup.CleanRequest, origin app.CleanOrigin, progress func(cleanup.Progress)) (app.OverviewOutcome, error)
	Undo(ctx context.Context, runID string) (app.RestoreOutcome, error)
	Growth(ctx context.Context, window time.Duration) ([]growth.Growth, error)
	TrashInfo(ctx context.Context) (app.TrashInfo, error)
}

// errNoOverview is what every action answers while no controller is wired (Deps.Overview nil).
var errNoOverview = errors.New("概览还没有接入")

// Overview mirrors the controller's state as a signal and runs its actions off the engine goroutine. Without a controller (Deps.Overview nil: tests of other pages, and the app until the controller is wired) the state stays OverviewState{Phase: "idle"}, which 概览 shows as 检查已停止, and every action answers 「概览还没有接入」.
type Overview struct {
	cx  *core.Scope
	ctx context.Context
	svc OverviewService
	// state is the controller's latest state.
	state *core.Signal[app.OverviewState]
	// busy is set while a Clean or an Undo started through the store runs: one thing at a time across 概览, the drill-down views and the toast.
	busy *core.Signal[bool]
	// undone is the last Undo that put something back.
	undone *core.Signal[Undone]
	// undid are the runs of every Undo that put something back, for Undid.
	undid map[string]bool
}

// Undone is an Undo through the store that put at least one item back: the run it undid, and how many such undos came before it plus one, so a reader tells two undos of one run apart.
type Undone struct {
	RunID string
	Seq   int
}

func newOverview(cx *core.Scope, ctx context.Context, svc OverviewService) *Overview {
	o := &Overview{cx: cx, ctx: ctx, svc: svc, state: core.NewSignalFunc(cx, app.OverviewState{Phase: app.OverviewIdle}, nil), busy: core.NewSignal(cx, false),
		undone: core.NewSignal(cx, Undone{}), undid: map[string]bool{}}
	if svc == nil {
		return o
	}
	// The subscription starts first, and a broadcast always wins over the first answer, which may be older.
	broadcast := false
	ch, unsubscribe := svc.Subscribe()
	async.Subscribe(cx, ctx, ch, unsubscribe, func(st app.OverviewState) {
		broadcast = true
		o.state.Set(st)
	})
	async.Do(cx, ctx, func(context.Context) (app.OverviewState, error) { return svc.State(), nil }, func(st app.OverviewState, _ error) {
		if !broadcast {
			o.state.Set(st)
		}
	})
	return o
}

// State returns the controller's state; reading it subscribes.
func (o *Overview) State() app.OverviewState { return o.state.Get() }

// Busy reports whether a Clean or an Undo started through the store runs; reading it subscribes.
func (o *Overview) Busy() bool { return o.busy.Get() }

// Connected reports whether a controller is wired.
func (o *Overview) Connected() bool { return o.svc != nil }

// Notices returns the controller's notifications for the shell, and the function that stops them; nil and a function that does nothing without a controller.
func (o *Overview) Notices() (<-chan app.Notice, func()) {
	if o.svc == nil {
		return nil, func() {}
	}
	return o.svc.Notices()
}

// CheckNow starts a foreground check and calls done with the error it was refused with, a sentence.
func (o *Overview) CheckNow(done func(error)) {
	if o.svc == nil {
		if done != nil {
			o.cx.Post(func() { done(errNoOverview) })
		}
		return
	}
	async.Do(o.cx, o.ctx, func(context.Context) (struct{}, error) { return struct{}{}, o.svc.CheckNow() }, func(_ struct{}, err error) {
		if done != nil {
			done(err)
		}
	})
}

// StopCheck stops a running check.
func (o *Overview) StopCheck() {
	if o.svc != nil {
		go o.svc.StopCheck()
	}
}

// Clean cleans req of the current result for origin. It runs on the app's context, so leaving the page does not stop it; the function it returns stops it after the current item (停止). progress and done run in cx, the caller's scope, on the engine goroutine, and are dropped once cx is disposed; outcome, when set, runs in the app's scope before done whatever happened to cx, for what must not be lost with the page (the toast with 撤销). Busy ends either way.
func (o *Overview) Clean(cx *core.Scope, req cleanup.CleanRequest, origin app.CleanOrigin, progress func(cleanup.Progress), done, outcome func(app.OverviewOutcome, error)) (stop func()) {
	if o.svc == nil {
		refuse(cx, done, app.OverviewOutcome{})
		refuse(o.cx, outcome, app.OverviewOutcome{})
		return func() {}
	}
	ctx, cancel := context.WithCancel(o.ctx)
	o.busy.Set(true)
	go func() {
		defer cancel()
		out, err := o.svc.Clean(ctx, req, origin, func(p cleanup.Progress) {
			if progress != nil {
				cx.Post(func() { progress(p) })
			}
		})
		o.cx.Post(func() {
			o.busy.Set(false)
			if outcome != nil {
				outcome(out, err)
			}
		})
		if done != nil {
			cx.Post(func() { done(out, err) })
		}
	}()
	return cancel
}

// Undo puts back what the run runID moved to the Trash. It runs with the app's context, so leaving the page does not stop it; done runs in cx and is dropped once cx is disposed, outcome (when set) runs in the app's scope before it whatever happened to cx.
func (o *Overview) Undo(cx *core.Scope, runID string, done, outcome func(app.RestoreOutcome, error)) {
	if o.svc == nil {
		refuse(cx, done, app.RestoreOutcome{})
		refuse(o.cx, outcome, app.RestoreOutcome{})
		return
	}
	o.busy.Set(true)
	go func() {
		out, err := o.svc.Undo(o.ctx, runID)
		o.cx.Post(func() {
			o.busy.Set(false)
			if err == nil && out.Restored > 0 {
				o.undid[runID] = true
				o.undone.Set(Undone{RunID: runID, Seq: o.undone.GetUntracked().Seq + 1})
			}
			if outcome != nil {
				outcome(out, err)
			}
		})
		if done != nil {
			cx.Post(func() { done(out, err) })
		}
	}()
}

// LastUndo returns the last Undo that put something back, zero before any; reading it subscribes. 空间透视 scans again when it undid one of its moves.
func (o *Overview) LastUndo() Undone { return o.undone.Get() }

// Undid reports whether an Undo of runID put something back since the app started, also while no page that cares was showing; it does not subscribe.
func (o *Overview) Undid(runID string) bool { return o.undid[runID] }

// Growth lists what grew over window, with ctx the caller's; done runs in cx.
func (o *Overview) Growth(cx *core.Scope, ctx context.Context, window time.Duration, done func([]growth.Growth, error)) {
	if o.svc == nil {
		refuse(cx, done, []growth.Growth(nil))
		return
	}
	async.Do(cx, ctx, func(ctx context.Context) ([]growth.Growth, error) { return o.svc.Growth(ctx, window) }, done)
}

// TrashInfo measures the Trash now, with ctx the caller's; done runs in cx.
func (o *Overview) TrashInfo(cx *core.Scope, ctx context.Context, done func(app.TrashInfo, error)) {
	if o.svc == nil {
		refuse(cx, done, app.TrashInfo{})
		return
	}
	async.Do(cx, ctx, o.svc.TrashInfo, done)
}

// refuse calls done with zero and errNoOverview in cx, later, as a service's answer would come.
func refuse[T any](cx *core.Scope, done func(T, error), zero T) {
	if done != nil {
		cx.Post(func() { done(zero, errNoOverview) })
	}
}
