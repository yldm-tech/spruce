// Package schedule decides when Spruce's background work may run and keeps it apart from cleans. It does not import any UI framework.
//
// # Activity (activity.go)
//
// Activity is the one place that knows whether a clean or background work runs, in this process and, through a cross-process lock on <data>/clean.lock (instance.LockFile), in another Spruce process (the app or the command line). Every clean of the app (Cleanup.CleanWith, Space.Trash, Uninstall.Remove, Maintenance.Run, a restore) runs inside BeginClean; the scheduled check runs inside BeginBackground and holds the lock for its whole run, automatic rules included, which run inside BeginAutoClean without taking the lock again (flock and LockFileEx lock per open file, so a second descriptor of the same process would be refused). A clean that begins cancels a running background run and waits for it; background work never starts while a clean runs. Shutdown ends every clean and background context when the app quits; Wait gives the cleans a moment to record what they did.
//
// The policy, the power and load conditions and the state of the scheduler are added beside it (policy.go, conditions*.go, state.go).
package schedule
