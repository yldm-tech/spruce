// Package health watches per-process CPU use and raises an Alert when one process stays above a threshold for a sustained period, e.g. "Xcode 已经持续占用 48% CPU 有 12 分钟". It only informs: it never ends, suspends or renices a process.
//
// # Units
//
// CPU percent everywhere in this package, on every platform, is the share of ONE logical CPU over the measurement window, as Activity Monitor and top show it (a process busy on two cores is 200). Config.Threshold, Config.Hysteresis, Alert.CPUPercent and Alert.Threshold all use it, so the same configuration catches a runaway single-threaded process on a 4-core laptop and on a 32-core workstation. Windows Task Manager divides by the number of logical CPUs instead, so there it shows a lower figure for the same process; the page says so next to the alert rather than this package converting it, because a whole-machine percent of a single runaway thread on a many-core PC (say 3 %) would not read as worth an alert.
//
// # API
//
//	// New returns a Monitor sampling src with cfg (validated with cfg.Validate). It does nothing until Run.
//	func New(cfg Config, src ProcessSource) (Monitor, error)
//
//	// NewProcessSource returns the gopsutil-backed ProcessSource of this platform.
//	func NewProcessSource() ProcessSource
//
//	// OpenSystemMonitor opens the system's process monitor: Activity Monitor on macOS ("open -b com.apple.ActivityMonitor" through runner), Task Manager on Windows (ShellExecute of %SystemRoot%\System32\Taskmgr.exe, because Task Manager's manifest asks for elevation when available and CreateProcess refuses such programs).
//	func OpenSystemMonitor(ctx context.Context, runner execx.Runner) error
//
//	// DisplayName is the name alerts give a process, for other views that list processes (the 健康监测 dashboard).
//	func DisplayName(ctx context.Context, p *process.Process) string
//
// # Algorithm
//
// Every Config.Interval the Monitor takes a Snapshot. Processes are keyed by PID plus CreateTime, so a reused PID starts with no history. A process's CPU percent is the growth of its cumulative CPUTime between the oldest and the newest of its samples inside the last Config.Window, divided by the wall time between them; it needs at least two samples, and a gap longer than Window (sleep, failed samples) starts its history over. A process becomes hot when that percent is at least Config.Threshold and stays hot until it falls below Threshold minus Config.Hysteresis. It becomes an Alert once it has been hot continuously for Config.Sustain, with Since set to the sample at which the hot period began and Duration to the latest sample minus Since. The alert clears as soon as the process is no longer hot or exits. Names in Config.Ignore never alert, and neither does the app's own process. Memory is bounded by the number of running processes times Window/Interval samples; processes that exit are forgotten on the next sample.
//
// Subscribers receive the complete current alert list (most CPU first) whenever the set, or the rounded percent or whole minutes of an alert, changes, and once immediately on Subscribe. The monitor's timestamps come from Snapshot.At, never from the wall clock, so tests drive it with a fake ProcessSource; a snapshot not later than the previous one is dropped.
//
// # Platform notes
//
// macOS: gopsutil reads CPU times through proc_pidinfo, which the kernel only answers for the user's own processes, so root-owned processes such as WindowServer or mds_stores read as idle and never alert. Display names prefer the outermost .app bundle in the executable path, so "Google Chrome Helper (Renderer)" shows as "Google Chrome".
//
// Windows: protected processes whose times cannot be read are left out. Display names prefer the executable's FileDescription (what Task Manager shows), falling back to the image name without ".exe".
package health
