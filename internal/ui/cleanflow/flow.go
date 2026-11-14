// Package cleanflow is what the four cleaner pages (系统垃圾, 开发者垃圾, 安装包, 智能扫描) share: the scan and clean controller (Flow), its pure logic (the selection, the clean plan, the done card's summary) and the views around it (ScanIdle, ScanProgress, CategoryList, CleanPlanDialog, CleaningOverlay, CleanDoneDialog and View, the whole body of a cleaner page).
package cleanflow

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/async"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Phase is where a cleaner page is.
type Phase uint8

// The phases: before a scan, scanning, showing the results, cleaning, and showing the done card.
const (
	PhaseIdle Phase = iota
	PhaseScanning
	PhaseResults
	PhaseCleaning
	PhaseDone
)

// progressEvery bounds how often a scan's or a clean's progress reaches the page; only the latest event counts (async.Feed).
const progressEvery = 80 * time.Millisecond

// Cleaner scans and cleans through the shared cleanup controller: *app.Cleanup.
type Cleaner interface {
	Scan(ctx context.Context, id app.CleanerID, progress func(cleanup.Progress)) (cleanup.ScanResult, error)
	Clean(ctx context.Context, id app.CleanerID, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error)
}

// Flow drives a cleaner page through Cleaner.Scan and Cleaner.Clean for one cleaner: the phase, the streamed progress, the scan result with its selection (starting from each category's Selected), the confirmation the user asked for in 设置 (General.ConfirmBeforeClean) and the outcome. Make it in the page's component (NewFlow), whose scope stops what runs when the page goes; its methods run on the engine goroutine.
type Flow struct {
	Phase *core.Signal[Phase]
	// Confirming is set while the page asks before a clean.
	Confirming *core.Signal[bool]
	// Progress is the latest event of the running scan or clean; the zero Progress before the first.
	Progress *core.Signal[cleanup.Progress]
	// Result is the last scan's, nil before one ended.
	Result *core.Signal[*cleanup.ScanResult]
	// Selection is what is checked among Result's items.
	Selection *core.Signal[Selection]
	// ScanErr is why the last scan failed (a sentence to show), "" when the user stopped it. CleanErr is why the clean could not start.
	ScanErr, CleanErr *core.Signal[string]
	// Outcome is how the clean ended, nil before.
	Outcome *core.Signal[*CleanOutcome]

	cx     *core.Scope
	ctx    context.Context
	c      Cleaner
	id     app.CleanerID
	stores *store.Stores
	// cancel stops the running scan or clean; seq tells a run's answer from a later run's.
	cancel context.CancelFunc
	seq    int
	// focusScan asks the idle view to take the focus to 扫描 once it is back (after 完成).
	focusScan bool
}

// NewFlow makes the controller for cleaner id in cx, the page's scope; leaving the page stops the scan or the clean it runs. It reads the confirmation setting and bumps the Trash from the app's stores when cx has them.
func NewFlow(cx *core.Scope, c Cleaner, id app.CleanerID) *Flow {
	return &Flow{cx: cx, ctx: cx.Context(), c: c, id: id, stores: store.Use(cx),
		Phase:      core.NewSignal(cx, PhaseIdle),
		Confirming: core.NewSignal(cx, false),
		Progress:   core.NewSignalFunc(cx, cleanup.Progress{}, nil),
		Result:     core.NewSignalFunc[*cleanup.ScanResult](cx, nil, nil),
		Selection:  core.NewSignalFunc(cx, NothingSelected, nil),
		ScanErr:    core.NewSignal(cx, ""),
		CleanErr:   core.NewSignal(cx, ""),
		Outcome:    core.NewSignalFunc[*CleanOutcome](cx, nil, nil),
	}
}

// Categories returns the last scan's categories, nil before one; reading it subscribes.
func (f *Flow) Categories() []cleanup.Category {
	if r := f.Result.Get(); r != nil {
		return r.Categories
	}
	return nil
}

// Totals returns the checked items' count and size; reading it subscribes.
func (f *Flow) Totals() Totals { return SelectedTotals(f.Categories(), f.Selection.Get()) }

// SetSelection replaces what is checked.
func (f *Flow) SetSelection(sel Selection) { f.Selection.Set(sel) }

// busy reports whether a scan or a clean runs.
func (f *Flow) busy() bool {
	p := f.Phase.GetUntracked()
	return p == PhaseScanning || p == PhaseCleaning
}

// start begins a run under the page's context: it returns the run's context, a sink for its progress and the run's number.
func (f *Flow) start() (context.Context, *async.Feed[cleanup.Progress], int) {
	f.seq++
	mine := f.seq
	run, cancel := context.WithCancel(f.ctx)
	f.cancel = cancel
	f.Progress.Set(cleanup.Progress{})
	feed := async.NewFeed(f.cx, progressEvery, func(p cleanup.Progress) {
		if mine == f.seq {
			f.Progress.Set(p)
		}
	})
	return run, feed, mine
}

