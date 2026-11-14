// Package advice turns a merged scan (系统垃圾, 开发者垃圾 and 安装包, through smartscan) into the recommendation cards of 概览: one concrete action each, with a reason and a consequence, sorted by size times safety. It is pure: it reads a cleanup.ScanResult and decides nothing about deleting, which stays with the cleaners. It does not import any UI framework.
package advice

import (
	"time"
)

// Action is what acting on a card's selected items does.
type Action string

const (
	// ActionTrash moves every item to the Trash: one click, then undo.
	ActionTrash Action = "trash"
	// ActionDelete deletes for good items that are rebuilt on demand, without a command: confirmed.
	ActionDelete Action = "delete"
	// ActionCommand runs a tool's own clean command: confirmed.
	ActionCommand Action = "command"
	// ActionMixed is items of more than one of the above: confirmed with the plan.
	ActionMixed Action = "mixed"
)

// ItemRef names one item of a merged scan.
type ItemRef struct {
	// CategoryID is the merged category ID.
	CategoryID string `json:"categoryId"`
	ItemID     string `json:"itemId"`
}

// TimeKind says how a reason's time is written: "relative" (format.RelativeTime) or "date" (format.MonthDay).
type TimeKind string

const (
	TimeRelative TimeKind = "relative"
	TimeDate     TimeKind = "date"
)

// Recommendation is one card of 概览.
type Recommendation struct {
	// ID is stable across checks (the card table of the design).
	ID string `json:"id"`
	// Source is "system-junk", "dev-junk" or "installers".
	Source string `json:"source"`
	// Icon is the first category's Icon.
	Icon  string `json:"icon"`
	Title string `json:"title"`
	// Reason is a full sentence; when it needs a time it holds the literal token "{time}", filled in by ReasonWith.
	Reason         string    `json:"reason"`
	ReasonTime     time.Time `json:"reasonTime,omitzero"`
	ReasonTimeKind TimeKind  `json:"reasonTimeKind,omitempty"`
	// Reason2 is an optional second line, e.g. 「2 个有未提交改动的项目没有选入」.
	Reason2     string `json:"reason2,omitempty"`
	Consequence string `json:"consequence"`
	// Action is that of the recommended items.
	Action Action `json:"action"`
	// Command is the command a command card runs, as typed in a terminal.
	Command    string    `json:"command,omitempty"`
	Bytes      int64     `json:"bytes"`
	Count      int       `json:"count"`
	Categories []string  `json:"categories"`
	Items      []ItemRef `json:"items"`
	// Newest is the latest time of the card's items.
	Newest time.Time `json:"newest"`
	// Rule is the rules.ID of the automatic rule that covers the card, "" for none.
	Rule  string  `json:"rule,omitempty"`
	Score float64 `json:"score"`
}

// Inputs is what Derive needs besides the scan.
type Inputs struct {
	Now time.Time
	// InstalledApps are the names of installed apps, for 「对应的应用都已装好」; nil leaves the fallback reason.
	InstalledApps []string
	Windows       bool
}
