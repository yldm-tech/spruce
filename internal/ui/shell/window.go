package shell

import (
	"strings"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/nav"
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
		return inPlace{}
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

// inPlace is the view in the app's one surface, which cannot hide or come to the front.
type inPlace struct{}

func (inPlace) Hide()  {}
func (inPlace) Focus() {}

// windows opens, hides and routes the main window for the tray menu, the Dock's reopen, a notification's press and a second copy of the app.
type windows struct {
	host windowHost
	nav  *nav.Navigator
	// view is the root view the main window shows.
	view func(cx *core.Scope) core.Node
	// keepInTray reports whether closing the window hides it instead (macOS with 关闭窗口后继续在菜单栏运行 on).
	keepInTray func() bool
}

// show shows the main window, on route unless it is "": the Dock icon comes back, a window opens when none is (the app started at login without one, or it was closed for good), and it comes to the front.
func (w *windows) show(route nav.Route) {
	w.host.SetDockVisible(true)
	if w.host.Main() == nil {
		w.host.SetMainView(w.view)
		w.host.OnClosing(w.closing)
	}
	if route != "" {
		w.nav.Go(route)
	}
	if m := w.host.Main(); m != nil {
		m.Focus()
	}
}

// closing handles the user closing the main window: with keepInTray the window hides and the Dock icon goes, keeping the page and whatever runs on it (the page area mounts the page again as the route changes, which would end its work), except 健康监测, whose sampling runs only while it shows; otherwise the window closes, which ends the app.
func (w *windows) closing(cancel func()) {
	if !w.keepInTray() {
		return
	}
	cancel()
	if m := w.host.Main(); m != nil {
		m.Hide()
	}
	w.host.SetDockVisible(false)
	if w.nav.Current() == nav.Health {
		w.nav.Go(nav.Overview)
	}
}

// Notification IDs the controller posts (app.Notice.ID): low-disk alone, auto-summary followed by the ISO week.
const (
	noticeLowDisk     = "low-disk"
	noticeAutoSummary = "auto-summary:"
)

// pressed opens what the notification id is about: 概览 for low space when it has something to free, else 空间透视; 操作历史 on its 自动清理 filter for the weekly summary; just the window for anything else.
func (w *windows) pressed(id string, hasRecommendations bool) {
	switch {
	case id == noticeLowDisk && hasRecommendations:
		w.show(nav.Overview)
	case id == noticeLowDisk:
		w.show(nav.Space)
	case strings.HasPrefix(id, noticeAutoSummary):
		w.show("")
		w.nav.GoHistory(history.KindAuto, "")
	default:
		w.show("")
	}
}
