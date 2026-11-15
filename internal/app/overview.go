package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"path/filepath"
	"slices"
	"sync"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/advice"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/devjunk"
	"github.com/yldm-tech/spruce/internal/engine/growth"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/rules"
	"github.com/yldm-tech/spruce/internal/engine/scancache"
	"github.com/yldm-tech/spruce/internal/engine/schedule"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/engine/trash"
	"github.com/yldm-tech/spruce/internal/engine/undo"
	"github.com/yldm-tech/spruce/internal/host/folderaccess"
	"github.com/yldm-tech/spruce/internal/ui/format"
)

// OverviewConfig is what the 概览 controller is built from. Every dependency is named; nil optional ones turn their feature off (tests).
type OverviewConfig struct {
	// Cleanup runs the scans and cleans; Foreground and Background are its two merged smartscan instances (CleanerSmartScan and CleanerSmartScanBackground): the checks the user starts and the scheduled ones, whose sources read through Gate.
	Cleanup                *Cleanup
	Foreground, Background CleanerID
	// Cache keeps the last result between launches, for showing only.
	Cache *scancache.Store
	// Restore puts back what a clean moved to the Trash (Undo); nil refuses.
	Restore  *Restore
	Settings *settings.Store
	Activity *schedule.Activity
	Policy   schedule.Policy
	// Conditions decide whether a scheduled check may run now (power, load).
	Conditions schedule.Conditions
	// State is background.json: when each rule last ran, the weekly summary, the low-disk alert.
	State *schedule.StateStore
	// Access is the read gate's memory of the prompting folders; Gate is the gate the sources read through.
	Access *folderaccess.Store
	Gate   *GateRecorder
	// Growth keeps the space snapshots; Watches are the locations a snapshot measures under the given settings, and GrowthOptions how (Access's Allow becomes its read gate).
	Growth        *growth.Store
	Watches       func(settings.AppSettings) []growth.Watch
	GrowthOptions growth.TakeOptions
	// Disk reads the system volume's space.
	Disk func(ctx context.Context) (DiskState, error)
	// InstalledApps are the names of the installed apps, from an uninstall engine of its own.
	InstalledApps func(ctx context.Context) ([]string, error)
	// TrashSize measures the Trash for the 清空废纸篓 dialog (Files.TrashSize).
	TrashSize func(ctx context.Context) (trash.TrashUsage, error)
	// Ledger is the undo ledger: what the user put back (rules never take it again) and what could still be put back.
	Ledger  *undo.Ledger
	History history.Store
	// Home is the user's home folder: the cards' folder names and the rules' Downloads folder.
	Home       string
	AppVersion string
	Now        func() time.Time
	// GOOS is runtime.GOOS: automatic rules and the weekly summary run only on "darwin".
	GOOS string
	Log  *slog.Logger
}

// Notices that wait for the shell to subscribe, at most.
const maxQueuedNotices = 4

// growthRows is the most rows Growth returns: what 空间透视's 最近变化 shows.
const growthRows = 20

// snapshotEvery is how old the newest space snapshot must be for another.
const snapshotEvery = 24 * time.Hour

// summaryWeek is the period the weekly summary covers and the least time between two of them.
const summaryWeek = 7 * 24 * time.Hour

// Below lowFreeShare of free space, or below lowFreeBytes, the system volume counts as low (the rule of 智能扫描's former disk card).
const (
	lowFreeShare       = 0.1
	lowFreeBytes int64 = 20e9
)

// DiskLow reports whether a volume of total bytes with free left is running out of space; a volume without a size is not.
func DiskLow(free, total int64) bool {
	if total <= 0 {
		return false
	}
	return float64(free)/float64(total) < lowFreeShare || free < lowFreeBytes
}

// SystemDisk is the disk the system runs from in l, or else its first disk; false without disks. At and Low are the controller's.
func SystemDisk(l SpaceLocations) (DiskState, bool) {
	for _, v := range l.Volumes {
		if v.Startup {
			return DiskState{Volume: cmp.Or(v.Name, v.Path), Free: v.Free, Total: v.Total}, true
		}
	}
	if len(l.Volumes) > 0 {
		v := l.Volumes[0]
		return DiskState{Volume: cmp.Or(v.Name, v.Path), Free: v.Free, Total: v.Total}, true
	}
	return DiskState{}, false
}

// gigabyte is the unit of the low-disk threshold, as sizes show on goos: 1000-based on macOS (Finder), 1024-based on Windows (Explorer).
func gigabyte(goos string) int64 {
	if goos == "windows" {
		return 1 << 30
	}
	return 1e9
}

