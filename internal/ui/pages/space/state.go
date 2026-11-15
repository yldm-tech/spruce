package space

import (
	"context"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/async"
	"github.com/yldm-tech/spruce/internal/ui/format"
)

// Phase is what the page shows.
type Phase uint8

// The phases: waiting for the last scan, the start screen, a scan running, and the results.
const (
	PhaseLoading Phase = iota
	PhaseStart
	PhaseScanning
	PhaseResults
)

// ScanRequest is where a scan goes: its path and what the page calls it while it runs.
type ScanRequest struct {
	Path, Name string
}

// progressEvery bounds how often the scan's progress reaches the page; only the latest counts matter (async.Feed).
const progressEvery = 80 * time.Millisecond

// State is the 空间透视 page's controller: the last scan, which the service keeps, and a scan while it runs. Make it in the page's component (NewState), whose scope stops a running scan when the page goes; its methods run on the engine goroutine.
type State struct {
	cx  *core.Scope
	ctx context.Context
	svc Service

	// Loaded is set once the service said which scan it keeps.
	Loaded *core.Signal[bool]
	// Current is the last scan, nil before the first.
	Current *core.Signal[*app.SpaceSummary]
	// Picking is set by 更换位置: the start screen shows although there is a scan to return to.
	Picking *core.Signal[bool]
	// Scanning is the scan running, nil when none.
	Scanning *core.Signal[*ScanRequest]
	// Progress is the running scan's last report, nil until the first.
	Progress *core.Signal[*app.SpaceProgress]
	// Err says why the last scan or the folder picker failed.
	Err *core.Signal[string]
	// Choosing is set while the system's folder picker is open.
	Choosing *core.Signal[bool]

	// stopped is set by Stop, so a scan the user stopped goes back without an error.
	stopped bool
	cancel  context.CancelFunc
	// undo is what of the page's moves to the Trash outlives the page, kept with the service (undoWatchFor).
	undo *undoWatch
}

// undoWatch is what 空间透视 keeps of its moves to the Trash beyond one showing of the page: the shell makes a new page (and State) on every visit, while the scan the service keeps and the toast's 撤销 stay, so an undo can come while another page shows.
type undoWatch struct {
	// moved are the history runs of the page's moves whose undo has not been seen; when 撤销 or 放回原处 puts one back, the page scans its location again (Undone).
	moved map[string]bool
	// pending is set when an undo was seen and the scan it asks for has not started: the results were not showing (the start screen over them, the page loading).
	pending bool
}

// undoWatches are the undoWatch of each service, one in the app (*app.Space); the mutex is for tests, which run apps side by side.
var undoWatches = struct {
	sync.Mutex
	m map[Service]*undoWatch
}{m: map[Service]*undoWatch{}}

// undoWatchFor returns svc's undoWatch, made at first.
func undoWatchFor(svc Service) *undoWatch {
	undoWatches.Lock()
	defer undoWatches.Unlock()
	w := undoWatches.m[svc]
	if w == nil {
		w = &undoWatch{moved: map[string]bool{}}
		undoWatches.m[svc] = w
	}
	return w
}

// NewState makes the controller in cx, the page's scope, and asks the service for the scan it keeps.
func NewState(cx *core.Scope, svc Service) *State {
	s := &State{cx: cx, ctx: cx.Context(), svc: svc,
		Loaded:   core.NewSignal(cx, false),
		Current:  core.NewSignalFunc[*app.SpaceSummary](cx, nil, nil),
		Picking:  core.NewSignal(cx, false),
		Scanning: core.NewSignal[*ScanRequest](cx, nil),
		Progress: core.NewSignalFunc[*app.SpaceProgress](cx, nil, nil),
		Err:      core.NewSignal(cx, ""),
		Choosing: core.NewSignal(cx, false),
		undo:     undoWatchFor(svc),
	}
	async.Do(cx, s.ctx, func(context.Context) (*app.SpaceSummary, error) { return svc.Current(), nil },
		func(sum *app.SpaceSummary, _ error) {
			// A scan that ended first already holds the newer result.
			if s.Current.GetUntracked() == nil {
				s.Current.Set(sum)
			}
			s.Loaded.Set(true)
			s.rescanIfUndone()
		})
	return s
}

// Phase returns what the page shows; reading it subscribes.
func (s *State) Phase() Phase {
	switch {
	case s.Scanning.Get() != nil:
		return PhaseScanning
	case !s.Loaded.Get():
		return PhaseLoading
	case s.Current.Get() != nil && !s.Picking.Get():
		return PhaseResults
	}
	return PhaseStart
}

