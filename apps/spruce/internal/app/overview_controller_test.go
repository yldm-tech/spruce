package app

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/scancache"
	"spruce/apps/spruce/internal/engine/schedule"
	"spruce/apps/spruce/internal/engine/settings"
	"spruce/apps/spruce/internal/engine/smartscan"
	"spruce/apps/spruce/internal/ui/format"
)

// fakeClock is the controller's clock; the tests move it.
type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Add(d time.Duration) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.t = c.t.Add(d)
}

// fakeConditions answers the power and the load as set, and counts how often power was asked.
type fakeConditions struct {
	mu     sync.Mutex
	power  schedule.Power
	err    error
	load   float64
	probes int
}

func (c *fakeConditions) Power(context.Context) (schedule.Power, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.probes++
	return c.power, c.err
}

func (c *fakeConditions) CPU(context.Context, time.Duration) (float64, error) {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.load, nil
}

func (c *fakeConditions) set(p schedule.Power) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.power = p
}

func (c *fakeConditions) asked() int {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.probes
}

// testCategory is one category a fakeSource finds: the files names under root, each counted as size bytes and as modified at mod.
type testCategory struct {
	cat   cleanup.Category
	root  string
	names []string
	size  int64
}

// fakeSource is a source of one of 概览's merged scans: every scan lists the files of its categories that exist now through a real Executor, so cleans go through every check of the engine. It reads each path of reads through mayRead first, as the real sources ask their read gate, and while hold is set a scan waits for it (or its context).
type fakeSource struct {
	exec    *cleanup.Executor
	cats    []testCategory
	mayRead func(string) bool
	reads   []string

	mu      sync.Mutex
	scans   int
	err     error
	hold    chan struct{}
	started chan struct{}
}

func (s *fakeSource) Scan(ctx context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	s.mu.Lock()
	s.scans++
	hold, started, err := s.hold, s.started, s.err
	s.mu.Unlock()
	for _, p := range s.reads {
		if s.mayRead != nil {
			s.mayRead(p)
		}
	}
	if started != nil {
		started <- struct{}{}
	}
	if hold != nil {
		select {
		case <-hold:
		case <-ctx.Done():
			return cleanup.ScanResult{}, ctx.Err()
		}
	}
	if err != nil {
		return cleanup.ScanResult{}, err
	}
	progress(cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 1, Total: 1})
	var cats []cleanup.Category
	for _, tc := range s.cats {
		c := tc.cat
		c.Roots = []string{tc.root}
		c.Items = nil
		for _, n := range tc.names {
			p := filepath.Join(tc.root, n)
			info, err := os.Lstat(p)
			if err != nil {
				continue
			}
			c.Items = append(c.Items, cleanup.NewItem(p, info, tc.size))
		}
		cats = append(cats, c)
	}
	res := cleanup.NewResult(cats, time.Now())
	if err := s.exec.Remember(res); err != nil {
		return cleanup.ScanResult{}, err
	}
	return res, nil
}

func (s *fakeSource) Clean(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	return s.exec.Clean(ctx, req, progress)
}

func (s *fakeSource) count() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.scans
}

// ovFixture is a controller over a temporary home and data folder, two merged scans of fake sources, a fake clock and fake conditions (on AC power and idle unless a test changes them). Tests drive it turn by turn with step instead of Run.
type ovFixture struct {
	t                  *testing.T
	home, data         string
	clock              *fakeClock
	cond               *fakeConditions
	settings           *settings.Store
	history            history.Store
	activity           *schedule.Activity
	cleanups           *Cleanup
	cache              *scancache.Store
	fgCaches, bgCaches *fakeSource
	fgInst, bgInst     *fakeSource
	cfg                OverviewConfig
	o                  *Overview

	diskMu sync.Mutex
	free   int64
}

// ovOptions change the fixture before the controller is built.
type ovOptions struct {
	home    string
	deleter cleanup.Deleter
	goos    string
}

const (
	cacheSize     = 300 << 20 // a card of its own
	installerSize = 150 << 20
)

