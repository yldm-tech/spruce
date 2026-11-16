package maintenance

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"sync"
	"time"

	"spruce/apps/spruce/internal/engine/elevate"
	"spruce/apps/spruce/internal/engine/taskresult"
)

// Errors of Service.Run.
var (
	// ErrNoTasks is returned for an empty selection.
	ErrNoTasks = errors.New("maintenance: no task selected")
	// ErrUnknownTask is returned (wrapped, naming the ID) for a selected ID that is not in the catalog.
	ErrUnknownTask = errors.New("maintenance: unknown task")
	// ErrBusy is returned when a run is already in progress.
	ErrBusy = errors.New("maintenance: a run is already in progress")
)

// EventKind is the kind of a progress Event.
type EventKind string

const (
	// EventStarted is sent when a task begins (for the elevated batch: when the batch, and its prompt, begins).
	EventStarted EventKind = "started"
	// EventFinished is sent when a task has its Result.
	EventFinished EventKind = "finished"
)

// Event reports progress of a run. Every selected task gets exactly one EventStarted followed by exactly one EventFinished.
type Event struct {
	Kind   EventKind
	TaskID string
	// Result is set for EventFinished.
	Result *taskresult.Result
	// Done is how many tasks have finished, including this one for EventFinished.
	Done int
	// Total is the number of selected tasks.
	Total int
}

// Service runs maintenance tasks of one catalog.
type Service struct {
	tasks    []TaskImpl
	env      Env
	elevator elevate.Elevator
	running  sync.Mutex
	now      func() time.Time
}

// NewService validates tasks (non-empty title, valid unique IDs, a Run function, an icon) and returns a Service that runs them with env, sending NeedsAdmin tasks to elevator. A nil env.Log logs nowhere.
func NewService(tasks []TaskImpl, env Env, elevator elevate.Elevator) (*Service, error) {
	if err := validateCatalog(tasks); err != nil {
		return nil, err
	}
	if elevator == nil && slices.ContainsFunc(tasks, func(t TaskImpl) bool { return t.NeedsAdmin }) {
		return nil, errors.New("maintenance: the catalog has admin tasks but no elevator")
	}
	if env.Log == nil {
		env.Log = slog.New(slog.DiscardHandler)
	}
	return &Service{tasks: slices.Clone(tasks), env: env, elevator: elevator, now: time.Now}, nil
}

func validateCatalog(tasks []TaskImpl) error {
	seen := make(map[string]bool, len(tasks))
	for i, t := range tasks {
		switch {
		case !elevate.ValidTaskID(t.ID):
			return fmt.Errorf("maintenance: task %d has invalid ID %q", i, t.ID)
		case seen[t.ID]:
			return fmt.Errorf("maintenance: task ID %q is declared twice", t.ID)
		case t.Title == "":
			return fmt.Errorf("maintenance: task %q has no title", t.ID)
		case t.Icon == "":
			return fmt.Errorf("maintenance: task %q has no icon", t.ID)
		case t.Run == nil:
			return fmt.Errorf("maintenance: task %q has no Run", t.ID)
		}
		seen[t.ID] = true
	}
	return nil
}

// Catalog returns the tasks in display order.
func (s *Service) Catalog() []Task {
	out := make([]Task, len(s.tasks))
	for i, t := range s.tasks {
		out[i] = t.Task
	}
	return out
}

