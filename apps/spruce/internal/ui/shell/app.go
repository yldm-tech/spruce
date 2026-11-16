// Package shell is the app around the pages: the app function nsgo.Run runs (App), which makes the stores, starts the background work, sets the menu, keeps the window's frame, and on macOS keeps Spruce in the menu bar (tray.go) with its window hidden rather than closed (window.go); the system notifications (notices.go); and the root view with the sidebar and the page area.
package shell

import (
	"cmp"
	"context"
	"log/slog"
	"runtime"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	spruceapp "spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/rules"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/store"
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
		kit.ToastShownKey.Provide(app, func() bool { return stores.Toasts.Current() != nil })
		// The navigator lives with the app, not the window: the tray, the notifications and a window opened again all go through it.
		navigator := nav.NewNavigator(app, cmp.Or(d.Start, nav.Start))
		nav.Key.Provide(app, navigator)
		// What the pages keep beyond one showing may read the stores and the navigator.
		providePages(app, d)
		for _, work := range d.Background {
			go work(ctx)
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
		// busy reports a clean, an uninstall, a deletion or a maintenance run of this process, which quitting stops after its current item.
		busy := func() bool {
			return core.Untracked(app, stores.Overview.State).Phase == spruceapp.OverviewCleaning || d.Activity != nil && d.Activity.Cleaning()
		}
		keepInTray := func() bool {
			return runtime.GOOS == "darwin" && core.Untracked(app, stores.Settings.Loaded) && core.Untracked(app, stores.Settings.Value).General.KeepInTray
		}
		asking := core.NewSignal(app, false)
		w := &windows{host: host, nav: navigator, keepInTray: keepInTray, view: func(cx *core.Scope) core.Node {
			return Root(cx, stores.Block, func(f core.WindowFrame) { frame = f }, asking, app.Quit)
		}, blocked: func() bool { return core.Untracked(app, stores.Block.Blocked) }, busy: busy, asking: asking, end: app.Quit}
		w.followRun(app)
		// The menu bar on macOS; on Windows, where the window has no menu bar, its shortcuts alone.
		app.Effect(func() {
			desktop.SetApplicationMenu(app, applicationMenu(navigator, stores.Block.Blocked(), runtime.GOOS == "darwin", w.quit))
		})

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

// trayIcon adds the menu bar icon, which lives as long as the app: its tooltip follows the free space and says when a check runs, its icon the free space, and its menu (TrayMenu) is made each time it opens; its 立即检查 tells how the check ended while the window is hidden (trayCheck). Without the icons (tests) or a platform that has none (headless, and Windows, where NSGo's NewTray answers nil) there is none.
func trayIcon(app *core.Scope, d Deps, stores *store.Stores, w *windows) {
	if len(d.TrayIcon) == 0 {
		return
	}
	check := trayCheck(app, stores.Overview, w.away)
	tray := desktop.NewTray(app, desktop.TrayOptions{Icon: d.TrayIcon, Template: true, Tooltip: trayTooltip(nil, false), Menu: func() []desktop.MenuItem {
		st := core.Untracked(app, stores.Overview.State)
		s := TrayState{Disk: st.Disk, Checking: st.Phase == spruceapp.OverviewChecking, HasResult: st.Result != nil,
			Cleaning: w.active(),
			RulesOn:  len(rules.Enabled(core.Untracked(app, stores.Settings.Value).Automation)),
			Open:     func() { w.show("") },
			Overview: func() { w.show(nav.Overview) },
			Check:    check,
			Auto:     w.automation,
			Quit:     w.quit,
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
	tip, low := trayTooltip(nil, false), false
	app.Effect(func() {
		st := stores.Overview.State()
		disk := st.Disk
		if t := trayTooltip(disk, st.Phase == spruceapp.OverviewChecking); t != tip {
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

// applicationMenu is the application menu: the app menu with 设置… and 退出 Spruce, which asks first while a run is active (quit); 编辑; 显示 with what the page shown registered (nav.UseCommands), 重新检查 and 查找; 前往 with the sidebar's pages on Cmd+1 to Cmd+7 and 返回 from a drill-down view; and on a Mac (mac) 窗口. Everything that changes the page is disabled while an overlay holds the sidebar (blocked), as the sidebar is. Reading it subscribes to the page shown and its commands. Windows draws no menu bar but chooses the items by their shortcuts, the roles' own too, so 窗口 is left out there: its Ctrl+W would close the window, which ends Spruce.
func applicationMenu(n *nav.Navigator, blocked, mac bool, quit func()) []desktop.MenuItem {
	sep := desktop.MenuItem{Separator: true}
	cmds := n.Commands()
	goTo := make([]desktop.MenuItem, 0, len(nav.Pages)+2)
	for i, r := range nav.Pages {
		goTo = append(goTo, desktop.MenuItem{Label: r.Meta().Title, Shortcut: "mod+" + strconv.Itoa(i+1), Disabled: blocked, OnSelect: func() { n.Go(r) }})
	}
	goTo = append(goTo, sep, desktop.MenuItem{Label: "返回", Shortcut: "mod+[", Disabled: blocked || n.Current().Meta().Parent == "", OnSelect: n.Back})
	menu := []desktop.MenuItem{
		{Label: "Spruce", Items: []desktop.MenuItem{
			{Role: desktop.MenuAbout}, sep,
			{Label: "设置…", Shortcut: "mod+,", Disabled: blocked, OnSelect: func() { n.Go(nav.Settings) }}, sep,
			{Role: desktop.MenuHide}, {Role: desktop.MenuHideOthers}, {Role: desktop.MenuShowAll}, sep,
			{Label: "退出 Spruce", Shortcut: "mod+q", OnSelect: quit},
		}},
		{Label: "编辑", Items: []desktop.MenuItem{
			{Role: desktop.MenuUndo}, {Role: desktop.MenuRedo}, sep,
			{Role: desktop.MenuCut}, {Role: desktop.MenuCopy}, {Role: desktop.MenuPaste}, {Role: desktop.MenuSelectAll},
		}},
		{Label: "显示", Items: []desktop.MenuItem{
			{Label: "重新检查", Shortcut: "mod+r", When: core.MenuNotEditing, Disabled: blocked || !n.CanRefresh(), OnSelect: cmds.Refresh},
			{Label: "查找", Shortcut: "mod+f", Disabled: blocked || cmds.Find == nil, OnSelect: cmds.Find},
		}},
		{Label: "前往", Items: goTo},
	}
	if !mac {
		return menu
	}
	return append(menu, desktop.MenuItem{Label: "窗口", Items: []desktop.MenuItem{
		{Role: desktop.MenuMinimize}, {Role: desktop.MenuZoom}, {Role: desktop.MenuToggleFullScreen}, sep,
		{Role: desktop.MenuClose},
	}})
}