// Overview is 概览's controller: it checks in the background on a schedule and when asked, keeps the result (also in the cache, for the next launch), cleans from it for the cards and the drill-down views, puts back through Restore, runs the automatic rules after a scheduled check (macOS), takes the space snapshots, watches free space and posts the notifications. Safe for concurrent use; Run is its loop.
type Overview struct {
	cfg     OverviewConfig
	log     *slog.Logger
	now     func() time.Time
	canUndo bool
	// wake asks the loop to look again at once.
	wake chan struct{}
	// saveMu orders the writes of the cache; saved is the generation last written.
	saveMu sync.Mutex
	saved  int

	mu sync.Mutex
	// result is the last result; from is the smartscan instance that produced it ("" for one from the cache) and version the app version that did.
	result  *OverviewResult
	from    CleanerID
	version string
	gen     int
	// lastCheck is when the last check ended (the cache's SavedAt at launch); launched is when Run started.
	lastCheck, launched time.Time
	running             bool
	// wantForeground is set by CheckNow until the loop starts the check.
	wantForeground bool
	// fg and bg cancel the foreground check and the background slot (its conditions, scan, rules and snapshot) that run.
	fg, bg context.CancelFunc
	// scanning is the instance that scans now, "" for none.
	scanning     CleanerID
	progress     *cleanup.Progress
	cleaning     bool
	autoCleaning bool
	waiting      string
	retryAt      time.Time
	errText      string
	disk         *DiskState
	diskAt       time.Time
	diskDue      bool
	state        schedule.State
	rules        []RuleStatus
	rulesDirty   bool
	lastAuto     *AutoRun
	// lastTrashed is what each rule's last run moved to the Trash.
	lastTrashed map[string]int64
	// apps are the installed apps the last check read; nil while unknown.
	apps    []string
	subs    map[chan OverviewState]struct{}
	notices map[chan Notice]struct{}
	queued  []Notice
}

// NewOverview returns the controller; it does no I/O until Run.
func NewOverview(cfg OverviewConfig) *Overview {
	if cfg.Now == nil {
		cfg.Now = time.Now
	}
	if cfg.Log == nil {
		cfg.Log = slog.New(slog.DiscardHandler)
	}
	if cfg.Policy.Tick <= 0 {
		cfg.Policy.Tick = schedule.DefaultPolicy().Tick
	}
	return &Overview{
		cfg: cfg, log: cfg.Log, now: cfg.Now, canUndo: trash.CanRestore() && cfg.Restore != nil,
		wake: make(chan struct{}, 1), rulesDirty: true,
		state:       schedule.State{Version: schedule.StateVersion, RuleRuns: map[string]time.Time{}},
		lastTrashed: map[string]int64{},
		subs:        map[chan OverviewState]struct{}{}, notices: map[chan Notice]struct{}{},
	}
}

// poke wakes the loop.
func (o *Overview) poke() {
	select {
	case o.wake <- struct{}{}:
	default:
	}
}

// Run is the controller's loop, until ctx ends: it shows the cached result, then every Policy.Tick, on every settings change, after CheckNow and after a clean it reads free space (every Policy.DiskEvery), runs the foreground check asked for, the scheduled check when it is due and allowed (then the due automatic rules and a space snapshot, inside the background slot) and the weekly summary. It runs once; a second call returns at once.
func (o *Overview) Run(ctx context.Context) {
	o.mu.Lock()
	if o.running {
		o.mu.Unlock()
		return
	}
	o.running = true
	o.launched = o.now()
	o.mu.Unlock()
	o.load()
	changes, unsubscribe := o.cfg.Settings.Subscribe()
	defer unsubscribe()
	// The subscription starts with the settings as they are, which the first turn reads anyway.
	<-changes
	tick := time.NewTicker(o.cfg.Policy.Tick)
	defer tick.Stop()
	for {
		o.step(ctx)
		select {
		case <-ctx.Done():
			return
		case <-tick.C:
		case <-o.wake:
		case _, ok := <-changes:
			if !ok {
				changes = nil
			}
			o.mu.Lock()
			o.rulesDirty, o.diskDue = true, true
			o.mu.Unlock()
		}
	}
}

// load reads what the last launch left: the cached result, background.json and the automatic runs in the history.
func (o *Overview) load() {
	if o.cfg.State != nil {
		st, err := o.cfg.State.Load()
		if err != nil {
			o.log.Warn("read background state", "err", err)
		}
		o.mu.Lock()
		o.state = st
		o.rulesDirty = true
		o.mu.Unlock()
	}
	if o.cfg.History != nil {
		if runs, err := o.cfg.History.List(0); err != nil {
			o.log.Warn("read history for the rules", "err", err)
		} else {
			o.mu.Lock()
			o.lastTrashed = lastTrashed(runs, o.state.RuleRuns)
			o.mu.Unlock()
		}
	}
	if o.cfg.Cache == nil {
		o.publish()
		return
	}
	f, ok, err := o.cfg.Cache.Load()
	if err != nil {
		o.log.Warn("read the cached check", "err", err)
	}
	o.mu.Lock()
	if ok && o.result == nil {
		r := o.deriveLocked(f.Scan, CheckMode(f.Mode), f.SettingsKey, f.Skipped)
		r.CheckedAt, r.FromCache = f.SavedAt, true
		o.result, o.from, o.version = &r, "", f.AppVersion
		o.lastCheck = f.SavedAt
		o.gen++
	}
	o.mu.Unlock()
	o.publish()
}

