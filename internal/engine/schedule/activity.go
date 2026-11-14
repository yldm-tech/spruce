package schedule

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"time"
)

var (
	// ErrBusyElsewhere is returned when another Spruce process (the app or the command line) holds the clean lock.
	ErrBusyElsewhere = errors.New("schedule: another Spruce process is cleaning or checking")
	// ErrBusy is returned by BeginBackground while a clean or another background run goes on in this process.
	ErrBusy = errors.New("schedule: busy")
	// ErrLock is returned (wrapping the cause) when the clean lock could not be read; a clean is refused rather than run without it.
	ErrLock = errors.New("schedule: the clean lock is not usable")
	// ErrNotAuto is what app.Cleanup.CleanWith returns for a KindAuto clean whose context did not come from BeginAutoClean, and what BeginAutoClean returns for a context that is not the current background run.
	ErrNotAuto = errors.New("schedule: an automatic clean outside the background slot")
)

// LockFunc takes the cross-process clean lock without waiting: instance.LockFile on <data>/clean.lock.
type LockFunc func() (release func() error, held bool, err error)

// Activity is the one place that knows whether a clean or background work runs, in this process and, through the cross-process lock, in another Spruce process. This process holds the lock (one open file) while any of its cleans or its background run goes on, and releases it when the last ends. Safe for concurrent use.
type Activity struct {
	lock LockFunc

	mu sync.Mutex
	// cleans counts the cleans of this process, automatic ones included, from the moment BeginClean is called.
	cleans int
	// holders counts what needs the lock: the cleans that took it or found it held, and the background run.
	holders int
	release func() error
	bg      *bgRun
	// idle is closed while no clean runs; Wait selects on it.
	idle chan struct{}

	quit context.Context
	stop context.CancelFunc
}

// bgRun is the background run that holds the slot.
type bgRun struct {
	cancel context.CancelFunc
	// done is closed when the run's end is called.
	done chan struct{}
	// stopping is set when a clean has asked the run to stop: no automatic clean may begin in it then.
	stopping bool
}

type (
	bgKey   struct{}
	autoKey struct{}
)

// NewActivity takes the cross-process lock function; nil means none (tests): the lock is always held.
func NewActivity(lock LockFunc) *Activity {
	quit, stop := context.WithCancel(context.Background())
	idle := make(chan struct{})
	close(idle)
	return &Activity{lock: lock, idle: idle, quit: quit, stop: stop}
}

// acquireLocked takes the cross-process lock unless this process holds it, and counts one more holder. a.mu is held.
func (a *Activity) acquireLocked() error {
	if a.holders == 0 && a.lock != nil {
		release, held, err := a.lock()
		switch {
		case err != nil:
			return fmt.Errorf("%w: %w", ErrLock, err)
		case !held:
			return ErrBusyElsewhere
		}
		a.release = release
	}
	a.holders++
	return nil
}

// releaseLocked counts one holder less and releases the lock with the last. a.mu is held.
func (a *Activity) releaseLocked() {
	a.holders--
	if a.holders == 0 && a.release != nil {
		_ = a.release()
		a.release = nil
	}
}

// startCleanLocked counts a clean. a.mu is held.
func (a *Activity) startCleanLocked() {
	if a.cleans == 0 {
		a.idle = make(chan struct{})
	}
	a.cleans++
}

// endCleanLocked counts a clean less. a.mu is held.
func (a *Activity) endCleanLocked() {
	a.cleans--
	if a.cleans == 0 {
		close(a.idle)
	}
}

// withQuit returns a context that ends with ctx or Shutdown.
func (a *Activity) withQuit(ctx context.Context) (context.Context, context.CancelFunc) {
	c, cancel := context.WithCancel(ctx)
	stop := context.AfterFunc(a.quit, cancel)
	return c, func() {
		stop()
		cancel()
	}
}

