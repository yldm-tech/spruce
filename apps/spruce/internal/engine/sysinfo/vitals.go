package sysinfo

import "time"

// Vitals is one sample of the machine. A nil metric could not be read for this sample; the others do not depend on it.
type Vitals struct {
	At     time.Time    `json:"at"`
	CPU    *CPUStats    `json:"cpu"`
	Memory *MemoryStats `json:"memory"`
	// Disk is the system volume.
	Disk   *DiskUsage `json:"disk"`
	DiskIO *DiskIO    `json:"diskIo"`
	// Network sums every interface except loopback.
	Network *NetworkIO `json:"network"`
	// Battery is nil when it could not be read; Present is false on a machine without one.
	Battery   *BatteryStats `json:"battery"`
	Processes *ProcessLists `json:"processes"`
}

// CPUStats is the CPU use since the previous sample.
type CPUStats struct {
	// Percent is the whole machine's use, 0 to 100.
	Percent float64 `json:"percent"`
	// PerCore is each logical CPU's use, 0 to 100, in the system's order.
	PerCore []float64 `json:"perCore"`
	// Load is the load average; only macOS has one.
	Load *LoadAverage `json:"load,omitempty"`
}

// LoadAverage is the number of runnable threads averaged over 1, 5 and 15 minutes.
type LoadAverage struct {
	One     float64 `json:"one"`
	Five    float64 `json:"five"`
	Fifteen float64 `json:"fifteen"`
}

// MemoryStats is the physical memory and swap in use.
type MemoryStats struct {
	Total uint64 `json:"total"`
	// Used is what gopsutil counts as used: total minus what is available without swapping (on macOS free plus inactive pages).
	Used    uint64  `json:"used"`
	Percent float64 `json:"percent"`
	// Swap is the swap files on macOS and the page file on Windows; nil when it could not be read.
	Swap *SwapStats `json:"swap"`
	// Pressure is the kernel's memory pressure level; only macOS reports one.
	Pressure MemoryPressure `json:"pressure,omitempty"`
}

// SwapStats is the swap space in use.
type SwapStats struct {
	Total uint64 `json:"total"`
	Used  uint64 `json:"used"`
}

// MemoryPressure is the kernel's memory pressure level (kern.memorystatus_vm_pressure_level), as Activity Monitor's memory pressure graph colours it.
type MemoryPressure string

const (
	PressureNormal   MemoryPressure = "normal"
	PressureWarning  MemoryPressure = "warning"
	PressureCritical MemoryPressure = "critical"
)

// DiskUsage is the system volume's space.
type DiskUsage struct {
	Total uint64 `json:"total"`
	// Used is Total minus Free, which on macOS includes the other volumes sharing the APFS container, as Finder counts it.
	Used uint64 `json:"used"`
	// Free is the space available to the user.
	Free uint64 `json:"free"`
}

// DiskIO is the throughput of every physical disk together since the previous sample, in bytes per second.
type DiskIO struct {
	ReadPerSec  float64 `json:"readPerSec"`
	WritePerSec float64 `json:"writePerSec"`
}

// NetworkIO is the throughput of every interface except loopback together since the previous sample, in bytes per second.
type NetworkIO struct {
	UploadPerSec   float64 `json:"uploadPerSec"`
	DownloadPerSec float64 `json:"downloadPerSec"`
}

// BatteryState is what the battery is doing.
type BatteryState string

const (
	BatteryCharging    BatteryState = "charging"
	BatteryDischarging BatteryState = "discharging"
	BatteryFull        BatteryState = "full"
	// BatteryNotCharging is on external power without charging, e.g. held at 80 % by optimized charging.
	BatteryNotCharging BatteryState = "notCharging"
	BatteryUnknown     BatteryState = "unknown"
)

// BatteryStats is the internal battery (all of them together on a machine with several).
type BatteryStats struct {
	Present bool `json:"present"`
	// Percent is the charge, 0 to 100.
	Percent float64      `json:"percent"`
	State   BatteryState `json:"state"`
	// MinutesRemaining is the system's estimate until empty while discharging or until full while charging; nil while unknown (still calculating, or on external power without charging).
	MinutesRemaining *int `json:"minutesRemaining,omitempty"`
	// CycleCount is nil when the battery does not report it.
	CycleCount *int `json:"cycleCount,omitempty"`
	// HealthPercent is the full charge capacity as a percent of the design capacity; nil when either is unknown.
	HealthPercent *float64 `json:"healthPercent,omitempty"`
}

// ProcessLists are the processes using the most CPU and the most memory.
type ProcessLists struct {
	// TopCPU is most CPU first; processes first seen in this sample have no CPU figure yet and are not in it.
	TopCPU []ProcessUsage `json:"topCpu"`
	// TopMemory is most memory first.
	TopMemory []ProcessUsage `json:"topMemory"`
}

// ProcessUsage is one process in a top list.
type ProcessUsage struct {
	PID int32 `json:"pid"`
	// Name is the display name package health uses for alerts, e.g. "Google Chrome" for its helpers on macOS.
	Name string `json:"name"`
	// CPUPercent is the share of one logical CPU since the previous sample (a process busy on two cores is 200); nil when unknown.
	CPUPercent *float64 `json:"cpuPercent"`
	// Memory is the resident set (RSS) on macOS and the working set on Windows, in bytes; nil when unknown.
	Memory *uint64 `json:"memory"`
}

// SystemInfo is what does not change while the app runs. Fields that could not be read are empty.
type SystemInfo struct {
	// OSName is e.g. "macOS" or "Windows 11 Pro".
	OSName string `json:"osName"`
	// OSVersion is e.g. "26.1" on macOS or "24H2" on Windows.
	OSVersion string `json:"osVersion"`
	// OSBuild is e.g. "25B78" on macOS or "26100.2314" on Windows.
	OSBuild string `json:"osBuild"`
	// Model is the hardware model: the identifier on macOS (hw.model, e.g. "Mac16,8"), the BIOS manufacturer and product name on Windows.
	Model    string `json:"model"`
	CPUModel string `json:"cpuModel"`
	// PhysicalCores and LogicalCores are 0 when unknown.
	PhysicalCores int `json:"physicalCores"`
	LogicalCores  int `json:"logicalCores"`
	// PerformanceCores and EfficiencyCores split PhysicalCores on Apple Silicon; 0 elsewhere.
	PerformanceCores int `json:"performanceCores,omitempty"`
	EfficiencyCores  int `json:"efficiencyCores,omitempty"`
	// BootTime is when the system started; absent when unknown.
	BootTime time.Time `json:"bootTime,omitzero"`
}