// lastTrashed is what each rule's latest automatic run moved to the Trash, counted only when that run ended at the rule's recorded last run (a later run that moved nothing records nothing in the history).
func lastTrashed(runs []history.Run, last map[string]time.Time) map[string]int64 {
	out := map[string]int64{}
	seen := map[string]bool{}
	for _, r := range runs {
		if r.Kind != history.KindAuto || r.Rule == "" || seen[r.Rule] {
			continue
		}
		seen[r.Rule] = true
		if r.Summary != nil && !r.EndedAt.Before(last[r.Rule].Add(-time.Minute)) {
			out[r.Rule] = r.Summary.Trashed
		}
	}
	return out
}

// step is one turn of the loop.
func (o *Overview) step(ctx context.Context) {
	if ctx.Err() != nil {
		return
	}
	s := o.cfg.Settings.Get()
	o.readDisk(ctx, s)
	o.mu.Lock()
	fg := o.wantForeground
	o.wantForeground = false
	o.mu.Unlock()
	if fg {
		o.foreground(ctx)
		s = o.cfg.Settings.Get()
	}
	o.background(ctx, s)
	o.summary(s)
	o.publish()
}

// busyError is CheckNow's and Clean's refusal while a clean runs.
var busyError = &appError{msg: "正在清理，完成后再检查", err: cleanup.ErrBusy}

// CheckNow asks for a foreground check: one the user started, not held back by power or load. It stops a scheduled check that runs, and is refused (cleanup.ErrBusy) while a clean runs; it returns at once and the check runs in the loop.
func (o *Overview) CheckNow() error {
	if o.cfg.Activity != nil && o.cfg.Activity.Cleaning() {
		return busyError
	}
	o.mu.Lock()
	if o.cleaning || o.autoCleaning {
		o.mu.Unlock()
		return busyError
	}
	o.askForegroundLocked()
	o.mu.Unlock()
	o.poke()
	return nil
}

// askForegroundLocked asks the loop for a foreground check unless one runs, stopping the background slot. o.mu is held.
func (o *Overview) askForegroundLocked() {
	if o.fg != nil {
		return
	}
	o.wantForeground = true
	if o.bg != nil {
		o.bg()
	}
}

// StopCheck stops the check that runs; a scheduled one waits Policy.Retry before it is tried again.
func (o *Overview) StopCheck() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.wantForeground = false
	if o.fg != nil {
		o.fg()
	}
	if o.bg != nil && o.scanning != "" {
		o.bg()
		o.retryAt = o.now().Add(o.cfg.Policy.Retry)
	}
}

// foreground runs the foreground check, then lets later background checks into the prompting folders it entered.
func (o *Overview) foreground(ctx context.Context) {
	run, cancel := context.WithCancel(ctx)
	defer cancel()
	o.mu.Lock()
	o.fg = cancel
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		o.fg = nil
		o.mu.Unlock()
	}()
	if o.check(run, CheckForeground, o.cfg.Foreground) {
		if err := o.cfg.Gate.observe(o.cfg.Gate.recorded(CheckForeground)); err != nil {
			o.log.Warn("remember the folders a check entered", "err", err)
		}
	}
}

// check scans with cleaner and, when the scan completes, makes its result the current one and caches it. It reports whether it did.
func (o *Overview) check(ctx context.Context, mode CheckMode, cleaner CleanerID) bool {
	key := scancache.Key(o.cfg.Settings.Get())
	o.cfg.Gate.begin(mode)
	o.mu.Lock()
	o.scanning, o.progress, o.errText = cleaner, nil, ""
	o.mu.Unlock()
	o.publish()
	res, err := o.cfg.Cleanup.Scan(ctx, cleaner, func(p cleanup.Progress) {
		o.mu.Lock()
		o.progress = &p
		o.mu.Unlock()
		o.publish()
	})
	var apps []string
	if err == nil && ctx.Err() == nil && o.cfg.InstalledApps != nil {
		var aerr error
		if apps, aerr = o.cfg.InstalledApps(ctx); aerr != nil {
			o.log.Info("list the installed apps", "err", aerr)
			apps = nil
		}
	}
	o.mu.Lock()
	o.scanning, o.progress = "", nil
	switch {
	case ctx.Err() != nil:
		// Stopped, or made way for a clean or a foreground check: not a failure.
		o.mu.Unlock()
		o.publish()
		return false
	case err != nil:
		o.errText = err.Error()
		o.mu.Unlock()
		o.publish()
		return false
	}
	if apps != nil {
		o.apps = apps
	}
	var skipped []string
	if mode == CheckBackground {
		skipped = o.cfg.Gate.recorded(CheckBackground)
	}
	r := o.deriveLocked(res, mode, key, skipped)
	r.CheckedAt = o.now()
	o.result, o.from, o.version = &r, cleaner, o.cfg.AppVersion
	o.lastCheck = r.CheckedAt
	o.rulesDirty = true
	o.gen++
	o.mu.Unlock()
	o.save()
	o.publish()
	return true
}

