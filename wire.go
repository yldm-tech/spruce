package main

import (
	"context"
	"log/slog"
	"path/filepath"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
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
	"github.com/yldm-tech/spruce/internal/host/loginitem"
	uidevjunk "github.com/yldm-tech/spruce/internal/ui/pages/devjunk"
	uihealth "github.com/yldm-tech/spruce/internal/ui/pages/health"
	uihistory "github.com/yldm-tech/spruce/internal/ui/pages/history"
	uiinstallers "github.com/yldm-tech/spruce/internal/ui/pages/installers"
	uimaintenance "github.com/yldm-tech/spruce/internal/ui/pages/maintenance"
	uisettings "github.com/yldm-tech/spruce/internal/ui/pages/settings"
	uismartscan "github.com/yldm-tech/spruce/internal/ui/pages/smartscan"
	uispace "github.com/yldm-tech/spruce/internal/ui/pages/space"
	uisystemjunk "github.com/yldm-tech/spruce/internal/ui/pages/systemjunk"
	uiuninstall "github.com/yldm-tech/spruce/internal/ui/pages/uninstall"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Files inside the app's data folder.
const (
	historyFile  = "history.jsonl"
	settingsFile = "settings.json"
	iconCache    = "app-icons"
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

	// The cleaners 智能扫描 combines. Each call builds a new instance: a scanner remembers its last scan for the clean that follows, so a page and 智能扫描 each get their own (see internal/engine/smartscan).
	newSystemJunk := func() cleanup.Scanner {
		return systemjunk.New(systemjunk.Env{Home: e.home, Runner: e.runner, Settings: settingsStore.Get, Log: e.log})
	}
	newDevJunk := func() cleanup.Scanner {
		return devjunk.New(devjunk.Options{Home: e.home, Runner: e.runner, Settings: func() settings.CleanupSettings { return settingsStore.Get().Cleanup }, Log: e.log})
	}
	newInstallers := func() cleanup.Scanner {
		return installers.New(installers.Env{Home: e.home, Runner: e.runner, Settings: settingsStore.Get, Log: e.log})
	}
	cleanups := app.NewCleanup(historyStore, e.home, e.log)
	app.RegisterCleaner(cleanups, app.CleanerSmartScan, smartscan.New([]smartscan.Source{
		{ID: string(app.CleanerSystemJunk), New: newSystemJunk},
		{ID: string(app.CleanerDevJunk), New: newDevJunk},
		{ID: string(app.CleanerInstallers), New: newInstallers},
	}, e.log))
	app.RegisterCleaner(cleanups, app.CleanerSystemJunk, newSystemJunk())
	app.RegisterCleaner(cleanups, app.CleanerDevJunk, newDevJunk())
	app.RegisterCleaner(cleanups, app.CleanerInstallers, newInstallers())

	files := app.NewFiles(e.runner, trash.System(), e.log)
	maint := app.NewMaintenance(svc, historyStore, e.log)
	healthSvc := app.NewHealth(monitor, func(ctx context.Context) error { return health.OpenSystemMonitor(ctx, e.runner) })
	uninstaller := app.NewUninstall(uninstall.New(uninstall.Config{
		Home:       e.home,
		IconCache:  filepath.Join(e.dataDir, iconCache),
		Runner:     e.runner,
		Exclusions: func() []string { return settingsStore.Get().Cleanup.Exclusions },
		Self:       e.exe,
	}), historyStore, e.home, e.log)
	spaceSvc := app.NewSpace(e.home, settingsStore.Get, trash.System(), historyStore, e.log)
	version := ""
	if info := core.App(); !info.Development {
		version = info.Version
	}
	settingsSvc := app.NewSettings(settingsStore, loginitem.New(appID), e.log, version, e.dataDir)

	return shell.Deps{
		Stores: store.Deps{Settings: settingsSvc, SettingsFeed: settingsStore, History: app.NewHistory(historyStore), Alerts: healthSvc,
			Permissions: permissions, Locations: spaceSvc},
		SmartScan:   uismartscan.Deps{Cleanup: cleanups, Files: files},
		SystemJunk:  uisystemjunk.Deps{Cleanup: cleanups, Files: files},
		DevJunk:     uidevjunk.Deps{Cleanup: cleanups, Files: files},
		Installers:  uiinstallers.Deps{Cleanup: cleanups, Files: files},
		Uninstall:   uiuninstall.Deps{Uninstall: uninstaller, Files: files},
		Space:       uispace.Deps{Space: spaceSvc, Files: files},
		Maintenance: uimaintenance.Deps{Maintenance: maint},
		Health:      uihealth.Deps{Sysinfo: app.NewSysinfo(vitals)},
		History:     uihistory.Deps{Catalog: maint},
		Settings:    uisettings.Deps{Files: files},
		Background: []func(ctx context.Context){
			func(ctx context.Context) {
				if err := monitor.Run(ctx); err != nil && ctx.Err() == nil {
					e.log.Error("health monitor stopped", "err", err)
				}
			},
			func(ctx context.Context) { app.FollowSettings(ctx, settingsStore, monitor, historyStore, e.log) },
		},
		DataDir: e.dataDir,
		Log:     e.log,
	}, nil
}
