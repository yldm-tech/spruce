package uninstall

import (
	"context"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/async"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/store"
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

// staleAfter is how old the list may be when the page shows again before it is read again, in the background: apps installed or removed meanwhile come and go without the user asking.
const staleAfter = 5 * time.Minute

// place is where the list was when an uninstall started, so the list lands near it after: the target and its row.
type place struct {
	id    string
	index int
}

// State is the 应用卸载 page's controller: the installed apps and their streamed details, the search, the order and the picked app, its leftovers (macOS) and the uninstall. Provide makes it once in the app's scope, so the list, the search, the order, the picked app and its checked leftovers stay from one visit of the page to the next, and a measure goes on while the page is away; its methods run on the engine goroutine.
type State struct {
	cx      *core.Scope
	ctx     context.Context
	svc     Service
	files   Files
	windows bool
	// stores are the app's: the Trash's generation, which every move to the Trash bumps, the toast and 撤销.
	stores *store.Stores

	// Apps is the installed apps, nil until the first answer; Loaded is set by that answer, Fetching while a load goes on, AppsErr when the last one failed.
	Apps     *core.Signal[[]app.UninstallApp]
	Loaded   *core.Signal[bool]
	Fetching *core.Signal[bool]
	AppsErr  *core.Signal[string]
	// Details is what Measure streamed by app ID; Measuring is set until the measure of the last list ends.
	Details   *core.Signal[map[string]AppDetails]
	Measuring *core.Signal[bool]

	Query *core.Signal[string]
	Sort  *core.Signal[SortKey]
	// Selected is the picked app's ID; Picked is the list's selection, which it follows both ways.
	Selected *core.Signal[string]
	Picked   *base.Selection
	// order is the IDs of the apps in the list's order (Order), kept while details stream in.
	order *core.Signal[[]string]
	// reveals counts the times the list is asked to bring the picked row into view: after the order changed and after an uninstall.
	reveals *core.Signal[int]

	// Plan is the picked app's leftovers (macOS), nil until found; PlanLoading while a lookup goes on, PlanErr when it failed. Checked are the leftovers to take along: an answer sets their defaults, or once the user changed them for this app, keeps what is still there (KeptChecked).
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
	// listedAt is when the list was last read.
	listedAt time.Time
	// touched is the app whose leftovers' checks the user changed, "" for none.
	touched string
	// startOnPlan is the app UninstallApp asks about once its leftovers are found, "" for none.
	startOnPlan string
	// targetIndex is the target's row when the uninstall started; landing, set as the done card or the toast ends it, is where the next list puts the selection. Until that list comes the target stays picked though it may be gone, so nothing asks to uninstall it.
	targetIndex int
	landing     *core.Signal[*place]
	// undoRun is the run the done card's 撤销 puts back, "" for none; undoable are the runs this page offered 撤销 for, so putting one back reads the list again.
	undoRun  string
	undoable map[string]bool
	// listPos is where the list was scrolled to when the page went.
	listPos    core.ListPosition
	hasListPos bool
}

// NewState makes the controller in cx, the app's scope; Visit reads the apps once the page shows.
func NewState(cx *core.Scope, d Deps, stores *store.Stores) *State {
	s := &State{cx: cx, ctx: cx.Context(), svc: d.Uninstall, files: d.Files, windows: d.Platform.windows(), stores: stores, undoable: map[string]bool{},
		Apps:         core.NewSignalFunc[[]app.UninstallApp](cx, nil, nil),
		Loaded:       core.NewSignal(cx, false),
		Fetching:     core.NewSignal(cx, false),
		AppsErr:      core.NewSignal(cx, ""),
		Details:      core.NewSignalFunc(cx, map[string]AppDetails{}, nil),
		Measuring:    core.NewSignal(cx, false),
		Query:        core.NewSignal(cx, ""),
		Sort:         core.NewSignal(cx, SortSize),
		Selected:     core.NewSignal(cx, ""),
		Picked:       base.NewSelection(cx, false),
		order:        core.NewSignalFunc[[]string](cx, nil, slices.Equal),
		reveals:      core.NewSignal(cx, 0),
		landing:      core.NewSignalFunc[*place](cx, nil, nil),
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
	s.visible = core.NewMemoFunc(cx, func() []app.UninstallApp { return Arrange(s.Apps.Get(), s.order.Get(), s.Query.Get()) }, nil)
	// A press or a key in the list picks the app.
	cx.Effect(func() {
		keys := s.Picked.Keys()
		cx.Untrack(func() { s.pickedInList(keys) })
	})
	// An app the search hides is not the one 卸载 is about: the first match takes its place.
	cx.Effect(func() {
		visible := s.visible.Get()
		cx.Untrack(func() { s.keepPickedVisible(visible) })
	})
	// 撤销 of one of this page's uninstalls, from the toast, the done card or 操作历史, brings the app back: the list is read again.
	cx.Effect(func() {
		run := stores.Overview.LastUndo().RunID
		cx.Untrack(func() {
			if s.undoable[run] {
				delete(s.undoable, run)
				s.LoadApps()
			}
		})
	})
	return s
}

// Visit is called as the page shows: it reads the apps the first time, and again in the background when the list is older than staleAfter; otherwise the page shows the list it had, as it was. A picked app that ran may have been quit meanwhile, so it is looked up again (Active).
func (s *State) Visit() {
	s.Active()
	if s.Fetching.GetUntracked() {
		return
	}
	if s.Apps.GetUntracked() == nil || s.cx.Now().Sub(s.listedAt) >= staleAfter {
		s.LoadApps()
	}
}

// pickedInList follows the list's selection, keys, into Selected; while an uninstall goes on the list's selection goes back to the target.
func (s *State) pickedInList(keys []any) {
	id := ""
	if len(keys) > 0 {
		id, _ = keys[0].(string)
	}
	if id == s.Selected.GetUntracked() {
		return
	}
	if s.Step.GetUntracked() != StepIdle {
		s.syncPicked(s.Selected.GetUntracked())
		return
	}
	s.Select(id)
}

// syncPicked makes the list's selection id alone, none for "".
func (s *State) syncPicked(id string) {
	keys := core.Untracked(s.cx, s.Picked.Keys)
	if id == "" {
		if len(keys) > 0 {
			s.Picked.Clear()
		}
		return
	}
	if len(keys) != 1 || keys[0] != id {
		s.Picked.Set(id)
	}
}

// keepPickedVisible picks the first of visible, or none, when the picked app is not among them.
func (s *State) keepPickedVisible(visible []app.UninstallApp) {
	id := s.Selected.GetUntracked()
	if id == "" || s.Step.GetUntracked() != StepIdle || s.landing.GetUntracked() != nil || slices.ContainsFunc(visible, func(a app.UninstallApp) bool { return a.ID == id }) {
		return
	}
	if len(visible) == 0 {
		s.Select("")
		return
	}
	s.Select(visible[0].ID)
	s.reveal()
}

// reveal asks the list to bring the picked row into view.
func (s *State) reveal() { s.reveals.Set(s.reveals.GetUntracked() + 1) }

// Reveals counts the list's requests to bring the picked row into view; reading it subscribes.
func (s *State) Reveals() int { return s.reveals.Get() }

// SetSort orders the list by key; the rows slide into the new order.
func (s *State) SetSort(key SortKey) {
	if s.Sort.GetUntracked() == key {
		return
	}
	s.Sort.Set(key)
	s.reorder()
}

// reorder puts the list in the order the sort gives now, the rows sliding there, and keeps the picked row in view.
func (s *State) reorder() {
	next := Order(s.Apps.GetUntracked(), s.Details.GetUntracked(), s.Sort.GetUntracked(), s.windows)
	if slices.Equal(next, s.order.GetUntracked()) {
		return
	}
	kit.SettleLayout(s.cx)
	s.order.Set(next)
	s.reveal()
}

// Index returns the picked app's row among the visible ones, -1 when it has none; it does not subscribe.
func (s *State) Index() int {
	id := s.Selected.GetUntracked()
	return slices.IndexFunc(s.visible.GetUntracked(), func(a app.UninstallApp) bool { return a.ID == id })
}

// Move picks the app delta rows from the picked one, the first when none is (↑ and ↓ in the search field).
func (s *State) Move(delta int) {
	apps := s.visible.GetUntracked()
	if len(apps) == 0 || s.Step.GetUntracked() != StepIdle {
		return
	}
	i := s.Index()
	if i < 0 {
		i = 0
	} else {
		i = min(max(i+delta, 0), len(apps)-1)
	}
	s.Select(apps[i].ID)
	s.reveal()
}

// PickFirst picks the first app the list shows (Return in the search field).
func (s *State) PickFirst() {
	apps := s.visible.GetUntracked()
	if len(apps) == 0 || s.Step.GetUntracked() != StepIdle {
		return
	}
	s.Select(apps[0].ID)
	s.reveal()
}

// SaveListPos keeps where the list is scrolled to as the page goes; ListPos returns it, false before.
func (s *State) SaveListPos(p core.ListPosition) { s.listPos, s.hasListPos = p, true }

// ListPos returns where the list was scrolled to when the page last went, false before it ever did.
func (s *State) ListPos() (core.ListPosition, bool) { return s.listPos, s.hasListPos }

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

// LoadApps reads the installed apps again; an answer starts measuring them. The answer of an older load is dropped. A list read again keeps showing the one it replaces until it arrives, and keeps what is known of each app's size, icon and last use.
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
			if s.landing.GetUntracked() != nil {
				// The target stays picked: its leftovers, dropped as the uninstall ended, are looked up again.
				s.landing.Set(nil)
				s.LoadPlan()
			}
			return
		}
		if apps == nil {
			apps = []app.UninstallApp{}
		}
		s.listedAt = s.cx.Now()
		s.AppsErr.Set("")
		order := Order(apps, s.Details.GetUntracked(), s.Sort.GetUntracked(), s.windows)
		s.cx.Batch(func() {
			if s.Apps.GetUntracked() != nil {
				// A list read again (after an uninstall, 刷新) closes over the apps that went.
				kit.SettleLayout(s.cx)
			}
			s.Apps.Set(apps)
			s.order.Set(order)
			s.land(Arrange(apps, order, s.Query.GetUntracked()))
		})
		s.measure()
	})
}