// deriveLocked builds a result of scan: the cards, the headline and the projects. o.mu is held.
func (o *Overview) deriveLocked(scan cleanup.ScanResult, mode CheckMode, key string, skipped []string) OverviewResult {
	s := o.cfg.Settings.Get()
	recs := advice.Derive(scan, advice.Inputs{Now: o.now(), InstalledApps: o.apps, Windows: o.cfg.GOOS == "windows", Home: o.cfg.Home, InstallerRoots: s.Cleanup.InstallerRoots})
	if recs == nil {
		recs = []advice.Recommendation{}
	}
	projects := devjunk.GroupProjects(scan)
	if projects == nil {
		projects = []devjunk.Project{}
	}
	return OverviewResult{Mode: mode, SettingsKey: key, Scan: scan, Recommendations: recs, Headline: advice.Headline(recs), Projects: projects, Skipped: skipped}
}

// save writes the current result to the cache, unless a newer one was written already. A result from the cache is not written back.
func (o *Overview) save() {
	if o.cfg.Cache == nil {
		return
	}
	o.mu.Lock()
	r, gen := o.result, o.gen
	if r == nil || r.FromCache {
		o.mu.Unlock()
		return
	}
	f := scancache.File{AppVersion: o.version, SavedAt: r.CheckedAt, Mode: string(r.Mode), SettingsKey: r.SettingsKey, Skipped: r.Skipped, Scan: r.Scan}
	o.mu.Unlock()
	o.saveMu.Lock()
	defer o.saveMu.Unlock()
	if gen <= o.saved {
		return
	}
	if err := o.cfg.Cache.Save(f); err != nil {
		o.log.Warn("cache the check", "err", err)
		return
	}
	o.saved = gen
}

// background runs the scheduled check when it is due (Policy.Interval after the last check, or the settings it ran under changed) and the machine allows it, inside the activity gate's background slot; after a completed check come the due automatic rules (macOS) and a space snapshot when the newest is a day old. The machine's power is watched while it runs; leaving AC power stops it.
func (o *Overview) background(ctx context.Context, s settings.AppSettings) {
	now := o.now()
	o.mu.Lock()
	due := s.Background.Enabled && (schedule.Due(o.cfg.Policy, now, o.lastCheck, o.launched) || o.result != nil && o.result.SettingsKey != scancache.Key(s))
	retry := now.Round(0).Before(o.retryAt.Round(0))
	busy := o.cleaning || o.autoCleaning
	o.mu.Unlock()
	switch {
	case !due:
		o.setWaiting("", time.Time{})
		return
	case busy || o.cfg.Activity.Cleaning():
		o.setWaiting(schedule.ReasonCleaning, time.Time{})
		return
	case retry:
		return
	}
	slot, cancel := context.WithCancel(ctx)
	defer cancel()
	o.mu.Lock()
	o.bg = cancel
	o.mu.Unlock()
	defer func() {
		o.mu.Lock()
		o.bg = nil
		o.mu.Unlock()
	}()
	if v := schedule.Check(slot, o.cfg.Policy, o.cfg.Conditions, false); !v.OK {
		if slot.Err() == nil {
			o.setWaiting(v.Reason, now.Add(o.cfg.Policy.Retry))
		}
		return
	}
	run, end, err := o.cfg.Activity.BeginBackground(slot)
	if err != nil {
		if slot.Err() == nil {
			o.setWaiting(schedule.BusyVerdict(err).Reason, now.Add(o.cfg.Policy.Retry))
		}
		return
	}
	defer end()
	o.setWaiting("", time.Time{})
	var watch sync.WaitGroup
	defer watch.Wait()
	watched, stopWatch := context.WithCancel(run)
	defer stopWatch()
	watch.Go(func() {
		schedule.WatchPower(watched, o.cfg.Policy, o.cfg.Conditions, func(v schedule.Verdict) {
			o.setWaiting(v.Reason, o.now().Add(o.cfg.Policy.Retry))
			cancel()
		})
	})
	if !o.check(run, CheckBackground, o.cfg.Background) {
		return
	}
	o.autoRules(run, s)
	o.snapshot(run, s)
}

// setWaiting records why a due scheduled check waits ("" for nothing) and, when retryAt is set, when the conditions are looked at again.
func (o *Overview) setWaiting(reason string, retryAt time.Time) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.waiting = reason
	if !retryAt.IsZero() {
		o.retryAt = retryAt
	}
}

