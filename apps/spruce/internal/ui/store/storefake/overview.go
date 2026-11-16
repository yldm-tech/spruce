package storefake

import (
	"context"
	"slices"
	"sync"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/growth"
)

// CleanCall is one call of Overview.Clean.
type CleanCall struct {
	Req    cleanup.CleanRequest
	Origin app.CleanOrigin
}

// Overview is a programmable store.OverviewService: its state is what State answers until Set broadcasts another; Clean and Undo answer through OnClean and OnUndo (or the zero outcome), and wait for CleanRelease and UndoRelease when they are set. It records every call. Put it in Services.Overview before Provide.
type Overview struct {
	mu sync.Mutex

	state    app.OverviewState
	stateCh  chan app.OverviewState
	noticeCh chan app.Notice

	// CheckErr is what CheckNow answers.
	CheckErr error
	// OnClean and OnUndo, when set, answer Clean and Undo.
	OnClean func(ctx context.Context, req cleanup.CleanRequest, origin app.CleanOrigin, progress func(cleanup.Progress)) (app.OverviewOutcome, error)
	OnUndo  func(ctx context.Context, runID string) (app.RestoreOutcome, error)
	// CleanRelease and UndoRelease, when set, hold each Clean and Undo until they receive or the context ends.
	CleanRelease, UndoRelease chan struct{}
	// GrowthList and GrowthErr are what Growth answers; TrashInfoValue and TrashErr what TrashInfo answers.
	GrowthList     []growth.Growth
	GrowthErr      error
	TrashInfoValue app.TrashInfo
	TrashErr       error

	checks, stops, disks int
	cleans               []CleanCall
	undos                []string
	windows              []time.Duration
}

// NewOverview returns a fake whose state is st. The fake is a controller that has loaded: every state it answers or broadcasts has Loaded set.
func NewOverview(st app.OverviewState) *Overview {
	st.Loaded = true
	return &Overview{state: st, stateCh: make(chan app.OverviewState, 1), noticeCh: make(chan app.Notice, 8)}
}

// Set broadcasts st as the controller's new state.
func (o *Overview) Set(st app.OverviewState) {
	st.Loaded = true
	o.mu.Lock()
	o.state = st
	o.mu.Unlock()
	select {
	case <-o.stateCh:
	default:
	}
	o.stateCh <- st
}

// Notify sends n as the controller's notification.
func (o *Overview) Notify(n app.Notice) { o.noticeCh <- n }

// Calls returns how often CheckNow and StopCheck were called, and what Clean, Undo and Growth received, oldest first.
func (o *Overview) Calls() (checks, stops int, cleans []CleanCall, undos []string, windows []time.Duration) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.checks, o.stops, slices.Clone(o.cleans), slices.Clone(o.undos), slices.Clone(o.windows)
}

// State implements store.OverviewService.
func (o *Overview) State() app.OverviewState {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.state
}

// Subscribe implements store.OverviewService.
func (o *Overview) Subscribe() (<-chan app.OverviewState, func()) { return o.stateCh, func() {} }

// Notices implements store.OverviewService.
func (o *Overview) Notices() (<-chan app.Notice, func()) { return o.noticeCh, func() {} }

// CheckNow implements store.OverviewService.
func (o *Overview) CheckNow() error {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.checks++
	return o.CheckErr
}

// StopCheck implements store.OverviewService.
func (o *Overview) StopCheck() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.stops++
}

// ReadDisk implements store.OverviewService.
func (o *Overview) ReadDisk() {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.disks++
}

// DiskReads returns how often ReadDisk was called.
func (o *Overview) DiskReads() int {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.disks
}

// Clean implements store.OverviewService.
func (o *Overview) Clean(ctx context.Context, req cleanup.CleanRequest, origin app.CleanOrigin, progress func(cleanup.Progress)) (app.OverviewOutcome, error) {
	o.mu.Lock()
	o.cleans = append(o.cleans, CleanCall{Req: req, Origin: origin})
	release, fn := o.CleanRelease, o.OnClean
	o.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	if fn != nil {
		return fn(ctx, req, origin, progress)
	}
	return app.OverviewOutcome{}, nil
}

// Undo implements store.OverviewService.
func (o *Overview) Undo(ctx context.Context, runID string) (app.RestoreOutcome, error) {
	o.mu.Lock()
	o.undos = append(o.undos, runID)
	release, fn := o.UndoRelease, o.OnUndo
	o.mu.Unlock()
	if release != nil {
		select {
		case <-release:
		case <-ctx.Done():
		}
	}
	if fn != nil {
		return fn(ctx, runID)
	}
	return app.RestoreOutcome{}, nil
}

// Growth implements store.OverviewService.
func (o *Overview) Growth(_ context.Context, window time.Duration) ([]growth.Growth, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	o.windows = append(o.windows, window)
	return slices.Clone(o.GrowthList), o.GrowthErr
}

// TrashInfo implements store.OverviewService.
func (o *Overview) TrashInfo(context.Context) (app.TrashInfo, error) {
	o.mu.Lock()
	defer o.mu.Unlock()
	return o.TrashInfoValue, o.TrashErr
}
