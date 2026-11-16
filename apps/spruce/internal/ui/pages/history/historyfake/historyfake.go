// Package historyfake is a store.HistoryService for tests that can fail its loads and its clear, says which runs 放回原处 can put back, and counts its calls. Only tests import it.
package historyfake

import (
	"context"
	"slices"
	"sync"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/history"
)

// Service answers from Runs, newest first.
type Service struct {
	mu   sync.Mutex
	runs []history.Run
	// listErrs are the errors the next List calls return, one each.
	listErrs []error
	clearErr error
	limits   []int
	clears   int
	ch       chan history.Run
	// restorable are the IDs of the runs 放回原处 can put back.
	restorable map[string]bool
}

// New returns a service holding runs.
func New(runs ...history.Run) *Service {
	return &Service{runs: runs, ch: make(chan history.Run, 16)}
}

// List returns up to limit runs, or the next queued error.
func (s *Service) List(limit int) ([]history.Run, error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.limits = append(s.limits, limit)
	if len(s.listErrs) > 0 {
		err := s.listErrs[0]
		s.listErrs = s.listErrs[1:]
		return nil, err
	}
	runs := s.runs
	if len(runs) > limit {
		runs = runs[:limit]
	}
	return append([]history.Run(nil), runs...), nil
}

// Clear drops every run, unless FailClear set an error.
func (s *Service) Clear() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clears++
	if s.clearErr != nil {
		return s.clearErr
	}
	s.runs = nil
	return nil
}

// Subscribe announces the runs Record adds.
func (s *Service) Subscribe() (<-chan history.Run, func(), bool) { return s.ch, func() {}, true }

// Record adds run at the top and announces it, as the history does when a page records a run.
func (s *Service) Record(run history.Run) {
	s.mu.Lock()
	s.runs = append([]history.Run{run}, s.runs...)
	s.mu.Unlock()
	s.ch <- run
}

// Restorable answers the runs among runs that SetRestorable named.
func (s *Service) Restorable(runs []history.Run, _ time.Time) map[string]bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := map[string]bool{}
	for _, r := range runs {
		if s.restorable[r.ID] {
			out[r.ID] = true
		}
	}
	return out
}

// SetRestorable makes the runs ids the ones 放回原处 can put back, from now on.
func (s *Service) SetRestorable(ids ...string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.restorable = map[string]bool{}
	for _, id := range ids {
		s.restorable[id] = true
	}
}

// FailList makes the next List calls return errs, one each.
func (s *Service) FailList(errs ...error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.listErrs = append(s.listErrs, errs...)
}

// FailClear makes Clear return err from now on (nil to succeed again).
func (s *Service) FailClear(err error) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.clearErr = err
}

// Limits returns the limit of every List call so far.
func (s *Service) Limits() []int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]int(nil), s.limits...)
}

// Clears counts the Clear calls.
func (s *Service) Clears() int {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.clears
}

// Catalog is a maintenance catalog that names a maintenance run's tasks: history.Catalog.
type Catalog []app.TaskView

// Catalog returns the tasks.
func (c Catalog) Catalog() []app.TaskView { return c }

// Files records the paths Reveal receives and fails with RevealErr.
type Files struct {
	mu       sync.Mutex
	revealed []string
	// RevealErr, when set, is what Reveal fails with.
	RevealErr error
}

// Reveal implements history.Files.
func (f *Files) Reveal(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.revealed = append(f.revealed, path)
	return f.RevealErr
}

// Revealed returns the paths Reveal received, oldest first.
func (f *Files) Revealed() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.revealed)
}
