package shell

import (
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/ui/nav"
)

// item returns the item at path of menu, its labels from the menu bar down.
func item(t *testing.T, menu []desktop.MenuItem, path ...string) desktop.MenuItem {
	t.Helper()
	for i, label := range path {
		k := slices.IndexFunc(menu, func(it desktop.MenuItem) bool { return it.Label == label })
		if k < 0 {
			t.Fatalf("no menu item %q", path[:i+1])
		}
		if i == len(path)-1 {
			return menu[k]
		}
		menu = menu[k].Items
	}
	return desktop.MenuItem{}
}

func TestApplicationMenu(t *testing.T) {
	root := core.NewRoot(core.RootOptions{Post: func(fn func()) { fn() }, Report: func(err error) { t.Errorf("graph failed: %v", err) }})
	t.Cleanup(root.Dispose)
	n := nav.NewNavigator(root, nav.Overview)
	quits := 0
	menu := applicationMenu(n, false, true, func() { quits++ })

	// The sidebar's pages on Cmd+1 to Cmd+7, in its order.
	goTo := item(t, menu, "前往").Items
	for i, r := range nav.Pages {
		if it := goTo[i]; it.Label != r.Meta().Title || it.Shortcut != "mod+"+string(rune('1'+i)) || it.Disabled {
			t.Errorf("前往 item %d: %+v", i, it)
		}
	}
	item(t, menu, "前往", "空间透视").OnSelect()
	if n.Current() != nav.Space {
		t.Errorf("前往 → 空间透视 shows %q", n.Current())
	}
	if !item(t, applicationMenu(n, false, true, nil), "前往", "返回").Disabled {
		t.Error("返回 enabled on a sidebar page")
	}
	n.Go(nav.DevJunk)
	back := item(t, applicationMenu(n, false, true, nil), "前往", "返回")
	if back.Disabled || back.Shortcut != "mod+[" {
		t.Fatalf("返回 on a drill-down view: %+v", back)
	}
	back.OnSelect()
	if n.Current() != nav.Space {
		t.Errorf("返回 from 开发者 entered from 空间透视 shows %q", n.Current())
	}

	settings := item(t, menu, "Spruce", "设置…")
	if settings.Shortcut != "mod+," {
		t.Errorf("设置… shortcut %q", settings.Shortcut)
	}
	settings.OnSelect()
	if n.Current() != nav.Settings {
		t.Errorf("设置… shows %q", n.Current())
	}
	quit := item(t, menu, "Spruce", "退出 Spruce")
	if quit.Shortcut != "mod+q" || quit.Role != desktop.MenuRoleNone {
		t.Errorf("退出 Spruce %+v: the platform's quit would skip the question", quit)
	}
	quit.OnSelect()
	if quits != 1 {
		t.Error("退出 Spruce did not go through quit")
	}

	if close := item(t, menu, "窗口").Items; close[len(close)-1].Role != desktop.MenuClose {
		t.Errorf("窗口 on a Mac ends with %+v, not 关闭", close[len(close)-1])
	}

	// Without a page's commands 重新检查 and 查找 do nothing.
	if !item(t, menu, "显示", "重新检查").Disabled || !item(t, menu, "显示", "查找").Disabled {
		t.Error("重新检查 or 查找 enabled without a page that has them")
	}

	// While an overlay holds the sidebar nothing changes the page; 退出 stays.
	blocked := applicationMenu(n, true, true, func() {})
	for _, path := range [][]string{{"前往", "概览"}, {"前往", "设置"}, {"Spruce", "设置…"}, {"显示", "重新检查"}} {
		if !item(t, blocked, path...).Disabled {
			t.Errorf("%v enabled while blocked", path)
		}
	}
	if item(t, blocked, "Spruce", "退出 Spruce").Disabled {
		t.Error("退出 Spruce disabled while blocked")
	}
}

// A page's commands drive 显示: 重新检查 (Cmd+R, not while typing) runs its Refresh until Busy says no, and they go with the page.
func TestMenuRunsThePageCommands(t *testing.T) {
	var refreshes, finds int
	var busy, mounted *core.Signal[bool]
	a := nsgotest.Start(t, core.AppRoot(func(app *core.Scope) {
		n := nav.NewNavigator(app, nav.Overview)
		nav.Key.Provide(app, n)
		busy, mounted = core.NewSignal(app, false), core.NewSignal(app, true)
		app.Effect(func() { desktop.SetApplicationMenu(app, applicationMenu(n, false, true, func() {})) })
		app.SetMainView(func(cx *core.Scope) core.Node {
			return core.Show{When: mounted.Get, Then: func(cx *core.Scope) core.Node {
				nav.UseCommands(cx, nav.Commands{Refresh: func() { refreshes++ }, Find: func() { finds++ }, Busy: busy.Get})
				return core.Text{Content: "页面"}
			}}
		})
	}))
	a.WaitFor("text:页面", 5*time.Second)
	a.ChooseMenu("显示", "重新检查")
	a.ChooseMenu("显示", "查找")
	if refreshes != 1 || finds != 1 {
		t.Fatalf("refreshes %d, finds %d", refreshes, finds)
	}
	entry := func(label string) (disabled, notInText bool, shortcut string) {
		for _, m := range a.Menu {
			for _, it := range m.Items {
				if it.Label == label {
					return it.Disabled, it.NotInText, it.Shortcut
				}
			}
		}
		t.Fatalf("no menu item %q", label)
		return
	}
	if _, notInText, key := entry("重新检查"); !notInText || key != "mod+r" {
		t.Errorf("重新检查: shortcut %q, not in text %v", key, notInText)
	}
	a.Post(func() { busy.Set(true) })
	a.Settle()
	if off, _, _ := entry("重新检查"); !off {
		t.Error("重新检查 enabled while the page is busy")
	}
	a.Post(func() { mounted.Set(false) })
	a.Settle()
	if off, _, _ := entry("查找"); !off {
		t.Error("查找 outlived the page")
	}
}

// Windows draws no menu bar but chooses items by their shortcuts, a role's own when it has none (but the editing roles', which the focused field handles): only the planned ones are there, and no Ctrl+W to close the window, which would end Spruce.
func TestApplicationMenuOnWindowsHasOnlyItsOwnShortcuts(t *testing.T) {
	root := core.NewRoot(core.RootOptions{Post: func(fn func()) { fn() }, Report: func(err error) { t.Errorf("graph failed: %v", err) }})
	t.Cleanup(root.Dispose)
	var keys []string
	var walk func(items []desktop.MenuItem)
	walk = func(items []desktop.MenuItem) {
		for _, it := range items {
			walk(it.Items)
			key := it.Shortcut
			if key == "" && !it.Role.EditsText() {
				key = it.Role.Shortcut(false)
			}
			if key != "" {
				keys = append(keys, key)
			}
		}
	}
	walk(applicationMenu(nav.NewNavigator(root, nav.Overview), false, false, func() {}))
	want := []string{"mod+,", "mod+q", "mod+r", "mod+f", "mod+1", "mod+2", "mod+3", "mod+4", "mod+5", "mod+6", "mod+7", "mod+["}
	if !slices.Equal(keys, want) {
		t.Errorf("shortcuts on Windows %v, want %v", keys, want)
	}
}
