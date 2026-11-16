package uninstall

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/async"
	"github.com/yldm-tech/spruce/internal/ui/kit"
)

// Step is where the page is in an uninstall.
type Step uint8

// The steps: choosing an app, confirming, moving it to the Trash (macOS, or a Windows program's leftovers), waiting for a Windows uninstaller, a Windows program that is still installed, the leftovers a Windows program left, and the done card.
const (
	StepIdle Step = iota
	StepConfirm
	StepRemoving
	StepWaiting
	StepNotRemoved
	StepLeftovers
	StepDone
)

// detailsEvery is how often streamed details reach the list, so a few hundred updates do not mean a few hundred renders; every update is kept (async.Batch), one per app.
const detailsEvery = 120 * time.Millisecond

// progressEvery bounds how often the removal's progress reaches the overlay; only the latest counts.
const progressEvery = 80 * time.Millisecond

// State is the 应用卸载 page's controller: the installed apps and their streamed details, the search, the order and the picked app, its leftovers (macOS) and the uninstall. Make it in the page's component (NewState), whose scope ends every load and removal when the page goes; its methods run on the engine goroutine.
type State struct {
	cx      *core.Scope
	ctx     context.Context
	svc     Service
	files   Files
	windows bool
	// onChanged runs after anything went to the Trash or an uninstaller ran.
	onChanged func()

	// Apps is the installed apps, nil until the first answer; Loaded is set by that answer, Fetching while a load goes on, AppsErr when the last one failed.
	Apps     *core.Signal[[]app.UninstallApp]
	Loaded   *core.Signal[bool]
	Fetching *core.Signal[bool]
	AppsErr  *core.Signal[string]
	// Details is what Measure streamed by app ID; Measuring is set until the measure of the last list ends.
	Details   *core.Signal[map[string]AppDetails]
	Measuring *core.Signal[bool]

	Query    *core.Signal[string]
	Sort     *core.Signal[SortKey]
	Selected *core.Signal[string]

	// Plan is the picked app's leftovers (macOS), nil until found; PlanLoading while a lookup goes on, PlanErr when it failed. Checked are the leftovers to take along, reset to their defaults by every answer.
	Plan        *core.Signal[*app.UninstallPlan]
	PlanLoading *core.Signal[bool]
	PlanErr     *core.Signal[string]
	Checked     *core.Signal[map[string]bool]
	// RevealErr is why the last 在访达中显示 failed.
	RevealErr *core.Signal[string]

	Step *core.Signal[Step]
	// Target is the app being uninstalled.
	Target   *core.Signal[app.UninstallApp]
	Progress *core.Signal[cleanup.Progress]
	// Launched is set once a Windows uninstaller runs.
	Launched *core.Signal[bool]
	// Waited, Checking and RecheckErr are the still-installed card's: whether the uninstaller exited, whether 再次检查 goes on, and what it found.
	Waited     *core.Signal[bool]
	Checking   *core.Signal[bool]
	RecheckErr *core.Signal[string]
	// Found and FoundChecked are what a Windows program left and the files to move to the Trash.
	Found        *core.Signal[app.UninstallPlan]
	FoundChecked *core.Signal[map[string]bool]
	Summary      *core.Signal[DoneSummary]

	// visible is the apps the list shows.
	visible *core.Memo[[]app.UninstallApp]

	appsSeq, planSeq, measureSeq int
	cancelMeasure, cancelPlan    context.CancelFunc
	cancelRun                    context.CancelFunc
}

