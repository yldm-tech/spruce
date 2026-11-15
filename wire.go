package main

import (
	"context"
	_ "embed"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/shirou/gopsutil/v4/disk"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/devjunk"
	"github.com/yldm-tech/spruce/internal/engine/elevate"
	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/growth"
	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/installers"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/scancache"
	"github.com/yldm-tech/spruce/internal/engine/schedule"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/engine/smartscan"
	"github.com/yldm-tech/spruce/internal/engine/sysinfo"
	"github.com/yldm-tech/spruce/internal/engine/systemjunk"
	"github.com/yldm-tech/spruce/internal/engine/trash"
	"github.com/yldm-tech/spruce/internal/engine/undo"
	"github.com/yldm-tech/spruce/internal/engine/uninstall"
	"github.com/yldm-tech/spruce/internal/host/folderaccess"
	"github.com/yldm-tech/spruce/internal/host/fulldisk"
	"github.com/yldm-tech/spruce/internal/host/instance"
	"github.com/yldm-tech/spruce/internal/host/loginitem"
	uidevjunk "github.com/yldm-tech/spruce/internal/ui/pages/devjunk"
	uihealth "github.com/yldm-tech/spruce/internal/ui/pages/health"
	uihistory "github.com/yldm-tech/spruce/internal/ui/pages/history"
	uiinstallers "github.com/yldm-tech/spruce/internal/ui/pages/installers"
	uimaintenance "github.com/yldm-tech/spruce/internal/ui/pages/maintenance"
	uioverview "github.com/yldm-tech/spruce/internal/ui/pages/overview"
	uisettings "github.com/yldm-tech/spruce/internal/ui/pages/settings"
	uispace "github.com/yldm-tech/spruce/internal/ui/pages/space"
	uisystemjunk "github.com/yldm-tech/spruce/internal/ui/pages/systemjunk"
	uiuninstall "github.com/yldm-tech/spruce/internal/ui/pages/uninstall"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Files inside the app's data folder.
// appIcon is the app's icon, which 设置's 关于 shows.
//
//go:embed assets/Icon.png
var appIcon []byte

const (
	historyFile  = "history.jsonl"
	settingsFile = "settings.json"
	iconCache    = "app-icons"
	// cleanLock is the lock every clean and the background work hold, shared with the command line.
	cleanLock = "clean.lock"
	// undoDir is the undo ledger: what each clean moved to the Trash, for 撤销 and 放回原处.
	undoDir = "undo"
	// overviewCacheFile is 概览's last check, shown at the next launch until a new one ends.
	overviewCacheFile = "overview-cache.json"
	// growthDir holds the space snapshots.
	growthDir = "growth"
	// backgroundFile is what the scheduled work remembers: the rules' last runs, the weekly summary, the low-disk alert.
	backgroundFile = "background.json"
	// folderAccessFile lists the folders that prompt for access which a check the user started has entered.
	folderAccessFile = "folder-access.json"
)

// appID is nsgo.yaml's id: the login item's name.
const appID = "ai.yldm.spruce"

// env is what wire builds the app from: the user's home and data folders, the running executable, the command runner, the maintenance catalog and the log.
type env struct {
	home, dataDir, exe string
	runner             execx.Runner
	tasks              []maintenance.TaskImpl
	log                *slog.Logger
}

// wire builds the engine and the controllers over it and returns what the shell and the pages read. Nothing reads or writes the user's data before first use, and nothing runs until the app starts Background.
func wire(e env) (shell.Deps, error) {
	permissions := app.Permissions{Runner: e.runner}
	mEnv := maintenance.Env{Runner: e.runner, Permissions: permissions, HomeDir: e.home, Log: e.log}
	svc, err := maintenance.NewService(e.tasks, mEnv, elevate.New(e.exe, elevate.NewLauncher(e.runner)))
	if err != nil {
		return shell.Deps{}, err
	}
	historyStore, err := history.Open(filepath.Join(e.dataDir, historyFile))
	if err != nil {
		return shell.Deps{}, err
	}
	settingsStore, err := settings.Open(filepath.Join(e.dataDir, settingsFile), e.home, func() settings.AppSettings { return settings.Defaults(e.home) })
	if err != nil {
		return shell.Deps{}, err
	}
	// The monitor starts with the shipped defaults; FollowSettings applies the user's threshold, sustain and on/off, and again on every change.
	monitor, err := health.New(health.DefaultConfig(), health.NewProcessSource())
	if err != nil {
		return shell.Deps{}, err
	}
	vitals, err := sysinfo.New(sysinfo.DefaultConfig(), sysinfo.NewSource(e.runner), e.log)
	if err != nil {
		return shell.Deps{}, err
	}

	// The cleaners 概览 combines. Each call builds a new instance: a scanner remembers its last scan for the clean that follows, so a page and each of 概览's two merged scans get their own (see internal/engine/smartscan). mayRead is the read gate of the instance's folder reads; nil reads everything.
	newSystemJunk := func(mayRead func(string) bool) cleanup.Scanner {
		return systemjunk.New(systemjunk.Env{Home: e.home, Runner: e.runner, Settings: settingsStore.Get, Log: e.log, MayRead: mayRead})
	}
	newDevJunk := func(mayRead func(string) bool) cleanup.Scanner {
		return devjunk.New(devjunk.Options{Home: e.home, Runner: e.runner, Settings: func() settings.CleanupSettings { return settingsStore.Get().Cleanup }, Log: e.log, MayRead: mayRead})
	}
	newInstallers := func(mayRead func(string) bool) cleanup.Scanner {
		return installers.New(installers.Env{Home: e.home, Runner: e.runner, Settings: settingsStore.Get, Log: e.log, MayRead: mayRead})
	}
	merged := func(mayRead func(string) bool) *smartscan.Scanner {
		return smartscan.New([]smartscan.Source{
			{ID: string(app.CleanerSystemJunk), New: func() cleanup.Scanner { return newSystemJunk(mayRead) }},
			{ID: string(app.CleanerDevJunk), New: func() cleanup.Scanner { return newDevJunk(mayRead) }},
			{ID: string(app.CleanerInstallers), New: func() cleanup.Scanner { return newInstallers(mayRead) }},
		}, e.log)
	}
	activity := schedule.NewActivity(cleanLockFunc(filepath.Join(e.dataDir, cleanLock), e.log))
	ledger, err := undo.Open(filepath.Join(e.dataDir, undoDir))
	if err != nil {
		return shell.Deps{}, err
	}
	// The read gate: background checks never enter a folder that prompts for access (macOS) until a check the user started has entered it, or Spruce holds Full Disk Access.
	access := folderaccess.Open(filepath.Join(e.dataDir, folderAccessFile), e.home, func() bool { return fulldisk.Check() == fulldisk.Granted })
	gate := app.NewGateRecorder(access)
	cleanups := app.NewCleanup(historyStore, e.home, e.log, activity)
	app.SetLedger(cleanups, ledger)
	app.RegisterCleaner(cleanups, app.CleanerSmartScan, merged(gate.Foreground))
	app.RegisterCleaner(cleanups, app.CleanerSmartScanBackground, merged(gate.Background))

	files := app.NewFiles(e.runner, trash.System(), e.log)
	maint := app.NewMaintenance(svc, historyStore, e.log, activity)
	healthSvc := app.NewHealth(monitor, func(ctx context.Context) error { return health.OpenSystemMonitor(ctx, e.runner) })
	uninstaller := app.NewUninstall(uninstall.New(uninstall.Config{
		Home:       e.home,
		IconCache:  filepath.Join(e.dataDir, iconCache),
		Runner:     e.runner,
		Exclusions: func() []string { return settingsStore.Get().Cleanup.Exclusions },
		Self:       e.exe,
	}), historyStore, e.home, e.log, activity)
	app.SetUninstallLedger(uninstaller, ledger)
	spaceSvc := app.NewSpace(e.home, settingsStore.Get, trash.System(), historyStore, e.log, activity)
	app.SetSpaceLedger(spaceSvc, ledger)
	version := ""
	if info := core.App(); !info.Development {
		version = info.Version
	}
	settingsSvc := app.NewSettings(settingsStore, loginitem.New(appID), e.log, version, e.dataDir)

	cache, err := scancache.Open(filepath.Join(e.dataDir, overviewCacheFile))
	if err != nil {
		return shell.Deps{}, err
	}
	snapshots, err := growth.Open(filepath.Join(e.dataDir, growthDir))
	if err != nil {
		return shell.Deps{}, err
	}
	historySvc := app.NewHistory(historyStore, ledger, e.log)
	// 清空历史 also clears the cached check and the space snapshots.
	app.OnClear(historySvc, cache.Clear)
	app.OnClear(historySvc, snapshots.Clear)
	// The installed apps name the installers' apps on the cards; an engine of its own, since Apps replaces the list 应用卸载's engine remembers.
	appList := uninstall.New(uninstall.Config{
		Home:       e.home,
		IconCache:  filepath.Join(e.dataDir, iconCache),
		Runner:     e.runner,
		Exclusions: func() []string { return settingsStore.Get().Cleanup.Exclusions },
		Self:       e.exe,
	})
	overview := app.NewOverview(app.OverviewConfig{
		Cleanup:    cleanups,
		Foreground: app.CleanerSmartScan,
		Background: app.CleanerSmartScanBackground,
		Cache:      cache,
		Restore:    app.NewRestore(ledger, trash.SystemRestorer(), historyStore, activity, e.home, e.log),
		Settings:   settingsStore,
		Activity:   activity,
		Policy:     schedule.DefaultPolicy(),
		Conditions: schedule.SystemConditions(e.runner),
		State:      schedule.OpenState(filepath.Join(e.dataDir, backgroundFile)),
		Access:     access,
		Gate:       gate,
		Growth:     snapshots,
		Watches: func(s settings.AppSettings) []growth.Watch {
			return growth.DefaultWatches(runtime.GOOS, e.home, os.Getenv, s.Cleanup.DevProjectRoots)
		},
		GrowthOptions: growth.TakeOptions{
			Usage:          diskUsage,
			FullDiskAccess: func() bool { return fulldisk.Check() == fulldisk.Granted },
		},
		Disk: func(ctx context.Context) (app.DiskState, error) {
			d, ok := app.SystemDisk(spaceSvc.Locations(ctx))
			if !ok {
				return app.DiskState{}, errors.New("no disk")
			}
			return d, nil
		},
		InstalledApps: func(ctx context.Context) ([]string, error) {
			apps, err := appList.Apps(ctx)
			if err != nil {
				return nil, err
			}
			names := make([]string, 0, len(apps))
			for _, a := range apps {
				names = append(names, a.Name)
			}
			return names, nil
		},
		TrashSize:  files.TrashSize,
		Ledger:     ledger,
		History:    historyStore,
		Home:       e.home,
		AppVersion: version,
		GOOS:       runtime.GOOS,
		Log:        e.log,
	})

	return shell.Deps{
		Stores: store.Deps{Settings: settingsSvc, SettingsFeed: settingsStore, History: historySvc, Alerts: healthSvc,
			Permissions: permissions, Locations: spaceSvc, Overview: overview},
		Overview:    uioverview.Deps{Files: files, Home: e.home},
		SystemJunk:  uisystemjunk.Deps{Files: files},
		DevJunk:     uidevjunk.Deps{Files: files},
		Installers:  uiinstallers.Deps{Files: files},
		Uninstall:   uiuninstall.Deps{Uninstall: uninstaller, Files: files},
		Space:       uispace.Deps{Space: spaceSvc, Files: files},
		Maintenance: uimaintenance.Deps{Maintenance: maint},
		Health:      uihealth.Deps{Sysinfo: app.NewSysinfo(vitals)},
		History:     uihistory.Deps{Catalog: maint},
		Settings:    uisettings.Deps{Files: files, Icon: appIcon},
		Background: []func(ctx context.Context){
			func(ctx context.Context) { app.RecoverInterrupted(ctx, ledger, historyStore, activity, e.log) },
			func(ctx context.Context) {
				if err := monitor.Run(ctx); err != nil && ctx.Err() == nil {
					e.log.Error("health monitor stopped", "err", err)
				}
			},
			func(ctx context.Context) {
				app.FollowSettings(ctx, settingsStore, monitor, historyStore, ledger, e.log)
			},
			overview.Run,
		},
		DataDir:  e.dataDir,
		Log:      e.log,
		Activity: activity,
	}, nil
}

// diskUsage is the size and free space of the volume holding path.
func diskUsage(path string) (total, free int64, err error) {
	u, err := disk.Usage(path)
	if err != nil {
		return 0, 0, err
	}
	return int64(u.Total), int64(u.Free), nil
}

// cleanLockFunc takes the clean lock at path for schedule.Activity, logging where it is, why it failed and who owns the file when it cannot be used; Activity then refuses the clean rather than run without it.
func cleanLockFunc(path string, log *slog.Logger) schedule.LockFunc {
	return func() (func() error, bool, error) {
		release, held, err := instance.LockFile(path)
		if err != nil {
			uid, known := instance.Owner(path)
			log.Error("clean lock unusable", "path", path, "err", err, "owner", uid, "ownerKnown", known)
		}
		return release, held, err
	}
}
