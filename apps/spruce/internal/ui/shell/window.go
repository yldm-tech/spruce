package shell

import (
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/ui/nav"
)

// mainWindow is the main window as the shell moves it: *core.Window in the app.
type mainWindow interface {
	Hide()
	Focus()
}

// windowHost is what the shell does to the app's main window and its Dock icon: desktopHost in the app, a fake in tests, since a headless NSGo opens no windows (OpenWindow, NewTray and SetMainView do nothing there and MainWindow is nil).
type windowHost interface {
	// Main returns the main window, nil when none is open.
	Main() mainWindow
	// SetMainView opens the main window showing view.
	SetMainView(view func(cx *core.Scope) core.Node)
	SetDockVisible(on bool)
	// OnClosing runs fn when the user asks to close the main window that is open now; calling cancel keeps it open.
	OnClosing(fn func(cancel func()))
}

// desktopHost is windowHost over package desktop, in the app's own scope.
type desktopHost struct {
	app *core.Scope
	set func(cx *core.Scope, view func(cx *core.Scope) core.Node)
	// inPlace is set once the view shows where the platform opens no windows (a headless run): the app's one surface shows it, which counts as an open window from then on.
	inPlace bool
}

// Main implements windowHost.
func (h *desktopHost) Main() mainWindow {
	if w := desktop.MainWindow(h.app); w != nil {
		return w
	}
	if h.inPlace {
		return surfaceWindow{}
	}
	return nil
}

// SetMainView implements windowHost.
func (h *desktopHost) SetMainView(view func(cx *core.Scope) core.Node) {
	h.set(h.app, view)
	if desktop.MainWindow(h.app) == nil {
		h.inPlace = true
	}
}

// SetDockVisible implements windowHost.
func (h *desktopHost) SetDockVisible(on bool) { desktop.SetDockVisible(h.app, on) }

// OnClosing implements windowHost.
func (h *desktopHost) OnClosing(fn func(cancel func())) {
	if w := desktop.MainWindow(h.app); w != nil {
		w.OnClosing(h.app, func(c *core.WindowClosing) { fn(c.Cancel) })
	}
}

// surfaceWindow is the view in the app's one surface, which cannot hide or come to the front.
type surfaceWindow struct{}

func (surfaceWindow) Hide()  {}
func (surfaceWindow) Focus() {}

// windows opens, hides and routes the main window for the tray menu, the Dock's reopen, a notification's press and a second copy of the app.
type windows struct {
	host windowHost
	nav  *nav.Navigator
	// view is the root view the main window shows.
	view func(cx *core.Scope) core.Node
	// keepInTray reports whether closing the window hides it instead (macOS with 关闭窗口后继续在菜单栏运行 on).
	keepInTray func() bool
	// hidden is set while closing has hidden the window.
	hidden bool
	// blocked reports, without subscribing, whether an overlay holds the sidebar (store.Block): a clean, an uninstall, a deletion or a maintenance run under its overlay, or a dialog. The menu bar and the notifications stay reachable meanwhile, and changing the page then would end the page's work, so they only bring the window forward. Nil is never blocked.
	blocked func() bool
	// busy reports, without subscribing, whether a clean, an uninstall, a deletion or a maintenance run is active, which quitting would stop after its current item; nil is never busy. Quitting or closing the window (when that ends the app) then asks first: asking is set while the window asks (Root's quitConfirm), whose 停止并退出 calls end.
	busy   func() bool
	asking *core.Signal[bool]
	end    func()
}

// away reports whether the main window is out of sight: hidden in the menu bar, or not open.
func (w *windows) away() bool { return w.hidden || w.host.Main() == nil }

// active reports whether a run is active now (busy).
func (w *windows) active() bool { return w.busy != nil && w.busy() }

// quit ends Spruce for 退出 Spruce in the menu bar and the menu bar icon's menu; while a run is active it shows the window asking first instead.
func (w *windows) quit() {
	if !w.active() {
		w.end()
		return
	}
	w.show("")
	w.asking.Set(true)
}

// quitPoll is how often a question before quitting looks whether its run has ended (followRun): schedule.Activity tells no one.
const quitPoll = 500 * time.Millisecond

// followRun quits for a question before quitting that is still up when its run ends: there is nothing left to ask about, and the user asked to quit. It looks every quitPoll while the question is up, in cx.
func (w *windows) followRun(cx *core.Scope) {
	cx.Effect(func() {
		if !w.asking.Get() {
			return
		}
		var look func()
		look = func() {
			if w.active() {
				cx.AfterFunc(quitPoll, look)
				return
			}
			w.asking.Set(false)
			w.end()
		}
		cx.AfterFunc(quitPoll, look)
	})
}

// held reports whether an overlay holds the sidebar now.
func (w *windows) held() bool { return w.blocked != nil && w.blocked() }

// show shows the main window, on route unless it is "" or an overlay holds the sidebar (held): the Dock icon comes back, a window opens when none is (the app started at login without one, or it was closed for good), and it comes to the front. A window that was hidden is counted as shown again (nav.Navigator.WindowShown), so 概览, still mounted, checks again as opening it does (DESIGN 2.3 state D).
func (w *windows) show(route nav.Route) {
	w.host.SetDockVisible(true)
	if w.host.Main() == nil {
		w.host.SetMainView(w.view)
		w.host.OnClosing(w.closing)
	}
	if route != "" && !w.held() {
		w.nav.Go(route)
	}
	if w.hidden {
		w.hidden = false
		w.nav.WindowShown()
	}
	if m := w.host.Main(); m != nil {
		m.Focus()
	}
}

// closing handles the user closing the main window: with keepInTray the window hides and the Dock icon goes, keeping the page and whatever runs on it (the page area mounts the page again as the route changes, which would end its work), except 健康监测, whose sampling runs only while it shows; otherwise the window closes, which ends the app, unless a run is active: then it stays and asks first, as quit does.
func (w *windows) closing(cancel func()) {
	if !w.keepInTray() {
		if w.active() {
			cancel()
			w.asking.Set(true)
		}
		return
	}
	cancel()
	if m := w.host.Main(); m != nil {
		m.Hide()
		w.hidden = true
	}
	w.host.SetDockVisible(false)
	if w.nav.Current() == nav.Health {
		w.nav.Go(nav.Overview)
	}
}

// Notification IDs: the controller's (app.Notice.ID), low-disk alone and auto-summary followed by the ISO week, and the shell's at the end of a check the menu bar icon started (trayCheck).
const (
	noticeLowDisk     = "low-disk"
	noticeAutoSummary = "auto-summary:"
	noticeCheck       = "check-done"
)

// pressed opens what the notification id is about: 概览 for low space when it has something to free, else 空间透视; 概览 for the end of a check; 操作历史 on its 自动清理 filter for the weekly summary; just the window for anything else, and also while an overlay holds the sidebar.
func (w *windows) pressed(id string, hasRecommendations bool) {
	switch {
	case id == noticeLowDisk && hasRecommendations:
		w.show(nav.Overview)
	case id == noticeLowDisk:
		w.show(nav.Space)
	case id == noticeCheck:
		w.show(nav.Overview)
	case strings.HasPrefix(id, noticeAutoSummary):
		w.show("")
		if !w.held() {
			w.nav.GoHistory(history.KindAuto, "")
		}
	default:
		w.show("")
	}
}

// automation opens 设置 → 自动清理 for the menu bar's rules item, or only the window while an overlay holds the sidebar.
func (w *windows) automation() {
	w.show("")
	if !w.held() {
		w.nav.GoSettings(nav.SettingsAuto)
	}
}
