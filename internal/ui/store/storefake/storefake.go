// Package storefake is store.Deps for tests: services that answer from fields, and Provide, which mounts the stores in a test's root the way the shell does. Only tests import it.
package storefake

import (
	"context"
	"sync"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Services are fakes of every service the stores read. Change their fields before Provide, or through their methods while a test runs.
type Services struct {
	mu sync.Mutex

	Settings settings.AppSettings
	Runs     []history.Run
	// Alerts is the first answer; Alert sends later lists.
	Alerts []health.Alert
	// Permission is what FullDiskAccess answers, Guided what the guide ends with, GuideErr its error.
	Permission maintenance.PermissionState
	Guided     maintenance.PermissionState
	GuideErr   error
	// GuideRelease, when set, holds the guide until it receives; CheckRelease holds every FullDiskAccess check the same way.
	GuideRelease chan struct{}
	CheckRelease chan struct{}
	// MonitorErr is what OpenSystemMonitor returns; Monitors counts its calls.
	MonitorErr error
	Monitors   int
	// Guides counts the guide's calls.
	Guides    int
	Locations app.SpaceLocations
	// ClearErr is what clearing the history fails with, clearing nothing; ClearRelease, when set, holds each clear until it receives.
	ClearErr     error
	ClearRelease chan struct{}

	settingsCh chan settings.AppSettings
	alertsCh   chan []health.Alert
	runsCh     chan history.Run
}

// New returns fakes holding the default settings for home, no runs, no alerts and Full Disk Access granted.
func New(home string) *Services {
	return &Services{Settings: settings.Defaults(home), Permission: maintenance.PermissionGranted, Guided: maintenance.PermissionGranted,
		settingsCh: make(chan settings.AppSettings, 1), alertsCh: make(chan []health.Alert, 1), runsCh: make(chan history.Run, 16)}
}

// Deps returns the fakes as store.Deps.
func (s *Services) Deps() store.Deps {
	return store.Deps{Settings: settingsSvc{s}, SettingsFeed: settingsSvc{s}, History: historySvc{s}, Alerts: alertSvc{s},
		Permissions: permissionSvc{s}, Locations: locationSvc{s}}
}

// Alert broadcasts list as the monitor's current alerts.
func (s *Services) Alert(list []health.Alert) {
	select {
	case <-s.alertsCh:
	default:
	}
	s.alertsCh <- list
}

// Record announces run as if a page recorded it.
func (s *Services) Record(run history.Run) {
	s.mu.Lock()
	s.Runs = append([]history.Run{run}, s.Runs...)
	s.mu.Unlock()
	s.runsCh <- run
}

// SetPermission changes what FullDiskAccess answers from now on.
func (s *Services) SetPermission(st maintenance.PermissionState) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Permission = st
}

// Counts returns how often the system monitor was opened and the guide started.
func (s *Services) Counts() (monitors, guides int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.Monitors, s.Guides
}

// Provide makes the stores in cx and provides them, and their Blocker, as the shell does.
func Provide(cx *core.Scope, d store.Deps) *store.Stores {
	st := store.New(cx, cx.Context(), d)
	store.Key.Provide(cx, st)
	kit.BlockerKey.Provide(cx, st.Block)
	return st
}

type settingsSvc struct{ s *Services }

func (f settingsSvc) Get() settings.AppSettings {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.s.Settings
}

func (f settingsSvc) Update(next settings.AppSettings) (settings.AppSettings, error) {
	f.s.mu.Lock()
	f.s.Settings = next
	f.s.mu.Unlock()
	return next, nil
}

func (f settingsSvc) Reset() (settings.AppSettings, error) { return f.Get(), nil }

func (f settingsSvc) Limits() app.SettingsLimits {
	return app.SettingsLimits{
		CPUThreshold:          app.SettingRange{Min: settings.MinCPUThreshold, Max: settings.MaxCPUThreshold},
		SustainMinutes:        app.SettingRange{Min: settings.MinSustainMinutes, Max: settings.MaxSustainMinutes},
		DevArtifactMinAgeDays: app.SettingRange{Min: settings.MinDevArtifactMinAge, Max: settings.MaxDevArtifactMinAge},
		LargeFileMinMB:        app.SettingRange{Min: settings.MinLargeFileMB, Max: settings.MaxLargeFileMB},
		RetentionDays:         app.SettingRange{Min: settings.MinRetentionDays, Max: settings.MaxRetentionDays},
		MaxPaths:              settings.MaxPaths,
	}
}

func (f settingsSvc) About() (app.AppAbout, error) { return app.AppAbout{Version: "0.1.0"}, nil }

func (f settingsSvc) Subscribe() (<-chan settings.AppSettings, func()) {
	f.s.settingsCh <- f.Get()
	return f.s.settingsCh, func() {}
}

type historySvc struct{ s *Services }

func (f historySvc) List(limit int) ([]history.Run, error) {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	runs := f.s.Runs
	if len(runs) > limit {
		runs = runs[:limit]
	}
	return append([]history.Run(nil), runs...), nil
}

func (f historySvc) Clear() error {
	f.s.mu.Lock()
	release := f.s.ClearRelease
	f.s.mu.Unlock()
	if release != nil {
		<-release
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	if f.s.ClearErr != nil {
		return f.s.ClearErr
	}
	f.s.Runs = nil
	return nil
}

func (f historySvc) Subscribe() (<-chan history.Run, func(), bool) {
	return f.s.runsCh, func() {}, true
}

type alertSvc struct{ s *Services }

func (f alertSvc) Alerts() []health.Alert {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.s.Alerts
}

func (f alertSvc) Subscribe() (<-chan []health.Alert, func()) { return f.s.alertsCh, func() {} }

func (f alertSvc) OpenSystemMonitor(context.Context) error {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	f.s.Monitors++
	return f.s.MonitorErr
}

type permissionSvc struct{ s *Services }

func (f permissionSvc) FullDiskAccess() (maintenance.PermissionState, error) {
	f.s.mu.Lock()
	release := f.s.CheckRelease
	f.s.mu.Unlock()
	if release != nil {
		<-release
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.s.Permission, nil
}

func (f permissionSvc) GuideFullDiskAccess(ctx context.Context) (maintenance.PermissionState, error) {
	f.s.mu.Lock()
	f.s.Guides++
	release := f.s.GuideRelease
	f.s.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	if f.s.GuideErr != nil {
		return f.s.Permission, f.s.GuideErr
	}
	f.s.Permission = f.s.Guided
	return f.s.Guided, nil
}

type locationSvc struct{ s *Services }

// SetLocations changes what Locations answers from now on.
func (s *Services) SetLocations(l app.SpaceLocations) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Locations = l
}

func (f locationSvc) Locations(context.Context) app.SpaceLocations {
	f.s.mu.Lock()
	defer f.s.mu.Unlock()
	return f.s.Locations
}
