package settings

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"

	enginesettings "spruce/apps/spruce/internal/engine/settings"
	"spruce/apps/spruce/internal/ui/store"
)

// saveDebounce is how long a slider or a number field waits after the last change before it saves.
const saveDebounce = 400 * time.Millisecond

// setting connects one control to one field of the settings: change shows the value at once and saves it (after debounce without another change, for the sliders and the number fields; a pending save is sent when the control goes), then reports a failure or an adjustment Go made. Only the answer to the control's latest save counts. It lives in the control's scope; every method runs on the engine goroutine.
type setting[T any] struct {
	cx       *core.Scope
	st       *store.Settings
	field    Field[T]
	debounce time.Duration
	equal    func(a, b T) bool
	adjusted func(stored T) string

	// draft is the value being saved, shown instead of the stored one; nil when none is.
	draft *core.Signal[*T]
	// err is the sentence the last save failed with, and failure that error, for a control that offers a way out of it; note says what Go stored instead of what was sent.
	err, note *core.Signal[string]
	failure   *core.Signal[error]
	// failed is the value the failed save could not store.
	failed T

	pending *T
	// timer holds the debounce's timer while a save waits; it is disposed as the save goes out, since the control's scope would otherwise keep every change's timer until the control goes.
	timer *core.Scope
	seq   int
}

func newSetting[T any](cx *core.Scope, st *store.Settings, field Field[T], debounce time.Duration, equal func(a, b T) bool, adjusted func(T) string) *setting[T] {
	s := &setting[T]{cx: cx, st: st, field: field, debounce: debounce, equal: equal, adjusted: adjusted,
		draft: core.NewSignal[*T](cx, nil), err: core.NewSignal(cx, ""), note: core.NewSignal(cx, ""), failure: core.NewSignal[error](cx, nil)}
	// A save still waiting for its debounce goes out as the control goes (the page is left, or 恢复默认设置 redraws it).
	cx.Cleanup(s.flush)
	// A failure is over once what it could not store is stored after all: the user allowed Spruce in Login Items and the settings were read again.
	cx.Effect(func() {
		if s.failure.Get() != nil && equal(field.Get(st.Value()), s.failed) {
			s.err.Set("")
			s.failure.Set(nil)
		}
	})
	return s
}

// value is what the control shows: the value being saved, else the stored one. Reading it subscribes.
func (s *setting[T]) value() T {
	if d := s.draft.Get(); d != nil {
		return *d
	}
	return s.field.Get(s.st.Value())
}

// change shows v and saves it, at once or after the debounce.
func (s *setting[T]) change(v T) {
	s.draft.Set(&v)
	s.err.Set("")
	s.failure.Set(nil)
	s.note.Set("")
	if s.debounce <= 0 {
		s.commit(v)
		return
	}
	s.pending = &v
	s.stopTimer()
	// The timer belongs to the control's scope, whichever branch the handler runs in.
	var timer *core.Scope
	s.cx.Run(func() { timer = s.cx.Child() })
	timer.Run(func() { timer.AfterFunc(s.debounce, func() { s.cx.Run(s.flush) }) })
	s.timer = timer
}

// stopTimer drops the waiting debounce, if any.
func (s *setting[T]) stopTimer() {
	if s.timer != nil {
		s.timer.Dispose()
		s.timer = nil
	}
}

// drop forgets a change that has not gone out (text in a number field that is no longer a valid number): its waiting save and its draft, so a slip such as 18 on the way from 180 to 1 is never saved.
func (s *setting[T]) drop() {
	s.stopTimer()
	s.pending = nil
	s.draft.Set(nil)
}

// flush sends a debounced save now, as the control loses the focus or goes.
func (s *setting[T]) flush() {
	s.stopTimer()
	p := s.pending
	s.pending = nil
	if p != nil {
		s.commit(*p)
	}
}

func (s *setting[T]) commit(v T) {
	s.seq++
	seq := s.seq
	next := s.field.Put(core.Untracked(s.cx, s.st.Value), v)
	s.st.Save(next, func(saved enginesettings.AppSettings, err error) {
		// A newer change is on its way, or the control is gone: that decides what to show.
		if seq != s.seq || s.pending != nil || s.cx.Disposed() {
			return
		}
		s.draft.Set(nil)
		if err != nil {
			s.failed = v
			s.err.Set(err.Error())
			s.failure.Set(err)
			return
		}
		if stored := s.field.Get(saved); !s.equal(stored, v) {
			s.note.Set(s.adjusted(stored))
		}
	})
}
