package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"slices"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/schedule"
	"spruce/apps/spruce/internal/engine/trash"
	"spruce/apps/spruce/internal/engine/undo"
)

// SetLedger makes c journal every clean's moves to the Trash and deletions for good, so a clean Spruce does not finish still gets its history run (RecoverInterrupted), and record the moves in l, so they can be put back (macOS; on Windows the ledger keeps none, undo.Keepable); nil records nothing. It is a function rather than a method so the page cannot call it.
func SetLedger(c *Cleanup, l *undo.Ledger) { c.ledger = l }

// SetSpaceLedger makes s record its moves to the Trash in l, with each entry's parent folder as the root it may come back into.
func SetSpaceLedger(s *Space, l *undo.Ledger) { s.ledger = l }

// SetUninstallLedger makes u record what a removal moved to the Trash in l, with the category roots of the plan.
func SetUninstallLedger(u *Uninstall, l *undo.Ledger) { u.ledger = l }

// undoRun journals one clean's moves as they reach the Trash and records them in the undo ledger when it ends.
type undoRun struct {
	ledger  *undo.Ledger
	journal *undo.Journal
	runID   string
	log     *slog.Logger
}

// beginUndo starts the journal of meta.RunID when l is set, and returns ctx with the journal ahead of the OnMoved and OnRemoved callbacks ctx carries. It journals on every platform, so RecoverInterrupted can count what a clean Spruce did not finish moved and deleted; where items cannot be put back (Windows) the ledger keeps none of the moves (undo.Keepable), so nothing is offered for 撤销. A journal that cannot start or be written is logged; the clean goes on.
func beginUndo(ctx context.Context, l *undo.Ledger, log *slog.Logger, meta undo.JournalMeta) (context.Context, *undoRun) {
	u := &undoRun{runID: meta.RunID, log: log}
	if l == nil {
		return ctx, u
	}
	u.ledger = l
	j, err := l.Begin(meta)
	if err != nil {
		log.Warn("start undo journal", "run", meta.RunID, "err", err)
		return ctx, u
	}
	u.journal = j
	next, nextRemoved := cleanup.OnMoved(ctx), cleanup.OnRemoved(ctx)
	ctx = cleanup.WithOnRemoved(ctx, func(r cleanup.Removed) {
		if err := j.AddRemoved(r); err != nil {
			log.Warn("write undo journal", "run", meta.RunID, "path", r.Path, "err", err)
		}
		nextRemoved(r)
	})
	return cleanup.WithOnMoved(ctx, func(m cleanup.Moved) {
		if err := j.Add(m); err != nil {
			log.Warn("write undo journal", "run", meta.RunID, "path", m.Path, "err", err)
		}
		next(m)
	}), u
}

// finish records moves as the run's ledger entry, made at, removes its journal and returns what the run can put back (nil for nothing). It does not use a context: it runs after the clean even when the clean was stopped.
func (u *undoRun) finish(at time.Time, moves []cleanup.Moved) *history.UndoInfo {
	if u.ledger == nil {
		return nil
	}
	if u.journal != nil {
		if err := u.journal.Close(); err != nil {
			u.log.Warn("close undo journal", "run", u.runID, "err", err)
		}
	}
	kept, err := u.ledger.Record(u.runID, at, moves)
	if err != nil {
		u.log.Error("record undo ledger", "run", u.runID, "err", err)
		return nil
	}
	if kept == 0 {
		return nil
	}
	_, bytes := undo.Kept(moves)
	return &history.UndoInfo{Items: kept, Bytes: bytes}
}

// undoLine is the history line of a clean that can be put back but also deleted items for good: k of them cannot come back.
func undoLine(info *history.UndoInfo, removed, moved int) string {
	if info == nil || removed-moved <= 0 {
		return ""
	}
	return fmt.Sprintf("这次清理里直接删除的 %d 项无法放回", removed-moved)
}

// Restore puts back what a run moved to the Trash: inside the activity gate, through the undo ledger, and recorded as a KindRestore run that names it. 概览's Undo uses it, the history page through that, and the command line.
type Restore struct {
	ledger   *undo.Ledger
	restorer trash.Restorer
	history  history.Store
	activity *schedule.Activity
	home     string
	log      *slog.Logger
	// canRestore is trash.CanRestore(); tests replace it.
	canRestore bool
}

// NewRestore returns the service over l, putting items back with r (trash.SystemRestorer() in the app) inside a's gate and recording each 放回原处 in h; home shortens paths in the history.
func NewRestore(l *undo.Ledger, r trash.Restorer, h history.Store, a *schedule.Activity, home string, log *slog.Logger) *Restore {
	return &Restore{ledger: l, restorer: r, history: h, activity: a, home: home, log: log, canRestore: trash.CanRestore()}
}