// Scan starts scanning req; it does nothing while a scan runs.
func (s *State) Scan(req ScanRequest) {
	if s.Scanning.GetUntracked() != nil {
		return
	}
	s.stopped = false
	s.Err.Set("")
	s.Progress.Set(nil)
	s.Scanning.Set(&req)
	feed := async.NewFeed(s.cx, progressEvery, func(p app.SpaceProgress) { s.Progress.Set(&p) })
	run, cancel := context.WithCancel(s.ctx)
	s.cancel = cancel
	async.Do(s.cx, run, func(ctx context.Context) (app.SpaceSummary, error) {
		defer feed.Flush()
		return s.svc.Scan(ctx, req.Path, feed.Send)
	}, func(sum app.SpaceSummary, err error) {
		cancel()
		s.cancel = nil
		if err == nil {
			s.Current.Set(&sum)
			s.Picking.Set(false)
			// The scan read the disk after the undo it may have waited for.
			s.undo.pending = false
		} else if !s.stopped {
			s.Err.Set(err.Error())
		}
		s.Progress.Set(nil)
		s.Scanning.Set(nil)
	})
}

// Stop cancels the running scan; the page goes back to where it was, without an error.
func (s *State) Stop() {
	if s.cancel == nil {
		return
	}
	s.stopped = true
	s.cancel()
}

// Rescan scans the last scan's location again.
func (s *State) Rescan() {
	if sum := s.Current.GetUntracked(); sum != nil {
		s.Scan(ScanRequest{Path: sum.Root.Path, Name: sum.Name})
	}
}

// Choose opens the system's folder picker and scans the folder picked; cancelling it leaves the page as it is.
func (s *State) Choose() {
	if s.Choosing.GetUntracked() {
		return
	}
	s.Err.Set("")
	s.Choosing.Set(true)
	s.cx.PickDirectories(core.PickDirectoriesOptions{}, func(dirs []core.Directory, err error) {
		s.Choosing.Set(false)
		if err != nil {
			s.Err.Set("无法打开选择文件夹的窗口")
			return
		}
		if len(dirs) == 0 || dirs[0].Path == "" {
			return
		}
		path := dirs[0].Path
		name := format.BaseName(path)
		if name == "" {
			name = path
		}
		s.Scan(ScanRequest{Path: path, Name: name})
	})
}

// ChangeLocation shows the start screen over the results (更换位置).
func (s *State) ChangeLocation() { s.Picking.Set(true) }

// Return goes back from the start screen to the last scan's results, scanning them again first when an undo changed them meanwhile.
func (s *State) Return() {
	s.Err.Set("")
	s.Picking.Set(false)
	s.rescanIfUndone()
}

// ScanPath scans the folder path, named after it (空间变化's 在空间透视中查看).
func (s *State) ScanPath(path string) {
	name := format.BaseName(path)
	if name == "" {
		name = path
	}
	s.Scan(ScanRequest{Path: path, Name: name})
}

// Moved remembers runID, the history run of a move to the Trash this page made, so its undo scans again.
func (s *State) Moved(runID string) {
	if runID != "" {
		s.undo.moved[runID] = true
	}
}

// Undone looks for moves of the page that undid (store.Overview.Undid) reports put back, also while another page showed: then the last scan's sizes are out of date, so the page scans its location again, at once when its results show, else once they do. The page calls it as it opens and after every undo.
func (s *State) Undone(undid func(runID string) bool) {
	for id := range s.undo.moved {
		if undid(id) {
			delete(s.undo.moved, id)
			s.undo.pending = true
		}
	}
	s.rescanIfUndone()
}

// rescanIfUndone scans the last scan's location again when an undo asked for it and its results show; a scan running already reads the disk as it is.
func (s *State) rescanIfUndone() {
	if !s.undo.pending || !s.Loaded.GetUntracked() || s.Current.GetUntracked() == nil || s.Picking.GetUntracked() || s.Scanning.GetUntracked() != nil {
		return
	}
	s.undo.pending = false
	s.Rescan()
}

// trashed replaces the root of the scan scanID, if it is still the current one, with root corrected after a move to the Trash.
func (s *State) trashed(scanID string, root app.SpaceEntry) {
	cur := s.Current.GetUntracked()
	if cur == nil || cur.ScanID != scanID {
		return
	}
	next := *cur
	next.Root = root
	s.Current.Set(&next)
}
