package cleanup

import (
	"sync"
	"time"
)

// Throttle returns a progress function that passes events to fn at most once per every, plus every event that completes a count (Done == Total, Total > 0), so a scan reporting each file does not flood the page. The returned function is safe for concurrent use and never calls fn concurrently. A nil fn gives a function that does nothing.
func Throttle(fn func(Progress), every time.Duration) func(Progress) {
	if fn == nil {
		return func(Progress) {}
	}
	var (
		mu   sync.Mutex
		last time.Time
	)
	return func(p Progress) {
		mu.Lock()
		defer mu.Unlock()
		now := time.Now()
		if p.Total > 0 && p.Done >= p.Total || now.Sub(last) >= every {
			last = now
			fn(p)
		}
	}
}