// autoRules runs the due automatic rules over the scheduled check that just completed, each as a clean of its own inside the background slot (schedule.Activity.BeginAutoClean, CleanWith with KindAuto, which only moves to the Trash). A rule with nothing to take counts as run. A clean the user starts stops them after the current item. macOS only.
func (o *Overview) autoRules(run context.Context, s settings.AppSettings) {
	if o.cfg.GOOS != "darwin" {
		return
	}
	enabled := rules.Enabled(s.Automation)
	if len(enabled) == 0 || o.pending(o.cfg.Background) {
		return
	}
	now := o.now()
	o.mu.Lock()
	due := rules.Due(enabled, o.state.RuleRuns, now)
	var scan cleanup.ScanResult
	if o.result != nil && o.from == o.cfg.Background {
		scan = o.result.Scan
	}
	apps := o.apps
	o.mu.Unlock()
	if len(due) == 0 || scan.Categories == nil {
		return
	}
	in := rules.Inputs{Now: now, Downloads: filepath.Join(o.cfg.Home, "Downloads"), InstalledApps: apps}
	if o.cfg.Ledger != nil {
		in.Restored = o.cfg.Ledger.Restored(now)
	}
	plans := map[rules.ID]rules.Planned{}
	for _, p := range rules.Plan(scan, due, in) {
		plans[p.Rule] = p
	}
	auto := AutoRun{}
	changed := false
	for _, spec := range due {
		if run.Err() != nil {
			break
		}
		p, ok := plans[spec.ID]
		if !ok {
			// Without the installed apps the installers rule cannot tell; it tries again after the next check.
			if spec.NeedInstalled && apps == nil {
				continue
			}
			o.ruleRan(spec.ID, o.now(), 0)
			changed = true
			continue
		}
		out, err := o.autoClean(run, spec, p)
		if err != nil {
			o.log.Warn("automatic rule", "rule", spec.ID, "err", err)
			continue
		}
		o.ruleRan(spec.ID, o.now(), out.Result.Trashed)
		changed = true
		if out.RunID != "" {
			auto.RunIDs = append(auto.RunIDs, out.RunID)
			auto.Trashed += out.Result.Trashed
		}
		o.mu.Lock()
		o.dropLocked(o.cfg.Background, out.Result)
		o.mu.Unlock()
	}
	if !changed {
		return
	}
	o.mu.Lock()
	if len(auto.RunIDs) > 0 {
		auto.At = o.now()
		o.lastAuto = &auto
	}
	st := o.state
	o.mu.Unlock()
	o.saveState(st)
	o.save()
	o.publish()
}

// autoClean runs one rule's plan as an automatic clean.
func (o *Overview) autoClean(run context.Context, spec rules.Spec, p rules.Planned) (CleanRun, error) {
	ctx, end, err := o.cfg.Activity.BeginAutoClean(run)
	if err != nil {
		return CleanRun{}, err
	}
	defer end()
	o.mu.Lock()
	o.autoCleaning = true
	o.mu.Unlock()
	o.publish()
	defer func() {
		o.mu.Lock()
		o.autoCleaning = false
		o.mu.Unlock()
	}()
	detail := fmt.Sprintf("%s：%d 项（%s）", spec.Title, p.Items, format.BytesIn(p.Bytes, o.cfg.GOOS == "windows"))
	return o.cfg.Cleanup.CleanWith(ctx, o.cfg.Background, p.Req, CleanOptions{Kind: history.KindAuto, Rule: string(spec.ID), Detail: detail}, nil)
}

// ruleRan records that rule ran at, moving trashed.
func (o *Overview) ruleRan(rule rules.ID, at time.Time, trashed int64) {
	o.mu.Lock()
	defer o.mu.Unlock()
	runs := make(map[string]time.Time, len(o.state.RuleRuns)+1)
	for k, v := range o.state.RuleRuns {
		runs[k] = v
	}
	runs[string(rule)] = at
	o.state.RuleRuns = runs
	o.lastTrashed[string(rule)] = trashed
	o.rulesDirty = true
}

// saveState writes background.json.
func (o *Overview) saveState(st schedule.State) {
	if o.cfg.State == nil {
		return
	}
	if err := o.cfg.State.Save(st); err != nil {
		o.log.Warn("save background state", "err", err)
	}
}

// snapshot takes a space snapshot inside the background slot when the newest kept is snapshotEvery old or there is none, then prunes the old ones.
func (o *Overview) snapshot(run context.Context, s settings.AppSettings) {
	if o.cfg.Growth == nil || o.cfg.Watches == nil || run.Err() != nil {
		return
	}
	now := o.now()
	times, err := o.cfg.Growth.List()
	if err != nil {
		o.log.Warn("list space snapshots", "err", err)
		return
	}
	if n := len(times); n > 0 && now.Round(0).Sub(times[n-1].Round(0)) < snapshotEvery && !times[n-1].After(now) {
		return
	}
	opts := o.cfg.GrowthOptions
	if o.cfg.Access != nil {
		opts.MayRead = o.cfg.Access.Allow
	}
	snap, err := growth.Take(run, o.cfg.Watches(s), opts)
	if err != nil {
		if run.Err() == nil {
			o.log.Warn("take a space snapshot", "err", err)
		}
		return
	}
	if err := o.cfg.Growth.Save(snap); err != nil {
		o.log.Warn("save a space snapshot", "err", err)
		return
	}
	if _, err := o.cfg.Growth.Prune(now, growth.DefaultRetention()); err != nil {
		o.log.Warn("prune space snapshots", "err", err)
	}
}

