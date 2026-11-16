package store

import (
	"context"
	"errors"
	"runtime"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/health"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/engine/settings"
)

type host struct {
	root  *core.Scope
	mu    sync.Mutex
	posts []func()
	// posters are the goroutines that posted, by goid.
	posters map[int64]bool
}

// goid is the current goroutine's ID, from its stack trace's first line ("goroutine 42 [running]:").
func goid() int64 {
	var buf [64]byte
	line := strings.TrimPrefix(string(buf[:runtime.Stack(buf[:], false)]), "goroutine ")
	id, err := strconv.ParseInt(line[:strings.IndexByte(line, ' ')], 10, 64)
	if err != nil {
		panic(err)
	}
	return id
}

// answer remembers the goroutine a fake service answered a request on, so a test can wait until that goroutine has posted the answer to the stores instead of guessing how long it takes.
type answer struct{ id atomic.Int64 }

// mark records the calling goroutine.
func (a *answer) mark() {
	if a != nil {
		a.id.Store(goid())
	}
}

// delivered waits until the goroutine a marked has posted, then runs every post.
func (h *host) delivered(t *testing.T, a *answer) {
	t.Helper()
	h.until(t, func() bool {
		id := a.id.Load()
		h.mu.Lock()
		defer h.mu.Unlock()
		return id != 0 && h.posters[id]
	})
	h.drain()
}

func newHost(t *testing.T) *host {
	h := &host{}
	h.root = core.NewRoot(core.RootOptions{
		Post: func(fn func()) {
			id := goid()
			h.mu.Lock()
			h.posts = append(h.posts, fn)
			if h.posters == nil {
				h.posters = map[int64]bool{}
			}
			h.posters[id] = true
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
	// answered, when set, records the goroutine Alerts answered on.
	answered *answer
}

func (f fakeAlerts) Alerts() []health.Alert {
	<-f.block
	f.answered.mark()
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

func newStores(t *testing.T, h *host, s SettingsService, alerts fakeAlerts, feed fakeFeed, runs chan history.Run) *Stores {
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

// orderedSettings answers each Update when its value's channel is closed, so a test decides the order of the answers; a value in fail fails.
type orderedSettings struct {
	fakeSettings
	gates map[int]chan struct{}
	fail  map[int]bool
}

func (f *orderedSettings) Update(next settings.AppSettings) (settings.AppSettings, error) {
	<-f.gates[next.Health.CPUThreshold]
	f.mu.Lock()
	defer f.mu.Unlock()
	if f.fail[next.Health.CPUThreshold] {
		return f.cur, errors.New("无法保存设置，详情已记录到日志")
	}
	f.cur = next
	return f.cur, nil
}

// TestSettingsOlderSaveAnswersLast: the newer save answers first, then the older one fails; once the older answer has been delivered (its done ran) the value is still the newer one.
func TestSettingsOlderSaveAnswersLast(t *testing.T) {
	h := newHost(t)
	fs := &orderedSettings{gates: map[int]chan struct{}{50: make(chan struct{}), 70: make(chan struct{})}, fail: map[int]bool{50: true}}
	fs.cur.Health.CPUThreshold = 40
	feed := fakeFeed{make(chan settings.AppSettings, 1)}
	feed.ch <- fs.cur
	block := make(chan struct{})
	close(block)
	st := newStores(t, h, fs, fakeAlerts{ch: make(chan []health.Alert), block: block}, feed, make(chan history.Run))
	h.until(t, st.Settings.Loaded)
	older, newer := st.Settings.Value(), st.Settings.Value()
	older.Health.CPUThreshold, newer.Health.CPUThreshold = 50, 70
	var olderErr error
	olderDone, newerDone := false, false
	st.Settings.Save(older, func(_ settings.AppSettings, err error) { olderErr, olderDone = err, true })
	st.Settings.Save(newer, func(settings.AppSettings, error) { newerDone = true })
	close(fs.gates[70])
	h.until(t, func() bool { return newerDone })
	if got := st.Settings.Value().Health.CPUThreshold; got != 70 {
		t.Fatalf("after the newer answer: %d", got)
	}
	close(fs.gates[50])
	h.until(t, func() bool { return olderDone })
	if got := st.Settings.Value().Health.CPUThreshold; olderErr == nil || got != 70 {
		t.Errorf("after the older, failed answer: value %d, err %v", got, olderErr)
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
	alerts := fakeAlerts{initial: []health.Alert{{Name: "old"}}, ch: ch, block: block, answered: &answer{}}
	st := newStores(t, h, &fakeSettings{}, alerts, fakeFeed{make(chan settings.AppSettings)}, make(chan history.Run))
	ch <- []health.Alert{{Name: "new"}}
	h.until(t, func() bool { return len(st.Alerts.List()) == 1 })
	// The older first answer arrives after the broadcast, and has been delivered before the check.
	close(block)
	h.delivered(t, alerts.answered)
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