// NewState makes the controller in cx, the page's scope, and loads the apps.
func NewState(cx *core.Scope, d Deps, onChanged func()) *State {
	s := &State{cx: cx, ctx: cx.Context(), svc: d.Uninstall, files: d.Files, windows: d.Platform.windows(), onChanged: onChanged,
		Apps:         core.NewSignalFunc[[]app.UninstallApp](cx, nil, nil),
		Loaded:       core.NewSignal(cx, false),
		Fetching:     core.NewSignal(cx, false),
		AppsErr:      core.NewSignal(cx, ""),
		Details:      core.NewSignalFunc(cx, map[string]AppDetails{}, nil),
		Measuring:    core.NewSignal(cx, false),
		Query:        core.NewSignal(cx, ""),
		Sort:         core.NewSignal(cx, SortSize),
		Selected:     core.NewSignal(cx, ""),
		Plan:         core.NewSignalFunc[*app.UninstallPlan](cx, nil, nil),
		PlanLoading:  core.NewSignal(cx, false),
		PlanErr:      core.NewSignal(cx, ""),
		Checked:      core.NewSignalFunc(cx, map[string]bool{}, nil),
		RevealErr:    core.NewSignal(cx, ""),
		Step:         core.NewSignal(cx, StepIdle),
		Target:       core.NewSignalFunc(cx, app.UninstallApp{}, nil),
		Progress:     core.NewSignalFunc(cx, cleanup.Progress{}, nil),
		Launched:     core.NewSignal(cx, false),
		Waited:       core.NewSignal(cx, false),
		Checking:     core.NewSignal(cx, false),
		RecheckErr:   core.NewSignal(cx, ""),
		Found:        core.NewSignalFunc(cx, app.UninstallPlan{}, nil),
		FoundChecked: core.NewSignalFunc(cx, map[string]bool{}, nil),
		Summary:      core.NewSignalFunc(cx, DoneSummary{}, nil),
	}
	s.visible = core.NewMemoFunc(cx, func() []app.UninstallApp {
		return VisibleApps(s.Apps.Get(), s.Details.Get(), s.Query.Get(), s.Sort.Get(), s.windows)
	}, nil)
	s.LoadApps()
	return s
}

// Windows reports whether the page follows the Windows flow.
func (s *State) Windows() bool { return s.windows }

// Visible returns the apps the list shows, searched and sorted; reading it subscribes.
func (s *State) Visible() []app.UninstallApp { return s.visible.Get() }

// App returns the picked app, false when none is (or it left the list).
func (s *State) App() (app.UninstallApp, bool) {
	id := s.Selected.Get()
	if id == "" {
		return app.UninstallApp{}, false
	}
	for _, a := range s.Apps.Get() {
		if a.ID == id {
			return a, true
		}
	}
	return app.UninstallApp{}, false
}

// DetailsOf returns what is known of the app with ID id; reading it subscribes.
func (s *State) DetailsOf(id string) AppDetails { return s.Details.Get()[id] }

// LoadApps reads the installed apps again; an answer starts measuring them. The answer of an older load is dropped.
func (s *State) LoadApps() {
	s.appsSeq++
	seq := s.appsSeq
	s.Fetching.Set(true)
	async.Do(s.cx, s.ctx, s.svc.Apps, func(apps []app.UninstallApp, err error) {
		if seq != s.appsSeq {
			return
		}
		s.Fetching.Set(false)
		s.Loaded.Set(true)
		if err != nil {
			s.AppsErr.Set(err.Error())
			return
		}
		if apps == nil {
			apps = []app.UninstallApp{}
		}
		s.AppsErr.Set("")
		if s.Apps.GetUntracked() != nil {
			// A list read again (after an uninstall, 刷新) closes over the apps that went.
			kit.SettleLayout(s.cx)
		}
		s.Apps.Set(apps)
		s.measure()
	})
}

