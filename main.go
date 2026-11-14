// Command spruce is the Spruce desktop app: a disk cleaner and system maintenance tool for macOS and Windows.
//
// main does three things, in this order: it serves the elevated child when started with elevate.Flag (before any mygo code runs), it wires the engine into the bound services, and it opens the window once the app is ready.
//
// Pages: every page of the sidebar wires its engine in the block between "---- Pages ----" and "---- End of pages ----" below, under its own "// page: <route>" anchor, and nowhere else in this file. A cleaner page registers its cleanup.Scanner with services.RegisterCleaner; any other page binds its own service with mygo.Bind; work that must run while the app is up goes into onReady. The values a page may use are the locals declared before the block: home, dataDir, runner, logger, permissions, settingsStore, historyStore, monitor and cleanups.
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
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/devjunk"
	"github.com/yldm-tech/spruce/internal/engine/elevate"
	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/installers"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/engine/smartscan"
	"github.com/yldm-tech/spruce/internal/engine/sysinfo"
	"github.com/yldm-tech/spruce/internal/engine/systemjunk"
	"github.com/yldm-tech/spruce/internal/engine/trash"
	"github.com/yldm-tech/spruce/internal/engine/uninstall"
	"github.com/yldm-tech/spruce/internal/services"
)

// Files inside the app's user data directory.
const (
	historyFile  = "history.jsonl"
	settingsFile = "settings.json"
)

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
	historyStore, err := history.Open(filepath.Join(dataDir, historyFile))
	if err != nil {
		log.Fatal(err)
	}
	// Neither store reads or writes before first use, so `mygo generate` (which runs main up to App.Run) never touches the user's data.
	settingsStore, err := settings.Open(filepath.Join(dataDir, settingsFile), home, func() settings.AppSettings { return settings.Defaults(home) })
	if err != nil {
		log.Fatal(err)
	}
	// The monitor starts with the shipped defaults; FollowSettings applies the user's threshold, sustain and on/off once the app is ready, and again on every change.
	monitor, err := health.New(health.DefaultConfig(), health.NewProcessSource())
	if err != nil {
		log.Fatal(err)
	}
	cleanups := services.NewCleanup(historyStore, home, logger)

	mygo.Bind(services.NewMaintenance(svc, historyStore, logger))
	mygo.Bind(services.NewHealth(monitor, func(ctx context.Context) error { return health.OpenSystemMonitor(ctx, runner) }))
	mygo.Bind(services.NewHistory(historyStore))
	mygo.Bind(permissions)
	mygo.Bind(services.NewSettings(settingsStore, mygo.App, logger))
	mygo.Bind(services.NewFiles(runner, trash.System(), logger))
	mygo.Bind(cleanups)

	// onReady holds work the pages start once the app is ready, with a context that ends when it quits.
	var onReady []func(ctx context.Context)

	// ---- Pages ----
	// One block per page, under its anchor; each page edits only its own block (see the file comment).

	// The cleaners 智能扫描 combines. Each call builds a new instance: a scanner remembers its last scan for the clean that follows, so the page and 智能扫描 each get their own (see internal/engine/smartscan).
	newSystemJunk := func() cleanup.Scanner {
		return systemjunk.New(systemjunk.Env{Home: home, Runner: runner, Settings: settingsStore.Get, Log: logger})
	}
	newDevJunk := func() cleanup.Scanner {
		return devjunk.New(devjunk.Options{
			Home:     home,
			Runner:   runner,
			Settings: func() settings.CleanupSettings { return settingsStore.Get().Cleanup },
			Log:      logger,
		})
	}
	newInstallers := func() cleanup.Scanner {
		return installers.New(installers.Env{Home: home, Runner: runner, Settings: settingsStore.Get, Log: logger})
	}

	// page: smart-scan
	services.RegisterCleaner(cleanups, services.CleanerSmartScan, smartscan.New([]smartscan.Source{
		{ID: string(services.CleanerSystemJunk), New: newSystemJunk},
		{ID: string(services.CleanerDevJunk), New: newDevJunk},
		{ID: string(services.CleanerInstallers), New: newInstallers},
	}, logger))

	// page: system-junk
	services.RegisterCleaner(cleanups, services.CleanerSystemJunk, newSystemJunk())

	// page: dev-junk
	services.RegisterCleaner(cleanups, services.CleanerDevJunk, newDevJunk())

	// page: installers
	services.RegisterCleaner(cleanups, services.CleanerInstallers, newInstallers())

	// page: uninstall
	mygo.Bind(services.NewUninstall(uninstall.New(uninstall.Config{
		Home:       home,
		IconCache:  filepath.Join(dataDir, "app-icons"),
		Runner:     runner,
		Exclusions: func() []string { return settingsStore.Get().Cleanup.Exclusions },
		Self:       exe,
	}), historyStore, home, logger))

	// page: space
	mygo.Bind(services.NewSpace(home, settingsStore.Get, trash.System(), historyStore, logger))

	// page: health
	// The dashboard's sampler runs only while the page watches it; health alerts come from monitor, as on the 维护 page.
	vitals, err := sysinfo.New(sysinfo.DefaultConfig(), sysinfo.NewSource(runner), logger)
	if err != nil {
		log.Fatal(err)
	}
	mygo.Bind(services.NewSysinfo(vitals))

	// page: history
	// Every run any page records reaches 操作历史 as events.historyAppended. Retention pruning (at start, on every change of 保留天数 and daily) is FollowSettings' job.
	onReady = append(onReady, func(ctx context.Context) { services.BroadcastHistory(ctx, historyStore, logger) })

	// page: settings

	// ---- End of pages ----

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
		go services.BroadcastSettings(ctx, settingsStore, mygo.App)
		go services.FollowSettings(ctx, settingsStore, monitor, historyStore, logger)
		for _, start := range onReady {
			go start(ctx)
		}
		openWindow()
	})
	if err := mygo.App.Run(); err != nil {
		log.Fatal(err)
	}
}
