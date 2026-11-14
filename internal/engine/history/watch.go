package history

import "sync"

// Watcher is implemented by stores that announce every run they record; the Store that Open returns is one. The services layer uses it to tell the 操作历史 page that a run was added, whichever page or part of the engine recorded it.
type Watcher interface {
	// Subscribe returns a channel that receives each run appended from now on, oldest first, and a function that ends the subscription and closes the channel (it may be called more than once). Append never waits for a subscriber: one that falls WatchBuffer runs behind misses the runs that do not fit.
	Subscribe() (<-chan Run, func())
}

// WatchBuffer is how many runs a subscriber's channel holds before Append drops runs for it.
const WatchBuffer = 16

// watchers is the subscriber list of a Store; its zero value has none.
type watchers struct {
	mu   sync.Mutex
	subs map[chan Run]struct{}
}

func (w *watchers) Subscribe() (<-chan Run, func()) {
	ch := make(chan Run, WatchBuffer)
	w.mu.Lock()
	if w.subs == nil {
		w.subs = make(map[chan Run]struct{})
	}
	w.subs[ch] = struct{}{}
	w.mu.Unlock()
	var once sync.Once
	return ch, func() {
		once.Do(func() {
			w.mu.Lock()
			delete(w.subs, ch)
			w.mu.Unlock()
			close(ch)
		})
	}
}

// notify hands run to every subscriber that has room for it.
func (w *watchers) notify(run Run) {
	w.mu.Lock()
	defer w.mu.Unlock()
	for ch := range w.subs {
		select {
		case ch <- run:
		default:
		}
	}
}
