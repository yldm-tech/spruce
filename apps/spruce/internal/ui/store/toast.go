package store

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Toast is one message of the toast at the bottom of the main area: after a move to the Trash 「已移到废纸篓 3.1 GB」 with 撤销, after a clean that left items 查看.
type Toast struct {
	Text string
	// UndoRun, when set, shows 撤销, which puts back what that history run moved (macOS).
	UndoRun string
	// ViewRun, when set, shows 查看, which opens that run in 操作历史.
	ViewRun string
	// Duration is how long it stays: 6 s, or 10 s with 撤销 or 查看.
	Duration time.Duration
}

// Toasts holds the one toast the shell shows, shared by 概览, the drill-down views, 空间透视 and the automatic rules; a new one replaces the one shown, whose 撤销 stays possible in 操作历史.
type Toasts struct {
	current *core.Signal[*Toast]
}

func newToasts(cx *core.Scope) *Toasts {
	return &Toasts{current: core.NewSignal[*Toast](cx, nil)}
}

// Show replaces the toast with x; showing the same message again starts its time again.
func (t *Toasts) Show(x Toast) { t.current.Set(&x) }

// Current returns the toast shown, nil for none; reading it subscribes.
func (t *Toasts) Current() *Toast { return t.current.Get() }

// Dismiss takes the toast away.
func (t *Toasts) Dismiss() { t.current.Set(nil) }
