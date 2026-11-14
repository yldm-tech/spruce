package services

import (
	"context"
	"errors"
	"log/slog"
	"time"

	"github.com/egoist/mygo"
	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

// SettingsChanged carries the complete settings to every window whenever they change, from any window or from Go.
var SettingsChanged = mygo.NewEvent[settings.AppSettings]("settings:changed")

// LoginItem starts the app at login; mygo.App implements it.
type LoginItem interface {
	SetOpenAtLogin(open bool) error
	OpenAtLogin() bool
}

// Settings is the bound service of the 设置 page.
type Settings struct {
	store *settings.Store
	login LoginItem
	log   *slog.Logger

	// pickFolder shows the system's folder picker and returns the chosen folder, or "" when the user cancels.
	pickFolder func(ctx context.Context, title string) (string, error)
	// version is the app's version; dataDir the app's user data folder.
	version func() string
	dataDir func() (string, error)
}

// NewSettings returns the service over store; login applies General.LaunchAtLogin. The folder picker, the version and the data folder are mygo's (Dialog.Open, App.Version, App.Path(PathUserData)).
func NewSettings(store *settings.Store, login LoginItem, log *slog.Logger) *Settings {
	return &Settings{
		store:      store,
		login:      login,
		log:        log,
		pickFolder: nativeFolderPicker,
		version:    mygo.App.Version,
		dataDir:    func() (string, error) { return mygo.App.Path(mygo.PathUserData) },
	}
}

// nativeFolderPicker shows mygo's open dialog for one folder, as a sheet of the window that asked where there is one.
func nativeFolderPicker(ctx context.Context, title string) (string, error) {
	paths, err := mygo.Dialog.Open(mygo.OpenDialogOptions{
		Parent:            mygo.CallerWindow(ctx),
		Title:             title,
		Message:           title,
		ButtonLabel:       "选择",
		Directory:         true,
		CreateDirectories: true,
	})
	if err != nil || len(paths) == 0 {
		return "", err
	}
	return paths[0], nil
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
		MaxPaths:              settings.MaxPaths,
	}
}

// ChooseFolder shows the system's folder picker titled title and returns the folder the user chose, or "" when they cancelled. It does not change the settings: the page adds the folder to a list and calls Update.
func (s *Settings) ChooseFolder(ctx context.Context, title string) (string, error) {
	p, err := s.pickFolder(ctx, title)
	if err != nil {
		s.log.Warn("choose folder", "err", err)
		return "", errors.New("无法打开选择文件夹的窗口")
	}
	return p, nil
}

// AppAbout is what the 关于 section shows.
type AppAbout struct {
	// Version is the app's version from mygo.config.ts, or "" in a build that does not carry one (go run).
	Version string `json:"version"`
	// DataDir is the folder holding the settings and the history.
	DataDir string `json:"dataDir"`
}

// About returns the app's version and its data folder.
func (s *Settings) About() (AppAbout, error) {
	dir, err := s.dataDir()
	if err != nil {
		s.log.Warn("user data folder", "err", err)
		return AppAbout{}, errors.New("无法读取 Spruce 的数据文件夹")
	}
	return AppAbout{Version: s.version(), DataDir: dir}, nil
}

// Get returns the current settings. General.LaunchAtLogin reports the system's state, which the user can also change in System Settings or Task Manager.
func (s *Settings) Get() settings.AppSettings {
	v := s.store.Get()
	v.General.LaunchAtLogin = s.login.OpenAtLogin()
	return v
}

// Update stores next after clamping it to the allowed ranges (see the settings package) and returns what was stored, which the page shows instead of what it sent. Every window then receives events.settingsChanged.
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

// BroadcastSettings forwards every change of store to all windows as SettingsChanged until ctx is done. main.go starts it once the app is ready.
func BroadcastSettings(ctx context.Context, store *settings.Store, login LoginItem) {
	ch, unsubscribe := store.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case v, ok := <-ch:
			if !ok {
				return
			}
			v.General.LaunchAtLogin = login.OpenAtLogin()
			// Broadcast fails only when the payload cannot be encoded as JSON, which never happens for AppSettings.
			_ = SettingsChanged.Broadcast(v)
		}
	}
}

// pruneEvery is how often FollowSettings prunes the history while the app runs.
const pruneEvery = 24 * time.Hour

// FollowSettings applies the settings that other parts of the engine read, now and on every change, until ctx is done: the health monitor's threshold, sustain and on/off (health.Monitor.Tune, effective from its next sample) and the history retention (history.Store.Prune, also once a day). main.go starts it once the app is ready.
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