// measure streams the sizes, icons and last use of the listed apps, merging them every detailsEvery; a later list stops the earlier measure.
func (s *State) measure() {
	if s.cancelMeasure != nil {
		s.cancelMeasure()
	}
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancelMeasure = cancel
	s.measureSeq++
	seq := s.measureSeq
	s.Measuring.Set(true)
	batch := async.NewBatch(s.cx, detailsEvery, func(updates []app.UninstallAppDetails) {
		// Rows slide into the order the new sizes and times give, instead of jumping.
		kit.SettleLayout(s.cx)
		s.Details.Set(MergeDetails(s.Details.GetUntracked(), updates))
	})
	async.Do(s.cx, ctx, func(ctx context.Context) (struct{}, error) {
		defer batch.Flush()
		// A failed measure leaves the sizes it did not reach unknown; the list says so.
		return struct{}{}, s.svc.Measure(ctx, batch.Send)
	}, func(struct{}, error) {
		if seq == s.measureSeq {
			s.Measuring.Set(false)
		}
	})
}

// Select picks the app with ID id and, on macOS, looks up its leftovers.
func (s *State) Select(id string) {
	if id == s.Selected.GetUntracked() {
		return
	}
	s.Selected.Set(id)
	s.Plan.Set(nil)
	s.PlanErr.Set("")
	s.Checked.Set(map[string]bool{})
	if id == "" || s.windows {
		s.stopPlan()
		return
	}
	s.LoadPlan()
}

func (s *State) stopPlan() {
	if s.cancelPlan != nil {
		s.cancelPlan()
		s.cancelPlan = nil
	}
	s.planSeq++
	s.PlanLoading.Set(false)
}

// LoadPlan looks up the picked app's leftovers (again): the app may have run and written new files meanwhile. Every answer resets the checked leftovers to their defaults.
func (s *State) LoadPlan() {
	id := s.Selected.GetUntracked()
	if id == "" || s.windows {
		return
	}
	s.stopPlan()
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancelPlan = cancel
	seq := s.planSeq
	s.PlanLoading.Set(true)
	s.PlanErr.Set("")
	async.Do(s.cx, ctx, func(ctx context.Context) (app.UninstallPlan, error) { return s.svc.Leftovers(ctx, id) },
		func(p app.UninstallPlan, err error) {
			if seq != s.planSeq {
				return
			}
			cancel()
			s.PlanLoading.Set(false)
			if err != nil {
				s.PlanErr.Set(err.Error())
				return
			}
			s.Plan.Set(&p)
			s.Checked.Set(DefaultChecked(p.Leftovers))
		})
}

// Toggle checks or unchecks the picked app's leftover with ID id.
func (s *State) Toggle(id string, on bool) { s.Checked.Set(with(s.Checked.GetUntracked(), id, on)) }

// with returns a copy of set with id in it or not.
func with(set map[string]bool, id string, on bool) map[string]bool {
	next := make(map[string]bool, len(set)+1)
	for k, v := range set {
		next[k] = v
	}
	if on {
		next[id] = true
	} else {
		delete(next, id)
	}
	return next
}

// all returns every leftover's ID checked.
func all(leftovers []app.UninstallLeftover) map[string]bool {
	out := make(map[string]bool, len(leftovers))
	for _, l := range leftovers {
		out[l.ID] = true
	}
	return out
}

// Running reports whether the picked app runs now, by its leftovers' lookup when there is one.
func (s *State) Running() bool {
	if p := s.Plan.Get(); p != nil {
		return p.Running
	}
	a, ok := s.App()
	return ok && a.Running
}

// BundleSize is the picked app's own size as its lookup measured it (macOS).
func (s *State) BundleSize() int64 {
	if p := s.Plan.Get(); p != nil && p.Bundle != nil {
		return p.Bundle.Size
	}
	return 0
}

// Totals counts and sizes the checked leftovers of the picked app.
func (s *State) Totals() (int, int64) {
	p := s.Plan.Get()
	if p == nil {
		return 0, 0
	}
	return CheckedTotals(p.Leftovers, s.Checked.Get())
}

// CanUninstall reports whether 卸载 may start: an app is picked and, on macOS, its leftovers are found and it does not run.
func (s *State) CanUninstall() bool {
	if _, ok := s.App(); !ok {
		return false
	}
	return s.windows || s.Plan.Get() != nil && !s.PlanLoading.Get() && !s.Running()
}

