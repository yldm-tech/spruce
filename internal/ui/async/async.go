package async

import (
	"context"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Do runs work with ctx on its own goroutine and posts done(result, err) to cx; done never runs once cx is disposed. ctx is the caller's (a controller's s.ctx or a run's child of it), never cx.Context() taken inside a handler.
func Do[T any](cx *core.Scope, ctx context.Context, work func(ctx context.Context) (T, error), done func(T, error)) {
	go func() {
		v, err := work(ctx)
		cx.Post(func() { done(v, err) })
	}()
}

// Subscribe forwards every value of ch to set on cx's goroutine until ctx ends or ch closes, then calls unsubscribe (when not nil).
func Subscribe[T any](cx *core.Scope, ctx context.Context, ch <-chan T, unsubscribe func(), set func(T)) {
	go func() {
		if unsubscribe != nil {
			defer unsubscribe()
		}
		for {
			select {
			case <-ctx.Done():
				return
			case v, ok := <-ch:
				if !ok {
					return
				}
				cx.Post(func() { set(v) })
			}
		}
	}()
}

// throttle is what Feed and Batch share: values gathered on any goroutine and handed to deliver on cx's goroutine, the first at once and the rest at most once per every, the last always (a trailing flush). The interval runs on the scope's clock (cx.AfterFunc), so nsgotest's ManualClock drives it; AfterFunc runs on the engine goroutine, so it is armed from inside a Post.
type throttle[T any] struct {
	cx      *core.Scope
	every   time.Duration
	deliver func(T)
	merge   func(acc T, v T) T

	mu      sync.Mutex
	pending T
	has     bool
	// window is set while a timer runs (or is about to be armed); values sent meanwhile wait for it.
	window bool
}

func (t *throttle[T]) send(v T) {
	t.mu.Lock()
	t.pending, t.has = t.merge(t.pending, v), true
	open := !t.window
	t.window = true
	t.mu.Unlock()
	if open {
		t.cx.Post(t.open)
	}
}

// take returns the pending value and clears it.
func (t *throttle[T]) take() (T, bool) {
	t.mu.Lock()
	defer t.mu.Unlock()
	v, has := t.pending, t.has
	var zero T
	t.pending, t.has = zero, false
	return v, has
}

// open runs on the engine goroutine: it delivers what waits and starts the window.
func (t *throttle[T]) open() {
	if v, ok := t.take(); ok {
		t.deliver(v)
	}
	t.arm()
}

// arm starts the timer that ends the window. A scope keeps every AfterFunc's cleanup until it is disposed, so each window's timer lives in a child scope of cx that goes as the window ends; armed on cx itself, a feed open for hours would keep one per window. tick runs with cx owning what it makes, as before.
func (t *throttle[T]) arm() {
	var w *core.Scope
	t.cx.Run(func() { w = t.cx.Child() })
	w.Run(func() {
		w.AfterFunc(t.every, func() {
			w.Dispose()
			t.cx.Run(t.tick)
		})
	})
}

// tick ends a window: it delivers what came during it and starts another, or closes.
func (t *throttle[T]) tick() {
	v, ok := t.take()
	if !ok {
		t.mu.Lock()
		// A value may have come between take and here; it then opens the next window itself.
		if t.has {
			t.mu.Unlock()
			t.open()
			return
		}
		t.window = false
		t.mu.Unlock()
		return
	}
	t.deliver(v)
	t.arm()
}

// flush posts what waits now, ahead of anything posted after it.
func (t *throttle[T]) flush() {
	if v, ok := t.take(); ok {
		t.cx.Post(func() { t.deliver(v) })
	}
}

// Feed is a goroutine-safe sink that keeps only the latest value and posts it to set at most once per every, always delivering the last value (trailing flush). Flush posts a pending value now; call it before the final result is posted.
type Feed[T any] struct{ t *throttle[T] }

// NewFeed returns a Feed delivering to set on cx's goroutine. Make it on the engine goroutine (in a component or a controller's constructor).
func NewFeed[T any](cx *core.Scope, every time.Duration, set func(T)) *Feed[T] {
	return &Feed[T]{&throttle[T]{cx: cx, every: every, deliver: set, merge: func(_, v T) T { return v }}}
}

// Send keeps v as the latest value; safe from any goroutine.
func (f *Feed[T]) Send(v T) { f.t.send(v) }

// Flush posts the pending value, if any, now.
func (f *Feed[T]) Flush() { f.t.flush() }

// Batch is a Feed that keeps every value sent since the last delivery and posts them together, in order.
type Batch[T any] struct{ t *throttle[[]T] }

// NewBatch returns a Batch delivering to flush on cx's goroutine. Make it on the engine goroutine.
func NewBatch[T any](cx *core.Scope, every time.Duration, flush func([]T)) *Batch[T] {
	return &Batch[T]{&throttle[[]T]{cx: cx, every: every, deliver: flush, merge: func(acc, v []T) []T { return append(acc, v...) }}}
}

// Send adds v to the pending batch; safe from any goroutine.
func (b *Batch[T]) Send(v T) { b.t.send([]T{v}) }

// Flush posts the pending batch, if any, now.
func (b *Batch[T]) Flush() { b.t.flush() }
