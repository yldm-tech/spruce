package store

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/growth"
	"spruce/apps/spruce/internal/engine/health"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/settings"
)

// fakeOverview answers State after block, and holds Clean until release.
type fakeOverview struct {
	mu      sync.Mutex
	initial app.OverviewState
	ch      chan app.OverviewState
	block   chan struct{}
	release chan struct{}
	cleans  []app.CleanOrigin
	// stopped counts the cleans that ended because their context did.
	stopped int
	// answered records the goroutine State answered on.
	answered answer
}

func (f *fakeOverview) State() app.OverviewState {
	<-f.block
	f.answered.mark()
	return f.initial
}
func (f *fakeOverview) Subscribe() (<-chan app.OverviewState, func()) { return f.ch, func() {} }
func (f *fakeOverview) Notices() (<-chan app.Notice, func())          { return nil, func() {} }
func (f *fakeOverview) CheckNow() error                               { return errors.New("正在清理，完成后再检查") }
func (f *fakeOverview) StopCheck()                                    {}
func (f *fakeOverview) ReadDisk()                                     {}
func (f *fakeOverview) Clean(ctx context.Context, _ cleanup.CleanRequest, origin app.CleanOrigin, progress func(cleanup.Progress)) (app.OverviewOutcome, error) {
	f.mu.Lock()
	f.cleans = append(f.cleans, origin)
	release := f.release
	f.mu.Unlock()
	progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Done: 1, Total: 2})
	select {
	case <-release:
		return app.OverviewOutcome{RunID: "r1", Undoable: true}, nil
	case <-ctx.Done():
		f.mu.Lock()
		f.stopped++
		f.mu.Unlock()
		return app.OverviewOutcome{RunID: "r0", Result: cleanup.CleanResult{Interrupted: true}}, nil
	}
}
func (f *fakeOverview) Undo(context.Context, string) (app.RestoreOutcome, error) {
	return app.RestoreOutcome{Restored: 2}, nil
}
func (f *fakeOverview) Growth(context.Context, time.Duration) ([]growth.Growth, error) {
	return nil, nil
}
func (f *fakeOverview) TrashInfo(context.Context) (app.TrashInfo, error) {
	return app.TrashInfo{Items: 3}, nil
}

func storesWith(t *testing.T, h *host, ov OverviewService) *Stores {
	block := make(chan struct{})
	close(block)
	return New(h.root, t.Context(), Deps{Settings: &fakeSettings{}, SettingsFeed: fakeFeed{make(chan settings.AppSettings)}, History: fakeHistory{make(chan history.Run)},
		Alerts: fakeAlerts{ch: make(chan []health.Alert), block: block}, Permissions: fakePermissions{}, Locations: fakeLocations{}, Overview: ov})
}

// TestOverviewWithoutAController: the stores start without a controller, 概览 reads 检查已停止's state and every action answers that it is not wired yet.
func TestOverviewWithoutAController(t *testing.T) {
	h := newHost(t)
	st := storesWith(t, h, nil)
	if got := st.Overview.State(); got.Phase != app.OverviewIdle || got.Result != nil || got.Disk != nil || st.Overview.Connected() {
		t.Fatalf("state %+v", got)
	}
	var errs []error
	keep := func(err error) { errs = append(errs, err) }
	st.Overview.CheckNow(keep)
	st.Overview.Clean(h.root, cleanup.CleanRequest{}, app.CleanOrigin{}, nil, func(_ app.OverviewOutcome, err error) { keep(err) }, func(_ app.OverviewOutcome, err error) { keep(err) })
	st.Overview.Undo(h.root, "r", func(_ app.RestoreOutcome, err error) { keep(err) }, func(_ app.RestoreOutcome, err error) { keep(err) })
	st.Overview.Growth(h.root, t.Context(), time.Hour, func(_ []growth.Growth, err error) { keep(err) })
	st.Overview.TrashInfo(h.root, t.Context(), func(_ app.TrashInfo, err error) { keep(err) })
	st.Overview.StopCheck()
	st.Overview.Clean(h.root, cleanup.CleanRequest{}, app.CleanOrigin{}, nil, nil, nil)()
	h.until(t, func() bool { return len(errs) == 7 })
	for _, err := range errs {
		if err == nil || err.Error() != "概览还没有接入" {
			t.Errorf("err %v", err)
		}
	}
	if ch, stop := st.Overview.Notices(); ch != nil {
		t.Error("notices without a controller")
	} else {
		stop()
	}
	if st.Overview.Busy() {
		t.Error("busy without a controller")
	}
}