func newOvFixture(t *testing.T, opt ovOptions) *ovFixture {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home := opt.home
	if home == "" {
		home = filepath.Join(dir, "home")
	}
	f := &ovFixture{t: t, home: home, data: filepath.Join(dir, "data"), clock: &fakeClock{t: time.Now().Round(0)},
		cond: &fakeConditions{power: schedule.Power{OnAC: true}}, free: 200e9}
	caches, downloads := filepath.Join(home, "Library", "Caches"), filepath.Join(home, "Downloads")
	for _, d := range []string{f.data, caches, downloads} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	f.writeCache("com.example.cache", 40*24*time.Hour)
	if err := os.WriteFile(filepath.Join(downloads, "App.dmg"), []byte("dmg"), 0o644); err != nil {
		t.Fatal(err)
	}
	f.settings, err = settings.Open(filepath.Join(f.data, "settings.json"), home, func() settings.AppSettings { return settings.Defaults(home) })
	if err != nil {
		t.Fatal(err)
	}
	f.history, err = history.Open(filepath.Join(f.data, "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	f.cache, err = scancache.Open(filepath.Join(f.data, "overview-cache.json"))
	if err != nil {
		t.Fatal(err)
	}
	del := opt.deleter
	if del == nil {
		del = &recordingDeleter{}
	}
	log := slog.New(slog.DiscardHandler)
	f.activity = schedule.NewActivity(nil)
	f.cleanups = NewCleanup(f.history, home, log, f.activity)
	newCaches := func() *fakeSource {
		return &fakeSource{exec: cleanup.NewExecutor(del), cats: []testCategory{{
			cat:  cleanup.Category{ID: "app-caches", Title: "应用缓存", Icon: "cache", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent, Selected: true},
			root: caches, names: []string{"com.example.cache"}, size: cacheSize,
		}}}
	}
	newInstallers := func() *fakeSource {
		return &fakeSource{exec: cleanup.NewExecutor(del), cats: []testCategory{{
			cat:  cleanup.Category{ID: "disk-images", Title: "磁盘映像", Icon: "disk", Kind: cleanup.KindInstaller, Deletion: cleanup.DeleteTrash, Selected: true},
			root: downloads, names: []string{"App.dmg"}, size: installerSize,
		}}}
	}
	f.fgCaches, f.bgCaches, f.fgInst, f.bgInst = newCaches(), newCaches(), newInstallers(), newInstallers()
	merged := func(caches, inst *fakeSource) *smartscan.Scanner {
		return smartscan.New([]smartscan.Source{
			{ID: string(CleanerSystemJunk), New: func() cleanup.Scanner { return caches }},
			{ID: string(CleanerInstallers), New: func() cleanup.Scanner { return inst }},
		}, log)
	}
	RegisterCleaner(f.cleanups, CleanerSmartScan, merged(f.fgCaches, f.fgInst))
	RegisterCleaner(f.cleanups, CleanerSmartScanBackground, merged(f.bgCaches, f.bgInst))
	policy := schedule.DefaultPolicy()
	// No power watch while a run goes on: the tests look at power before a run.
	policy.Watch = 0
	f.cfg = OverviewConfig{
		Cleanup: f.cleanups, Foreground: CleanerSmartScan, Background: CleanerSmartScanBackground,
		Cache: f.cache, Settings: f.settings, Activity: f.activity, Policy: policy, Conditions: f.cond,
		State: schedule.OpenState(filepath.Join(f.data, "background.json")),
		Disk: func(context.Context) (DiskState, error) {
			f.diskMu.Lock()
			defer f.diskMu.Unlock()
			return DiskState{Volume: "Macintosh HD", Free: f.free, Total: 500e9}, nil
		},
		InstalledApps: func(context.Context) ([]string, error) { return []string{"App"}, nil },
		History:       f.history, Home: home, AppVersion: "2.0.0", Now: f.clock.Now, GOOS: cmpOr(opt.goos, "darwin"), Log: log,
	}
	return f
}

func cmpOr(a, b string) string {
	if a != "" {
		return a
	}
	return b
}

// writeCache writes the cache file name, modified age before the fixture's clock.
func (f *ovFixture) writeCache(name string, age time.Duration) {
	f.t.Helper()
	p := filepath.Join(f.home, "Library", "Caches", name)
	if err := os.WriteFile(p, []byte("cache"), 0o644); err != nil {
		f.t.Fatal(err)
	}
	at := f.clock.Now().Add(-age)
	if err := os.Chtimes(p, at, at); err != nil {
		f.t.Fatal(err)
	}
}

// start builds the controller and does what Run does before its loop.
func (f *ovFixture) start() *Overview {
	f.o = NewOverview(f.cfg)
	if !f.o.begin() {
		f.t.Fatal("a new controller is running already")
	}
	return f.o
}

// step is one turn of the loop.
func (f *ovFixture) step() { f.o.step(f.t.Context()) }

// setFree changes the free space the disk reports.
func (f *ovFixture) setFree(free int64) {
	f.diskMu.Lock()
	defer f.diskMu.Unlock()
	f.free = free
}

func (f *ovFixture) update(fn func(*settings.AppSettings)) {
	f.t.Helper()
	if _, err := f.settings.Update(fn); err != nil {
		f.t.Fatal(err)
	}
}

func (f *ovFixture) runs() []history.Run {
	f.t.Helper()
	runs, err := f.history.List(0)
	if err != nil {
		f.t.Fatal(err)
	}
	return runs
}

// rec returns the card id of st's result.
func rec(t *testing.T, st OverviewState, id string) (r cleanupRequest) {
	t.Helper()
	if st.Result == nil {
		t.Fatal("no result")
	}
	for _, c := range st.Result.Recommendations {
		if c.ID == id {
			r.req = cleanup.CleanRequest{CategoryIDs: c.Categories}
			for _, it := range c.Items {
				r.req.ItemIDs = append(r.req.ItemIDs, it.ItemID)
			}
			r.bytes = c.Bytes
			return r
		}
	}
	t.Fatalf("no card %q in %+v", id, st.Result.Recommendations)
	return r
}

type cleanupRequest struct {
	req   cleanup.CleanRequest
	bytes int64
}

func waitUntil(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(10 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(5 * time.Millisecond)
	}
}

func TestOverviewChecksInTheBackgroundOnACPower(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	o := f.start()
	st := o.State()
	if st.Result != nil || st.Phase != OverviewIdle || len(st.Rules) != 4 {
		t.Fatalf("before any check: %+v", st)
	}
	// Within LaunchDelay of launch nothing is checked; the disk is read at once.
	f.step()
	if f.bgCaches.count() != 0 || f.fgCaches.count() != 0 {
		t.Fatal("checked within the launch delay")
	}
	if st := o.State(); st.Disk == nil || st.Disk.Free != 200e9 || st.Disk.Low || st.Next.IsZero() {
		t.Errorf("disk %+v next %v", st.Disk, st.Next)
	}
	f.clock.Add(3 * time.Minute)
	f.step()
	if f.bgCaches.count() != 1 || f.fgCaches.count() != 0 {
		t.Fatalf("background scans %d, foreground %d", f.bgCaches.count(), f.fgCaches.count())
	}
	st = o.State()
	r := st.Result
	if r == nil || r.Mode != CheckBackground || r.FromCache || r.Stale || !r.Actionable || r.SettingsKey != scancache.Key(f.settings.Get()) {
		t.Fatalf("result %+v", r)
	}
	if r.Headline != cacheSize+installerSize || len(r.Recommendations) != 2 || st.Waiting != "" || st.Phase != OverviewIdle {
		t.Errorf("headline %d cards %d waiting %q phase %q", r.Headline, len(r.Recommendations), st.Waiting, st.Phase)
	}
	if got := r.Recommendations[0].Reason; got == "" {
		t.Error("a card without a reason")
	}
	// The result is cached for the next launch.
	file, ok, err := f.cache.Load()
	if err != nil || !ok || file.Mode != string(CheckBackground) || file.AppVersion != "2.0.0" || file.Scan.Size != r.Scan.Size {
		t.Errorf("cache %+v %v %v", file, ok, err)
	}
	// The next one is due an Interval later, not on the next tick.
	f.clock.Add(time.Hour)
	f.step()
	if f.bgCaches.count() != 1 {
		t.Error("checked again within the interval")
	}
	f.clock.Add(6 * time.Hour)
	f.step()
	if f.bgCaches.count() != 2 {
		t.Error("not checked again after the interval")
	}
	// Turned off, nothing is scheduled.
	f.update(func(s *settings.AppSettings) { s.Background.Enabled = false })
	f.clock.Add(7 * time.Hour)
	f.step()
	if f.bgCaches.count() != 2 || !o.State().Next.IsZero() {
		t.Error("a scheduled check while they are off")
	}
}

func TestOverviewWaitsOnBattery(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	f.cond.set(schedule.Power{OnAC: false})
	o := f.start()
	f.clock.Add(3 * time.Minute)
	f.step()
	st := o.State()
	if f.bgCaches.count() != 0 || st.Waiting != schedule.ReasonBattery || st.Result != nil {
		t.Fatalf("on battery: scans %d, waiting %q", f.bgCaches.count(), st.Waiting)
	}
	if want := f.clock.Now().Add(f.cfg.Policy.Retry); !st.Next.Equal(want) {
		t.Errorf("next %v, want %v", st.Next, want)
	}
	// Before Retry the conditions are not looked at again.
	asked := f.cond.asked()
	f.cond.set(schedule.Power{OnAC: true})
	f.clock.Add(5 * time.Minute)
	f.step()
	if f.cond.asked() != asked || f.bgCaches.count() != 0 || o.State().Waiting != schedule.ReasonBattery {
		t.Errorf("looked again before Retry: probes %d → %d", asked, f.cond.asked())
	}
	f.clock.Add(11 * time.Minute)
	f.step()
	if st := o.State(); f.bgCaches.count() != 1 || st.Waiting != "" || st.Result == nil || st.Result.Mode != CheckBackground {
		t.Errorf("back on AC: scans %d, waiting %q", f.bgCaches.count(), st.Waiting)
	}
	// Low Power Mode holds it back too; a foreground check is not held back at all.
	f.cond.set(schedule.Power{OnAC: true, Saver: true})
	f.clock.Add(7 * time.Hour)
	f.step()
	if st := o.State(); f.bgCaches.count() != 1 || st.Waiting != schedule.ReasonSaver {
		t.Errorf("in Low Power Mode: scans %d, waiting %q", f.bgCaches.count(), st.Waiting)
	}
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	if st := o.State(); f.fgCaches.count() != 1 || st.Result.Mode != CheckForeground {
		t.Errorf("foreground check: scans %d, result %+v", f.fgCaches.count(), st.Result)
	}
}

func TestOverviewFailedCheckWaitsRetry(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	for _, s := range []*fakeSource{f.bgCaches, f.bgInst} {
		s.err = errors.New("unreadable")
	}
	o := f.start()
	f.clock.Add(3 * time.Minute)
	f.step()
	if st := o.State(); st.Error == "" || st.Result != nil || f.bgCaches.count() != 1 {
		t.Fatalf("failed check: %+v", st)
	}
	f.clock.Add(5 * time.Minute)
	f.step()
	if f.bgCaches.count() != 1 {
		t.Error("a failed check ran again on the next tick")
	}
	f.clock.Add(11 * time.Minute)
	f.step()
	if f.bgCaches.count() != 2 {
		t.Error("a failed check was not tried again after Retry")
	}
}

func TestOverviewCleanCancelsTheBackgroundCheck(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	o := f.start()
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	st := o.State()
	if st.Result == nil || st.Result.Mode != CheckForeground || !st.Result.Actionable {
		t.Fatalf("foreground result %+v", st.Result)
	}
	caches := rec(t, st, "app-caches")

	// A scheduled check starts on the other instance and is held inside its scan.
	f.bgCaches.hold, f.bgCaches.started = make(chan struct{}), make(chan struct{}, 1)
	f.clock.Add(7 * time.Hour)
	stepped := make(chan struct{})
	go func() {
		defer close(stepped)
		f.step()
	}()
	<-f.bgCaches.started
	waitUntil(t, "the scheduled check", func() bool { return o.State().Phase == OverviewChecking })
	// The foreground result stays actionable: the other instance's check does not touch it.
	if !o.State().Result.Actionable {
		t.Fatal("a check of the other instance made the result not actionable")
	}
	// The clean stops the scheduled check and waits for it before it deletes anything.
	out, err := o.Clean(t.Context(), caches.req, CleanOrigin{Recommendation: "app-caches"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	<-stepped
	if out.Result.Removed != 1 || out.RunID == "" {
		t.Errorf("clean %+v", out)
	}
	st = o.State()
	if st.Result.Mode != CheckForeground || st.Result.Headline != installerSize || len(st.Result.Recommendations) != 1 || st.Phase != OverviewIdle {
		t.Errorf("after the clean: mode %q headline %d cards %d phase %q", st.Result.Mode, st.Result.Headline, len(st.Result.Recommendations), st.Phase)
	}
	runs := f.runs()
	// The card's line says what the clean did: the items removed and the space they gave back.
	if want := "应用缓存：已清理 1 项，释放 " + format.BytesIn(out.Result.Freed+out.Result.Trashed, false); len(runs) != 1 || runs[0].Kind != history.KindSmartScan || len(runs[0].Details) == 0 || runs[0].Details[0] != want {
		t.Errorf("history %+v, want the first line %q", runs, want)
	}
	// The cache holds what is left.
	if file, ok, _ := f.cache.Load(); !ok || file.Scan.Size != installerSize {
		t.Errorf("cache after the clean: %+v", file.Scan)
	}
	// The stopped scheduled check runs again once the clean ended.
	close(f.bgCaches.hold)
	f.bgCaches.hold, f.bgCaches.started = nil, nil
	before := f.bgCaches.count()
	f.step()
	if f.bgCaches.count() != before+1 || o.State().Result.Mode != CheckBackground {
		t.Error("the scheduled check did not run after the clean")
	}
}

func TestOverviewCheckNowAndCleanWhileAnotherCleanRuns(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	o := f.start()
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	_, end, err := f.activity.BeginClean(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if err := o.CheckNow(); !errors.Is(err, cleanup.ErrBusy) || err.Error() != "正在清理，完成后再检查" {
		t.Errorf("CheckNow during a clean: %v", err)
	}
	// A scheduled check waits for it.
	f.clock.Add(7 * time.Hour)
	f.step()
	if f.bgCaches.count() != 0 || o.State().Waiting != schedule.ReasonCleaning {
		t.Errorf("scheduled check during a clean: scans %d waiting %q", f.bgCaches.count(), o.State().Waiting)
	}
	end()
	o.mu.Lock()
	o.cleaning = true
	o.mu.Unlock()
	if _, err := o.Clean(t.Context(), rec(t, o.State(), "app-caches").req, CleanOrigin{}, nil); !errors.Is(err, cleanup.ErrBusy) || err.Error() != "正在清理另一项，完成后再试" {
		t.Errorf("a second clean: %v", err)
	}
}

func TestOverviewCachedResultIsShownButNotCleaned(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	o := f.start()
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	checked := o.State().Result
	req := rec(t, o.State(), "app-caches").req

	// The next launch shows the cached result at once; before it has loaded it says so.
	f.clock.Add(time.Hour)
	if NewOverview(f.cfg).State().Loaded {
		t.Error("a controller that has not loaded reports Loaded")
	}
	o2 := f.start()
	st := o2.State()
	if !st.Loaded {
		t.Error("a loaded controller does not report Loaded")
	}
	r := st.Result
	if r == nil || !r.FromCache || r.Actionable || r.Stale || !r.CheckedAt.Equal(checked.CheckedAt) || r.Headline != checked.Headline || len(r.Recommendations) != 2 {
		t.Fatalf("cached result %+v", r)
	}
	if _, err := o2.Clean(t.Context(), req, CleanOrigin{Recommendation: "app-caches"}, nil); err == nil || err.Error() != "检查结果可能已经变化，请重新检查后再清理" {
		t.Errorf("clean of a cached result: %v", err)
	}
	if len(f.runs()) != 0 {
		t.Error("a cached result was cleaned")
	}
	// A day later it shows as stale.
	f.clock.Add(25 * time.Hour)
	if !o2.State().Result.Stale {
		t.Error("an old cached result is not stale")
	}
	// Changed exclusions make it stale at once and due right after the launch delay.
	f.clock.Add(-25 * time.Hour)
	f.update(func(s *settings.AppSettings) {
		s.Cleanup.Exclusions = append(s.Cleanup.Exclusions, filepath.Join(f.home, "Keep"))
	})
	if !o2.State().Result.Stale {
		t.Error("a result of other settings is not stale")
	}
	f.clock.Add(3 * time.Minute)
	f.step()
	if f.bgCaches.count() != 1 || o2.State().Result.FromCache {
		t.Error("a result of other settings was not checked again")
	}
}

func TestOverviewLowDiskNoticeOnlyWhenCrossing(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	f.update(func(s *settings.AppSettings) {
		s.General.LowDiskAlert, s.General.LowDiskGB = true, 20
		s.Background.Enabled = false
	})
	o := f.start()
	notices, stop := o.Notices()
	defer stop()
	next := func() (Notice, bool) {
		select {
		case n := <-notices:
			return n, true
		default:
			return Notice{}, false
		}
	}
	f.step()
	if n, ok := next(); ok {
		t.Fatalf("a notice with plenty of space: %+v", n)
	}
	// Free space is read again only every DiskEvery.
	f.setFree(15e9)
	f.clock.Add(time.Minute)
	f.step()
	if _, ok := next(); ok || o.State().Disk.Free != 200e9 {
		t.Fatal("free space read again before DiskEvery")
	}
	f.clock.Add(15 * time.Minute)
	f.step()
	n, ok := next()
	if !ok || n.Kind != NoticeLowDisk || n.ID != "low-disk" || n.Volume != "Macintosh HD" || n.Free != 15e9 || n.Total != 500e9 {
		t.Fatalf("crossing the threshold: %+v %v", n, ok)
	}
	if st := o.State(); !st.Disk.Low {
		t.Error("15 GB free is not low")
	}
	// Staying below it, or dropping a little further, says nothing more.
	f.setFree(13e9)
	f.clock.Add(16 * time.Minute)
	f.step()
	if n, ok := next(); ok {
		t.Errorf("a second notice below the threshold: %+v", n)
	}
	// The state survives a restart.
	o2 := f.start()
	n2, stop2 := o2.Notices()
	defer stop2()
	f.clock.Add(16 * time.Minute)
	f.step()
	select {
	case n := <-n2:
		t.Errorf("a notice after a restart below the threshold: %+v", n)
	default:
	}
	// Back above the threshold and down again, it notifies again (on another day).
	f.setFree(30e9)
	f.clock.Add(16 * time.Minute)
	f.step()
	f.setFree(10e9)
	f.clock.Add(25 * time.Hour)
	f.step()
	select {
	case n := <-n2:
		if n.Kind != NoticeLowDisk {
			t.Errorf("notice %+v", n)
		}
	default:
		t.Error("no notice after crossing again")
	}
	// Turned off, nothing is posted.
	f.update(func(s *settings.AppSettings) { s.General.LowDiskAlert = false })
	f.setFree(30e9)
	f.clock.Add(16 * time.Minute)
	f.step()
	f.setFree(1e9)
	f.clock.Add(25 * time.Hour)
	f.step()
	select {
	case n := <-n2:
		t.Errorf("a notice with the alert off: %+v", n)
	default:
	}
}

func TestOverviewWeeklySummary(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	loc := time.FixedZone("JST", 9*3600)
	// Monday 16 November 2026, 11:00.
	f.clock.t = time.Date(2026, 11, 16, 11, 0, 0, 0, loc)
	f.update(func(s *settings.AppSettings) { s.Background.Enabled = false })
	for _, r := range []history.Run{
		{ID: "a", Kind: history.KindAuto, Rule: "app-caches", StartedAt: time.Date(2026, 11, 12, 3, 0, 0, 0, loc), Summary: &history.Totals{Items: 3, Trashed: 4e9}},
		{ID: "b", Kind: history.KindAuto, Rule: "installers", StartedAt: time.Date(2026, 11, 13, 3, 0, 0, 0, loc), Summary: &history.Totals{Items: 1, Trashed: 1e9}},
		// Before the week summed up.
		{ID: "c", Kind: history.KindAuto, Rule: "installers", StartedAt: time.Date(2026, 11, 2, 3, 0, 0, 0, loc), Summary: &history.Totals{Items: 1, Trashed: 9e9}},
	} {
		r.EndedAt = r.StartedAt.Add(time.Minute)
		if err := f.history.Append(r); err != nil {
			t.Fatal(err)
		}
	}
	o := f.start()
	notices, stop := o.Notices()
	defer stop()
	f.step()
	select {
	case n := <-notices:
		if n.Kind != NoticeAutoSummary || n.ID != "auto-summary:2026-W47" || n.Trashed != 5e9 || len(n.Rules) != 2 {
			t.Errorf("summary %+v", n)
		}
	default:
		t.Fatal("no weekly summary")
	}
	// Once a week.
	f.clock.Add(2 * time.Hour)
	f.step()
	o2 := f.start()
	n2, stop2 := o2.Notices()
	defer stop2()
	f.step()
	select {
	case n := <-n2:
		t.Errorf("a second summary in the same week: %+v", n)
	default:
	}
	// Off, nothing is sent.
	f.clock.Add(7 * 24 * time.Hour)
	f.update(func(s *settings.AppSettings) { s.Automation.WeeklySummary = false })
	f.step()
	select {
	case n := <-n2:
		t.Errorf("a summary while it is off: %+v", n)
	default:
	}
}

func TestOverviewStopCheck(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	o := f.start()
	// Past the launch delay a scheduled check is due; stopping the foreground check must not start it.
	f.clock.Add(3 * time.Minute)
	f.fgCaches.hold, f.fgCaches.started = make(chan struct{}), make(chan struct{}, 1)
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	stepped := make(chan struct{})
	go func() {
		defer close(stepped)
		f.step()
	}()
	<-f.fgCaches.started
	o.StopCheck()
	<-stepped
	st := o.State()
	if st.Result != nil || st.Error != "" || st.Phase != OverviewIdle {
		t.Errorf("stopped: %+v", st)
	}
	if f.fgCaches.count() != 1 {
		t.Errorf("foreground scans %d", f.fgCaches.count())
	}
	// A stopped check leaves its instance unable to clean until the next one.
	if !f.o.pending(CleanerSmartScan) {
		t.Error("the stopped instance is not pending")
	}
	if f.bgCaches.count() != 0 {
		t.Fatal("a scheduled check started in the turn the user stopped the check")
	}
	// It waits Retry, as after a stopped scheduled check.
	f.clock.Add(5 * time.Minute)
	f.step()
	if f.bgCaches.count() != 0 || o.State().Phase != OverviewIdle {
		t.Fatal("a scheduled check started right after the user stopped the check")
	}
	if want := f.clock.Now().Add(-5 * time.Minute).Add(f.cfg.Policy.Retry); !o.State().Next.Equal(want) {
		t.Errorf("next %v, want %v", o.State().Next, want)
	}
	f.clock.Add(f.cfg.Policy.Retry)
	f.step()
	if f.bgCaches.count() != 1 {
		t.Error("no scheduled check after Retry")
	}
}

// TestOverviewReadDisk: ReadDisk reads free space again at once, well before DiskEvery and without waiting for the loop's turn (which may be inside a check), as the window comes back or something outside a clean (a move in 空间透视, an uninstall) frees space; it also has the next turn read it again; without it the next turn keeps the reading.
func TestOverviewReadDisk(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	f.update(func(s *settings.AppSettings) { s.Background.Enabled = false })
	o := f.start()
	f.step()
	f.setFree(150e9)
	f.clock.Add(time.Minute)
	f.step()
	if got := o.State().Disk.Free; got != 200e9 {
		t.Fatalf("free %v read again before DiskEvery without ReadDisk", got)
	}
	o.ReadDisk()
	if got := o.State().Disk.Free; got != 150e9 {
		t.Errorf("free %v at once after ReadDisk, want 150e9", got)
	}
	f.setFree(140e9)
	f.step()
	if got := o.State().Disk.Free; got != 140e9 {
		t.Errorf("free %v at the turn after ReadDisk, want 140e9", got)
	}
	select {
	case <-o.wake:
	default:
		t.Error("ReadDisk did not wake the loop")
	}
}

// TestOverviewRun drives the real loop: CheckNow wakes it, a settings change makes it look at the rules and the disk again, and a second Run returns at once.
func TestOverviewRun(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	f.cfg.Policy.Tick = time.Hour
	var reads sync.Mutex
	disks := 0
	disk := f.cfg.Disk
	f.cfg.Disk = func(ctx context.Context) (DiskState, error) {
		reads.Lock()
		disks++
		reads.Unlock()
		return disk(ctx)
	}
	diskReads := func() int {
		reads.Lock()
		defer reads.Unlock()
		return disks
	}
	o := NewOverview(f.cfg)
	f.o = o
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		defer close(done)
		o.Run(ctx)
	}()
	defer func() {
		cancel()
		<-done
	}()
	waitUntil(t, "the first turn", func() bool { return o.State().Loaded && diskReads() == 1 })
	// A second Run returns at once.
	second := make(chan struct{})
	go func() {
		defer close(second)
		o.Run(ctx)
	}()
	select {
	case <-second:
	case <-time.After(10 * time.Second):
		t.Fatal("a second Run did not return")
	}
	// CheckNow alone starts the check, an hour before the next tick.
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	waitUntil(t, "the foreground check", func() bool {
		st := o.State()
		return f.fgCaches.count() == 1 && st.Result != nil && st.Result.Mode == CheckForeground && st.Phase == OverviewIdle
	})
	// A settings change is a turn of its own, which reads the disk again (it is not yet DiskEvery old) and the rules.
	before := diskReads()
	f.update(func(s *settings.AppSettings) { s.Automation.AppCaches = true })
	waitUntil(t, "the turn after the settings change", func() bool {
		if diskReads() <= before {
			return false
		}
		for _, r := range o.State().Rules {
			if r.ID == "app-caches" {
				return r.Enabled
			}
		}
		return false
	})
}

// TestOverviewCleanWaitsForAnAskedCheck: between CheckNow and the loop starting the check, 概览 already shows it as checking and refuses a clean; a check asked for while a clean runs waits until the clean ends (one thing at a time).
func TestOverviewCleanWaitsForAnAskedCheck(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	o := f.start()
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	caches := rec(t, o.State(), "app-caches")

	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	st := o.State()
	if st.Phase != OverviewChecking || st.Result.Actionable {
		t.Fatalf("asked, not started: phase %q actionable %v", st.Phase, st.Result.Actionable)
	}
	if _, err := o.Clean(t.Context(), caches.req, CleanOrigin{Recommendation: "app-caches"}, nil); err == nil {
		t.Fatal("a clean while a check was asked for")
	}
	if len(f.runs()) != 0 || f.fgCaches.count() != 1 {
		t.Fatalf("history %+v, foreground scans %d", f.runs(), f.fgCaches.count())
	}
	f.step()
	if st := o.State(); f.fgCaches.count() != 2 || st.Phase != OverviewIdle || !st.Result.Actionable {
		t.Fatalf("after the check: scans %d phase %q", f.fgCaches.count(), st.Phase)
	}

	// Asked for while a clean runs (here another page's clean, through the same gate), the check waits for it.
	o.mu.Lock()
	o.askForegroundLocked()
	o.mu.Unlock()
	_, end, err := f.activity.BeginClean(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	f.step()
	if f.fgCaches.count() != 2 {
		t.Error("a foreground check ran while a clean ran")
	}
	if st := o.State(); st.Phase != OverviewChecking || st.Result.Actionable {
		t.Errorf("waiting check: phase %q actionable %v", st.Phase, st.Result.Actionable)
	}
	end()
	f.step()
	if st := o.State(); f.fgCaches.count() != 3 || st.Phase != OverviewIdle || !st.Result.Actionable {
		t.Errorf("after the clean: scans %d phase %q", f.fgCaches.count(), st.Phase)
	}

	// Taken back by StopCheck before the loop starts it, it does not run.
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	o.StopCheck()
	f.step()
	if st := o.State(); f.fgCaches.count() != 3 || st.Phase != OverviewIdle || !st.Result.Actionable {
		t.Errorf("stopped before it started: scans %d phase %q", f.fgCaches.count(), st.Phase)
	}
}

// TestOverviewCleanFromADrillDownView: a drill-down view's clean is recorded with its source's kind, without a card's line, and a category cleaned by its command leaves the result once the command ran, while an emptied category of items stays.
func TestOverviewCleanFromADrillDownView(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	caches := filepath.Join(f.home, "Library", "Caches")
	if err := os.MkdirAll(filepath.Join(caches, "tool"), 0o755); err != nil {
		t.Fatal(err)
	}
	f.fgCaches.cats = append(f.fgCaches.cats, testCategory{
		cat:  cleanup.Category{ID: "tool-cache", Title: "工具缓存", Icon: "cache", Kind: cleanup.KindDevCache, Deletion: cleanup.DeletePermanent, Command: "tool cache clean", Selected: true},
		root: caches, names: []string{"tool"}, size: 1 << 20,
	})
	o := f.start()
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	categoryOf := func(kind cleanup.CategoryKind) (cleanup.Category, bool) {
		for _, c := range o.State().Result.Scan.Categories {
			if c.Kind == kind {
				return c, true
			}
		}
		return cleanup.Category{}, false
	}
	inst, ok := categoryOf(cleanup.KindInstaller)
	if !ok {
		t.Fatal("no installers category")
	}
	if _, err := o.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{inst.ID}}, CleanOrigin{Source: string(CleanerInstallers)}, nil); err != nil {
		t.Fatal(err)
	}
	runs := f.runs()
	if len(runs) != 1 || runs[0].Kind != history.KindInstallers {
		t.Fatalf("history %+v", runs)
	}
	for _, d := range runs[0].Details {
		if strings.Contains(d, "：已清理") {
			t.Errorf("a card's line in a drill-down view's clean: %q", d)
		}
	}
	if c, ok := categoryOf(cleanup.KindInstaller); !ok || len(c.Items) != 0 {
		t.Errorf("the emptied installers category: %+v %v", c, ok)
	}
	tool, ok := categoryOf(cleanup.KindDevCache)
	if !ok || len(tool.Items) != 1 {
		t.Fatalf("the command category: %+v %v", tool, ok)
	}
	if _, err := o.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{tool.ID}}, CleanOrigin{Source: string(CleanerSystemJunk)}, nil); err != nil {
		t.Fatal(err)
	}
	if runs := f.runs(); len(runs) != 2 || runs[0].Kind != history.KindSystemJunk {
		t.Errorf("history %+v", runs)
	}
	if c, ok := categoryOf(cleanup.KindDevCache); ok {
		t.Errorf("the command category stayed after its command ran: %+v", c)
	}
}

// TestOverviewCleanAll: 概览's 全部清理 cleans the items of several sources in one clean, recorded as 清理建议 with its own first line.
func TestOverviewCleanAll(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	o := f.start()
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	var req cleanup.CleanRequest
	for _, c := range o.State().Result.Scan.Categories {
		req.CategoryIDs = append(req.CategoryIDs, c.ID)
		for _, it := range c.Items {
			req.ItemIDs = append(req.ItemIDs, it.ID)
		}
	}
	if len(req.CategoryIDs) != 2 {
		t.Fatalf("categories %v, want one per source", req.CategoryIDs)
	}
	out, err := o.Clean(t.Context(), req, CleanOrigin{All: true}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Result.Removed != 2 {
		t.Errorf("removed %d, want both sources' items", out.Result.Removed)
	}
	runs := f.runs()
	if len(runs) != 1 || runs[0].Kind != history.KindSmartScan || len(runs[0].Details) == 0 || !strings.HasPrefix(runs[0].Details[0], "全部清理：已清理 2 项，释放 ") {
		t.Errorf("history %+v", runs)
	}
}

// TestCleanWithDetailOf: the first Details line can be made from what the clean did.
func TestCleanWithDetailOf(t *testing.T) {
	c, store := newGatedCleanup(t, schedule.NewActivity(nil))
	s := newExecScanner(t)
	RegisterCleaner(c, CleanerSmartScan, s)
	// recordingDeleter frees one byte per item; DetailOf wins over Detail.
	opts := CleanOptions{Detail: "unused", DetailOf: func(res cleanup.CleanResult) string {
		return fmt.Sprintf("缓存：已清理 %d 项，释放 %d", res.Removed, res.Freed+res.Trashed)
	}}
	if _, err := c.CleanWith(t.Context(), CleanerSmartScan, cleanup.CleanRequest{CategoryIDs: []string{"caches"}}, opts, nil); err != nil {
		t.Fatal(err)
	}
	if len(store.appended) != 1 || store.appended[0].Details[0] != "缓存：已清理 2 项，释放 2" {
		t.Errorf("history %+v", store.appended)
	}
}
