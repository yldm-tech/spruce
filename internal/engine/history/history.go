package history

import (
	"time"

	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// Kind is what kind of operation a Run was.
type Kind string

const (
	// KindMaintenance is a run of the 维护 page.
	KindMaintenance Kind = "maintenance"
	// KindSystemJunk is a clean of the 系统垃圾 page.
	KindSystemJunk Kind = "systemJunk"
	// KindDevJunk is a clean of the 开发者垃圾 page.
	KindDevJunk Kind = "devJunk"
	// KindInstallers is a clean of the 安装包 page.
	KindInstallers Kind = "installers"
	// KindUninstall is an app removal of the 应用卸载 page.
	KindUninstall Kind = "uninstall"
	// KindSpace is a deletion from the 空间透视 page.
	KindSpace Kind = "space"
	// KindSmartScan is a clean of a recommendation of 概览 (formerly the 智能扫描 page) or of the command line.
	KindSmartScan Kind = "smartScan"
	// KindAuto is an automatic rule run: one Run per rule that moved or failed anything, Rule naming it.
	KindAuto Kind = "auto"
	// KindRestore is a 放回原处: RestoreOf names the run it put back.
	KindRestore Kind = "restore"
)

// MaxRuns is how many runs the store keeps; older ones are dropped.
const MaxRuns = 500

// Run is one recorded operation.
type Run struct {
	// ID is unique per run; the services layer generates it (time-ordered, e.g. "20261114T010412Z-3f9a").
	ID        string    `json:"id"`
	Kind      Kind      `json:"kind"`
	StartedAt time.Time `json:"startedAt"`
	EndedAt   time.Time `json:"endedAt"`
	// Results has one entry per task that was selected, in catalog order (maintenance runs; other kinds leave it empty).
	Results []taskresult.Result `json:"results"`
	// Summary totals a clean-up (every kind but maintenance).
	Summary *Totals `json:"summary,omitempty"`
	// Details are human-readable lines in Simplified Chinese, e.g. "系统缓存：已清理 34 项" or "无法删除 ~/Library/Caches/x：文件正在使用，已跳过".
	Details []string `json:"details,omitempty"`
	// Rule is the automatic rule of a KindAuto run (rules.ID as a string).
	Rule string `json:"rule,omitempty"`
	// Undo is what the undo ledger holds for this run when it was recorded; nil when nothing can be put back. The store has no update, so it is settled before Append.
	Undo *UndoInfo `json:"undo,omitempty"`
	// RestoreOf is the run a KindRestore run put back.
	RestoreOf string `json:"restoreOf,omitempty"`
}

// UndoInfo is what a run moved to the Trash that the undo ledger can put back.
type UndoInfo struct {
	Items int   `json:"items"`
	Bytes int64 `json:"bytes"`
}

// Totals is what a clean-up achieved.
type Totals struct {
	// Freed is the space given back by permanent deletion, in bytes.
	Freed int64 `json:"freed"`
	// Trashed is the size moved to the Trash, in bytes; it is freed once the Trash is emptied.
	Trashed int64 `json:"trashed,omitempty"`
	// Items counts items removed or moved to the Trash.
	Items int `json:"items"`
	// Failed counts items left in place.
	Failed int `json:"failed"`
}

// Store keeps runs.
type Store interface {
	// Append records run.
	Append(run Run) error
	// List returns up to limit runs, newest first; limit <= 0 means all.
	List(limit int) ([]Run, error)
	// Prune drops the runs that started before cutoff and returns how many it dropped.
	Prune(cutoff time.Time) (int, error)
	// Clear drops every run.
	Clear() error
}

// RetentionCutoff is the cutoff Prune takes to keep days days of history as of now.
func RetentionCutoff(now time.Time, days int) time.Time {
	return now.AddDate(0, 0, -days)
}
