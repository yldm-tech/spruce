// Package uninstallfake is the 应用卸载 page's Service and Files for tests: answers from fields or from functions the test sets, and a record of the calls. Only tests import it.
package uninstallfake

import (
	"context"
	"errors"
	"sync"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// Service answers the page's calls. A function field, when set, answers in place of the plain fields; every call is recorded.
type Service struct {
	mu sync.Mutex

	List    []app.UninstallApp
	ListErr error
	// Details are streamed by Measure, in order.
	Details []app.UninstallAppDetails
	Plan    app.UninstallPlan
	PlanErr error

	OnApps           func(ctx context.Context) ([]app.UninstallApp, error)
	OnLeftovers      func(ctx context.Context, appID string) (app.UninstallPlan, error)
	OnRemove         func(ctx context.Context, req app.UninstallRequest, progress func(cleanup.Progress)) (app.UninstallOutcome, error)
	OnRunUninstaller func(ctx context.Context, appID string, status func(app.UninstallerStatus)) (app.UninstallerOutcome, error)
	OnCheckRemoved   func(ctx context.Context, appID string) (app.UninstallerOutcome, error)

	apps, leftovers, uninstallers, checks int
	removes                               []app.UninstallRequest
}

// Apps answers OnApps, else List and ListErr.
func (s *Service) Apps(ctx context.Context) ([]app.UninstallApp, error) {
	s.mu.Lock()
	s.apps++
	on, list, err := s.OnApps, s.List, s.ListErr
	s.mu.Unlock()
	if on != nil {
		return on(ctx)
	}
	return append([]app.UninstallApp(nil), list...), err
}

// Measure sends Details.
func (s *Service) Measure(ctx context.Context, details func(app.UninstallAppDetails)) error {
	s.mu.Lock()
	all := append([]app.UninstallAppDetails(nil), s.Details...)
	s.mu.Unlock()
	for _, d := range all {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		details(d)
	}
	return nil
}

// Leftovers answers OnLeftovers, else Plan and PlanErr.
func (s *Service) Leftovers(ctx context.Context, appID string) (app.UninstallPlan, error) {
	s.mu.Lock()
	s.leftovers++
	on, plan, err := s.OnLeftovers, s.Plan, s.PlanErr
	s.mu.Unlock()
	if on != nil {
		return on(ctx, appID)
	}
	return plan, err
}

// Remove answers OnRemove, else an outcome with nothing done.
func (s *Service) Remove(ctx context.Context, req app.UninstallRequest, progress func(cleanup.Progress)) (app.UninstallOutcome, error) {
	s.mu.Lock()
	s.removes = append(s.removes, req)
	on := s.OnRemove
	s.mu.Unlock()
	if on != nil {
		return on(ctx, req, progress)
	}
	return app.UninstallOutcome{Result: Result(cleanup.CleanResult{})}, nil
}

// RunUninstaller answers OnRunUninstaller, else an error: a test that runs one says how it goes.
func (s *Service) RunUninstaller(ctx context.Context, appID string, status func(app.UninstallerStatus)) (app.UninstallerOutcome, error) {
	s.mu.Lock()
	s.uninstallers++
	on := s.OnRunUninstaller
	s.mu.Unlock()
	if on != nil {
		return on(ctx, appID, status)
	}
	return app.UninstallerOutcome{}, errors.New("没能运行卸载程序，详情已记录到日志")
}

// CheckRemoved answers OnCheckRemoved, else the program is still there.
func (s *Service) CheckRemoved(ctx context.Context, appID string) (app.UninstallerOutcome, error) {
	s.mu.Lock()
	s.checks++
	on := s.OnCheckRemoved
	s.mu.Unlock()
	if on != nil {
		return on(ctx, appID)
	}
	return app.UninstallerOutcome{Waited: true}, nil
}

// SetPlan changes what Leftovers answers from now on.
func (s *Service) SetPlan(p app.UninstallPlan) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.Plan = p
}

// Calls returns how often Apps, Leftovers, RunUninstaller and CheckRemoved were called.
func (s *Service) Calls() (apps, leftovers, uninstallers, checks int) {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.apps, s.leftovers, s.uninstallers, s.checks
}

// Removes returns the requests Remove received.
func (s *Service) Removes() []app.UninstallRequest {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]app.UninstallRequest(nil), s.removes...)
}

// Files records the paths it was asked to reveal and answers Err.
type Files struct {
	mu    sync.Mutex
	Err   error
	paths []string
}

// Reveal records path.
func (f *Files) Reveal(_ context.Context, path string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.paths = append(f.paths, path)
	return f.Err
}

// Paths returns the revealed paths.
func (f *Files) Paths() []string {
	f.mu.Lock()
	defer f.mu.Unlock()
	return append([]string(nil), f.paths...)
}

// Result fills what app.Uninstall always sets on a clean result: non-nil lists, a start and a duration.
func Result(r cleanup.CleanResult) cleanup.CleanResult {
	if r.Failed == nil {
		r.Failed = []cleanup.Failure{}
	}
	if r.Categories == nil {
		r.Categories = []cleanup.CategoryOutcome{}
	}
	r.StartedAt, r.Duration = time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC), time.Second
	return r
}

// Leftover is a leftover of kind under ~/Library.
func Leftover(id string, kind app.UninstallLeftoverKind, size int64, selected bool) app.UninstallLeftover {
	return app.UninstallLeftover{ID: id, Path: "/Users/me/Library/" + string(kind) + "/" + id, Kind: kind, Size: size, Dir: true, Selected: selected}
}

// Size returns a pointer to n, for UninstallAppDetails.Size.
func Size(n int64) *int64 { return &n }
