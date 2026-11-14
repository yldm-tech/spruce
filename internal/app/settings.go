package app

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

// LoginItem starts the app at login; internal/host/loginitem implements it.
type LoginItem interface {
	SetOpenAtLogin(open bool) error
	OpenAtLogin() bool
}

// Settings is the controller of the 设置 page.
type Settings struct {
	store *settings.Store
	login LoginItem
	log   *slog.Logger

	// version is the app's version ("" in a development build); dataDir the app's user data folder.
	version string
	dataDir string
}

// NewSettings returns the service over store; login applies General.LaunchAtLogin. version is the app's version, "" in a development build (main passes core.App().Version unless core.App().Development), and dataDir the folder holding the settings and the history.
func NewSettings(store *settings.Store, login LoginItem, log *slog.Logger, version, dataDir string) *Settings {
	return &Settings{store: store, login: login, log: log, version: version, dataDir: dataDir}
}

// SettingRange is the inclusive range Settings.Update clamps one number to.
type SettingRange struct {
	Min int `json:"min"`
	Max int `json:"max"`
}

// SettingsLimits are the ranges of the settings package, for the page's sliders, fields and validation messages.
type SettingsLimits struct {
	CPUThreshold          SettingRange `json:"cpuThreshold"`
	SustainMinutes        SettingRange `json:"sustainMinutes"`
	DevArtifactMinAgeDays SettingRange `json:"devArtifactMinAgeDays"`
	LargeFileMinMB        SettingRange `json:"largeFileMinMB"`
	RetentionDays         SettingRange `json:"retentionDays"`
	LowDiskGB             SettingRange `json:"lowDiskGB"`
	// MaxPaths is how many entries each folder list keeps.
	MaxPaths int `json:"maxPaths"`
}

// Limits returns the ranges Update clamps to.
func (s *Settings) Limits() SettingsLimits {
	return SettingsLimits{
		CPUThreshold:          SettingRange{settings.MinCPUThreshold, settings.MaxCPUThreshold},
		SustainMinutes:        SettingRange{settings.MinSustainMinutes, settings.MaxSustainMinutes},
		DevArtifactMinAgeDays: SettingRange{settings.MinDevArtifactMinAge, settings.MaxDevArtifactMinAge},
		LargeFileMinMB:        SettingRange{settings.MinLargeFileMB, settings.MaxLargeFileMB},
		RetentionDays:         SettingRange{settings.MinRetentionDays, settings.MaxRetentionDays},
		LowDiskGB:             SettingRange{settings.MinLowDiskGB, settings.MaxLowDiskGB},
		MaxPaths:              settings.MaxPaths,
	}
}

// AppAbout is what the 关于 section shows.
type AppAbout struct {
	// Version is the app's version from nsgo.yaml, or "" in a development build.
	Version string `json:"version"`
	// DataDir is the folder holding the settings and the history.
	DataDir string `json:"dataDir"`
}

// About returns the app's version and its data folder. The error is always nil: main resolved the folder before the app started, and the signature stays for the 设置 page.
func (s *Settings) About() (AppAbout, error) {
	return AppAbout{Version: s.version, DataDir: s.dataDir}, nil
}

// Get returns the current settings. General.LaunchAtLogin reports the system's state, which the user can also change in System Settings or Task Manager.
func (s *Settings) Get() settings.AppSettings {
	v := s.store.Get()
	v.General.LaunchAtLogin = s.login.OpenAtLogin()
	return v
}

// Update stores next after clamping it to the allowed ranges (see the settings package) and returns what was stored, which the page shows instead of what it sent. settings.Store then announces the change to its subscribers.
func (s *Settings) Update(next settings.AppSettings) (settings.AppSettings, error) {
	if next.General.LaunchAtLogin != s.login.OpenAtLogin() {
		if err := s.login.SetOpenAtLogin(next.General.LaunchAtLogin); err != nil {
			s.log.Warn("set open at login", "open", next.General.LaunchAtLogin, "err", err)
			return s.Get(), errors.New("无法更改「登录时启动」，其他设置没有保存")
		}
	}
	if _, err := s.store.Set(next); err != nil {
		s.log.Error("save settings", "err", err)
		return s.Get(), errors.New("无法保存设置，详情已记录到日志")
	}
	return s.Get(), nil
}

// Reset restores every default, including turning off 登录时启动.
func (s *Settings) Reset() (settings.AppSettings, error) {
	if s.login.OpenAtLogin() {
		if err := s.login.SetOpenAtLogin(false); err != nil {
			s.log.Warn("set open at login", "open", false, "err", err)
		}
	}
	if _, err := s.store.Reset(); err != nil {
		s.log.Error("reset settings", "err", err)
		return s.Get(), errors.New("无法恢复默认设置，详情已记录到日志")
	}
	return s.Get(), nil
}

// pruneEvery is how often FollowSettings prunes the history while the app runs.
const pruneEvery = 24 * time.Hour

// FollowSettings applies the settings that other parts of the engine read, now and on every change, until ctx is done: the health monitor's threshold, sustain and on/off (health.Monitor.Tune, effective from its next sample) and the history retention (history.Store.Prune, also once a day). The app starts it as it opens.
func FollowSettings(ctx context.Context, store *settings.Store, mon health.Monitor, hist history.Store, log *slog.Logger) {
	ch, unsubscribe := store.Subscribe()
	defer unsubscribe()
	ticker := time.NewTicker(pruneEvery)
	defer ticker.Stop()
	retention := 0
	prune := func() {
		if n, err := hist.Prune(history.RetentionCutoff(time.Now(), retention)); err != nil {
			log.Warn("prune history", "err", err)
		} else if n > 0 {
			log.Info("pruned history", "runs", n, "retentionDays", retention)
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			prune()
		case v, ok := <-ch:
			if !ok {
				return
			}
			h := v.Health
			if err := mon.Tune(float64(h.CPUThreshold), time.Duration(h.SustainMinutes)*time.Minute, h.Enabled); err != nil {
				log.Warn("apply health settings", "err", err)
			}
			if v.History.RetentionDays != retention {
				retention = v.History.RetentionDays
				prune()
			}
		}
	}
}