// Run runs the tasks named by ids and returns one Result per selected task, in catalog order. Duplicated IDs run once. Unknown IDs reject the whole selection before anything runs.
//
// Order: every Precheck first (in the app process); then the NeedsAdmin tasks that passed, as one elevated batch behind one prompt (a declined prompt makes them Skipped, a failed launch makes them Failed); then the other tasks one by one. progress, when non-nil, is called synchronously from the calling goroutine.
//
// When ctx is cancelled the tasks not yet run are reported Skipped and Run returns the results together with ctx's error. A precheck is not started once ctx is cancelled, and one that ends after the cancellation is reported as interrupted instead of with its own verdict. An elevated batch sees the cancellation too: the launcher stops waiting, which on macOS closes a system prompt the user has not answered, and its tasks are reported Skipped as stopped without knowing whether they ran, since a child already running as root cannot be stopped (see package elevate). A batch that answered anyway keeps the results the child reported.
func (s *Service) Run(ctx context.Context, ids []string, progress func(Event)) ([]taskresult.Result, error) {
	selected, err := s.selection(ids)
	if err != nil {
		return nil, err
	}
	if !s.running.TryLock() {
		return nil, ErrBusy
	}
	defer s.running.Unlock()

	r := &run{s: s, ctx: ctx, progress: progress, total: len(selected), results: make(map[string]taskresult.Result, len(selected))}

	var admin, user []TaskImpl
	for _, t := range selected {
		if ctx.Err() != nil {
			r.finished(t, interrupted(), 0)
			continue
		}
		if t.Precheck != nil {
			r.started(t.ID)
			start := s.now()
			if res := t.Precheck(ctx, s.env); res != nil {
				// A precheck whose command was killed by the cancellation would otherwise report that as a verdict (e.g. "cannot tell whether a VPN is connected").
				if ctx.Err() != nil {
					r.finished(t, interrupted(), 0)
					continue
				}
				r.finished(t, *res, s.now().Sub(start))
				continue
			}
		}
		if t.NeedsAdmin {
			admin = append(admin, t)
		} else {
			user = append(user, t)
		}
	}

	if len(admin) > 0 {
		r.runElevated(admin)
	}
	for _, t := range user {
		if ctx.Err() != nil {
			r.finished(t, interrupted(), 0)
			continue
		}
		r.started(t.ID)
		start := s.now()
		res := t.Run(ctx, s.env)
		r.finished(t, res, s.now().Sub(start))
	}

	out := make([]taskresult.Result, 0, len(selected))
	for _, t := range selected {
		out = append(out, r.results[t.ID])
	}
	return out, ctx.Err()
}

// selection resolves ids to tasks in catalog order.
func (s *Service) selection(ids []string) ([]TaskImpl, error) {
	if len(ids) == 0 {
		return nil, ErrNoTasks
	}
	want := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !slices.ContainsFunc(s.tasks, func(t TaskImpl) bool { return t.ID == id }) {
			return nil, fmt.Errorf("%w: %q", ErrUnknownTask, id)
		}
		want[id] = true
	}
	var out []TaskImpl
	for _, t := range s.tasks {
		if want[t.ID] {
			out = append(out, t)
		}
	}
	return out, nil
}

// run is the state of one Service.Run call.
type run struct {
	s          *Service
	ctx        context.Context
	progress   func(Event)
	total      int
	done       int
	startedIDs map[string]bool
	results    map[string]taskresult.Result
}

func (r *run) started(id string) {
	if r.startedIDs == nil {
		r.startedIDs = make(map[string]bool)
	}
	if r.startedIDs[id] {
		return
	}
	r.startedIDs[id] = true
	if r.progress != nil {
		r.progress(Event{Kind: EventStarted, TaskID: id, Done: r.done, Total: r.total})
	}
}

func (r *run) finished(t TaskImpl, res taskresult.Result, d time.Duration) {
	r.started(t.ID)
	res.TaskID = t.ID
	if d > 0 {
		res.Duration = d
	}
	if !res.Outcome.Valid() {
		r.s.env.Log.Error("maintenance task returned an invalid outcome", "task", t.ID, "outcome", res.Outcome)
		res.Detail = fmt.Sprintf("invalid outcome %q: %s", res.Outcome, res.Detail)
		res.Outcome = taskresult.Failed
	}
	r.results[t.ID] = res
	r.done++
	if r.progress != nil {
		r.progress(Event{Kind: EventFinished, TaskID: t.ID, Result: &res, Done: r.done, Total: r.total})
	}
}

