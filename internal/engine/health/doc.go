// Package health watches per-process CPU use and raises an Alert when one process stays above a threshold for a sustained period, e.g. "Xcode 已经持续占用 48% CPU 有 12 分钟". It only informs: it never ends or throttles a process.
//
// # Foundation (health.go)
//
// Config with DefaultConfig and Validate, Alert, Monitor, ProcessSource, Snapshot and ProcessSample are fixed here. CPU percent everywhere in this package is the share of ONE logical CPU over the measurement window, as Activity Monitor and top show it: a process busy on two cores is 200. Threshold uses the same unit.
//
// # Health agent
//
// The health agent implements, in this package:
//
//	// New returns a Monitor sampling src with cfg (validated with cfg.Validate). It does nothing until Run.
//	func New(cfg Config, src ProcessSource) (Monitor, error)
//
//	// NewProcessSource returns the gopsutil-backed ProcessSource of this platform.
//	func NewProcessSource() ProcessSource
//
//	// OpenSystemMonitor opens the system's process monitor: Activity Monitor on macOS ("open -b com.apple.ActivityMonitor"), Task Manager on Windows ("taskmgr.exe"), through runner.
//	func OpenSystemMonitor(ctx context.Context, runner execx.Runner) error
//
// Algorithm the Monitor must follow: every Config.Interval take a Snapshot; a process's CPU percent is the growth of its cumulative CPUTime across the samples inside the last Config.Window divided by the wall time between them (so it needs at least two samples, and a PID whose CreateTime changed is a new process); a process is "hot" while that percent is at least Config.Threshold; it becomes an Alert once it has been hot continuously for Config.Sustain, with Since set to when the hot period began and Duration to now minus Since; the alert clears as soon as the process is no longer hot or exits. Names in Config.Ignore never alert, and neither does the app's own process. Subscribers receive the complete current alert list (most CPU first) whenever the set or the rounded percent/minutes of an alert changes, and once immediately on Subscribe. The monitor's timestamps come from Snapshot.At, never from the wall clock, so tests drive it with a fake ProcessSource.
package health