// summary posts the weekly summary of the automatic rules (macOS): on the first turn after Monday 10:00, at least a week after the last one, when the rules moved anything over the past week less what was put back.
func (o *Overview) summary(s settings.AppSettings) {
	if o.cfg.GOOS != "darwin" || !s.Automation.WeeklySummary || o.cfg.History == nil {
		return
	}
	now := o.now()
	o.mu.Lock()
	last := o.state.LastSummary
	o.mu.Unlock()
	monday := lastMonday10(now)
	if !last.IsZero() && (!last.Before(monday) || now.Sub(last) < summaryWeek) {
		return
	}
	runs, err := o.cfg.History.List(0)
	if err != nil {
		o.log.Warn("read history for the weekly summary", "err", err)
		return
	}
	sum := rules.Summarize(runs, now.Add(-summaryWeek), now)
	o.mu.Lock()
	o.state.LastSummary = now
	st := o.state
	if sum.Trashed > 0 {
		year, week := now.ISOWeek()
		o.notifyLocked(Notice{Kind: NoticeAutoSummary, ID: fmt.Sprintf("%s:%d-W%02d", NoticeAutoSummary, year, week), Trashed: sum.Trashed, Rules: sum.ByRule})
	}
	o.mu.Unlock()
	o.saveState(st)
}

// lastMonday10 is the latest Monday 10:00 at or before t, in t's time zone.
func lastMonday10(t time.Time) time.Time {
	days := (int(t.Weekday()) + 6) % 7
	m := time.Date(t.Year(), t.Month(), t.Day()-days, 10, 0, 0, 0, t.Location())
	if m.After(t) {
		m = m.AddDate(0, 0, -7)
	}
	return m
}

// readDisk reads free space every Policy.DiskEvery (and after a clean or a settings change) and posts the low-disk notification when the alert is on and schedule.LowDiskAlert says so.
func (o *Overview) readDisk(ctx context.Context, s settings.AppSettings) {
	if o.cfg.Disk == nil {
		return
	}
	now := o.now()
	o.mu.Lock()
	due := o.diskDue || o.diskAt.IsZero() || now.Round(0).Sub(o.diskAt.Round(0)) >= o.cfg.Policy.DiskEvery || now.Before(o.diskAt)
	o.mu.Unlock()
	if !due {
		return
	}
	d, err := o.cfg.Disk(ctx)
	o.mu.Lock()
	o.diskAt, o.diskDue = now, false
	if err != nil {
		o.mu.Unlock()
		o.log.Info("read free space", "err", err)
		return
	}
	d.Low, d.At = DiskLow(d.Free, d.Total), now
	o.disk = &d
	if !s.General.LowDiskAlert {
		o.mu.Unlock()
		return
	}
	notify, next := schedule.LowDiskAlert(o.state.LowDisk, d.Free, int64(s.General.LowDiskGB)*gigabyte(o.cfg.GOOS), now)
	if next == o.state.LowDisk && !notify {
		o.mu.Unlock()
		return
	}
	o.state.LowDisk = next
	st := o.state
	if notify {
		n := Notice{Kind: NoticeLowDisk, ID: string(NoticeLowDisk), Volume: d.Volume, Free: d.Free, Total: d.Total}
		if o.result != nil {
			n.Recommended = o.result.Headline
		}
		o.notifyLocked(n)
	}
	o.mu.Unlock()
	o.saveState(st)
}

// Clean cleans req from the current result for origin, through the smartscan instance that produced it, and returns what it did; the result loses what was handled and is cached again. Only a result of this process's own check may be cleaned, while it is Actionable, and one clean at a time; a card's clean is recorded as 清理建议 with the card's line first, a drill-down view's with its source's kind. Errors are sentences in Simplified Chinese.
func (o *Overview) Clean(ctx context.Context, req cleanup.CleanRequest, origin CleanOrigin, progress func(cleanup.Progress)) (OverviewOutcome, error) {
	o.mu.Lock()
	r := o.result
	switch {
	case o.cleaning || o.autoCleaning:
		o.mu.Unlock()
		return OverviewOutcome{}, &appError{msg: "正在清理另一项，完成后再试", err: cleanup.ErrBusy}
	case r == nil:
		o.mu.Unlock()
		return OverviewOutcome{}, errors.New("还没有检查结果，请先检查")
	case !o.actionableLocked(r, o.cfg.Settings.Get(), o.now()):
		o.mu.Unlock()
		return OverviewOutcome{}, errors.New("检查结果可能已经变化，请重新检查后再清理")
	}
	cleaner := o.from
	o.cleaning = true
	opts := o.cleanOptions(*r, req, origin)
	o.mu.Unlock()
	o.publish()
	run, err := o.cfg.Cleanup.CleanWith(ctx, cleaner, req, opts, progress)
	o.mu.Lock()
	o.cleaning = false
	o.diskDue = true
	if err == nil {
		o.dropLocked(cleaner, run.Result)
	}
	o.mu.Unlock()
	if err == nil {
		o.save()
	}
	o.publish()
	o.poke()
	if err != nil {
		return OverviewOutcome{}, err
	}
	out := OverviewOutcome{Result: run.Result, RunID: run.RunID, Undoable: run.Undo != nil}
	if run.Undo != nil {
		out.NotUndoable = max(len(run.Result.Moved)-run.Undo.Items, 0)
	} else if o.canUndo {
		out.NotUndoable = len(run.Result.Moved)
	}
	return out, nil
}

