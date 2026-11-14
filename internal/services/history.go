package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/egoist/mygo"
	"github.com/yldm-tech/spruce/internal/engine/history"
)

// HistoryAppended carries each run the history records, from any page or from Go, to every window, so 操作历史 shows it without being reopened.
var HistoryAppended = mygo.NewEvent[history.Run]("history:appended")

// History is the bound service of the 操作历史 page.
type History struct {
	store history.Store
}

// NewHistory returns the service over store.
func NewHistory(store history.Store) *History {
	return &History{store: store}
}

// List returns up to limit recorded runs, newest first; limit must be between 1 and history.MaxRuns.
func (h *History) List(limit int) ([]history.Run, error) {
	if limit < 1 || limit > history.MaxRuns {
		return nil, fmt.Errorf("limit must be between 1 and %d, got %d", history.MaxRuns, limit)
	}
	return h.store.List(limit)
}

// Clear deletes every recorded run. It cannot be undone; the page confirms first.
func (h *History) Clear() error {
	if err := h.store.Clear(); err != nil {
		return errors.New("无法清空操作历史")
	}
	return nil
}

// BroadcastHistory forwards every run store records to all windows as HistoryAppended until ctx is done. main.go starts it once the app is ready. The store must be a history.Watcher (the one history.Open returns is); any other store is logged and has nothing to forward.
func BroadcastHistory(ctx context.Context, store history.Store, log *slog.Logger) {
	w, ok := store.(history.Watcher)
	if !ok {
		log.Warn("history store does not announce its runs; 操作历史 refreshes only when it is opened", "store", fmt.Sprintf("%T", store))
		return
	}
	ch, unsubscribe := w.Subscribe()
	defer unsubscribe()
	// Broadcast fails only when the payload cannot be encoded as JSON, which never happens for a history.Run.
	forwardRuns(ctx, ch, func(r history.Run) { _ = HistoryAppended.Broadcast(r) })
}

// forwardRuns hands every run from ch to send until ctx is done or ch is closed.
func forwardRuns(ctx context.Context, ch <-chan history.Run, send func(history.Run)) {
	for {
		select {
		case <-ctx.Done():
			return
		case r, ok := <-ch:
			if !ok {
				return
			}
			send(r)
		}
	}
}
