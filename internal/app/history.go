package app

import (
	"errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/trash"
	"github.com/yldm-tech/spruce/internal/engine/undo"
)

// History is the controller of the 操作历史 page.
type History struct {
	store  history.Store
	ledger *undo.Ledger
	log    *slog.Logger
	// canRestore is trash.CanRestore(); tests replace it.
	canRestore bool

	mu sync.Mutex
	// onClear are what OnClear registered, called by Clear after the runs and the ledger.
	onClear []func() error
}

// NewHistory returns the service over store; ledger (nil for none) says which runs can still be put back and is cleared with the history.
func NewHistory(store history.Store, ledger *undo.Ledger, log *slog.Logger) *History {
	return &History{store: store, ledger: ledger, log: log, canRestore: trash.CanRestore()}
}

// OnClear registers fn to run when the history is cleared, for the data that goes with it (the cached check result, the space snapshots), so this package needs none of their types. It is a function rather than a method so the page cannot call it.
func OnClear(h *History, fn func() error) {
	h.mu.Lock()
	defer h.mu.Unlock()
	h.onClear = append(h.onClear, fn)
}

// List returns up to limit recorded runs, newest first; limit must be between 1 and history.MaxRuns.
func (h *History) List(limit int) ([]history.Run, error) {
	if limit < 1 || limit > history.MaxRuns {
		return nil, fmt.Errorf("limit must be between 1 and %d, got %d", history.MaxRuns, limit)
	}
	return h.store.List(limit)
}

// Restorable returns the IDs of the runs among runs that 放回原处 can put back at now: on macOS, a run that recorded Run.Undo, within undo.Window, not wholly put back by a later KindRestore run, whose ledger entry still offers a move (one neither put back nor tried by a 放回原处 that ran to its end, so the button goes once a restore covered every item, even when some could not come back).
func (h *History) Restorable(runs []history.Run, now time.Time) map[string]bool {
	out := map[string]bool{}
	if !h.canRestore || h.ledger == nil {
		return out
	}
	// A KindRestore with nothing left in the Trash put everything back; one that stopped part-way counts what it did not try as Failed.
	done := map[string]bool{}
	for _, r := range runs {
		if r.Kind == history.KindRestore && r.RestoreOf != "" && r.Summary != nil && r.Summary.Failed == 0 {
			done[r.RestoreOf] = true
		}
	}
	for _, r := range runs {
		if r.Undo == nil || done[r.ID] || now.Sub(r.EndedAt) > undo.Window {
			continue
		}
		if _, _, ok := h.ledger.Restorable(r.ID, now); ok {
			out[r.ID] = true
		}
	}
	return out
}

// Clear deletes every recorded run, the undo ledger (keeping restored.json) and whatever OnClear registered. It cannot be undone; the page confirms first.
func (h *History) Clear() error {
	if err := h.store.Clear(); err != nil {
		return errors.New("无法清空操作历史")
	}
	var errs []error
	if h.ledger != nil {
		if err := h.ledger.Clear(); err != nil {
			errs = append(errs, fmt.Errorf("clear undo ledger: %w", err))
		}
	}
	h.mu.Lock()
	fns := append([]func() error(nil), h.onClear...)
	h.mu.Unlock()
	for _, fn := range fns {
		if err := fn(); err != nil {
			errs = append(errs, err)
		}
	}
	if err := errors.Join(errs...); err != nil {
		h.log.Error("clear what goes with the history", "err", err)
		return errors.New("操作历史已清空，但有些相关记录没能删除，详情已记录到日志")
	}
	return nil
}

// Subscribe returns a subscription to every run the store records from now on, and the function that ends it. ok is false when the store does not announce its runs (only a history.Watcher does, as the one history.Open returns).
func (h *History) Subscribe() (runs <-chan history.Run, unsubscribe func(), ok bool) {
	w, ok := h.store.(history.Watcher)
	if !ok {
		return nil, nil, false
	}
	runs, unsubscribe = w.Subscribe()
	return runs, unsubscribe, true
}
