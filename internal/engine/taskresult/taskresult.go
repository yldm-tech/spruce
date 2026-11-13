package taskresult

import "time"

// Outcome is how a maintenance task ended.
type Outcome string

const (
	// Applied means the task did its work, e.g. the DNS cache was flushed.
	Applied Outcome = "applied"
	// Unchanged means the task ran and found nothing to do, e.g. every database already passed its integrity check and was compact.
	Unchanged Outcome = "unchanged"
	// Skipped means the task did not run because a prerequisite the user can fix was missing, e.g. Safari was still open, Full Disk Access was not granted, or the administrator prompt was declined. Message tells the user what to do.
	Skipped Outcome = "skipped"
	// Unavailable means the task cannot run on this system at all, e.g. a tool that this OS version no longer ships.
	Unavailable Outcome = "unavailable"
	// Failed means the task ran and an error stopped it. Detail carries the technical cause.
	Failed Outcome = "failed"
)

// Outcomes lists every Outcome in a stable order, for validation and for counting.
func Outcomes() []Outcome {
	return []Outcome{Applied, Unchanged, Skipped, Unavailable, Failed}
}

// Valid reports whether o is one of the declared outcomes. Results that cross a process boundary (the elevated child's response) are validated with it.
func (o Outcome) Valid() bool {
	switch o {
	case Applied, Unchanged, Skipped, Unavailable, Failed:
		return true
	}
	return false
}

// Result is what one task did.
type Result struct {
	// TaskID is the stable ID of the task (maintenance.Task.ID).
	TaskID string `json:"taskId"`
	// Outcome is how the task ended.
	Outcome Outcome `json:"outcome"`
	// Message is a short user-facing sentence in Simplified Chinese, shown in the UI and in the history, e.g. "已刷新 DNS 缓存" or "Safari 仍在运行，已跳过".
	Message string `json:"message"`
	// Detail is technical context for diagnostics (command, exit code, stderr excerpt). It is never shown as the primary text.
	Detail string `json:"detail,omitempty"`
	// Duration is how long the task took; in JSON (and TypeScript) it is a number of nanoseconds.
	Duration time.Duration `json:"duration"`
}
