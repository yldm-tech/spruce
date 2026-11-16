package shell

import (
	"fmt"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/store"
)

// Toast times: a message alone stays 6 seconds, one with 撤销 or 查看 10.
const (
	toastShort = 6 * time.Second
	toastLong  = 10 * time.Second
)

// Root is the window's root view: the sidebar and the page area over the page background, with the toast at the bottom of the page area, above the pages and below the dialogs. It shows the page of the navigator the app provides (nav.Key), which outlives the window, so a window opened again shows the page the last one did; it hides the system's title bar (the sidebar and the page headers drag the window) and reports the window's frame to keepFrame as it moves. Escape (back) leaves a drill-down view unless an overlay holds the sidebar. While asking is set it asks before quitting during a run, whose 停止并退出 calls quit.
func Root(cx *core.Scope, block *store.Block, keepFrame func(core.WindowFrame), asking *core.Signal[bool], quit func()) core.Node {
	desktop.SetTitleBar(cx, desktop.TitleBarHidden, 0)
	navigator := nav.Use(cx)
	// The dialogs and overlays, modal layers, take back first.
	cx.OnBackPress(func() bool {
		if block.Blocked() || navigator.Current().Meta().Parent == "" {
			return false
		}
		navigator.Back()
		return true
	})
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
				// core.Func mounts the page untracked: what the page reads as it is made does not remake it. Each page comes in rising as it is shown.
				return kit.Appear{Entrance: kit.EnterRise, TestID: "page-area",
					Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), MinWidth: core.Pt(0), Position: core.Relative},
					Children: []core.Node{core.Func(pages[navigator.Current()])}}
			}),
			toastLayer(cx, store.Use(cx), navigator),
		}}
	return kit.PageBackground{Children: []core.Node{Sidebar{Nav: navigator, Block: block}, main, quitConfirm(asking, quit)}}
}

// quitConfirm asks before Spruce quits while a run is active: 继续清理, which has the focus, keeps it running; 停止并退出 calls quit, and the run stops after its current item.
func quitConfirm(asking *core.Signal[bool], quit func()) core.Node {
	return kit.ConfirmDialog{Open: asking.Get, Title: "正在清理", Lines: []string{"现在退出，清理会在当前项目完成后停止。"},
		ConfirmLabel: "停止并退出", CancelLabel: "继续清理", Danger: true, OnConfirm: quit, OnCancel: func() { asking.Set(false) }, TestID: "quit-confirm"}
}

// toastLayer shows the stores' toast centred at the bottom of the page area, a new kit.Toast for every message so its time starts again. 撤销 puts the run's items back through 概览's controller and then says how it went; 查看 opens the run in 操作历史.
func toastLayer(cx *core.Scope, stores *store.Stores, navigator *nav.Navigator) core.Node {
	if stores == nil {
		return core.Group{}
	}
	toasts := stores.Toasts
	// shown is the toast on screen: it stays after the stores dismissed it until its exit has played (leaving), so it sinks away instead of vanishing.
	shown := core.NewSignal[*store.Toast](cx, nil)
	leaving := core.NewSignal(cx, false)
	cx.Effect(func() {
		t := toasts.Current()
		cx.Untrack(func() {
			if t != nil {
				leaving.Set(false)
				shown.Set(t)
			} else if shown.GetUntracked() != nil {
				leaving.Set(true)
			}
		})
	})
	gone := func() {
		if leaving.GetUntracked() {
			leaving.Set(false)
			shown.Set(nil)
		}
	}
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
		}, nil)
	}
	return core.View{PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(24), AlignItems: core.AlignCenter,
			Padding: core.Symmetric(0, 24), ZIndex: 10},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			t := shown.Get()
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
			return kit.Toast{Text: text, Actions: actions, Duration: t.Duration, OnClose: toasts.Dismiss, Leaving: leaving.Get, OnGone: gone}
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