// cleanOptions are how a clean for origin is recorded: a card's as 清理建议 with 「{Title}：{n} 项（{size}）」 first, the items it names; a drill-down view's with its source's kind.
func (o *Overview) cleanOptions(r OverviewResult, req cleanup.CleanRequest, origin CleanOrigin) CleanOptions {
	if origin.Source != "" {
		if kind, ok := cleanerKinds[CleanerID(origin.Source)]; ok && origin.Recommendation == "" {
			return CleanOptions{Kind: kind}
		}
	}
	if origin.Recommendation == "" {
		return CleanOptions{}
	}
	opts := CleanOptions{Kind: history.KindSmartScan}
	for _, rec := range r.Recommendations {
		if rec.ID == origin.Recommendation {
			n, bytes := requested(r.Scan, req)
			opts.Detail = fmt.Sprintf("%s：%d 项（%s）", rec.Title, n, format.BytesIn(bytes, o.cfg.GOOS == "windows"))
			break
		}
	}
	return opts
}

// requested counts the items of scan req names, and their size.
func requested(scan cleanup.ScanResult, req cleanup.CleanRequest) (int, int64) {
	named := make(map[string]bool, len(req.ItemIDs))
	for _, id := range req.ItemIDs {
		named[id] = true
	}
	n, bytes := 0, int64(0)
	for _, c := range scan.Categories {
		if !slices.Contains(req.CategoryIDs, c.ID) {
			continue
		}
		for _, it := range c.Items {
			if len(named) == 0 || named[it.ID] {
				n++
				bytes += it.Size
			}
		}
	}
	return n, bytes
}

// dropLocked takes what a clean of cleaner handled out of the current result, when it is that instance's, as the instance itself forgets it: the items, and a command category whose command ran as a whole. The cards are derived again. o.mu is held.
func (o *Overview) dropLocked(cleaner CleanerID, res cleanup.CleanResult) {
	if o.result == nil || o.result.FromCache || o.from != cleaner || len(res.Handled) == 0 {
		return
	}
	prev := o.result.Scan
	scan := cleanup.Drop(prev, res.Handled)
	kept := scan.Categories[:0:0]
	for i, c := range scan.Categories {
		if c.Command != "" && len(prev.Categories[i].Items) > 0 && len(c.Items) == 0 {
			continue
		}
		kept = append(kept, c)
	}
	scan.Categories = kept
	r := o.deriveLocked(scan, o.result.Mode, o.result.SettingsKey, o.result.Skipped)
	r.CheckedAt = o.result.CheckedAt
	o.result = &r
	o.rulesDirty = true
	o.gen++
}

// pending reports whether the smartscan instance id cannot clean its last scan: it did not finish with every source (smartscan.Scanner.Pending).
func (o *Overview) pending(id CleanerID) bool {
	s, err := o.cfg.Cleanup.scanner(id)
	if err != nil {
		return true
	}
	p, ok := s.(interface{ Pending() bool })
	return ok && p.Pending()
}

// actionableLocked is OverviewResult.Actionable: not from the cache, its instance neither scanning nor Pending, checked under the current settings and within scancache.StaleAfter. o.mu is held.
func (o *Overview) actionableLocked(r *OverviewResult, s settings.AppSettings, now time.Time) bool {
	if r.FromCache || o.from == "" || o.scanning == o.from || o.pending(o.from) {
		return false
	}
	return !o.staleLocked(r, s, now)
}

// staleLocked is OverviewResult.Stale: scancache.Stale of the result. o.mu is held.
func (o *Overview) staleLocked(r *OverviewResult, s settings.AppSettings, now time.Time) bool {
	return scancache.Stale(scancache.File{SavedAt: r.CheckedAt, SettingsKey: r.SettingsKey, AppVersion: o.version}, scancache.Key(s), o.cfg.AppVersion, now)
}

// Undo puts back what the run runID moved to the Trash, then checks again in the foreground (the cards are not pieced back together).
func (o *Overview) Undo(ctx context.Context, runID string) (RestoreOutcome, error) {
	if o.cfg.Restore == nil {
		return RestoreOutcome{}, errors.New("这台电脑上不能放回原处")
	}
	out, err := o.cfg.Restore.Run(ctx, runID)
	if err == nil && out.Restored > 0 {
		o.mu.Lock()
		o.diskDue = true
		o.askForegroundLocked()
		o.mu.Unlock()
		o.poke()
	}
	return out, err
}

// Growth lists what grew over window up to the newest space snapshot, most first; growth.ErrNoHistory while fewer than two are kept.
func (o *Overview) Growth(_ context.Context, window time.Duration) ([]growth.Growth, error) {
	if o.cfg.Growth == nil {
		return nil, growth.ErrNoHistory
	}
	return o.cfg.Growth.Changes(o.now(), window, growthRows)
}