// land puts the selection where an uninstall left the list, visible being the new list's rows: on the target when it is still installed, its leftovers looked up again; else on the row now where it was.
func (s *State) land(visible []app.UninstallApp) {
	l := s.landing.GetUntracked()
	if l == nil {
		return
	}
	s.landing.Set(nil)
	switch {
	case slices.ContainsFunc(visible, func(a app.UninstallApp) bool { return a.ID == l.id }):
		s.Select(l.id)
		s.LoadPlan()
	case len(visible) == 0:
		s.Select("")
	default:
		s.Select(visible[min(max(l.index, 0), len(visible)-1)].ID)
	}
	s.reveal()
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
	// The rows keep their places while the details stream in, so none moves under the pointer; they slide into the order the details give once the measure ends.
	batch := async.NewBatch(s.cx, detailsEvery, func(updates []app.UninstallAppDetails) {
		s.Details.Set(MergeDetails(s.Details.GetUntracked(), updates))
	})
	async.Do(s.cx, ctx, func(ctx context.Context) (struct{}, error) {
		defer batch.Flush()
		// A failed measure leaves the sizes it did not reach unknown; the list says so.
		return struct{}{}, s.svc.Measure(ctx, batch.Send)
	}, func(struct{}, error) {
		if seq == s.measureSeq {
			s.Measuring.Set(false)
			s.reorder()
		}
	})
}

