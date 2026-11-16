package shell_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/shell"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// sidebarApp shows the sidebar beside an overlay or a dialog, as overlay says, that open opens.
func sidebarApp(t *testing.T, overlay bool) (*nsgotest.App, *core.Signal[bool]) {
	t.Helper()
	var open *core.Signal[bool]
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		st := storefake.Provide(cx, storefake.New(t.TempDir()).Deps())
		open = core.NewSignal(cx, false)
		var layer core.Node = kit.ConfirmDialog{Open: open.Get, Title: "确认清理", OnConfirm: func() {}, OnCancel: func() {}}
		if overlay {
			layer = kit.Overlay{Open: open.Get, Title: "正在清理…"}
		}
		return core.View{Style: core.Style{Direction: core.Row, Width: core.Pt(1000), Height: core.Pt(700)}, Children: []core.Node{
			shell.Sidebar{Nav: nav.NewNavigator(cx, nav.Overview), Block: st.Block}, layer,
		}}
	}, nsgotest.Size(1000, 700), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	a.WaitFor("id:sidebar", wait)
	return a, open
}

// While an overlay holds the shell the sidebar fades and its entries are disabled; under the pointer it says why.
func TestSidebarFadesWhileBlocked(t *testing.T) {
	a, open := sidebarApp(t, true)
	if op := a.Find("id:sidebar").Style.Opacity; op != 0 && op < 1 {
		t.Fatalf("the sidebar is faded with nothing open: %v", op)
	}
	a.Post(func() { open.Set(true) })
	a.WaitFor("text:正在清理…", wait)
	if op := a.Find("id:sidebar").Style.Opacity; op == 0 || op >= 0.5 {
		t.Errorf("the sidebar does not fade while blocked: opacity %v", op)
	}
	if !a.FindMounted("id:nav-space").Props.Disabled {
		t.Error("an entry stays enabled while blocked")
	}
	a.Hover("id:sidebar-absorber")
	a.Advance(time.Second)
	if !a.Exists("text:完成或停止当前操作后可切换页面") {
		t.Error("the sidebar does not say why it takes no clicks")
	}
	a.HoverOut()
	a.Post(func() { open.Set(false) })
	a.WaitForGone("text:正在清理…", wait)
	if op := a.Find("id:sidebar").Style.Opacity; op != 0 && op < 1 {
		t.Errorf("the sidebar stays faded: %v", op)
	}
}

// A dialog fades the sidebar as well, but asks a question rather than doing work, so the sidebar says nothing under the pointer.
func TestSidebarHasNoTipBesideADialog(t *testing.T) {
	a, open := sidebarApp(t, false)
	a.Post(func() { open.Set(true) })
	a.WaitFor("id:confirm-ok", wait)
	if op := a.Find("id:sidebar").Style.Opacity; op == 0 || op >= 0.5 {
		t.Errorf("the sidebar does not fade beside a dialog: opacity %v", op)
	}
	a.Hover("id:sidebar-absorber")
	a.Advance(time.Second)
	if a.Exists("text:完成或停止当前操作后可切换页面") {
		t.Error("the overlay's tip shows beside a dialog")
	}
}
