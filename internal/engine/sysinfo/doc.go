// Package sysinfo samples the machine for the 健康监测 dashboard: CPU (total, per core, load average on macOS), memory (with swap and, on macOS, the kernel's memory pressure level), the system volume's usage, disk and network throughput, the battery, the processes using the most CPU and memory, and static facts about the system. It only reads: it never changes a setting or touches a process.
//
// It is separate from package health, which watches for one process keeping the CPU busy for minutes and runs for as long as the app does. This package runs only while a page listens.
//
// # API
//
//	// New returns a Sampler over src with cfg (validated with cfg.Validate). It takes no samples until the first Subscribe.
//	func New(cfg Config, src Source, log *slog.Logger) (*Sampler, error)
//
//	// NewSource returns the gopsutil-backed Source of this platform; on macOS runner runs ioreg for the battery.
//	func NewSource(runner execx.Runner) Source
//
//	// Subscribe starts sampling if nothing else listens and returns the samples kept from the last Config.History intervals (oldest first), a channel of every later sample and a function that unsubscribes; sampling stops when the last subscriber leaves, and the history stays for the next one.
//	func (s *Sampler) Subscribe() (history []Vitals, samples <-chan Vitals, unsubscribe func())
//
//	// Info returns the static facts (OS, model, CPU model, cores, boot time), read once and then cached.
//	func (s *Sampler) Info(ctx context.Context) SystemInfo
//
// # Units and conventions
//
// Throughput is in bytes per second, averaged between two consecutive samples; counters that went backwards (an interface reset, a disk ejected) count as no traffic rather than a negative rate. CPU percent of the whole machine is 0 to 100. Per-process CPU percent is the share of ONE logical CPU, as in package health and Activity Monitor (a process busy on two cores is 200); Windows Task Manager divides by the number of logical CPUs instead, and the page says so.
//
// # Errors
//
// Every metric is read independently: one that fails is nil in that sample (the page shows 无法读取 for it) and the others are unaffected. A metric that starts failing is logged once, not on every sample. The battery is read every Config.BatteryEvery and reused in between; static facts are read once.
//
// # Platform notes
//
// macOS: the system volume is the Data volume (/System/Volumes/Data), whose free space is the APFS container's, so used is total minus free as Finder shows it. Disk throughput sums the physical disks (gopsutil reads IOBlockStorageDriver statistics, which APFS volumes on top of them do not repeat). Processes of other users (root daemons such as WindowServer) cannot be inspected without privileges and are left out. The battery comes from `ioreg -rn AppleSmartBattery -a`; on Apple Silicon CurrentCapacity and MaxCapacity are percentages and the capacities in mAh are AppleRawMaxCapacity and DesignCapacity, which recent macOS versions keep under BatteryData instead of at the top level.
//
// Windows: the system volume is %SystemDrive%. There is no load average or memory pressure. The battery comes from the battery driver (github.com/distatus/battery) and its cycle count, when the firmware reports one, from WMI (root\WMI BatteryCycleCount). The model is the BIOS manufacturer and product name from the registry.
package sysinfo
