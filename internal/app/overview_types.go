package app

import (
	"time"

	"github.com/yldm-tech/spruce/internal/engine/advice"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/devjunk"
	"github.com/yldm-tech/spruce/internal/engine/rules"
)

// OverviewPhase is what 概览's controller is doing.
type OverviewPhase string

const (
	OverviewIdle     OverviewPhase = "idle"
	OverviewChecking OverviewPhase = "checking"
	OverviewCleaning OverviewPhase = "cleaning"
)

// CheckMode is how a check ran: scheduled in the background, or asked for in the foreground (opening the window, 重新检查).
type CheckMode string

const (
	CheckBackground CheckMode = "background"
	CheckForeground CheckMode = "foreground"
)

// OverviewResult is the conclusion of a check: the merged scan and what 概览 shows of it.
type OverviewResult struct {
	CheckedAt time.Time `json:"checkedAt"`
	Mode      CheckMode `json:"mode"`
	// FromCache is set for a result read from overview-cache.json; it is never actionable.
	FromCache bool `json:"fromCache"`
	Stale     bool `json:"stale"`
	// Actionable is whether Clean may act on this result now: not FromCache, the smartscan instance that produced it is neither scanning nor Pending, SettingsKey equals scancache.Key of the current settings, and CheckedAt is within scancache.StaleAfter. The controller recomputes it on every publish and on every tick.
	Actionable      bool                    `json:"actionable"`
	SettingsKey     string                  `json:"settingsKey"`
	Scan            cleanup.ScanResult      `json:"scan"`
	Recommendations []advice.Recommendation `json:"recommendations"`
	Headline        int64                   `json:"headline"`
	Projects        []devjunk.Project       `json:"projects"`
	// Skipped are the prompting folders a background check did not read.
	Skipped []string `json:"skipped,omitempty"`
}

// DiskState is the system volume's space.
type DiskState struct {
	Volume string `json:"volume"`
	Free   int64  `json:"free"`
	Total  int64  `json:"total"`
	// Low is the rule of DiskLow.
	Low bool      `json:"low"`
	At  time.Time `json:"at"`
}

// RuleStatus is one automatic rule as 设置 → 自动清理 shows it.
type RuleStatus struct {
	ID      string `json:"id"`
	Enabled bool   `json:"enabled"`
	// Matching and MatchingBytes are what rules.Plan finds in the current result, due or not.
	Matching      int       `json:"matching"`
	MatchingBytes int64     `json:"matchingBytes"`
	LastRun       time.Time `json:"lastRun"`
	LastTrashed   int64     `json:"lastTrashed"`
}

// AutoRun is the last run of the automatic rules.
type AutoRun struct {
	RunIDs  []string  `json:"runIds"`
	Trashed int64     `json:"trashed"`
	At      time.Time `json:"at"`
}

// OverviewState is everything 概览, the tray and the drill-down views read.
type OverviewState struct {
	Phase    OverviewPhase     `json:"phase"`
	Result   *OverviewResult   `json:"result"`
	Progress *cleanup.Progress `json:"progress"`
	// Waiting is schedule.Verdict.Reason when a scheduled check is due but blocked.
	Waiting  string       `json:"waiting"`
	Next     time.Time    `json:"next"`
	Error    string       `json:"error"`
	Disk     *DiskState   `json:"disk"`
	Rules    []RuleStatus `json:"rules"`
	LastAuto *AutoRun     `json:"lastAuto"`
	// CanUndo is trash.CanRestore().
	CanUndo bool `json:"canUndo"`
	// Loaded is set once the controller has read what it keeps (its background state, the history and the cached check), so a reader tells its first answer from a state it has not answered yet, whatever else that answer holds.
	Loaded bool `json:"loaded"`
}

// CleanOrigin says what a clean of 概览 acts for, which decides how it is recorded.
type CleanOrigin struct {
	// Recommendation is a card ID, recorded as KindSmartScan with the card's Detail line.
	Recommendation string
	// Source is a drill-down source, recorded with that source's kind.
	Source string
}

// OverviewOutcome is what a clean of 概览 did.
type OverviewOutcome struct {
	Result cleanup.CleanResult `json:"result"`
	RunID  string              `json:"runId"`
	// Undoable is set when the ledger kept at least one move.
	Undoable bool `json:"undoable"`
	// NotUndoable counts the moves the system put outside the Trash folders Restore accepts.
	NotUndoable int `json:"notUndoable"`
}

// RestoreOutcome is what a 放回原处 did.
type RestoreOutcome struct {
	// RunID is the KindRestore run that records it.
	RunID    string   `json:"runId"`
	Restored int      `json:"restored"`
	Failed   int      `json:"failed"`
	Bytes    int64    `json:"bytes"`
	Reasons  []string `json:"reasons,omitempty"`
}

// TrashInfo is the live Trash for the 清空废纸篓 dialog.
type TrashInfo struct {
	Items int   `json:"items"`
	Bytes int64 `json:"bytes"`
	// Restorable is undo.Ledger.Pending: items Spruce moved there that could still be put back.
	Restorable int `json:"restorable"`
}

// NoticeKind is a kind of system notification.
type NoticeKind string

const (
	NoticeLowDisk     NoticeKind = "low-disk"
	NoticeAutoSummary NoticeKind = "auto-summary"
)

// Notice is a system notification for the shell to post.
type Notice struct {
	Kind NoticeKind
	// ID replaces an earlier notification of the same ID: "low-disk", "auto-summary:2026-W46".
	ID          string
	Volume      string
	Free, Total int64
	// Recommended is what 概览 can free (low-disk).
	Recommended int64
	// Trashed and Rules are what the automatic rules moved this week (auto-summary).
	Trashed int64
	Rules   []rules.RuleTotal
}
