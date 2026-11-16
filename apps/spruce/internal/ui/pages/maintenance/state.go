package maintenance

import (
	"context"
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/ui/async"
)

// Phase is where the page is.
type Phase uint8

// The phases: choosing tasks, running them, and the done card.
const (
	PhaseIdle Phase = iota
	PhaseRunning
	PhaseDone
)

// progressEvery bounds how often the run's progress reaches the page; every event is kept (async.Batch), since the admin note needs every finished task.
const progressEvery = 80 * time.Millisecond

// State is the 维护 page's controller: the catalog, the user's choices and the run. Make it in the page's component (NewState), whose scope ends the run when the page goes; its methods run on the engine goroutine.
type State struct {
	cx  *core.Scope
	ctx context.Context
	svc Service

	// Catalog is the platform's tasks in display order, nil until loaded.
	Catalog *core.Signal[[]app.TaskView]
	// overrides are the user's choices; a task without one keeps its DefaultOn.
	overrides *core.Signal[map[string]bool]
	Phase     *core.Signal[Phase]
	Progress  *core.Signal[RunProgress]
	// RunTasks are the tasks of the run, in catalog order.
	RunTasks *core.Signal[[]app.TaskView]
	// Summary is the run's summary once it ended, nil when it could not start (RunErr says why).
	Summary *core.Signal[*app.RunSummary]
	RunErr  *core.Signal[string]
	// Guided is set once the Full Disk Access guide granted the permission since the run started, so the done card may say that running again helps.
	Guided *core.Signal[bool]
	// Stopping is set once 停止 was pressed, until the run ends.
	Stopping *core.Signal[bool]

	// cancel ends the run going on; nil when none is.
	cancel context.CancelFunc

	// onSettled runs after each run, to read the permission again.
	onSettled func()
}

// NewState makes the controller in cx, the page's scope, and loads the catalog.
func NewState(cx *core.Scope, svc Service, onSettled func()) *State {
	s := &State{cx: cx, ctx: cx.Context(), svc: svc, onSettled: onSettled,
		Catalog:   core.NewSignalFunc[[]app.TaskView](cx, nil, nil),
		overrides: core.NewSignalFunc(cx, map[string]bool{}, nil),
		Phase:     core.NewSignal(cx, PhaseIdle),
		Progress:  core.NewSignalFunc(cx, RunProgress{}, nil),
		RunTasks:  core.NewSignalFunc[[]app.TaskView](cx, nil, nil),
		Summary:   core.NewSignalFunc[*app.RunSummary](cx, nil, nil),
		RunErr:    core.NewSignal(cx, ""),
		Guided:    core.NewSignal(cx, false),
		Stopping:  core.NewSignal(cx, false),
	}
	async.Do(cx, s.ctx, func(context.Context) ([]app.TaskView, error) { return svc.Catalog(), nil },
		func(tasks []app.TaskView, _ error) { s.Catalog.Set(tasks) })
	return s
}

// On reports whether t is chosen; reading it subscribes.
func (s *State) On(t app.TaskView) bool {
	if on, ok := s.overrides.Get()[t.ID]; ok {
		return on
	}
	return t.DefaultOn
}

// Toggle records the user's choice for the task with ID id.
func (s *State) Toggle(id string, on bool) {
	s.overrides.Update(func(m map[string]bool) map[string]bool {
		next := make(map[string]bool, len(m)+1)
		for k, v := range m {
			next[k] = v
		}
		next[id] = on
		return next
	})
}

// Selected returns the chosen tasks in catalog order.
func (s *State) Selected() []app.TaskView {
	var out []app.TaskView
	for _, t := range s.Catalog.Get() {
		if s.On(t) {
			out = append(out, t)
		}
	}
	return out
}

// Estimate returns the chosen tasks' summed estimate, in seconds.
func (s *State) Estimate() int {
	sum := 0
	for _, t := range s.Selected() {
		sum += t.EstimatedSeconds
	}
	return sum
}

// Run starts the chosen tasks; it does nothing when none is chosen or a run goes on.
func (s *State) Run() {
	if s.Phase.GetUntracked() != PhaseIdle {
		return
	}
	s.start(core.Untracked(s.cx, s.Selected))
}

// Rerun closes the done card and runs again the tasks with IDs ids (Unfinished), whatever is chosen on the cards.
func (s *State) Rerun(ids []string) {
	if s.Phase.GetUntracked() != PhaseDone {
		return
	}
	var tasks []app.TaskView
	for _, t := range s.Catalog.GetUntracked() {
		if slices.Contains(ids, t.ID) {
			tasks = append(tasks, t)
		}
	}
	s.Finish()
	s.start(tasks)
}

// start runs tasks, in catalog order; it does nothing for none.
func (s *State) start(tasks []app.TaskView) {
	if len(tasks) == 0 {
		return
	}
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
	}
	// A grant made before this run is already in effect for it; only a grant made on the done card may say "run again".
	s.Guided.Set(false)
	s.Summary.Set(nil)
	s.RunErr.Set("")
	s.Progress.Set(RunProgress{Total: len(tasks)})
	s.RunTasks.Set(tasks)
	s.Phase.Set(PhaseRunning)
	events := async.NewBatch(s.cx, progressEvery, func(evs []app.ProgressEvent) {
		p := s.Progress.GetUntracked()
		for _, ev := range evs {
			p = ApplyProgress(p, ev)
		}
		s.Progress.Set(p)
	})
	run, cancel := context.WithCancel(s.ctx)
	s.cancel = cancel
	async.Do(s.cx, run, func(ctx context.Context) (app.RunSummary, error) {
		defer events.Flush()
		return s.svc.Run(ctx, ids, events.Send)
	}, func(sum app.RunSummary, err error) {
		cancel()
		s.cancel = nil
		s.Stopping.Set(false)
		if err != nil {
			s.RunErr.Set(err.Error())
		} else {
			s.Summary.Set(&sum)
		}
		s.Phase.Set(PhaseDone)
		if s.onSettled != nil {
			s.onSettled()
		}
	})
}

// Stop ends the run going on after its current task, and closes a system prompt for administrator rights the user has not answered; the done card then says the run was stopped.
func (s *State) Stop() {
	if s.cancel == nil || s.Stopping.GetUntracked() {
		return
	}
	s.Stopping.Set(true)
	s.cancel()
}

// Finish closes the done card and returns to choosing tasks.
func (s *State) Finish() {
	s.Phase.Set(PhaseIdle)
	s.Progress.Set(RunProgress{})
	s.RunTasks.Set(nil)
	s.Summary.Set(nil)
	s.RunErr.Set("")
	s.Guided.Set(false)
}