// Select picks the app with ID id and, on macOS, looks up its leftovers.
func (s *State) Select(id string) {
	s.syncPicked(id)
	if id == s.Selected.GetUntracked() {
		return
	}
	s.Selected.Set(id)
	s.touched, s.startOnPlan = "", ""
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

// LoadPlan looks up the picked app's leftovers (again): the app may have run and written new files meanwhile. An answer checks the leftovers that start checked, or, once the user changed the checks of this app, keeps those still there checked and leaves new ones unchecked. It also tells the list whether the app runs.
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
				s.startOnPlan = ""
				s.PlanErr.Set(err.Error())
				return
			}
			checked := DefaultChecked(p.Leftovers)
			if s.touched == id {
				checked = KeptChecked(s.Checked.GetUntracked(), p.Leftovers)
			}
			s.Plan.Set(&p)
			s.Checked.Set(checked)
			s.setRunning(id, p.Running)
			if s.startOnPlan == id {
				s.startOnPlan = ""
				s.Start()
			}
		})
}

// setRunning records in the list whether the app with ID id runs, as a lookup of its leftovers found.
func (s *State) setRunning(id string, running bool) {
	apps := s.Apps.GetUntracked()
	i := slices.IndexFunc(apps, func(a app.UninstallApp) bool { return a.ID == id })
	if i < 0 || apps[i].Running == running {
		return
	}
	next := slices.Clone(apps)
	next[i].Running = running
	s.Apps.Set(next)
}

