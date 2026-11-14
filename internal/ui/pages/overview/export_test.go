package overview

import "time"

// SetNow makes the page's clock now and returns what puts the real one back.
func SetNow(now time.Time) func() {
	prev := timeNow
	timeNow = func() time.Time { return now }
	return func() { timeNow = prev }
}
