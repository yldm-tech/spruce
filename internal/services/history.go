package services

import (
	"fmt"

	"github.com/yldm-tech/spruce/internal/engine/history"
)

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