// Run puts back every unrestored item of the run runID, one at a time, records a KindRestore run with what could not come back and why, and returns what it did. Nothing is overwritten and nothing is retried. Cancelling ctx stops after the current item. Errors are sentences in Simplified Chinese.
func (r *Restore) Run(ctx context.Context, runID string) (RestoreOutcome, error) {
	if r.ledger == nil || !r.canRestore {
		return RestoreOutcome{}, errors.New("这台电脑上不能放回原处")
	}
	clean, end, err := r.activity.BeginClean(ctx)
	if err != nil {
		return RestoreOutcome{}, gateError(ctx, r.log, err)
	}
	defer end()
	started := time.Now()
	outs, err := r.ledger.Restore(clean, runID, r.restorer)
	switch {
	case errors.Is(err, undo.ErrNoEntry), errors.Is(err, undo.ErrBadRunID):
		return RestoreOutcome{}, errors.New("这条记录已经不能放回原处了")
	case errors.Is(err, undo.ErrRestoring):
		return RestoreOutcome{}, errors.New("正在放回这条记录里的项目，请稍候")
	case errors.Is(err, undo.ErrNotSaved):
		r.log.Error("record what was put back", "run", runID, "err", err)
	}
	// Items the restore did not get to (it stopped with ctx) stay offered by the ledger; they count as left in the Trash, so the run does not read as wholly put back.
	untried := 0
	if clean.Err() != nil {
		untried, _, _ = r.ledger.Restorable(runID, time.Now())
	}
	interrupted := untried > 0
	out := RestoreOutcome{}
	tried := 0
	for _, o := range outs {
		if errors.Is(o.Err, context.Canceled) || errors.Is(o.Err, context.DeadlineExceeded) {
			continue
		}
		tried++
		if o.Err == nil {
			out.Restored++
			out.Bytes += o.Move.Size
			continue
		}
		out.Failed++
		r.log.Info("could not put back", "run", runID, "path", o.Move.Path, "err", o.Err)
		out.Reasons = append(out.Reasons, shortPath(r.home, o.Move.Path)+"："+o.Reason)
	}
	if tried == 0 {
		if interrupted {
			return RestoreOutcome{}, errors.New("已取消")
		}
		return RestoreOutcome{}, errors.New("这条记录里的项目都已放回原处")
	}
	lines := slices.Clone(out.Reasons)
	if len(lines) > maxDetailFailures {
		lines = append(lines[:maxDetailFailures], fmt.Sprintf("另有 %d 项没能放回", out.Failed-maxDetailFailures))
	}
	if interrupted {
		lines = append(lines, "放回在中途停止，其余项目还在"+binName()+"里")
	}
	run := history.Run{
		ID:        newRunID(started),
		Kind:      history.KindRestore,
		StartedAt: started,
		EndedAt:   time.Now(),
		// Items and Trashed of a KindRestore run are what came back out of the Trash; Failed what stayed there, tried or not.
		Summary:   &history.Totals{Items: out.Restored, Trashed: out.Bytes, Failed: out.Failed + untried},
		Details:   lines,
		RestoreOf: runID,
	}
	if err := r.history.Append(run); err != nil {
		// The items are back; failing the call would tell the user they were not.
		r.log.Error("record restore in history", "run", runID, "err", err)
	} else {
		out.RunID = run.ID
	}
	return out, nil
}

// RecoverInterrupted turns the journals of cleans Spruce did not finish (it quit or ended mid-clean) into ledger entries and adds the history run each one is missing. It runs once as the app opens (the first of shell.Deps.Background), inside the activity gate so it never takes the journal of a clean another Spruce process (the command line) is running; when that process holds the lock it leaves them for the next launch.
func RecoverInterrupted(ctx context.Context, l *undo.Ledger, h history.Store, a *schedule.Activity, log *slog.Logger) {
	if l == nil {
		return
	}
	_, end, err := a.BeginClean(ctx)
	if err != nil {
		if ctx.Err() == nil {
			log.Info("left interrupted cleans for later", "err", err)
		}
		return
	}
	defer end()
	recs, err := l.Recover()
	if err != nil {
		log.Warn("recover interrupted cleans", "err", err)
	}
	for _, rec := range recs {
		if len(rec.Moves) == 0 && len(rec.Removed) == 0 {
			continue
		}
		var trashed, freed int64
		for _, m := range rec.Moves {
			trashed += m.Size
		}
		for _, r := range rec.Removed {
			freed += r.Freed
		}
		var details []string
		if len(rec.Moves) > 0 {
			details = append(details, fmt.Sprintf("已移到%s %d 项", binName(), len(rec.Moves)))
		}
		if len(rec.Removed) > 0 {
			details = append(details, fmt.Sprintf("已删除 %d 项", len(rec.Removed)))
		}
		details = append(details, "清理在中途停止，其余项目没有改动", "Spruce 在清理时退出，这条记录是下次打开时补上的")
		run := history.Run{
			ID:        rec.Meta.RunID,
			Kind:      cmp.Or(rec.Meta.Kind, history.KindSmartScan),
			StartedAt: rec.Meta.Started,
			EndedAt:   rec.Ended,
			Summary:   &history.Totals{Freed: freed, Trashed: trashed, Items: len(rec.Moves) + len(rec.Removed)},
			Details:   details,
			Rule:      rec.Meta.Rule,
		}
		if rec.Kept > 0 {
			_, bytes := undo.Kept(rec.Moves)
			run.Undo = &history.UndoInfo{Items: rec.Kept, Bytes: bytes}
		}
		if err := h.Append(run); err != nil {
			log.Error("record interrupted clean in history", "run", run.ID, "err", err)
			continue
		}
		log.Info("recorded an interrupted clean", "run", run.ID, "items", run.Summary.Items)
	}
}