func TestOverviewFollowsTheControllerAndCleans(t *testing.T) {
	h := newHost(t)
	f := &fakeOverview{initial: app.OverviewState{Phase: app.OverviewChecking}, ch: make(chan app.OverviewState, 1), block: make(chan struct{}), release: make(chan struct{})}
	st := storesWith(t, h, f)
	// A broadcast wins over the first answer, which comes later and is older.
	f.ch <- app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{Headline: 42}}
	h.until(t, func() bool { return st.Overview.State().Result != nil })
	// The older first answer arrives after the broadcast, and has been delivered before the check.
	close(f.block)
	h.delivered(t, &f.answered)
	if got := st.Overview.State(); got.Phase != app.OverviewIdle || got.Result.Headline != 42 {
		t.Errorf("state %+v", got)
	}

	var progress []cleanup.Progress
	var out *app.OverviewOutcome
	var outcome *app.OverviewOutcome
	st.Overview.Clean(h.root, cleanup.CleanRequest{CategoryIDs: []string{"x"}}, app.CleanOrigin{Recommendation: "app-caches"},
		func(p cleanup.Progress) { progress = append(progress, p) },
		func(o app.OverviewOutcome, err error) {
			if err != nil {
				t.Error(err)
			}
			if outcome == nil {
				t.Error("done ran before outcome")
			}
			out = &o
		},
		func(o app.OverviewOutcome, _ error) { outcome = &o })
	if !st.Overview.Busy() {
		t.Error("not busy while cleaning")
	}
	h.until(t, func() bool { return len(progress) == 1 })
	close(f.release)
	h.until(t, func() bool { return out != nil })
	if st.Overview.Busy() || out.RunID != "r1" || len(f.cleans) != 1 || f.cleans[0].Recommendation != "app-caches" {
		t.Errorf("busy %v out %+v cleans %+v", st.Overview.Busy(), out, f.cleans)
	}

	var checkErr error
	st.Overview.CheckNow(func(err error) { checkErr = err })
	h.until(t, func() bool { return checkErr != nil })
	var info app.TrashInfo
	st.Overview.TrashInfo(h.root, t.Context(), func(i app.TrashInfo, _ error) { info = i })
	h.until(t, func() bool { return info.Items == 3 })
	if u := st.Overview.LastUndo(); u != (Undone{}) {
		t.Errorf("an undo before any: %+v", u)
	}
	var restored int
	st.Overview.Undo(h.root, "r1", func(r app.RestoreOutcome, _ error) { restored = r.Restored }, nil)
	h.until(t, func() bool { return restored == 2 && !st.Overview.Busy() })
	if u := st.Overview.LastUndo(); u != (Undone{RunID: "r1", Seq: 1}) {
		t.Errorf("last undo %+v", u)
	}
	if !st.Overview.Undid("r1") || st.Overview.Undid("r2") {
		t.Error("Undid does not follow the undos")
	}
	restored = 0
	st.Overview.Undo(h.root, "r1", func(r app.RestoreOutcome, _ error) { restored = r.Restored }, nil)
	h.until(t, func() bool { return restored == 2 && st.Overview.LastUndo().Seq == 2 })
}

