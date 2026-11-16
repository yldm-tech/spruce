package kit_test

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/ui/kit"
)

var macDesktop = nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop})

// dialogApp shows the dialog dialog makes, its open state in a signal, in a macOS window, and waits for the selector wait.
func dialogApp(t *testing.T, wait string, dialog func(open *core.Signal[bool]) core.Node) *nsgotest.App {
	t.Helper()
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		open := core.NewSignal(cx, true)
		return core.View{Style: core.Style{Width: core.Pt(900), Height: core.Pt(600)}, Children: []core.Node{dialog(open)}}
	}, nsgotest.Size(900, 600), macDesktop)
	a.WaitFor(wait, 5*time.Second)
	return a
}

// bindsReturn reports whether the app binds Return, which on macOS is the only way a dialog takes it: Ref.Focus cannot put the keyboard's focus on a button there, so no button is focused for Return to press, although nsgotest focuses it.
func bindsReturn(a *nsgotest.App) bool { return slices.Contains(a.Keymap, "enter") }

func TestReturnConfirmsANonDangerConfirmDialog(t *testing.T) {
	confirms, cancels := 0, 0
	a := dialogApp(t, "id:confirm-ok", func(open *core.Signal[bool]) core.Node {
		return kit.ConfirmDialog{Open: open.Get, Title: "确认清理", Lines: []string{"将移到废纸篓"}, ConfirmLabel: "清理",
			OnConfirm: func() { confirms++; open.Set(false) }, OnCancel: func() { cancels++; open.Set(false) }}
	})
	if !bindsReturn(a) {
		t.Fatal("the dialog does not bind Return")
	}
	a.Key("enter")
	if confirms != 1 || cancels != 0 {
		t.Errorf("Return: confirms %d, cancels %d", confirms, cancels)
	}
	if bindsReturn(a) {
		t.Error("Return stays bound after the dialog closed")
	}
}

func TestReturnDoesNotConfirmWhileBusy(t *testing.T) {
	confirms := 0
	a := dialogApp(t, "id:confirm-ok", func(open *core.Signal[bool]) core.Node {
		return kit.ConfirmDialog{Open: open.Get, Title: "确认清理", Busy: func() bool { return true }, OnConfirm: func() { confirms++ }}
	})
	a.Key("enter")
	if confirms != 0 {
		t.Errorf("Return confirmed a starting action %d times", confirms)
	}
}

func TestReturnNeverConfirmsADangerDialog(t *testing.T) {
	confirms := 0
	a := dialogApp(t, "id:confirm-ok", func(open *core.Signal[bool]) core.Node {
		return kit.ConfirmDialog{Open: open.Get, Title: "清空历史", Danger: true, ConfirmLabel: "清空", OnConfirm: func() { confirms++ }, OnCancel: func() {}}
	})
	if bindsReturn(a) {
		t.Error("a danger dialog binds Return")
	}
	a.Key("enter")
	if confirms != 0 {
		t.Errorf("Return confirmed a danger dialog %d times", confirms)
	}
}

func TestReturnClosesADoneDialog(t *testing.T) {
	closes := 0
	a := dialogApp(t, "id:done-close", func(open *core.Signal[bool]) core.Node {
		return kit.DoneDialog{Open: open.Get, Title: "清理完成", Big: "3.1 GB", Succeeded: true, OnClose: func() { closes++; open.Set(false) }}
	})
	if !bindsReturn(a) {
		t.Fatal("the dialog does not bind Return")
	}
	a.Key("enter")
	if closes != 1 {
		t.Errorf("Return: closes %d", closes)
	}
}

func TestReturnPressesTheInitialActionOfADialog(t *testing.T) {
	keeps, removes := 0, 0
	actions := func(danger bool) func(open *core.Signal[bool]) core.Node {
		return func(open *core.Signal[bool]) core.Node {
			return kit.Dialog{Open: open.Get, Title: "留下的文件", Danger: danger, Actions: []kit.DialogAction{
				{Text: "保留", Variant: kit.ButtonGhost, OnPress: func() { keeps++ }, TestID: "keep"},
				{Text: "移到废纸篓", Variant: kit.ButtonLight, OnPress: func() { removes++ }, Initial: true, TestID: "remove"},
			}}
		}
	}
	a := dialogApp(t, "id:remove", actions(false))
	if !bindsReturn(a) {
		t.Fatal("the dialog does not bind Return")
	}
	a.Key("enter")
	if removes != 1 || keeps != 0 {
		t.Errorf("Return: removes %d, keeps %d", removes, keeps)
	}

	removes = 0
	d := dialogApp(t, "id:remove", actions(true))
	if bindsReturn(d) {
		t.Error("a danger dialog binds Return")
	}
}

func TestReturnSkipsADisabledInitialAction(t *testing.T) {
	removes := 0
	a := dialogApp(t, "id:remove", func(open *core.Signal[bool]) core.Node {
		return kit.Dialog{Open: open.Get, Title: "留下的文件", Actions: []kit.DialogAction{
			{Text: "移到废纸篓", Variant: kit.ButtonLight, OnPress: func() { removes++ }, Disabled: func() bool { return true }, Initial: true, TestID: "remove"},
		}}
	})
	a.Key("enter")
	if removes != 0 {
		t.Errorf("Return pressed a disabled action %d times", removes)
	}
}

// A dialog opened while a search field has the keyboard takes it from the field: on macOS Ref.Focus cannot focus the dialog's buttons, so the field would keep the keyboard and with it Return and Escape, and typing would change the page under the dialog. A press does not take the keyboard from a field, so the uninstall flow (type in the search, press an app, press 卸载) opens its dialog so; the dialog here focuses none of its actions, as macOS has every dialog.
func TestOpeningADialogTakesTheKeyboardFromThePage(t *testing.T) {
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		open := core.NewSignal(cx, false)
		query := core.NewSignal(cx, "")
		return core.View{Style: core.Style{Width: core.Pt(900), Height: core.Pt(600)}, Children: []core.Node{
			kit.Field{Value: query.Get, OnChange: query.Set, Label: "搜索应用", TestID: "search"},
			kit.Button{Text: "卸载", Variant: kit.ButtonDanger, OnPress: func() { open.Set(true) }, TestID: "open"},
			kit.Dialog{Open: open.Get, Title: "留下的文件", OnClose: func() { open.Set(false) }, Actions: []kit.DialogAction{
				{Text: "关闭", Variant: kit.ButtonLight, OnPress: func() { open.Set(false) }, TestID: "dialog-close"},
			}},
		}}
	}, nsgotest.Size(900, 600), macDesktop)
	a.Type("id:search", "Xcode")
	a.Press("id:open")
	a.WaitFor("id:dialog-close", 5*time.Second)
	if a.Find("id:search").Focused {
		t.Error("the page's field keeps the keyboard under the dialog")
	}
}
