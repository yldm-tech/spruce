package schedule

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"sync"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/fsx"
)

// StateVersion is the version of background.json this build writes; a file of another version reads as the zero State.
const StateVersion = 1

// State is what the scheduler remembers between launches, in <data>/background.json.
type State struct {
	Version int `json:"version"`
	// RuleRuns is the end of each automatic rule's last run, by rules.ID.
	RuleRuns map[string]time.Time `json:"ruleRuns"`
	// LastSummary is when the last weekly summary was sent.
	LastSummary time.Time `json:"lastSummary"`
	LowDisk     LowDisk   `json:"lowDisk"`
}

// LowDisk is where the low-disk alert stands.
type LowDisk struct {
	// LastAt and LastFree are when the last alert went out and the free space it reported.
	LastAt   time.Time `json:"lastAt"`
	LastFree int64     `json:"lastFree"`
	// Armed is set once free space has risen well above the threshold again: the next drop below it alerts.
	Armed bool `json:"armed"`
}

// StateStore keeps State in one file. Safe for concurrent use.
type StateStore struct {
	path string
	mu   sync.Mutex
}

// OpenState returns the store of the file at path (<data>/background.json). It does no I/O.
func OpenState(path string) *StateStore {
	return &StateStore{path: path}
}

// Load reads the state. A missing file, a corrupt one or one of another version reads as the zero State (with an empty RuleRuns) and no error, and is replaced on the next Save; only a file that cannot be read is an error.
func (s *StateStore) Load() (State, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	empty := State{Version: StateVersion, RuleRuns: map[string]time.Time{}}
	b, err := os.ReadFile(s.path)
	if errors.Is(err, fs.ErrNotExist) {
		return empty, nil
	}
	if err != nil {
		return empty, fmt.Errorf("schedule: read state: %w", err)
	}
	var st State
	if json.Unmarshal(b, &st) != nil || st.Version != StateVersion {
		return empty, nil
	}
	if st.RuleRuns == nil {
		st.RuleRuns = map[string]time.Time{}
	}
	return st, nil
}

// Save writes st atomically (a temporary file in the same folder, fsync, rename), readable by the user only.
func (s *StateStore) Save(st State) error {
	st.Version = StateVersion
	b, err := json.Marshal(st)
	if err != nil {
		return fmt.Errorf("schedule: encode state: %w", err)
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if err := fsx.WriteFileAtomic(s.path, b); err != nil {
		return fmt.Errorf("schedule: save state: %w", err)
	}
	return nil
}

// Low-disk alert spacing.
const (
	// lowDiskDrop is how much further free space must fall after an alert for another one.
	lowDiskDrop = 5 << 30
	// lowDiskRearm is how far above the threshold free space must rise to re-arm the alert.
	lowDiskRearm = 2 << 30
	// lowDiskGap is the least time between two alerts.
	lowDiskGap = 24 * time.Hour
)

// LowDiskAlert decides whether free space below threshold should notify: once per crossing, again only after a further 5 GB drop, and never more than once a day; the alert is re-armed when free space rises 2 GB above the threshold. A state that has never alerted counts as armed. An alert that lies in the future (the clock was set back) does not hold the next one back. A threshold of zero or less never notifies. Pure.
func LowDiskAlert(st LowDisk, free, threshold int64, now time.Time) (notify bool, next LowDisk) {
	next = st
	if threshold <= 0 {
		return false, next
	}
	if free >= threshold {
		if free >= threshold+lowDiskRearm {
			next.Armed = true
		}
		return false, next
	}
	never := st.LastAt.IsZero()
	crossing := st.Armed || never
	dropped := !never && st.LastFree-free >= lowDiskDrop
	spaced := never || now.Sub(st.LastAt) >= lowDiskGap || now.Before(st.LastAt)
	if (crossing || dropped) && spaced {
		return true, LowDisk{LastAt: now, LastFree: free}
	}
	return false, next
}