// BeginClean cancels a running background run and waits for it to return (until ctx ends), takes the cross-process lock unless this process holds it already (ErrBusyElsewhere when another process holds it, ErrLock wrapping the error when it cannot be taken), and returns a context that ends with ctx or Shutdown, and end, which must be called once the clean is over. Cleans of this process may overlap (each controller keeps its own one-at-a-time rule); background work waits for all of them.
func (a *Activity) BeginClean(ctx context.Context) (clean context.Context, end func(), err error) {
	a.mu.Lock()
	if err := a.quit.Err(); err != nil {
		a.mu.Unlock()
		return nil, nil, err
	}
	a.startCleanLocked()
	bg := a.bg
	if bg != nil {
		bg.stopping = true
	}
	a.mu.Unlock()

	if bg != nil {
		bg.cancel()
		select {
		case <-bg.done:
		case <-ctx.Done():
			a.mu.Lock()
			a.endCleanLocked()
			a.mu.Unlock()
			return nil, nil, ctx.Err()
		}
	}

	a.mu.Lock()
	if err := a.acquireLocked(); err != nil {
		a.endCleanLocked()
		a.mu.Unlock()
		return nil, nil, err
	}
	a.mu.Unlock()
	c, cancel := a.withQuit(ctx)
	var once sync.Once
	return c, func() {
		once.Do(func() {
			cancel()
			a.mu.Lock()
			a.releaseLocked()
			a.endCleanLocked()
			a.mu.Unlock()
		})
	}, nil
}

// BeginBackground starts background work when no clean runs in this process (ErrBusy otherwise, and while another background run goes on) and no other process holds the lock (ErrBusyElsewhere; ErrLock when it is unusable): it holds the cross-process lock for the whole run, so the command line cannot clean while the app checks, and the run's automatic rules need no second lock. run ends when ctx ends, a clean begins or Shutdown; end must be called once the work has returned.
func (a *Activity) BeginBackground(ctx context.Context) (run context.Context, end func(), err error) {
	a.mu.Lock()
	defer a.mu.Unlock()
	if err := a.quit.Err(); err != nil {
		return nil, nil, err
	}
	if a.cleans > 0 || a.bg != nil {
		return nil, nil, ErrBusy
	}
	if err := a.acquireLocked(); err != nil {
		return nil, nil, err
	}
	c, cancel := a.withQuit(ctx)
	bg := &bgRun{cancel: cancel, done: make(chan struct{})}
	a.bg = bg
	var once sync.Once
	return context.WithValue(c, bgKey{}, bg), func() {
		once.Do(func() {
			cancel()
			a.mu.Lock()
			if a.bg == bg {
				a.bg = nil
			}
			a.releaseLocked()
			a.mu.Unlock()
			close(bg.done)
		})
	}, nil
}

// BeginAutoClean, inside the background slot (run must be the context BeginBackground returned, or one derived from it), counts an automatic rule's clean as a clean of this process without cancelling run and without taking the lock again (the run holds it). The returned context is run marked as automatic (IsAuto); a clean BeginClean starts meanwhile still cancels run, so the user's action wins and the rule stops after its current item. It returns ErrNotAuto for a context that is not the current background run, and run's error once run has ended or a clean has asked it to stop.
func (a *Activity) BeginAutoClean(run context.Context) (auto context.Context, end func(), err error) {
	bg, _ := run.Value(bgKey{}).(*bgRun)
	a.mu.Lock()
	defer a.mu.Unlock()
	switch {
	case bg == nil || a.bg != bg:
		return nil, nil, ErrNotAuto
	case run.Err() != nil:
		return nil, nil, run.Err()
	case bg.stopping:
		return nil, nil, context.Canceled
	}
	a.startCleanLocked()
	var once sync.Once
	return context.WithValue(run, autoKey{}, true), func() {
		once.Do(func() {
			a.mu.Lock()
			a.endCleanLocked()
			a.mu.Unlock()
		})
	}, nil
}

// IsAuto reports whether ctx came from BeginAutoClean.
func IsAuto(ctx context.Context) bool {
	auto, _ := ctx.Value(autoKey{}).(bool)
	return auto
}

// Cleaning reports whether a clean (automatic ones included) runs in this process.
func (a *Activity) Cleaning() bool {
	a.mu.Lock()
	defer a.mu.Unlock()
	return a.cleans > 0
}

// Shutdown ends every clean and background context (each clean stops after its current item and still records what it did) and refuses new ones; main calls it after nsgo.Run returns.
func (a *Activity) Shutdown() { a.stop() }

// Wait blocks until no clean runs or timeout passes, and reports whether they all ended.
func (a *Activity) Wait(timeout time.Duration) bool {
	a.mu.Lock()
	idle := a.idle
	a.mu.Unlock()
	t := time.NewTimer(timeout)
	defer t.Stop()
	select {
	case <-idle:
		return true
	case <-t.C:
		return false
	}
}
