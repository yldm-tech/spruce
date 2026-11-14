package services

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/engine/trash"
)

type fakeBin struct {
	usage trash.TrashUsage
	err   error
	empty int
}

func (b *fakeBin) MoveToTrash(context.Context, []string) ([]trash.Outcome, error) {
	return nil, errors.New("not used")
}
func (b *fakeBin) Size(context.Context) (trash.TrashUsage, error) { return b.usage, b.err }
func (b *fakeBin) Empty(context.Context) (trash.TrashUsage, error) {
	b.empty++
	return b.usage, b.err
}

func TestFilesTrash(t *testing.T) {
	bin := &fakeBin{usage: trash.TrashUsage{Bytes: 1 << 20, Items: 3}}
	f := NewFiles(nil, bin, slog.New(slog.DiscardHandler))
	if u, err := f.TrashSize(t.Context()); err != nil || u.Items != 3 {
		t.Errorf("TrashSize = %+v, %v", u, err)
	}
	if u, err := f.EmptyTrash(t.Context()); err != nil || u.Bytes != 1<<20 || bin.empty != 1 {
		t.Errorf("EmptyTrash = %+v, %v", u, err)
	}
	bin.err = trash.ErrNoAccess
	if _, err := f.TrashSize(t.Context()); err == nil || err.Error() != "需要为 Spruce 开启「完全磁盘访问权限」才能读取"+binName() {
		t.Errorf("no access: %v", err)
	}
	bin.err = errors.New("io")
	if _, err := f.EmptyTrash(t.Context()); err == nil || err.Error() != "无法读取"+binName()+"，详情已记录到日志" {
		t.Errorf("other error: %v", err)
	}
	if err := f.Reveal(t.Context(), filepath.Join(t.TempDir(), "gone")); err == nil || err.Error() != "这个项目已经不存在了" {
		t.Errorf("reveal missing: %v", err)
	}
}

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
	s := NewSettings(st, login, slog.New(slog.DiscardHandler))
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
		FollowSettings(ctx, st, mon, hist, slog.New(slog.DiscardHandler))
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
