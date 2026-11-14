package shell

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Toast times: a message alone stays 6 seconds, one with 撤销 or 查看 10.
const (
	toastShort = 6 * time.Second
	toastLong  = 10 * time.Second
)

// Root is the window's root view: the sidebar and the page area over the page background, with the toast at the bottom of the page area, above the pages and below the dialogs. It shows the page of the navigator the app provides (nav.Key), which outlives the window, so a window opened again shows the page the last one did; it hides the system's title bar (the sidebar and the page headers drag the window) and reports the window's frame to keepFrame as it moves.
func Root(cx *core.Scope, block *store.Block, keepFrame func(core.WindowFrame)) core.Node {
	desktop.SetTitleBar(cx, desktop.TitleBarHidden, 0)
	navigator := nav.Use(cx)
	cx.Effect(func() {
		if f := desktop.Frame(cx); f.Width > 0 && keepFrame != nil {
			keepFrame(f)
		}
	})
	main := core.View{TestID: "main",
		Style: core.Style{FlexGrow: 999, FlexShrink: 1, FlexBasis: core.Pt(kit.MainMinWidth), MinWidth: core.Pt(0), Overflow: core.Hidden,
			Position: core.Relative},
		Children: []core.Node{
			core.Dynamic(func(cx *core.Scope) core.Node {
				// core.Func mounts the page untracked: what the page reads as it is made does not remake it.
				return core.Func(pages[navigator.Current()])
			}),
			toastLayer(cx, store.Use(cx), navigator),
		}}
	return kit.PageBackground{Children: []core.Node{Sidebar{Nav: navigator, Block: block}, main}}
}

// toastLayer shows the stores' toast centred at the bottom of the page area, a new kit.Toast for every message so its time starts again. 撤销 puts the run's items back through 概览's controller and then says how it went; 查看 opens the run in 操作历史.
func toastLayer(cx *core.Scope, stores *store.Stores, navigator *nav.Navigator) core.Node {
	if stores == nil {
		return core.Group{}
	}
	toasts := stores.Toasts
	undoing := core.NewSignal(cx, false)
	undo := func(runID string) {
		if undoing.GetUntracked() {
			return
		}
		undoing.Set(true)
		stores.Overview.Undo(cx, runID, func(out app.RestoreOutcome, err error) {
			undoing.Set(false)
			stores.Trash.Bump()
			stores.Locations.Bump()
			toasts.Show(undoToast(out, err))
		})
	}
	return core.View{PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(24), AlignItems: core.AlignCenter,
			Padding: core.Symmetric(0, 24), ZIndex: 10},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			t := toasts.Current()
			if t == nil {
				return core.Group{}
			}
			text := func() string {
				if t.UndoRun != "" && undoing.Get() {
					return "正在放回…"
				}
				return t.Text
			}
			var actions []kit.ToastAction
			if t.UndoRun != "" {
				runID := t.UndoRun
				actions = append(actions, kit.ToastAction{Text: "撤销", TestID: "toast-undo", Busy: undoing.Get, OnPress: func() { undo(runID) }})
			}
			if t.ViewRun != "" {
				runID := t.ViewRun
				actions = append(actions, kit.ToastAction{Text: "查看", TestID: "toast-details", OnPress: func() { navigator.GoHistory("", runID) }})
			}
			return kit.Toast{Text: text, Actions: actions, Duration: t.Duration, OnClose: toasts.Dismiss}
		})}}
}

// undoToast is the toast after a 撤销: all put back, some put back with 查看 to read why the rest was not, or the error.
func undoToast(out app.RestoreOutcome, err error) store.Toast {
	switch {
	case err != nil:
		return store.Toast{Text: err.Error(), Duration: toastShort}
	case out.Failed == 0:
		return store.Toast{Text: fmt.Sprintf("已放回原处 %d 项", out.Restored), Duration: toastShort}
	}
	return store.Toast{Text: fmt.Sprintf("已放回 %d 项，%d 项没能放回", out.Restored, out.Failed), ViewRun: out.RunID, Duration: toastLong}
}
