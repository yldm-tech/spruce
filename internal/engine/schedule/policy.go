package schedule

import "time"

// Policy is the timing of the scheduled checks.
type Policy struct {
	// Interval is the time between scheduled checks.
	Interval time.Duration
	// LaunchDelay is how long after launch the first one waits.
	LaunchDelay time.Duration
	// Retry is how long to wait before the conditions are looked at again after a check was held back.
	Retry time.Duration
	// Tick is the loop tick; due times compare wall clocks (time.Now().Round(0)), since timers stop while a Mac sleeps.
	Tick time.Duration
	// CPUWindow is the length of the load sample taken before a check.
	CPUWindow time.Duration
	// MaxCPUPercent is the load above which the machine counts as busy.
	MaxCPUPercent float64
	// Watch is how often power is checked again while a run goes on.
	Watch time.Duration
	// DiskEvery is how often free space is read again.
	DiskEvery time.Duration
}

// DefaultPolicy is the policy of the app: a check every 6 hours, the first 2 minutes after launch.
func DefaultPolicy() Policy {
	return Policy{
		Interval:      6 * time.Hour,
		LaunchDelay:   2 * time.Minute,
		Retry:         15 * time.Minute,
		Tick:          5 * time.Minute,
		CPUWindow:     10 * time.Second,
		MaxCPUPercent: 40,
		Watch:         60 * time.Second,
		DiskEvery:     15 * time.Minute,
	}
}

// Due reports whether a scheduled check is due at now, given the end of the last check (zero: never; the cache's SavedAt) and the launch time. Pure.
func Due(p Policy, now, last, launched time.Time) bool {
	return !now.Round(0).Before(Next(p, now, last, launched))
}

// Next is when the next scheduled check becomes due: not before LaunchDelay after launch, and not before Interval after the last check. A last check that lies in the future (the clock was set back) is not trusted and counts as none, so checks do not stop until the clock catches up; a check is read-only, so one more is harmless. Times compare as wall clocks. Pure.
func Next(p Policy, now, last, launched time.Time) time.Time {
	now, last, launched = now.Round(0), last.Round(0), launched.Round(0)
	next := launched.Add(p.LaunchDelay)
	if last.IsZero() || last.After(now) {
		return next
	}
	if after := last.Add(p.Interval); after.After(next) {
		next = after
	}
	return next
}
