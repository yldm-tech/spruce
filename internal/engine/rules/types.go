// Package rules is the automatic cleaning rules of 设置 → 自动清理 (macOS): which categories each rule may take items from, how old an item must be, how often the rule runs, and the weekly summary. Rules only ever move to the Trash (cleanup.ModeTrashOnly, forced again by app.Cleanup.CleanWith), never run a command and never touch an item the user put back. The package is platform-independent so it can be tested anywhere; only macOS runs the rules. It does not import any UI framework.
package rules

import (
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// ID names a rule; its string is what history.Run.Rule and the settings record.
type ID string

const (
	AppCaches           ID = "app-caches"
	Installers          ID = "installers"
	BuildOutput         ID = "build-output"
	IncompleteDownloads ID = "incomplete-downloads"
)

// Spec is one rule.
type Spec struct {
	ID ID
	// Title is the rule's label, e.g. 「旧的应用缓存」.
	Title string
	// Categories are the merged category IDs, or prefixes of them, the rule may take items from.
	Categories []string
	// MinAge is how old an item must be (see Age).
	MinAge time.Duration
	// Every is how often the rule runs: 7 days or 1 day.
	Every time.Duration
	// NeedGit, for build output, requires Git GitClean or GitNone and no Hold (tracked files).
	NeedGit bool
	// InDownloads limits the rule to items directly or indirectly inside Inputs.Downloads (installers).
	InDownloads bool
	// NeedInstalled requires advice.MatchInstalled(file name, Inputs.InstalledApps) and a successful read of the installed apps (installers).
	NeedInstalled bool
}

// Inputs is what Plan needs besides the scan.
type Inputs struct {
	Now time.Time
	// Downloads is the Downloads folder.
	Downloads string
	// InstalledApps are the installed apps' names; nil means they could not be read, and NeedInstalled rules then plan nothing.
	InstalledApps []string
	// Restored are the item IDs the user put back (undo.Ledger.Restored); they are never planned.
	Restored map[string]time.Time
}

// Planned is what one rule will clean.
type Planned struct {
	Rule ID
	// Req is in cleanup.ModeTrashOnly with every item named.
	Req   cleanup.CleanRequest
	Items int
	Bytes int64
}

// RuleTotal is what one rule moved to the Trash over a period.
type RuleTotal struct {
	Rule  ID
	Items int
	Bytes int64
}

// Summary is what the automatic rules moved over a period, less what was put back.
type Summary struct {
	Runs, Items int
	Trashed     int64
	ByRule      []RuleTotal
}
