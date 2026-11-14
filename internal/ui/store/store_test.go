package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

type host struct {
	root  *core.Scope
	mu    sync.Mutex
	posts []func()
}

func newHost(t *testing.T) *host {
	h := &host{}
	h.root = core.NewRoot(core.RootOptions{
		Post: func(fn func()) {
			h.mu.Lock()
			h.posts = append(h.posts, fn)
			h.mu.Unlock()
		},
		Clock:  core.NewManualClock(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)),
		Report: func(err error) { t.Errorf("graph failed: %v", err) },
	})
	t.Cleanup(h.root.Dispose)
	return h
}

func (h *host) drain() {
	for {
		h.mu.Lock()
		posts := h.posts
		h.posts = nil
		h.mu.Unlock()
		if len(posts) == 0 {
			return
		}
		for _, fn := range posts {
			fn()
		}
	}
}

func (h *host) until(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		h.drain()
		time.Sleep(time.Millisecond)
	}
}

// fakeSettings answers Update after release receives, clamping CPUThreshold to at least 10.
type fakeSettings struct {
	mu      sync.Mutex
	cur     settings.AppSettings
	release chan struct{}
	fail    bool
}

func (f *fakeSettings) Get() settings.AppSettings {
	f.mu.Lock()
	defer f.mu.Unlock()
	return f.cur
}

func (f *fakeSettings) Update(next settings.AppSettings) (settings.AppSettings, error) {
	if f.release != nil {
		<-f.release
	}
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail {
		return f.cur, errors.New("无法保存设置，详情已记录到日志")
	}
	next.Health.CPUThreshold = max(next.Health.CPUThreshold, 10)
	f.cur = next
	return f.cur, nil
}

func (f *fakeSettings) Reset() (settings.AppSettings, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.cur = settings.AppSettings{Health: settings.HealthSettings{CPUThreshold: 40}}
	return f.cur, nil
}

func (f *fakeSettings) Limits() app.SettingsLimits   { return app.SettingsLimits{MaxPaths: 200} }
func (f *fakeSettings) About() (app.AppAbout, error) { return app.AppAbout{Version: "0.1.0"}, nil }

type fakeFeed struct{ ch chan settings.AppSettings }

func (f fakeFeed) Subscribe() (<-chan settings.AppSettings, func()) { return f.ch, func() {} }

type fakeHistory struct{ runs chan history.Run }

func (f fakeHistory) List(int) ([]history.Run, error) { return []history.Run{{ID: "a"}}, nil }
func (f fakeHistory) Clear() error                    { return nil }
func (f fakeHistory) Subscribe() (<-chan history.Run, func(), bool) {
	return f.runs, func() {}, true
}
func (f fakeHistory) Restorable([]history.Run, time.Time) map[string]bool { return nil }

type fakeAlerts struct {
	initial []health.Alert
	ch      chan []health.Alert
	block   chan struct{}
}

func (f fakeAlerts) Alerts() []health.Alert {
	<-f.block
	return f.initial
}
func (f fakeAlerts) Subscribe() (<-chan []health.Alert, func()) { return f.ch, func() {} }
func (f fakeAlerts) OpenSystemMonitor(context.Context) error    { return nil }

type fakePermissions struct{ state maintenance.PermissionState }

func (f fakePermissions) FullDiskAccess() (maintenance.PermissionState, error) { return f.state, nil }
func (f fakePermissions) GuideFullDiskAccess(context.Context) (maintenance.PermissionState, error) {
	return maintenance.PermissionGranted, nil
}

type fakeLocations struct{}

func (fakeLocations) Locations(context.Context) app.SpaceLocations {
	return app.SpaceLocations{Targets: []app.SpaceTarget{{ID: "home"}}}
}

func newStores(t *testing.T, h *host, s *fakeSettings, alerts fakeAlerts, feed fakeFeed, runs chan history.Run) *Stores {
	return New(h.root, t.Context(), Deps{Settings: s, SettingsFeed: feed, History: fakeHistory{runs}, Alerts: alerts,
		Permissions: fakePermissions{maintenance.PermissionDenied}, Locations: fakeLocations{}})
}

func TestSettingsSaveKeepsOnlyTheLatestAnswer(t *testing.T) {
	h := newHost(t)
	fs := &fakeSettings{release: make(chan struct{})}
	feed := fakeFeed{make(chan settings.AppSettings, 1)}
	feed.ch <- settings.AppSettings{}
	block := make(chan struct{})
	close(block)
	st := newStores(t, h, fs, fakeAlerts{ch: make(chan []health.Alert), block: block}, feed, make(chan history.Run))
	h.until(t, st.Settings.Loaded)

	first, second := st.Settings.Value(), st.Settings.Value()
	first.Health.CPUThreshold, second.Health.CPUThreshold = 5, 70
	var answers []int
	st.Settings.Save(first, func(v settings.AppSettings, err error) { answers = append(answers, v.Health.CPUThreshold) })
	st.Settings.Save(second, func(v settings.AppSettings, err error) { answers = append(answers, v.Health.CPUThreshold) })
	if got := st.Settings.Value().Health.CPUThreshold; got != 70 {
		t.Fatalf("optimistic value %d", got)
	}
	fs.release <- struct{}{}
	fs.release <- struct{}{}
	h.until(t, func() bool { return len(answers) == 2 })
	if got := st.Settings.Value().Health.CPUThreshold; got != 70 && got != 10 {
		t.Fatalf("value %d", got)
	}
	// Whatever order the answers came in, the value is the last Save's answer.
	if fs.Get().Health.CPUThreshold == 70 && st.Settings.Value().Health.CPUThreshold != 70 {
		t.Errorf("an older answer overwrote the latest: %d", st.Settings.Value().Health.CPUThreshold)
	}

	gen := st.Settings.Generation()
	done := false
	st.Settings.Reset(func(error) { done = true })
	h.until(t, func() bool { return done })
	if st.Settings.Generation() != gen+1 || st.Settings.Value().Health.CPUThreshold != 40 {
		t.Errorf("after reset: gen %d, value %+v", st.Settings.Generation(), st.Settings.Value().Health)
	}
}