// TrashInfo measures the Trash now, with how many of its items Spruce could still put back.
func (o *Overview) TrashInfo(ctx context.Context) (TrashInfo, error) {
	if o.cfg.TrashSize == nil {
		return TrashInfo{}, errors.New("无法读取" + binName())
	}
	u, err := o.cfg.TrashSize(ctx)
	if err != nil {
		return TrashInfo{}, err
	}
	info := TrashInfo{Items: int(u.Items), Bytes: u.Bytes}
	if o.cfg.Ledger != nil && o.canUndo {
		info.Restorable = o.cfg.Ledger.Pending(o.now())
	}
	return info, nil
}

// State returns the controller's state now.
func (o *Overview) State() OverviewState {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.stateLocked()
}

// Subscribe returns a channel that holds the current state at once and then always the latest, and the function that ends it.
func (o *Overview) Subscribe() (<-chan OverviewState, func()) {
	ch := make(chan OverviewState, 1)
	o.mu.Lock()
	o.subs[ch] = struct{}{}
	ch <- o.stateLocked()
	o.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			o.mu.Lock()
			delete(o.subs, ch)
			close(ch)
			o.mu.Unlock()
		})
	}
}

// Notices returns the notifications to post, and the function that ends them. Those sent before the first subscriber wait for it (a few).
func (o *Overview) Notices() (<-chan Notice, func()) {
	ch := make(chan Notice, 8)
	o.mu.Lock()
	o.notices[ch] = struct{}{}
	for _, n := range o.queued {
		ch <- n
	}
	o.queued = nil
	o.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			o.mu.Lock()
			delete(o.notices, ch)
			close(ch)
			o.mu.Unlock()
		})
	}
}

// notifyLocked hands n to every subscriber, or queues it while there is none. o.mu is held.
func (o *Overview) notifyLocked(n Notice) {
	if len(o.notices) == 0 {
		if len(o.queued) < maxQueuedNotices {
			o.queued = append(o.queued, n)
		}
		return
	}
	for ch := range o.notices {
		select {
		case ch <- n:
		default:
			o.log.Warn("notification dropped", "id", n.ID)
		}
	}
}

// publish sends the state to every subscriber.
func (o *Overview) publish() {
	o.mu.Lock()
	defer o.mu.Unlock()
	st := o.stateLocked()
	for ch := range o.subs {
		select {
		case <-ch:
		default:
		}
		ch <- st
	}
}

// stateLocked is the state now, with Stale, Actionable and the rules' figures worked out afresh. Nothing it returns is changed afterwards. o.mu is held.
func (o *Overview) stateLocked() OverviewState {
	s := o.cfg.Settings.Get()
	now := o.now()
	st := OverviewState{Phase: OverviewIdle, Progress: o.progress, Waiting: o.waiting, Error: o.errText, Disk: o.disk, LastAuto: o.lastAuto, CanUndo: o.canUndo}
	switch {
	case o.cleaning || o.autoCleaning:
		st.Phase = OverviewCleaning
	case o.scanning != "":
		st.Phase = OverviewChecking
	}
	if o.result != nil {
		r := *o.result
		r.Stale = o.staleLocked(o.result, s, now)
		r.Actionable = o.actionableLocked(o.result, s, now)
		st.Result = &r
	}
	if s.Background.Enabled {
		st.Next = schedule.Next(o.cfg.Policy, now, o.lastCheck, o.launched)
		if o.retryAt.After(st.Next) {
			st.Next = o.retryAt
		}
	}
	if o.rulesDirty {
		o.rules = o.ruleStatusesLocked(s, now)
		o.rulesDirty = false
	}
	st.Rules = o.rules
	return st
}

// ruleStatusesLocked are the rules as 设置 → 自动清理 shows them: what each would take from the current result now, due or not, and its last run. o.mu is held.
func (o *Overview) ruleStatusesLocked(s settings.AppSettings, now time.Time) []RuleStatus {
	enabled := rules.Enabled(s.Automation)
	catalog := rules.Catalog()
	matching := map[rules.ID]rules.Planned{}
	if o.result != nil {
		in := rules.Inputs{Now: now, Downloads: filepath.Join(o.cfg.Home, "Downloads"), InstalledApps: o.apps}
		if o.cfg.Ledger != nil {
			in.Restored = o.cfg.Ledger.Restored(now)
		}
		for _, p := range rules.Plan(o.result.Scan, catalog, in) {
			matching[p.Rule] = p
		}
	}
	out := make([]RuleStatus, 0, len(catalog))
	for _, spec := range catalog {
		id := string(spec.ID)
		out = append(out, RuleStatus{ID: id, Enabled: slices.Contains(enabled, spec.ID), Matching: matching[spec.ID].Items, MatchingBytes: matching[spec.ID].Bytes,
			LastRun: o.state.RuleRuns[id], LastTrashed: o.lastTrashed[id]})
	}
	return out
}
