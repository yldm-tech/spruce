package history

import (
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	enginehist "github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/pages/maintenance"
)

// Limit is how many runs the page asks for: everything the store keeps.
const Limit = enginehist.MaxRuns

// KindOrder lists the kinds in the order the filter chips show them: the sidebar's.
var KindOrder = []enginehist.Kind{
	enginehist.KindSmartScan,
	enginehist.KindSystemJunk,
	enginehist.KindDevJunk,
	enginehist.KindInstallers,
	enginehist.KindUninstall,
	enginehist.KindSpace,
	enginehist.KindMaintenance,
}

var kindRoutes = map[enginehist.Kind]nav.Route{
	enginehist.KindMaintenance: nav.Maintenance,
	enginehist.KindSystemJunk:  nav.SystemJunk,
	enginehist.KindDevJunk:     nav.DevJunk,
	enginehist.KindInstallers:  nav.Installers,
	enginehist.KindUninstall:   nav.Uninstall,
	enginehist.KindSpace:       nav.Space,
	enginehist.KindSmartScan:   nav.SmartScan,
}

// KindMeta is the page a run's kind belongs to (its title, tile tone and glyph). A kind this build does not know, written by a newer version, shows as the history page's own, titled 其他操作.
func KindMeta(k enginehist.Kind) nav.Meta {
	if r, ok := kindRoutes[k]; ok {
		return r.Meta()
	}
	m := nav.History.Meta()
	m.Title = "其他操作"
	return m
}

// DayGroup is the runs of one local calendar day, newest first.
type DayGroup struct {
	// Key is the day as YYYY-MM-DD, unique per group.
	Key string
	// Label is 今天, 昨天, or the date: "11月2日", "2025年11月2日" in another year.
	Label string
	Runs  []enginehist.Run
}

func dayKey(t time.Time) string { return t.Format("2006-01-02") }

// DayLabel names date's day as seen on now's day, in now's location.
func DayLabel(date, now time.Time) string {
	date = date.In(now.Location())
	if dayKey(date) == dayKey(now) {
		return "今天"
	}
	if dayKey(date) == dayKey(time.Date(now.Year(), now.Month(), now.Day()-1, 12, 0, 0, 0, now.Location())) {
		return "昨天"
	}
	return format.MonthDay(date, now)
}

// GroupByDay splits runs (newest first, as History.List returns them) into days in now's location by their start, keeping the order.
func GroupByDay(runs []enginehist.Run, now time.Time) []DayGroup {
	var groups []DayGroup
	index := map[string]int{}
	for _, r := range runs {
		started := r.StartedAt.In(now.Location())
		key := dayKey(started)
		i, ok := index[key]
		if !ok {
			i = len(groups)
			index[key] = i
			groups = append(groups, DayGroup{Key: key, Label: DayLabel(started, now)})
		}
		groups[i].Runs = append(groups[i].Runs, r)
	}
	return groups
}

// Clock renders t's local time of day, "14:03", or with seconds "14:03:12".
func Clock(t time.Time, seconds bool) string {
	if seconds {
		return t.Local().Format("15:04:05")
	}
	return format.Clock(t.Local())
}

// RunDuration is how long a run took, e.g. "28 秒".
func RunDuration(r enginehist.Run) string { return format.Duration(r.EndedAt.Sub(r.StartedAt)) }

// CompletedCount counts the maintenance tasks that completed, as the 维护 done card's big number does: applied or nothing to change.
func CompletedCount(results []taskresult.Result) int {
	n := 0
	for _, r := range results {
		if r.Outcome == taskresult.Applied || r.Outcome == taskresult.Unchanged {
			n++
		}
	}
	return n
}

