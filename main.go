// Command spruce is the Spruce desktop app: a disk cleaner and system maintenance tool for macOS and Windows.
//
// main does four things, in this order: it serves the elevated child when started with elevate.Flag (before any NSGo code runs), it runs the command line when the first argument is one of its commands (cli.IsCommand; without the single-instance lock or any UI), it takes the single-instance lock in the data folder, and it wires the engine into the UI (wire) and runs it with nsgo.Run.
package main

import (
	"context"
	_ "embed"
	"log"
	"log/slog"
	"os"
	"os/signal"
	"path/filepath"
	"runtime"
	"sync"
	"syscall"
	"time"

	"github.com/nsgo-dev/nsgo"
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/cli"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/elevate"
	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/engine/trash"
	"github.com/yldm-tech/spruce/internal/engine/undo"
	"github.com/yldm-tech/spruce/internal/host/datadir"
	"github.com/yldm-tech/spruce/internal/host/instance"
	"github.com/yldm-tech/spruce/internal/host/loginitem"
	"github.com/yldm-tech/spruce/internal/ui/shell"
)

// trayIcon and trayAlertIcon are the menu bar icon's template images (36 by 36 pixels, 18 points at 2x), the second for low space; they live here because go:embed cannot reach outside a package's folder.
var (
	//go:embed assets/tray.png
	trayIcon []byte
	//go:embed assets/tray-alert.png
	trayAlertIcon []byte
)

// backgroundGrace is how long main waits for the background work to stop after the app quits; cleanGrace is how long it gives a running clean to finish its current item and record what it did.
const (
	backgroundGrace = 2 * time.Second
	cleanGrace      = 30 * time.Second
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

	// The command line: the same executable with a command as its first argument. It never takes the single-instance lock nor talks to a running app; it shares only the clean lock with it.
	if cli.IsCommand(os.Args[1:]) {
		// Spruce.exe is a GUI program; the shell's console has to be attached before anything is written.
		cli.AttachConsole()
		cliLog := slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn}))
		os.Exit(runCLI(os.Args[1:], cliLog, runner))
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
	dataDir, err := datadir.Dir()
	if err != nil {
		log.Fatal(err)
	}

	inst, first, err := instance.Acquire(dataDir)
	switch {
	case err != nil:
		logger.Warn("single instance: running without the lock", "err", err)
	case !first:
		return // the running copy was told to come to the front
	}

	deps, err := wire(env{home: home, dataDir: dataDir, exe: exe, runner: runner, tasks: tasks, log: logger})
	if err != nil {
		log.Fatal(err)
	}
	deps.Instance = inst
	deps.SetMainView = nsgo.SetMainView
	deps.TrayIcon, deps.TrayAlertIcon = trayIcon, trayAlertIcon
	// The Apple event that says the system opened Spruce at login is read as the app finishes launching, so the observer goes in before nsgo.Run.
	loginitem.WatchLaunch()
	deps.LaunchedAtLogin = loginitem.LaunchedAtLogin
	if err := loginitem.New(appID).Refresh(); err != nil {
		logger.Warn("login item: rewrite the launch agent", "err", err)
	}
	var background sync.WaitGroup
	deps.Background = tracked(&background, deps.Background)

	opts := []nsgo.Option{
		nsgo.WindowSize(1280, 820),
		nsgo.WindowConstraints(core.WindowConstraints{MinWidth: 960, MinHeight: 680}),
		nsgo.Fonts("fonts/Inter-Regular.ttf", "fonts/Inter-Medium.ttf", "fonts/Inter-SemiBold.ttf", "fonts/Inter-Bold.ttf"),
	}
	if frame, ok := shell.LoadFrame(dataDir); ok {
		opts = append(opts, nsgo.WindowFrame(frame))
	}
	// Staying in the menu bar, Spruce starts out of the Dock, so its icon never flashes there at a login start; opening the window shows it.
	if runtime.GOOS == "darwin" && deps.Stores.Settings.Get().General.KeepInTray {
		opts = append(opts, nsgo.HideDock())
	}
	nsgo.Run(shell.App(deps), opts...)

	// NSGo cannot keep the app running for a clean: every clean stops after its current item and still writes its history.
	deps.Activity.Shutdown()
	if !deps.Activity.Wait(cleanGrace) {
		logger.Warn("a clean did not stop in time")
	}
	// The app's OnQuit cancelled the background work; give it a moment to stop before the lock goes.
	done := make(chan struct{})
	go func() {
		background.Wait()
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(backgroundGrace):
		logger.Warn("background work did not stop in time")
	}
	if inst != nil {
		if err := inst.Close(); err != nil {
			logger.Warn("release the single-instance lock", "err", err)
		}
	}
}

// tracked wraps each of work so wg counts it while it runs.
func tracked(wg *sync.WaitGroup, work []func(ctx context.Context)) []func(ctx context.Context) {
	out := make([]func(ctx context.Context), len(work))
	for i, w := range work {
		wg.Add(1)
		out[i] = func(ctx context.Context) {
			defer wg.Done()
			w(ctx)
		}
	}
	return out
}

// runCLI runs a command of the command line over the app's engine (wireEngine) and returns its exit code. Ctrl+C and SIGTERM cancel it: a scan stops, a clean stops after its current item and still records what it did. Logs go to stderr, warnings and errors only.
func runCLI(args []string, log *slog.Logger, runner execx.Runner) int {
	slog.SetDefault(log)
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	home, err := os.UserHomeDir()
	if err != nil {
		log.Error("home folder", "err", err)
		return cli.ExitFailed
	}
	d := cli.Deps{Version: appVersion(), Home: home, GOOS: runtime.GOOS, CanRestore: trash.CanRestore()}
	d.Engine = func(sources []string) (cli.Engine, error) {
		exe, err := os.Executable()
		if err != nil {
			return nil, err
		}
		if exe, err = filepath.EvalSymlinks(exe); err != nil {
			return nil, err
		}
		dataDir, err := datadir.Dir()
		if err != nil {
			return nil, err
		}
		eng, err := wireEngine(env{home: home, dataDir: dataDir, exe: exe, runner: runner, log: log, sources: sources})
		if err != nil {
			return nil, err
		}
		return cliEngine{eng}, nil
	}
	return cli.Main(ctx, args, d, os.Stdout, os.Stderr)
}

// cliEngine is the command line's view of the engine: 概览's foreground merged scan, cleaned as the command line through app.Cleanup.
type cliEngine struct{ e engine }

func (c cliEngine) Scan(ctx context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	return c.e.cleanups.Scan(ctx, app.CleanerSmartScan, progress)
}

func (c cliEngine) InstalledApps(ctx context.Context) ([]string, error) {
	return c.e.installedApps(ctx)
}

func (c cliEngine) Settings() settings.AppSettings { return c.e.settings.Get() }

func (c cliEngine) Clean(ctx context.Context, req cleanup.CleanRequest, opts app.CleanOptions, progress func(cleanup.Progress)) (app.CleanRun, error) {
	return c.e.cleanups.CleanWith(ctx, app.CleanerSmartScan, req, opts, progress)
}

func (c cliEngine) History(limit int) ([]history.Run, error) { return c.e.historySvc.List(limit) }

func (c cliEngine) Restorable(runs []history.Run, now time.Time) map[string]bool {
	return c.e.historySvc.Restorable(runs, now)
}

func (c cliEngine) Entry(runID string) (undo.Entry, error) { return c.e.ledger.Load(runID) }

func (c cliEngine) Restore(ctx context.Context, runID string) (app.RestoreOutcome, error) {
	return c.e.restore.Run(ctx, runID)
}
