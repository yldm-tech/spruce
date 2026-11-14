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
}

// NewSettings returns the service over store; login applies General.LaunchAtLogin.
func NewSettings(store *settings.Store, login LoginItem, log *slog.Logger) *Settings {
	return &Settings{store: store, login: login, log: log}
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