func TestSettingsSaveErrorShowsWhatIsStored(t *testing.T) {
	h := newHost(t)
	fs := &fakeSettings{cur: settings.AppSettings{Health: settings.HealthSettings{CPUThreshold: 40}}, fail: true}
	feed := fakeFeed{make(chan settings.AppSettings, 1)}
	block := make(chan struct{})
	close(block)
	st := newStores(t, h, fs, fakeAlerts{ch: make(chan []health.Alert), block: block}, feed, make(chan history.Run))
	next := settings.AppSettings{Health: settings.HealthSettings{CPUThreshold: 90}}
	var gotErr error
	called := false
	st.Settings.Save(next, func(_ settings.AppSettings, err error) { gotErr, called = err, true })
	h.until(t, func() bool { return called })
	if gotErr == nil || st.Settings.Value().Health.CPUThreshold != 40 {
		t.Errorf("err %v, value %d", gotErr, st.Settings.Value().Health.CPUThreshold)
	}
}

func TestAlertsBroadcastWinsOverTheFirstAnswer(t *testing.T) {
	h := newHost(t)
	block := make(chan struct{})
	ch := make(chan []health.Alert, 1)
	alerts := fakeAlerts{initial: []health.Alert{{Name: "old"}}, ch: ch, block: block}
	st := newStores(t, h, &fakeSettings{}, alerts, fakeFeed{make(chan settings.AppSettings)}, make(chan history.Run))
	ch <- []health.Alert{{Name: "new"}}
	h.until(t, func() bool { return len(st.Alerts.List()) == 1 })
	close(block)
	time.Sleep(20 * time.Millisecond)
	h.drain()
	if got := st.Alerts.List(); len(got) != 1 || got[0].Name != "new" {
		t.Errorf("alerts %+v", got)
	}
}

func TestHistoryGenerationFollowsTheRuns(t *testing.T) {
	h := newHost(t)
	runs := make(chan history.Run, 2)
	block := make(chan struct{})
	close(block)
	st := newStores(t, h, &fakeSettings{}, fakeAlerts{ch: make(chan []health.Alert), block: block}, fakeFeed{make(chan settings.AppSettings)}, runs)
	runs <- history.Run{ID: "1"}
	runs <- history.Run{ID: "2"}
	h.until(t, func() bool { return st.History.Generation() == 2 })
	done := false
	st.History.Clear(func(error) { done = true })
	h.until(t, func() bool { return done })
	if st.History.Generation() != 3 {
		t.Errorf("generation %d", st.History.Generation())
	}
}

func TestPermissionLocationsAndBlock(t *testing.T) {
	h := newHost(t)
	block := make(chan struct{})
	close(block)
	st := newStores(t, h, &fakeSettings{}, fakeAlerts{ch: make(chan []health.Alert), block: block}, fakeFeed{make(chan settings.AppSettings)}, make(chan history.Run))
	// The first check has not answered yet: nothing is known.
	if st.Permission.Checked() || st.Permission.FullDiskAccess() != maintenance.PermissionUnknown {
		t.Errorf("before the first answer: checked %v, %q", st.Permission.Checked(), st.Permission.FullDiskAccess())
	}
	h.until(t, func() bool { return st.Permission.FullDiskAccess() == maintenance.PermissionDenied })
	if !st.Permission.Checked() {
		t.Error("not checked after the answer")
	}
	var guided maintenance.PermissionState
	st.Permission.Guide(func(s maintenance.PermissionState, err error) { guided = s })
	if !st.Permission.Guiding() {
		t.Error("not guiding")
	}
	h.until(t, func() bool { return guided != "" })
	if st.Permission.FullDiskAccess() != maintenance.PermissionGranted || st.Permission.Guiding() {
		t.Errorf("after the guide: %q, guiding %v", st.Permission.FullDiskAccess(), st.Permission.Guiding())
	}

	if _, ok := st.Locations.Get(); ok {
		t.Error("locations before they were read")
	}
	h.until(t, func() bool { _, ok := st.Locations.Get(); return ok })

	child := h.root.Child()
	st.Block.Hold(child)
	st.Block.Hold(child)
	if !st.Block.Blocked() {
		t.Error("not blocked")
	}
	child.Dispose()
	if st.Block.Blocked() {
		t.Error("still blocked after its scope went")
	}
}
