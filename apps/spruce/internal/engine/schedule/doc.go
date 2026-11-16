// Package schedule decides when Spruce's background work may run and keeps it apart from cleans. It does not import any UI framework.
//
// # Activity (activity.go)
//
// Activity is the one place that knows whether a clean or background work runs, in this process and, through a cross-process lock on <data>/clean.lock (instance.LockFile), in another Spruce process (the app or the command line). Every clean of the app (Cleanup.CleanWith, Space.Trash, Uninstall.Remove, Maintenance.Run, a restore) runs inside BeginClean; the scheduled check runs inside BeginBackground and holds the lock for its whole run, automatic rules included, which run inside BeginAutoClean without taking the lock again (flock and LockFileEx lock per open file, so a second descriptor of the same process would be refused). A clean that begins cancels a running background run and waits for it; background work never starts while a clean runs. Shutdown ends every clean and background context when the app quits; Wait gives the cleans a moment to record what they did.
//
// # Policy (policy.go)
//
// Policy is the timing: a check every 6 hours, the first 2 minutes after launch, 15 minutes before held-back conditions are looked at again, a 5 minute loop tick, a 10 second load sample. Due and Next compare wall clocks, since timers stop while a Mac sleeps.
//
// # Conditions (conditions*.go, power_status.go)
//
// Check lets a scheduled check start only with no clean in this process, on AC power, outside Low Power Mode or battery saver, below the serious thermal state and with the machine's load at most 40%; a probe that fails holds the check back. WatchPower repeats the power part while a run goes on. SystemConditions reads the battery through sysinfo (ioreg) and NSProcessInfo on macOS, after respondsToSelector:, since isLowPowerModeEnabled only exists from macOS 12; GetSystemPowerStatus on Windows; and the CPU times through gopsutil. Nothing it reads prompts for a permission. BusyVerdict words the errors of BeginBackground.
//
// # State (state.go)
//
// StateStore keeps <data>/background.json: when each automatic rule last ran, when the weekly summary last went out, and the low-disk alert, which LowDiskAlert advances (once per crossing, again after a further 5 GB drop, at most once a day, re-armed 2 GB above the threshold).
package schedule