// runElevated runs the admin tasks in one batch through the elevator.
func (r *run) runElevated(tasks []TaskImpl) {
	ids := make([]string, len(tasks))
	for i, t := range tasks {
		ids[i] = t.ID
		r.started(t.ID)
	}
	if r.ctx.Err() != nil {
		for _, t := range tasks {
			r.finished(t, interrupted(), 0)
		}
		return
	}
	// Stopping the run must close a system prompt the user has not answered (it may be hidden behind another window, holding the whole run), so the batch gets r.ctx. Once the user answered, the root child cannot be stopped from here: on macOS the root shell that "do shell script ... with administrator privileges" started cannot be signalled by this user-level process, and on Windows TerminateProcess may lack the right. Its tasks are then reported as not known to have run rather than as skipped.
	results, err := r.s.elevator.Run(r.ctx, ids)
	switch {
	case err != nil && r.ctx.Err() != nil:
		for _, t := range tasks {
			r.finished(t, taskresult.Result{Outcome: taskresult.Skipped, Message: StoppedUncertain, Detail: err.Error()}, 0)
		}
		return
	case errors.Is(err, elevate.ErrCancelled):
		for _, t := range tasks {
			r.finished(t, taskresult.Result{Outcome: taskresult.Skipped, Message: "已取消管理员授权，未执行", Detail: err.Error()}, 0)
		}
		return
	case err != nil:
		r.s.env.Log.Error("elevated batch failed", "tasks", ids, "err", err)
		for _, t := range tasks {
			r.finished(t, taskresult.Result{Outcome: taskresult.Failed, Message: "无法以管理员身份运行", Detail: err.Error()}, 0)
		}
		return
	}
	// The elevator returns results in the order of ids (elevate.Elevator contract); Durations were measured in the child.
	for i, t := range tasks {
		r.finished(t, results[i], 0)
	}
}

// StoppedUncertain is the message of an admin task whose batch was stopped once the system prompt may have been answered: its root child may have run it, or may still be running it.
const StoppedUncertain = "已停止，无法确认是否执行"

// MayStillRun reports whether r is an admin task stopped while its batch may still be running as root, so running it again at once could run it twice side by side.
func MayStillRun(r taskresult.Result) bool {
	return r.Outcome == taskresult.Skipped && r.Message == StoppedUncertain
}

func interrupted() taskresult.Result {
	return taskresult.Result{Outcome: taskresult.Skipped, Message: "维护已中断，未执行", Detail: context.Canceled.Error()}
}

// ElevatedHandler returns the elevate.Handler that the elevated child runs: it executes the requested NeedsAdmin tasks of tasks with env (whose Elevated is forced true and HomeDir and Permissions cleared). An ID that is unknown or names a task without NeedsAdmin yields a Failed result instead of running anything.
func ElevatedHandler(tasks []TaskImpl, env Env) elevate.Handler {
	env.Elevated = true
	env.HomeDir = ""
	env.Permissions = nil
	if env.Log == nil {
		env.Log = slog.New(slog.DiscardHandler)
	}
	return func(ctx context.Context, ids []string) []taskresult.Result {
		out := make([]taskresult.Result, 0, len(ids))
		for _, id := range ids {
			i := slices.IndexFunc(tasks, func(t TaskImpl) bool { return t.ID == id })
			if i < 0 || !tasks[i].NeedsAdmin || tasks[i].Run == nil {
				out = append(out, taskresult.Result{TaskID: id, Outcome: taskresult.Failed, Message: "无法执行该任务", Detail: "not an administrator task of this build"})
				continue
			}
			if ctx.Err() != nil {
				res := interrupted()
				res.TaskID = id
				out = append(out, res)
				continue
			}
			start := time.Now()
			res := tasks[i].Run(ctx, env)
			res.TaskID = id
			res.Duration = time.Since(start)
			if !res.Outcome.Valid() {
				res.Detail = fmt.Sprintf("invalid outcome %q: %s", res.Outcome, res.Detail)
				res.Outcome = taskresult.Failed
			}
			out = append(out, res)
		}
		return out
	}
}