// Reveal shows path in Finder or Explorer; a failure shows under the app's facts.
func (s *State) Reveal(path string) {
	s.RevealErr.Set("")
	async.Do(s.cx, s.ctx, func(ctx context.Context) (struct{}, error) { return struct{}{}, s.files.Reveal(ctx, path) },
		func(_ struct{}, err error) {
			if err != nil {
				s.RevealErr.Set(err.Error())
			}
		})
}

// Refresh reads the apps again and, on macOS, the picked app's leftovers.
func (s *State) Refresh() {
	s.LoadApps()
	if _, ok := s.picked(); ok {
		s.LoadPlan()
	}
}

// picked is App without subscribing.
func (s *State) picked() (app.UninstallApp, bool) {
	var ok bool
	a := core.Untracked(s.cx, func() app.UninstallApp {
		var a app.UninstallApp
		a, ok = s.App()
		return a
	})
	return a, ok
}

// Start asks before uninstalling the picked app.
func (s *State) Start() {
	if !core.Untracked(s.cx, s.CanUninstall) || s.Step.GetUntracked() != StepIdle {
		return
	}
	a, _ := s.picked()
	s.Target.Set(a)
	s.Step.Set(StepConfirm)
}

// Cancel closes the confirmation.
func (s *State) Cancel() {
	if s.Step.GetUntracked() == StepConfirm {
		s.Step.Set(StepIdle)
	}
}

// Confirm starts the uninstall the confirmation asked about: the move to the Trash with the checked leftovers (macOS), or the program's own uninstaller (Windows).
func (s *State) Confirm() {
	if s.Step.GetUntracked() != StepConfirm {
		return
	}
	target := s.Target.GetUntracked()
	if s.windows {
		s.runUninstaller(target)
		return
	}
	var ids []string
	if p := s.Plan.GetUntracked(); p != nil {
		ids = CheckedIDs(p.Leftovers, s.Checked.GetUntracked())
	}
	s.remove(target, app.UninstallRequest{AppID: target.ID, LeftoverIDs: ids})
}

// run returns a context for one removal or wait, which Stop cancels.
func (s *State) run() (context.Context, context.CancelFunc) {
	ctx, cancel := context.WithCancel(s.ctx)
	s.cancelRun = cancel
	return ctx, cancel
}

// Stop stops the removal after the current item, or stops waiting for a Windows uninstaller (never the uninstaller itself).
func (s *State) Stop() {
	if s.cancelRun != nil {
		s.cancelRun()
	}
}

func (s *State) finish(sum DoneSummary) {
	s.Summary.Set(sum)
	s.Step.Set(StepDone)
}

func (s *State) changed() {
	if s.onChanged != nil {
		s.onChanged()
	}
}

func (s *State) remove(target app.UninstallApp, req app.UninstallRequest) {
	s.Progress.Set(cleanup.Progress{})
	s.Target.Set(target)
	s.Step.Set(StepRemoving)
	progress := async.NewFeed(s.cx, progressEvery, s.Progress.Set)
	ctx, cancel := s.run()
	async.Do(s.cx, ctx, func(ctx context.Context) (app.UninstallOutcome, error) {
		defer progress.Flush()
		return s.svc.Remove(ctx, req, progress.Send)
	}, func(out app.UninstallOutcome, err error) {
		cancel()
		if err != nil {
			s.finish(FailedSummary(err.Error()))
		} else {
			s.finish(RemoveSummary(out, s.windows))
		}
		s.changed()
	})
}