// TestOverviewCleanOutlivesThePage: a clean runs on the app's context, so the page that started it going (its scope disposed) neither stops it nor loses its outcome; only the stop it returns ends it early.
func TestOverviewCleanOutlivesThePage(t *testing.T) {
	h := newHost(t)
	f := &fakeOverview{ch: make(chan app.OverviewState, 1), block: make(chan struct{}), release: make(chan struct{})}
	close(f.block)
	st := storesWith(t, h, f)
	page := h.root.Child()
	var done, outcome *app.OverviewOutcome
	var progressed bool
	st.Overview.Clean(page, cleanup.CleanRequest{CategoryIDs: []string{"x"}}, app.CleanOrigin{Recommendation: "app-caches"},
		func(cleanup.Progress) { progressed = true },
		func(o app.OverviewOutcome, _ error) { done = &o },
		func(o app.OverviewOutcome, _ error) { outcome = &o })
	h.until(t, func() bool { return progressed })
	page.Dispose()
	h.drain()
	// The page gone, the store still shows what runs and how far it got.
	if o, ok := st.Overview.Running(); !ok || o.Recommendation != "app-caches" || st.Overview.CleanProgress().Done != 1 {
		t.Errorf("running %+v %v, progress %+v", o, ok, st.Overview.CleanProgress())
	}
	close(f.release)
	h.until(t, func() bool { return outcome != nil })
	h.drain()
	if outcome.RunID != "r1" || f.stopped != 0 || done != nil || st.Overview.Busy() {
		t.Errorf("outcome %+v, stopped %d, done %+v, busy %v", outcome, f.stopped, done, st.Overview.Busy())
	}
	if _, ok := st.Overview.Running(); ok || st.Overview.CleanProgress().Done != 0 {
		t.Error("the store still shows a clean that ended")
	}
	// 停止 ends it after the current item, and the outcome still comes; the store's StopClean does the same as the function Clean returns.
	for _, viaStore := range []bool{false, true} {
		f.mu.Lock()
		f.release = make(chan struct{})
		stoppedBefore := f.stopped
		f.mu.Unlock()
		outcome = nil
		stop := st.Overview.Clean(h.root, cleanup.CleanRequest{CategoryIDs: []string{"x"}}, app.CleanOrigin{}, nil, nil,
			func(o app.OverviewOutcome, _ error) { outcome = &o })
		if viaStore {
			st.Overview.StopClean()
		} else {
			stop()
		}
		h.until(t, func() bool { return outcome != nil })
		if !outcome.Result.Interrupted || f.stopped != stoppedBefore+1 {
			t.Errorf("stopped clean %+v, stopped %d", outcome, f.stopped)
		}
	}
	// With nothing running, StopClean does nothing.
	st.Overview.StopClean()
	// The same for an undo: its outcome is not lost with the page.
	page = h.root.Child()
	var undone, undoDone *app.RestoreOutcome
	st.Overview.Undo(page, "r1", func(r app.RestoreOutcome, _ error) { undoDone = &r }, func(r app.RestoreOutcome, _ error) { undone = &r })
	page.Dispose()
	h.until(t, func() bool { return undone != nil })
	h.drain()
	if undone.Restored != 2 || undoDone != nil {
		t.Errorf("undo outcome %+v, done %+v", undone, undoDone)
	}
}

func TestToasts(t *testing.T) {
	h := newHost(t)
	st := storesWith(t, h, nil)
	if st.Toasts.Current() != nil {
		t.Fatal("a toast at start")
	}
	st.Toasts.Show(Toast{Text: "已移到废纸篓 3.1 GB", UndoRun: "r1", Duration: 10 * time.Second})
	first := st.Toasts.Current()
	if first == nil || first.UndoRun != "r1" {
		t.Fatalf("toast %+v", first)
	}
	st.Toasts.Show(*first)
	if st.Toasts.Current() == first {
		t.Error("showing the same message again did not make a new toast")
	}
	st.Toasts.Dismiss()
	if st.Toasts.Current() != nil {
		t.Error("not dismissed")
	}
}
