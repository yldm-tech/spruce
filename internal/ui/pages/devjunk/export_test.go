package devjunk

import "time"

// SetNow fixes the view's clock at t and returns what puts it back.
func SetNow(t time.Time) func() {
	prev := timeNow
	timeNow = func() time.Time { return t }
	return func() { timeNow = prev }
}