// settle ends run number mine: it reports whether the run is still the current one and whether the user stopped it.
func (f *Flow) settle(run context.Context, mine int) (current, stopped bool) {
	stopped = run.Err() != nil
	if mine != f.seq {
		return false, stopped
	}
	f.cancel()
	f.cancel = nil
	f.Progress.Set(cleanup.Progress{})
	return true, stopped
}

// Scan starts a scan; it does nothing while a scan or a clean runs.
func (f *Flow) Scan() {
	if f.busy() {
		return
	}
	f.cx.Batch(func() {
		f.ScanErr.Set("")
		f.CleanErr.Set("")
		f.Outcome.Set(nil)
		f.Confirming.Set(false)
		f.Result.Set(nil)
		f.Phase.Set(PhaseScanning)
	})
	run, feed, mine := f.start()
	async.Do(f.cx, run, func(ctx context.Context) (cleanup.ScanResult, error) {
		defer feed.Flush()
		return f.c.Scan(ctx, f.id, feed.Send)
	}, func(res cleanup.ScanResult, err error) {
		current, stopped := f.settle(run, mine)
		if !current {
			return
		}
		if err != nil {
			// A scan the user stopped goes back to idle without an error.
			if !stopped {
				f.ScanErr.Set(err.Error())
			}
			f.Phase.Set(PhaseIdle)
			return
		}
		f.Result.Set(&res)
		f.Selection.Set(DefaultSelection(res.Categories))
		f.Phase.Set(PhaseResults)
	})
}

// Stop stops the scan (back to idle) or the clean (after the current item; the outcome says it was interrupted).
func (f *Flow) Stop() {
	if f.cancel != nil {
		f.cancel()
	}
}

// confirmFirst reports whether the user wants to confirm every clean: the setting once loaded, yes before.
func (f *Flow) confirmFirst() bool {
	if f.stores == nil {
		return true
	}
	return core.Untracked(f.cx, func() bool {
		s := f.stores.Settings
		return !s.Loaded() || s.Value().General.ConfirmBeforeClean
	})
}

// RequestClean cleans the selection, or first asks when the user wants to confirm every clean. It does nothing without results or with nothing checked.
func (f *Flow) RequestClean() {
	if f.Phase.GetUntracked() != PhaseResults || core.Untracked(f.cx, f.Totals).Count == 0 {
		return
	}
	if f.confirmFirst() {
		f.Confirming.Set(true)
		return
	}
	f.clean()
}

// Confirm answers the confirmation with 清理.
func (f *Flow) Confirm() {
	f.Confirming.Set(false)
	if f.Phase.GetUntracked() == PhaseResults {
		f.clean()
	}
}

// Cancel answers the confirmation with 取消.
func (f *Flow) Cancel() { f.Confirming.Set(false) }

// clean cleans what is checked.
func (f *Flow) clean() {
	req := core.Untracked(f.cx, func() cleanup.CleanRequest { return ToCleanRequest(f.Categories(), f.Selection.Get()) })
	f.CleanErr.Set("")
	f.Outcome.Set(nil)
	f.Phase.Set(PhaseCleaning)
	run, feed, mine := f.start()
	async.Do(f.cx, run, func(ctx context.Context) (cleanup.CleanResult, error) {
		defer feed.Flush()
		return f.c.Clean(ctx, f.id, req, feed.Send)
	}, func(res cleanup.CleanResult, err error) {
		if current, _ := f.settle(run, mine); !current {
			return
		}
		if err != nil {
			f.CleanErr.Set(err.Error())
			f.Outcome.Set(&CleanOutcome{Err: err.Error()})
		} else {
			f.Outcome.Set(&CleanOutcome{Result: &res})
		}
		f.Phase.Set(PhaseDone)
		// Whatever the clean did, the Trash may hold more now.
		if f.stores != nil {
			f.stores.Trash.Bump()
		}
	})
}

// Finish closes the done card and returns to idle, ready to scan again; the idle view then takes the focus to 扫描.
func (f *Flow) Finish() {
	f.focusScan = true
	f.cx.Batch(func() {
		f.Result.Set(nil)
		f.Selection.Set(NothingSelected)
		f.Outcome.Set(nil)
		f.CleanErr.Set("")
		f.Confirming.Set(false)
		f.Phase.Set(PhaseIdle)
	})
}

// TakeFocusRequest reports, once, that 完成 asked for the focus to go back to 扫描; a page that draws its own idle view calls it there and focuses its button.
func (f *Flow) TakeFocusRequest() bool {
	asked := f.focusScan
	f.focusScan = false
	return asked
}
