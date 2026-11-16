package app

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"log/slog"
	"time"

	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/engine/schedule"
	"spruce/apps/spruce/internal/engine/taskresult"
)

// TaskView is one card of the 维护 page.
type TaskView struct {
	ID          string `json:"id"`
	Title       string `json:"title"`
	Description string `json:"description"`
	// Hint, when set, replaces Description in the accent colour (a prerequisite such as "需要先退出 Safari 和邮件").
	Hint             string           `json:"hint,omitempty"`
	Icon             maintenance.Icon `json:"icon"`
	DefaultOn        bool             `json:"defaultOn"`
	NeedsAdmin       bool             `json:"needsAdmin"`
	EstimatedSeconds int              `json:"estimatedSeconds"`
}

// ProgressKind is the kind of a ProgressEvent.
type ProgressKind string

const (
	// ProgressStarted means the task began.
	ProgressStarted ProgressKind = "started"
	// ProgressFinished means the task has its result.
	ProgressFinished ProgressKind = "finished"
)

// ProgressEvent is reported to the page while a run goes on. Each selected task gets one started and then one finished event.
type ProgressEvent struct {
	Kind   ProgressKind `json:"kind"`
	TaskID string       `json:"taskId"`
	// Result is set on finished events.
	Result *taskresult.Result `json:"result,omitempty"`
	Done   int                `json:"done"`
	Total  int                `json:"total"`
}

// RunSummary is the outcome of a whole run.
type RunSummary struct {
	// RunID identifies the run in the history.
	RunID     string              `json:"runId"`
	StartedAt time.Time           `json:"startedAt"`
	EndedAt   time.Time           `json:"endedAt"`
	Results   []taskresult.Result `json:"results"`
	// Counts by outcome.
	Applied     int `json:"applied"`
	Unchanged   int `json:"unchanged"`
	Skipped     int `json:"skipped"`
	Unavailable int `json:"unavailable"`
	Failed      int `json:"failed"`
	// Interrupted is true when the run stopped early because its context ended (the page went away).
	Interrupted bool `json:"interrupted"`
}

// Maintenance is the controller of the 维护 page.
type Maintenance struct {
	svc      *maintenance.Service
	history  history.Store
	log      *slog.Logger
	activity *schedule.Activity
}

// NewMaintenance returns the service over svc, running inside activity's gate (background work waits while a task runs) and recording every run in store.
func NewMaintenance(svc *maintenance.Service, store history.Store, log *slog.Logger, activity *schedule.Activity) *Maintenance {
	return &Maintenance{svc: svc, history: store, log: log, activity: activity}
}

// Catalog returns the maintenance tasks of this platform in display order.
func (m *Maintenance) Catalog() []TaskView {
	tasks := m.svc.Catalog()
	out := make([]TaskView, len(tasks))
	for i, t := range tasks {
		out[i] = TaskView{
			ID:               t.ID,
			Title:            t.Title,
			Description:      t.Description,
			Hint:             t.Hint,
			Icon:             t.Icon,
			DefaultOn:        t.DefaultOn,
			NeedsAdmin:       t.NeedsAdmin,
			EstimatedSeconds: t.EstimatedSeconds,
		}
	}
	return out
}

// Run runs the tasks named by ids, reports progress (nil reports nothing), records the run in the history and returns its summary. It fails without running anything when ids is empty, names an unknown task, or another run is in progress; the error's text is a sentence in Simplified Chinese for the page to show, and the engine's own error goes to the log. Admin tasks run together behind one system prompt. Cancelling ctx stops the run after the current task; the summary then says it was interrupted.
func (m *Maintenance) Run(ctx context.Context, ids []string, progress func(ProgressEvent)) (RunSummary, error) {
	run, end, err := m.activity.BeginClean(ctx)
	if err != nil {
		return RunSummary{}, gateError(ctx, m.log, err)
	}
	defer end()
	ctx = run
	started := time.Now()
	results, err := m.svc.Run(ctx, ids, func(ev maintenance.Event) {
		if progress != nil {
			progress(ProgressEvent{Kind: ProgressKind(ev.Kind), TaskID: ev.TaskID, Result: ev.Result, Done: ev.Done, Total: ev.Total})
		}
	})
	interrupted := err != nil && ctx.Err() != nil && errors.Is(err, ctx.Err())
	if err != nil && !interrupted {
		return RunSummary{}, m.userError(err)
	}
	summary := summarize(newRunID(started), started, time.Now(), results)
	summary.Interrupted = interrupted
	if err := m.history.Append(history.Run{ID: summary.RunID, Kind: history.KindMaintenance, StartedAt: summary.StartedAt, EndedAt: summary.EndedAt, Results: summary.Results}); err != nil {
		// The tasks already ran; failing the call would tell the user they did not.
		m.log.Error("record maintenance run in history", "run", summary.RunID, "err", err)
	}
	return summary, nil
}

// userError turns an error of maintenance.Service.Run into the sentence the done card shows.
func (m *Maintenance) userError(err error) error {
	m.log.Warn("maintenance run rejected", "err", err)
	switch {
	case errors.Is(err, maintenance.ErrBusy):
		return errors.New("已有维护正在进行，请等它结束后再试")
	case errors.Is(err, maintenance.ErrUnknownTask):
		return errors.New("维护项目已更新，请重新打开维护页面后再试")
	case errors.Is(err, maintenance.ErrNoTasks):
		return errors.New("没有选择任何维护项目")
	}
	return errors.New("维护没能开始，详情已记录到日志")
}

func summarize(id string, start, end time.Time, results []taskresult.Result) RunSummary {
	s := RunSummary{RunID: id, StartedAt: start, EndedAt: end, Results: results}
	for _, r := range results {
		switch r.Outcome {
		case taskresult.Applied:
			s.Applied++
		case taskresult.Unchanged:
			s.Unchanged++
		case taskresult.Skipped:
			s.Skipped++
		case taskresult.Unavailable:
			s.Unavailable++
		case taskresult.Failed:
			s.Failed++
		}
	}
	return s
}

// newRunID returns a time-ordered unique ID such as "20261114T010412Z-3f9a0c1d".
func newRunID(t time.Time) string {
	var b [4]byte
	_, _ = rand.Read(b[:])
	return t.UTC().Format("20060102T150405Z") + "-" + hex.EncodeToString(b[:])
}
