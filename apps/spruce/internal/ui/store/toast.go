package store

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
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

// UndoneToast is the toast after a 撤销, from the toast or 应用卸载's done card: all put back, some put back with 查看 to read why the rest was not, or the error.
func UndoneToast(out app.RestoreOutcome, err error) Toast {
	switch {
	case err != nil:
		return Toast{Text: err.Error(), Duration: 6 * time.Second}
	case out.Failed == 0:
		return Toast{Text: fmt.Sprintf("已放回原处 %d 项", out.Restored), Duration: 6 * time.Second}
	}
	return Toast{Text: fmt.Sprintf("已放回 %d 项，%d 项没能放回", out.Restored, out.Failed), ViewRun: out.RunID, Duration: 10 * time.Second}
}
