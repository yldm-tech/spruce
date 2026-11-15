// Package shell is the app around the pages: the app function nsgo.Run runs (App), which makes the stores, starts the background work, sets the menu, keeps the window's frame, and on macOS keeps Spruce in the menu bar (tray.go) with its window hidden rather than closed (window.go); the system notifications (notices.go); and the root view with the sidebar and the page area.
package shell

import (
	"cmp"
	"context"
	"log/slog"
	"runtime"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	spruceapp "github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/rules"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// App returns the app function: it runs once, in the app's own scope, which lives until the app quits. Besides the stores and the background work it keeps the navigator, the menu bar icon (macOS), the notifications and the main window, which it opens at once unless the system started Spruce at login with 关闭窗口后继续在菜单栏运行 on.
func App(d Deps) func(app *core.Scope) {
	return func(app *core.Scope) {
		log := d.Log
		if log == nil {
			log = slog.New(slog.DiscardHandler)
		}
		ctx, cancel := context.WithCancel(context.Background())
		stores := store.New(app, ctx, d.Stores)
		store.Key.Provide(app, stores)
		kit.BlockerKey.Provide(app, stores.Block)
		providePages(app, d)
		// The navigator lives with the app, not the window: the tray, the notifications and a window opened again all go through it.
		navigator := nav.NewNavigator(app, cmp.Or(d.Start, nav.Start))
		nav.Key.Provide(app, navigator)
		for _, work := range d.Background {
			go work(ctx)
		}
		if runtime.GOOS == "darwin" {
			desktop.SetApplicationMenu(app, applicationMenu())
		}

		// The root view keeps the window's frame here, so the app's own OnQuit, which outlives the window, writes it.
		var frame core.WindowFrame
		app.OnQuit(func(context.Context) {
			cancel()
			if err := SaveFrame(d.DataDir, frame); err != nil {
				log.Warn("save window frame", "err", err)
			}
		})

		set := d.SetMainView
		if set == nil {
			set = func(cx *core.Scope, view func(cx *core.Scope) core.Node) { cx.SetMainView(view) }
		}
		host := d.host
		if host == nil {
			host = &desktopHost{app: app, set: set}
		}
		keepInTray := func() bool {
			return runtime.GOOS == "darwin" && core.Untracked(app, stores.Settings.Loaded) && core.Untracked(app, stores.Settings.Value).General.KeepInTray
		}
		w := &windows{host: host, nav: navigator, keepInTray: keepInTray, view: func(cx *core.Scope) core.Node {
			return Root(cx, stores.Block, func(f core.WindowFrame) { frame = f })
		}, blocked: func() bool { return core.Untracked(app, stores.Block.Blocked) }}

		// Closing the window ends the app unless it is to stay in the menu bar.
		app.Effect(func() {
			mode := desktop.ShutdownOnMainWindowClose
			if runtime.GOOS == "darwin" && stores.Settings.Loaded() && stores.Settings.Value().General.KeepInTray {
				mode = desktop.ShutdownOnExplicit
			}
			desktop.SetShutdownMode(app, mode)
		})
		desktop.OnReopen(app, func() { w.show("") })
		app.OnNotificationPress(func(id string) {
			st := core.Untracked(app, stores.Overview.State)
			w.pressed(id, st.Result != nil && st.Result.Headline > 0)
		})
		postNotices(app, ctx, stores.Overview)
		if runtime.GOOS == "darwin" {
			trayIcon(app, d, stores, w)
		}

		if d.Instance != nil {
			go func() {
				err := d.Instance.Serve(ctx, func() { app.Post(func() { w.show("") }) })
				if err != nil {
					log.Warn("single instance: a later copy cannot bring this one to the front", "err", err)
				}
			}()
		}
		openAtLaunch(app, d.LaunchedAtLogin, func() bool {
			return runtime.GOOS == "darwin" && d.Stores.Settings.Get().General.KeepInTray
		}, w)
	}
}

// launchWait is how long the app waits to learn whether the system started it at login, asking every launchPoll.
const (
	launchWait = time.Second
	launchPoll = 50 * time.Millisecond
)

// openAtLaunch opens the main window as the app starts, unless the system started it at login (launched) and stayInTray reports 关闭窗口后继续在菜单栏运行 on (macOS): then only the menu bar icon shows, and its menu opens the window. While the launch is unknown it waits up to launchWait; still unknown, the window opens, as it always did. A nil launched counts as a launch the user made. stayInTray reads the stored settings, as main does for nsgo.HideDock: the store's copy arrives later.
func openAtLaunch(app *core.Scope, launched func() (atLogin, known bool), stayInTray func() bool, w *windows) {
	if launched == nil {
		launched = func() (bool, bool) { return false, true }
	}
	var waited time.Duration
	var try func()
	try = func() {
		atLogin, known := launched()
		switch {
		case known && atLogin && stayInTray():
		case known || waited >= launchWait:
			w.show("")
		default:
			waited += launchPoll
			app.AfterFunc(launchPoll, try)
		}
	}
	try()
}

// trayIcon adds the menu bar icon, which lives as long as the app: its tooltip and its icon follow the free space, and its menu (TrayMenu) is made each time it opens. Without the icons (tests) or a platform that has none (headless, and Windows, where NSGo's NewTray answers nil) there is none.
func trayIcon(app *core.Scope, d Deps, stores *store.Stores, w *windows) {
	if len(d.TrayIcon) == 0 {
		return
	}
	tray := desktop.NewTray(app, desktop.TrayOptions{Icon: d.TrayIcon, Template: true, Tooltip: trayTooltip(nil), Menu: func() []desktop.MenuItem {
		st := core.Untracked(app, stores.Overview.State)
		s := TrayState{Disk: st.Disk, Checking: st.Phase == spruceapp.OverviewChecking, HasResult: st.Result != nil,
			Cleaning: st.Phase == spruceapp.OverviewCleaning || d.Activity != nil && d.Activity.Cleaning(),
			RulesOn:  len(rules.Enabled(core.Untracked(app, stores.Settings.Value).Automation)),
			Open:     func() { w.show("") },
			Overview: func() { w.show(nav.Overview) },
			Check:    func() { stores.Overview.CheckNow(nil) },
			Auto:     w.automation,
			Quit:     app.Quit,
		}
		if st.Result != nil {
			s.Headline = st.Result.Headline
		}
		return TrayMenu(s)
	}})
	if tray == nil {
		return
	}
	// Each call makes a new NSImage, so only a change is sent.
	tip, low := trayTooltip(nil), false
	app.Effect(func() {
		disk := stores.Overview.State().Disk
		if t := trayTooltip(disk); t != tip {
			tip = t
			tray.SetTooltip(t)
		}
		if l := disk != nil && disk.Low; l != low {
			low = l
			icon := d.TrayIcon
			if l && len(d.TrayAlertIcon) > 0 {
				icon = d.TrayAlertIcon
			}
			tray.SetIcon(icon, true)
		}
	})
}

// applicationMenu is the macOS menu bar: the app menu, 编辑 and 窗口, with the platform's items and shortcuts (Cmd+Q, Cmd+H, Cmd+W, Cmd+M, ...).
func applicationMenu() []desktop.MenuItem {
	sep := desktop.MenuItem{Separator: true}
	return []desktop.MenuItem{
		{Label: "Spruce", Items: []desktop.MenuItem{
			{Role: desktop.MenuAbout}, sep,
			{Role: desktop.MenuHide}, {Role: desktop.MenuHideOthers}, {Role: desktop.MenuShowAll}, sep,
			{Role: desktop.MenuQuit},
		}},
		{Label: "编辑", Items: []desktop.MenuItem{
			{Role: desktop.MenuUndo}, {Role: desktop.MenuRedo}, sep,
			{Role: desktop.MenuCut}, {Role: desktop.MenuCopy}, {Role: desktop.MenuPaste}, {Role: desktop.MenuSelectAll},
		}},
		{Label: "窗口", Items: []desktop.MenuItem{
			{Role: desktop.MenuMinimize}, {Role: desktop.MenuZoom}, {Role: desktop.MenuToggleFullScreen}, sep,
			{Role: desktop.MenuClose},
		}},
	}
}
