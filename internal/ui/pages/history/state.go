package history

import (
	"context"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	enginehist "github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/async"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// State is the 操作历史 page's controller: the recorded runs, loaded again whenever the history's generation changes (a run recorded anywhere, 清空历史), which of them 放回原处 can put back, the chosen filter and the maintenance catalog that names a maintenance run's tasks. Make it in the page's component (NewState); its methods run on the engine goroutine.
type State struct {
	cx     *core.Scope
	ctx    context.Context
	hist   *store.History
	stores *store.Stores

	// Runs are the recorded runs, newest first, as the last successful load left them.
	Runs *core.Signal[[]enginehist.Run]
	// Loaded is set once a load succeeded.
	Loaded *core.Signal[bool]
	// LoadErr is the sentence of the last load's failure, "" once a load succeeds.
	LoadErr *core.Signal[string]
	// Filter is the kind the user chose, "" for every kind.
	Filter *core.Signal[enginehist.Kind]
	// Catalog is the maintenance tasks, which name a maintenance run's tasks; nil until loaded.
	Catalog *core.Signal[[]app.TaskView]
	// Restorable are the IDs of the runs 放回原处 can put back, as of the last load.
	Restorable *core.Signal[map[string]bool]
	// Restoring is the run being put back, "" for none.
	Restoring *core.Signal[string]
	// Opened is the run GoHistory asked last to show expanded, "" for none; every request notifies, so a row opens again when asked again.
	Opened *core.Signal[string]

	kinds *core.Memo[[]enginehist.Kind]
	// seq numbers the loads, so only the latest one's answer is shown.
	seq int
}

// NewState makes the controller in cx, the page's scope, loads the catalog, and loads the runs now and on every change of the history's generation. The filter and the expanded run start as GoHistory asked, when it did, and follow every later request while the page is open (查看 in a toast shown over it); nav is nil in tests that mount the page alone.
func NewState(cx *core.Scope, stores *store.Stores, catalog Catalog, n *nav.Navigator) *State {
	hist := stores.History
	s := &State{cx: cx, ctx: cx.Context(), hist: hist, stores: stores,
		Runs:       core.NewSignalFunc[[]enginehist.Run](cx, nil, nil),
		Loaded:     core.NewSignal(cx, false),
		LoadErr:    core.NewSignal(cx, ""),
		Filter:     core.NewSignal[enginehist.Kind](cx, ""),
		Catalog:    core.NewSignalFunc[[]app.TaskView](cx, nil, nil),
		Restorable: core.NewSignalFunc[map[string]bool](cx, nil, nil),
		Restoring:  core.NewSignal(cx, ""),
		Opened:     core.NewSignalFunc(cx, "", nil),
	}
	if n != nil {
		take := func() {
			kind, run := n.TakeHistoryFilter()
			s.Filter.Set(kind)
			s.Opened.Set(run)
		}
		var seen int
		cx.Untrack(func() {
			seen = n.HistoryAsked()
			take()
		})
		cx.Effect(func() {
			asked := n.HistoryAsked()
			if asked == seen {
				return
			}
			seen = asked
			cx.Untrack(take)
		})
	}
	s.kinds = core.NewMemoFunc(cx, func() []enginehist.Kind { return KindsIn(s.Runs.Get()) }, slices.Equal)
	if catalog != nil {
		async.Do(cx, s.ctx, func(context.Context) ([]app.TaskView, error) { return catalog.Catalog(), nil },
			func(tasks []app.TaskView, _ error) { s.Catalog.Set(tasks) })
	}
	cx.Effect(func() {
		hist.Generation()
		cx.Untrack(s.Load)
	})
	return s
}

// Load reads the runs again; the answer of an older load that arrives later is dropped.
func (s *State) Load() {
	s.seq++
	seq := s.seq
	s.hist.Load(s.cx, s.ctx, Limit, func(runs []enginehist.Run, err error) {
		if seq != s.seq {
			return
		}
		if err != nil {
			s.LoadErr.Set("无法读取操作历史：" + err.Error())
			return
		}
		s.LoadErr.Set("")
		s.Runs.Set(runs)
		s.Loaded.Set(true)
		s.hist.Restorable(s.cx, s.ctx, runs, func(ids map[string]bool) {
			if seq == s.seq {
				s.Restorable.Set(ids)
			}
		})
	})
}

// CanRestore reports whether the run id shows 放回原处; reading it subscribes.
func (s *State) CanRestore(id string) bool { return s.Restorable.Get()[id] }

// Restore puts back what the run id moved to the Trash, through 概览's controller (which then checks again), shows the outcome in the toast and loads the runs again. One at a time: it does nothing while anything is being cleaned or put back.
func (s *State) Restore(id string) {
	if s.Restoring.GetUntracked() != "" || s.stores.Overview.Busy() {
		return
	}
	s.Restoring.Set(id)
	s.stores.Overview.Undo(s.cx, id, func(out app.RestoreOutcome, err error) {
		s.Restoring.Set("")
		s.stores.Trash.Bump()
		s.stores.Locations.Bump()
		if err != nil {
			s.stores.Toasts.Show(store.Toast{Text: err.Error(), Duration: toastShort})
		} else {
			text, view, d := RestoreToast(out)
			s.stores.Toasts.Show(store.Toast{Text: text, ViewRun: view, Duration: d})
		}
		s.Load()
	})
}

// Kinds returns the kinds the runs hold, in the sidebar's order; reading it subscribes.
func (s *State) Kinds() []enginehist.Kind { return s.kinds.Get() }

// Active returns the filter in effect: the chosen kind while runs of it remain (清空历史 takes them away), else "" for every kind.
func (s *State) Active() enginehist.Kind {
	f := s.Filter.Get()
	if f != "" && slices.Contains(s.Kinds(), f) {
		return f
	}
	return ""
}

// Shown returns the runs the filter lets through.
func (s *State) Shown() []enginehist.Run {
	runs, active := s.Runs.Get(), s.Active()
	if active == "" {
		return runs
	}
	return slices.DeleteFunc(slices.Clone(runs), func(r enginehist.Run) bool { return r.Kind != active })
}

// Groups returns the shown runs by local day as of now.
func (s *State) Groups() []DayGroup { return GroupByDay(s.Shown(), time.Now()) }