func (s *State) runUninstaller(target app.UninstallApp) {
	s.Launched.Set(false)
	s.Step.Set(StepWaiting)
	ctx, cancel := s.run()
	status := func(app.UninstallerStatus) {
		s.cx.Post(func() {
			if s.Step.GetUntracked() == StepWaiting {
				s.Launched.Set(true)
			}
		})
	}
	async.Do(s.cx, ctx, func(ctx context.Context) (app.UninstallerOutcome, error) {
		return s.svc.RunUninstaller(ctx, target.ID, status)
	},
		func(out app.UninstallerOutcome, err error) {
			cancel()
			s.changed()
			switch {
			case err != nil:
				s.finish(FailedSummary(err.Error()))
			case out.Removed:
				s.offerLeftovers(target)
			default:
				s.Waited.Set(out.Waited)
				s.Checking.Set(false)
				s.RecheckErr.Set("")
				s.Step.Set(StepNotRemoved)
			}
		})
}

// offerLeftovers looks up what a removed Windows program left and offers it, or ends when it left nothing.
func (s *State) offerLeftovers(target app.UninstallApp) {
	async.Do(s.cx, s.ctx, func(ctx context.Context) (app.UninstallPlan, error) { return s.svc.Leftovers(ctx, target.ID) },
		func(p app.UninstallPlan, err error) {
			switch {
			case err != nil:
				sum := UninstalledSummary(target.Name, 0)
				sum.Lines = []string{sum.Lines[0], "没能查找它留下的文件：" + err.Error()}
				s.finish(sum)
			case len(p.Leftovers) == 0:
				s.finish(UninstalledSummary(target.Name, 0))
			default:
				s.Found.Set(p)
				s.FoundChecked.Set(DefaultChecked(p.Leftovers))
				s.Step.Set(StepLeftovers)
			}
		})
}

// Recheck asks again whether a Windows program is gone, for an uninstaller that went on after the process Spruce started exited.
func (s *State) Recheck() {
	if s.Step.GetUntracked() != StepNotRemoved || s.Checking.GetUntracked() {
		return
	}
	target := s.Target.GetUntracked()
	s.Checking.Set(true)
	s.RecheckErr.Set("")
	async.Do(s.cx, s.ctx, func(ctx context.Context) (app.UninstallerOutcome, error) { return s.svc.CheckRemoved(ctx, target.ID) },
		func(out app.UninstallerOutcome, err error) {
			switch {
			case err != nil:
				s.Checking.Set(false)
				s.RecheckErr.Set(err.Error())
			case out.Removed:
				s.offerLeftovers(target)
			default:
				s.Checking.Set(false)
				s.RecheckErr.Set("「" + target.Name + "」仍在已安装的程序中")
			}
		})
}

// ToggleFound checks or unchecks a leftover a Windows program left.
func (s *State) ToggleFound(id string, on bool) {
	s.FoundChecked.Set(with(s.FoundChecked.GetUntracked(), id, on))
}

// Keep ends a Windows uninstall leaving what the program left.
func (s *State) Keep() {
	if s.Step.GetUntracked() == StepLeftovers {
		s.finish(UninstalledSummary(s.Target.GetUntracked().Name, len(s.Found.GetUntracked().Leftovers)))
	}
}

// RemoveFound moves the checked leftovers of a removed Windows program to the Recycle Bin.
func (s *State) RemoveFound() {
	if s.Step.GetUntracked() != StepLeftovers {
		return
	}
	target, found := s.Target.GetUntracked(), s.Found.GetUntracked()
	ids := CheckedIDs(found.Leftovers, s.FoundChecked.GetUntracked())
	if len(ids) == 0 {
		return
	}
	s.remove(target, app.UninstallRequest{AppID: target.ID, LeftoverIDs: ids})
}

// Close ends the done card: the page returns to choosing, with no app picked, and reads the apps again.
func (s *State) Close() {
	s.Step.Set(StepIdle)
	s.Select("")
	s.LoadApps()
}

// LiveText is what screen readers hear while an uninstall goes on.
func (s *State) LiveText() string {
	switch s.Step.Get() {
	case StepRemoving:
		return "正在卸载「" + s.Target.Get().Name + "」…"
	case StepWaiting:
		return "正在等待「" + s.Target.Get().Name + "」的卸载程序…"
	}
	return ""
}
