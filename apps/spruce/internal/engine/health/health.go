package health

import (
	"context"
	"errors"
	"fmt"
	"time"
)

// Config tunes the monitor.
type Config struct {
	// Interval is the time between samples.
	Interval time.Duration
	// Window is the sliding window over which a process's CPU percent is averaged; at least two Intervals.
	Window time.Duration
	// Threshold is the CPU percent (of one logical CPU) at or above which a process becomes hot.
	Threshold float64
	// Hysteresis is how many percentage points below Threshold a hot process must fall before it stops being hot, so a process hovering around Threshold neither flaps nor restarts its Sustain clock; 0 means it stops at Threshold.
	Hysteresis float64
	// Sustain is how long a process must stay hot before it alerts.
	Sustain time.Duration
	// Ignore lists process names that never alert (exact, case-sensitive match against ProcessSample.Name).
	Ignore []string
}

// DefaultConfig is the configuration the app ships with: sample every 5 s, average over 1 min, alert at 40 % of a core sustained for 10 min, and stop being hot below 30 %. Kernel, idle and memory-manager pseudo-processes are ignored (macOS kernel_task; Windows System Idle Process, System, Registry, Memory Compression); WindowServer and other real system processes are not.
func DefaultConfig() Config {
	return Config{
		Interval:   5 * time.Second,
		Window:     time.Minute,
		Threshold:  40,
		Hysteresis: 10,
		Sustain:    10 * time.Minute,
		Ignore:     []string{"kernel_task", "System Idle Process", "System", "Idle", "Registry", "Memory Compression"},
	}
}

// Validate reports a configuration the monitor cannot use.
func (c Config) Validate() error {
	switch {
	case c.Interval <= 0:
		return errors.New("health: Interval must be positive")
	case c.Window < 2*c.Interval:
		return fmt.Errorf("health: Window %v must be at least two Intervals (%v)", c.Window, 2*c.Interval)
	case c.Threshold <= 0:
		return errors.New("health: Threshold must be positive")
	case c.Hysteresis < 0 || c.Hysteresis >= c.Threshold:
		return fmt.Errorf("health: Hysteresis %v must be at least 0 and below Threshold %v", c.Hysteresis, c.Threshold)
	case c.Sustain < 0:
		return errors.New("health: Sustain must not be negative")
	}
	return nil
}

// Alert is one process that has stayed above the threshold.
type Alert struct {
	PID int32 `json:"pid"`
	// Name is the process's display name, e.g. "Xcode".
	Name string `json:"name"`
	// CPUPercent is its CPU use over the last Window, as a percent of one logical CPU on every platform (Activity Monitor's convention; Windows Task Manager divides by the number of logical CPUs instead).
	CPUPercent float64 `json:"cpuPercent"`
	// Since is when the sustained period began.
	Since time.Time `json:"since"`
	// Duration is how long it has been hot, as of the latest sample; in JSON a number of nanoseconds.
	Duration time.Duration `json:"duration"`
	// Threshold is the threshold it crossed, for the UI's wording.
	Threshold float64 `json:"threshold"`
}

// Monitor samples processes and keeps the current alerts.
type Monitor interface {
	// Run samples until ctx is done and returns ctx's error, or an error that stops sampling altogether. A failed sample is logged and skipped, not fatal.
	Run(ctx context.Context) error
	// Alerts returns the current alerts, most CPU first.
	Alerts() []Alert
	// Subscribe returns a channel that receives the complete current alert list immediately and then on every change, and a function that unsubscribes and closes the channel. A slow subscriber only misses intermediate lists, never the latest one.
	Subscribe() (<-chan []Alert, func())
	// Tune replaces Threshold and Sustain and turns alerting on or off, from the next sample on, without a restart (main.go follows the user's settings with it). A disabled monitor takes no samples and publishes an empty list. It rejects a threshold not above Config.Hysteresis or a negative sustain.
	Tune(threshold float64, sustain time.Duration, enabled bool) error
}

// ProcessSample is one process at one instant.
type ProcessSample struct {
	PID  int32
	Name string
	// CPUTime is the process's cumulative user plus system CPU time.
	CPUTime time.Duration
	// CreateTime tells a reused PID from the process that had it before.
	CreateTime time.Time
}

// Snapshot is every process at one instant.
type Snapshot struct {
	At        time.Time
	Processes []ProcessSample
}

// ProcessSource takes snapshots of the running processes. Processes the app may not inspect are left out rather than failing the snapshot.
type ProcessSource interface {
	Sample(ctx context.Context) (Snapshot, error)
}
