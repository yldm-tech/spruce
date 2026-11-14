// Command spruce is the Spruce desktop app: a disk cleaner and system maintenance tool for macOS and Windows.
//
// main does three things, in this order: it serves the elevated child when started with elevate.Flag (before any mygo code runs), it wires the engine into the bound services, and it opens the window once the app is ready.
package main

import (
	"context"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"sync"

	"github.com/egoist/mygo"
	"github.com/yldm-tech/spruce/internal/engine/elevate"
	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/services"
)

// historyFile is the name of the history file inside the app's user data directory.
const historyFile = "history.jsonl"

func main() {
	logger := slog.New(slog.NewTextHandler(os.Stderr, nil))
	runner := execx.OSRunner{}
	tasks := maintenance.PlatformTasks()

	// The elevated child: the same executable relaunched with administrator rights by elevate.ProcessElevator. It accepts only the IDs of this catalog's admin tasks, runs them and exits without starting any UI.
	if elevate.IsChild(os.Args[1:]) {
		var adminIDs []string
		for _, t := range tasks {
			if t.NeedsAdmin {
				adminIDs = append(adminIDs, t.ID)
			}
		}
		ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt)
		code := elevate.ServeChild(ctx, os.Args[1:], adminIDs, maintenance.ElevatedHandler(tasks, maintenance.Env{Runner: runner, Log: logger}))
		stop()
		os.Exit(code)
	}

	if !mygo.App.RequestSingleInstanceLock() {
		return // the running instance comes to the front
	}

	exe, err := os.Executable()
	if err != nil {
		log.Fatal(err)
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		log.Fatal(err)
	}
	home, err := os.UserHomeDir()
	if err != nil {
		log.Fatal(err)
	}
	dataDir, err := mygo.App.Path(mygo.PathUserData)
	if err != nil {
		log.Fatal(err)
	}

	permissions := services.Permissions{}
	env := maintenance.Env{Runner: runner, Permissions: permissions, HomeDir: home, Log: logger}
	svc, err := maintenance.NewService(tasks, env, elevate.New(exe, elevate.NewLauncher(runner)))
	if err != nil {
		log.Fatal(err)
	}
	store, err := history.Open(filepath.Join(dataDir, historyFile))
	if err != nil {
		log.Fatal(err)
	}
	monitor, err := health.New(health.DefaultConfig(), health.NewProcessSource())
	if err != nil {
		log.Fatal(err)
	}

	mygo.Bind(services.NewMaintenance(svc, store, logger))
	mygo.Bind(services.NewHealth(monitor, func(ctx context.Context) error { return health.OpenSystemMonitor(ctx, runner) }))
	mygo.Bind(services.NewHistory(store))
	mygo.Bind(permissions)

	var (
		mu  sync.Mutex
		win *mygo.Window
	)
	openWindow := func() {
		mu.Lock()
		defer mu.Unlock()
		if win != nil {
			win.Restore()
			win.Focus()
			return
		}
		win = mygo.NewWindow(mygo.WindowOptions{
			Title:     "Spruce",
			Width:     1280,
			Height:    820,
			MinWidth:  960,
			MinHeight: 680,
			// The page's darkest background (--color-bg-deep in src/styles/tokens.css), so the window never flashes white while the page loads.
			BackgroundColor: "#1C0F26",
			// The sidebar runs to the top of the window under the traffic lights (macOS) and the caption buttons sit over the page (Windows).
			TitleBarStyle: mygo.TitleBarHidden,
			StateKey:      "main",
			URL:           "/",
		})
		win.OnClosed(func() {
			mu.Lock()
			win = nil
			mu.Unlock()
		})
	}
	mygo.App.OnSecondInstance(func([]string, string) { openWindow() })
	mygo.App.OnActivate(func(hasVisibleWindows bool) {
		if !hasVisibleWindows {
			openWindow()
		}
	})

	ctx, cancel := context.WithCancel(context.Background())
	mygo.App.OnQuit(cancel)
	mygo.App.WhenReady(func() {
		go func() {
			if err := monitor.Run(ctx); err != nil && ctx.Err() == nil {
				logger.Error("health monitor stopped", "err", err)
			}
		}()
		go services.BroadcastAlerts(ctx, monitor)
		openWindow()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
