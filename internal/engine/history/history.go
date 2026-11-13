package history

import (
	"time"

	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// Kind is what kind of operation a Run was.
type Kind string

const (
	// KindMaintenance is a run of the 维护 page.
	KindMaintenance Kind = "maintenance"
)

// MaxRuns is how many runs the store keeps; older ones are dropped.
const MaxRuns = 500

// Run is one recorded operation.
type Run struct {
	// ID is unique per run; the services layer generates it (time-ordered, e.g. "20261114T010412Z-3f9a").
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	// Results has one entry per task that was selected, in catalog order.
	Results []taskresult.Result `json:"results"`
}

// Store keeps runs.
type Store interface {
	// Append records run.
	Append(run Run) error
	// List returns up to limit runs, newest first; limit <= 0 means all.
	List(limit int) ([]Run, error)
}