// RunSummary is a row's one line: for maintenance "完成 5 项，1 项未完成"; for a clean-up the space freed, what went to the Trash, the items and the failures, e.g. "释放 1.2 GB，34 项，2 项未能处理".
func RunSummary(r enginehist.Run) string {
	if r.Kind == enginehist.KindMaintenance {
		done := CompletedCount(r.Results)
		if left := len(r.Results) - done; left > 0 {
			return "完成 " + strconv.Itoa(done) + " 项，" + strconv.Itoa(left) + " 项未完成"
		}
		return "完成 " + strconv.Itoa(done) + " 项"
	}
	s := r.Summary
	if s == nil {
		return "没有记录结果"
	}
	var parts []string
	if s.Freed > 0 || s.Trashed == 0 {
		parts = append(parts, "释放 "+format.Bytes(s.Freed))
	}
	if s.Trashed > 0 {
		parts = append(parts, "移到"+format.TrashName()+" "+format.Bytes(s.Trashed))
	}
	parts = append(parts, strconv.Itoa(s.Items)+" 项")
	if s.Failed > 0 {
		parts = append(parts, strconv.Itoa(s.Failed)+" 项未能处理")
	}
	return strings.Join(parts, "，")
}

// TotalFreed adds up the space the clean-ups among runs freed for good (what went to the Trash is freed only once it is emptied); cleanups counts those runs.
func TotalFreed(runs []enginehist.Run) (bytes int64, cleanups int) {
	for _, r := range runs {
		if r.Summary == nil {
			continue
		}
		bytes += r.Summary.Freed
		cleanups++
	}
	return bytes, cleanups
}

// KindsIn lists the kinds that occur in runs, in KindOrder, unknown kinds last in the order they first occur.
func KindsIn(runs []enginehist.Run) []enginehist.Kind {
	var kinds []enginehist.Kind
	for _, r := range runs {
		if !slices.Contains(kinds, r.Kind) {
			kinds = append(kinds, r.Kind)
		}
	}
	rank := func(k enginehist.Kind) int {
		if i := slices.Index(KindOrder, k); i >= 0 {
			return i
		}
		return len(KindOrder)
	}
	slices.SortStableFunc(kinds, func(a, b enginehist.Kind) int { return rank(a) - rank(b) })
	return kinds
}

// CountText is the overview's count of the records, with the days the history keeps when the settings are known.
func CountText(n, retentionDays int, known bool) string {
	if !known {
		return "共 " + strconv.Itoa(n) + " 条记录"
	}
	return "共 " + strconv.Itoa(n) + " 条记录，保留最近 " + strconv.Itoa(retentionDays) + " 天"
}

// TaskLine is one task of a recorded maintenance run.
type TaskLine struct {
	TaskID string
	Title  string
	// Status is 已完成, 部分完成, 无需处理, 已跳过, 不可用 or 失败.
	Status string
	// Message is the task's own sentence, as the 维护 done card shows it.
	Message string
	// Attention is set when the task needs the user's attention (maintenance.IsIssue).
	Attention bool
}

// StatusLabel names how a maintenance task ended.
func StatusLabel(r taskresult.Result) string {
	switch r.Outcome {
	case taskresult.Applied:
		if r.Partial {
			return "部分完成"
		}
		return "已完成"
	case taskresult.Unchanged:
		return "无需处理"
	case taskresult.Skipped:
		return "已跳过"
	case taskresult.Unavailable:
		return "不可用"
	case taskresult.Failed:
		return "失败"
	}
	return string(r.Outcome)
}

// TaskLines lists a maintenance run's tasks as the done card orders them: those needing attention first, each part in catalog order. A task that is not in this build's catalog shows its ID.
func TaskLines(results []taskresult.Result, catalog []app.TaskView) []TaskLine {
	titles := make(map[string]string, len(catalog))
	order := make(map[string]int, len(catalog))
	for i, t := range catalog {
		titles[t.ID] = t.Title
		order[t.ID] = i
	}
	rank := func(r taskresult.Result) int {
		pos, ok := order[r.TaskID]
		if !ok {
			pos = len(catalog)
		}
		if maintenance.IsIssue(r) {
			return pos
		}
		return len(catalog) + 1 + pos
	}
	sorted := slices.Clone(results)
	slices.SortStableFunc(sorted, func(a, b taskresult.Result) int { return rank(a) - rank(b) })
	lines := make([]TaskLine, len(sorted))
	for i, r := range sorted {
		title, ok := titles[r.TaskID]
		if !ok {
			title = r.TaskID
		}
		lines[i] = TaskLine{TaskID: r.TaskID, Title: title, Status: StatusLabel(r), Message: r.Message, Attention: maintenance.IsIssue(r)}
	}
	return lines
}
