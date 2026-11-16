package app

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/health"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/settings"
)

type fakeLogin struct {
	mu   sync.Mutex
	open bool
	err  error
	sets []bool
}

func (l *fakeLogin) SetOpenAtLogin(open bool) error {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.sets = append(l.sets, open)
	if l.err != nil {
		return l.err
	}
	l.open = open
	return nil
}

func (l *fakeLogin) OpenAtLogin() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.open
}

func newSettingsStore(t *testing.T) *settings.Store {
	t.Helper()
	home := t.TempDir()
	st, err := settings.Open(filepath.Join(t.TempDir(), "settings.json"), home, func() settings.AppSettings { return settings.Defaults(home) })
	if err != nil {
		t.Fatal(err)
	}
	return st
}

func TestSettingsService(t *testing.T) {
	st := newSettingsStore(t)
	login := &fakeLogin{}
	s := NewSettings(st, login, slog.New(slog.DiscardHandler), "", t.TempDir())
	v := s.Get()
	v.Health.CPUThreshold = 1
	v.General.LaunchAtLogin = true
	got, err := s.Update(v)
	if err != nil || got.Health.CPUThreshold != settings.MinCPUThreshold || !got.General.LaunchAtLogin || !login.open {
		t.Fatalf("Update = %+v, %v (login %v)", got, err, login.open)
	}
	// The user turned it off in System Settings: Get reports the system's state.
	login.open = false
	if s.Get().General.LaunchAtLogin {
		t.Error("Get ignores the system's login item state")
	}

	login.err = errors.New("no bundle")
	v = s.Get()
	v.General.LaunchAtLogin = true
	v.History.RetentionDays = 30
	if _, err := s.Update(v); err == nil || err.Error() != "无法更改「登录时启动」，其他设置没有保存" {
		t.Errorf("failed login item: %v", err)
	}
	if st.Get().History.RetentionDays == 30 {
		t.Error("settings were saved although the update failed")
	}

	login.err, login.open = nil, true
	r, err := s.Reset()
	if err != nil || r.Health.CPUThreshold != 40 || r.General.LaunchAtLogin || login.open {
		t.Errorf("Reset = %+v, %v (login %v)", r, err, login.open)
	}
}

type tuneCall struct {
	threshold float64
	sustain   time.Duration
	enabled   bool
}

type tuningMonitor struct {
	fakeMonitor
	calls chan tuneCall
}

func (m tuningMonitor) Tune(threshold float64, sustain time.Duration, enabled bool) error {
	m.calls <- tuneCall{threshold, sustain, enabled}
	return nil
}

var _ health.Monitor = tuningMonitor{}

func TestFollowSettings(t *testing.T) {
	st := newSettingsStore(t)
	mon := tuningMonitor{calls: make(chan tuneCall, 4)}
	hist := &fakeStore{}
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		FollowSettings(ctx, st, mon, hist, nil, slog.New(slog.DiscardHandler))
		close(done)
	}()
	wait := func() tuneCall {
		select {
		case c := <-mon.calls:
			return c
		case <-time.After(5 * time.Second):
			t.Fatal("monitor not tuned")
			return tuneCall{}
		}
	}
	if c := wait(); c != (tuneCall{40, 10 * time.Minute, true}) {
		t.Errorf("initial tune %+v", c)
	}
	if _, err := st.Update(func(a *settings.AppSettings) {
		a.Health = settings.HealthSettings{CPUThreshold: 90, SustainMinutes: 3, Enabled: false}
		a.History.RetentionDays = 30
	}); err != nil {
		t.Fatal(err)
	}
	if c := wait(); c != (tuneCall{90, 3 * time.Minute, false}) {
		t.Errorf("tune after change %+v", c)
	}
	cancel()
	<-done
	hist.mu.Lock()
	defer hist.mu.Unlock()
	if len(hist.cutoffs) != 2 {
		t.Fatalf("pruned %d times, want at start and on the retention change", len(hist.cutoffs))
	}
	if d := hist.cutoffs[1].Sub(history.RetentionCutoff(time.Now(), 30)); d > time.Minute || d < -time.Minute {
		t.Errorf("cutoff %v", hist.cutoffs[1])
	}
}

func TestSettingsLimitsAreThePackageRanges(t *testing.T) {
	s := NewSettings(newSettingsStore(t), &fakeLogin{}, slog.New(slog.DiscardHandler), "", t.TempDir())
	l := s.Limits()
	if l.CPUThreshold != (SettingRange{settings.MinCPUThreshold, settings.MaxCPUThreshold}) ||
		l.SustainMinutes != (SettingRange{settings.MinSustainMinutes, settings.MaxSustainMinutes}) ||
		l.DevArtifactMinAgeDays != (SettingRange{settings.MinDevArtifactMinAge, settings.MaxDevArtifactMinAge}) ||
		l.LargeFileMinMB != (SettingRange{settings.MinLargeFileMB, settings.MaxLargeFileMB}) ||
		l.RetentionDays != (SettingRange{settings.MinRetentionDays, settings.MaxRetentionDays}) ||
		l.MaxPaths != settings.MaxPaths {
		t.Errorf("Limits = %+v", l)
	}
	// Every range is what Update clamps to: values outside come back at its ends.
	v := s.Get()
	v.Health.CPUThreshold, v.Health.SustainMinutes = l.CPUThreshold.Max+1, l.SustainMinutes.Min-1
	v.Cleanup.DevArtifactMinAgeDays, v.Cleanup.LargeFileMinMB = l.DevArtifactMinAgeDays.Min-1, l.LargeFileMinMB.Max+1
	v.History.RetentionDays = l.RetentionDays.Min - 1
	got, err := s.Update(v)
	if err != nil {
		t.Fatal(err)
	}
	if got.Health.CPUThreshold != l.CPUThreshold.Max || got.Health.SustainMinutes != l.SustainMinutes.Min ||
		got.Cleanup.DevArtifactMinAgeDays != l.DevArtifactMinAgeDays.Min || got.Cleanup.LargeFileMinMB != l.LargeFileMinMB.Max ||
		got.History.RetentionDays != l.RetentionDays.Min {
		t.Errorf("clamped to %+v, limits %+v", got, l)
	}
}

func TestSettingsAbout(t *testing.T) {
	dir := t.TempDir()
	s := NewSettings(newSettingsStore(t), &fakeLogin{}, slog.New(slog.DiscardHandler), "1.2.3", dir)
	if a, err := s.About(); err != nil || a != (AppAbout{Version: "1.2.3", DataDir: dir}) {
		t.Errorf("About = %+v, %v", a, err)
	}
}
