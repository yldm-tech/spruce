// Package shell is the app around the pages: the app function nsgo.Run runs (App), which makes the stores, starts the background work, sets the menu and keeps the window's frame, and the root view with the sidebar and the page area.
package shell

import (
	"context"
	"log/slog"
	"runtime"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// App returns the app function: it runs once, in the app's own scope, which lives until the app quits.
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
		for _, work := range d.Background {
			go work(ctx)
		}
		if runtime.GOOS == "darwin" {
			desktop.SetApplicationMenu(app, applicationMenu())
		}

		// The root view keeps the window's frame here, so the app's own OnQuit, which outlives the window, writes it.
		var frame core.WindowFrame
		var root *core.Scope
		app.OnQuit(func(context.Context) {
			cancel()
			if err := SaveFrame(d.DataDir, frame); err != nil {
				log.Warn("save window frame", "err", err)
			}
		})
		if d.Instance != nil {
			go func() {
				err := d.Instance.Serve(ctx, func() {
					app.Post(func() {
						if root != nil && !root.Disposed() {
							desktop.OrderFront(root)
						}
					})
				})
				if err != nil {
					log.Warn("single instance: a later copy cannot bring this one to the front", "err", err)
				}
			}()
		}

		set := d.SetMainView
		if set == nil {
			set = func(cx *core.Scope, view func(cx *core.Scope) core.Node) { cx.SetMainView(view) }
		}
		set(app, func(cx *core.Scope) core.Node {
			root = cx
			return Root(cx, stores.Block, d.Start, func(f core.WindowFrame) { frame = f })
		})
	}
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