// Active is called as the app becomes active again: the picked app may have been quit meanwhile, so a running one is looked up again.
func (s *State) Active() {
	if _, ok := s.picked(); ok && !s.PlanLoading.GetUntracked() && core.Untracked(s.cx, s.Running) {
		s.LoadPlan()
	}
}

// Toggle checks or unchecks the picked app's leftover with ID id.
func (s *State) Toggle(id string, on bool) { s.SetChecked(with(s.Checked.GetUntracked(), id, on)) }

// SetChecked replaces the picked app's checked leftovers (全选, 全不选); the next lookup of its leftovers keeps them.
func (s *State) SetChecked(set map[string]bool) {
	s.touched = s.Selected.GetUntracked()
	s.Checked.Set(set)
}

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

// CanUninstall reports whether 卸载 may start: an app is picked, the list an uninstall reads again has come and, on macOS, the app's leftovers are found and it does not run.
func (s *State) CanUninstall() bool {
	if _, ok := s.App(); !ok || s.landing.Get() != nil {
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
	s.targetIndex = s.Index()
	s.Target.Set(a)
	s.Step.Set(StepConfirm)
}

// UninstallApp picks the app with ID id and asks before uninstalling it (Return or a double click in the list, the row's menu), once its leftovers are found when they are still being looked up.
func (s *State) UninstallApp(id string) {
	if s.Step.GetUntracked() != StepIdle {
		return
	}
	s.Select(id)
	if s.PlanLoading.GetUntracked() {
		s.startOnPlan = id
		return
	}
	s.Start()
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

func (s *State) changed() { s.stores.Trash.Bump() }

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
		s.changed()
		if err != nil {
			s.finish(FailedSummary(err.Error()))
			return
		}
		if out.Undoable && out.RunID != "" {
			s.undoable[out.RunID] = true
		}
		// A removal that did all it was asked ends in the toast, with 撤销; the done card explains the rest.
		if t, ok := RemoveToast(out, s.windows); ok {
			s.Close()
			s.stores.Toasts.Show(t)
			return
		}
		if out.Undoable {
			s.undoRun = out.RunID
		}
		s.finish(RemoveSummary(out, s.windows))
	})
}

// UndoRun is the run the done card's 撤销 puts back, "" when it has none.
func (s *State) UndoRun() string { return s.undoRun }

// Undo closes the done card and puts back what its removal moved to the Trash; the toast says it goes on, then how it went, and the list is read again once the app is back.
func (s *State) Undo() {
	run := s.undoRun
	if run == "" || s.Step.GetUntracked() != StepDone {
		return
	}
	s.Close()
	stores := s.stores
	stores.Toasts.Show(store.Toast{Text: "正在放回…"})
	stores.Overview.Undo(s.cx, run, nil, func(out app.RestoreOutcome, err error) {
		stores.Trash.Bump()
		stores.Locations.Bump()
		stores.Toasts.Show(store.UndoneToast(out, err))
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

// Close ends the done card (or the toast takes its place): the page returns to choosing and reads the apps again, landing on the target when it is still installed, else on the row now where it was.
func (s *State) Close() {
	s.Step.Set(StepIdle)
	s.undoRun = ""
	s.landing.Set(&place{id: s.Target.GetUntracked().ID, index: s.targetIndex})
	// The target's leftovers are no longer what they were; land looks them up again when it is still installed.
	s.stopPlan()
	s.Plan.Set(nil)
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
