package history

import (
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"spruce/apps/spruce/internal/app"
	enginehist "spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/rules"
	"spruce/apps/spruce/internal/engine/taskresult"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit/tone"
	"spruce/apps/spruce/internal/ui/nav"
)

// Limit is how many runs the page asks for: everything the store keeps.
const Limit = enginehist.MaxRuns

// KindOrder lists the kinds in the order the filter chips show them: the sidebar's, 自动清理 after 清理建议 and 放回原处 last.
var KindOrder = []enginehist.Kind{
	enginehist.KindSmartScan,
	enginehist.KindAuto,
	enginehist.KindSystemJunk,
	enginehist.KindDevJunk,
	enginehist.KindInstallers,
	enginehist.KindUninstall,
	enginehist.KindSpace,
	enginehist.KindMaintenance,
	enginehist.KindRestore,
}

var kindRoutes = map[enginehist.Kind]nav.Route{
	enginehist.KindMaintenance: nav.Maintenance,
	enginehist.KindSystemJunk:  nav.SystemJunk,
	enginehist.KindDevJunk:     nav.DevJunk,
	enginehist.KindInstallers:  nav.Installers,
	enginehist.KindUninstall:   nav.Uninstall,
	enginehist.KindSpace:       nav.Space,
	enginehist.KindSmartScan:   nav.Overview,
}

// kindMetas are the kinds that belong to no page of their own: the automatic rules and 放回原处.
var kindMetas = map[enginehist.Kind]nav.Meta{
	enginehist.KindAuto:    {Title: "自动清理", Tone: tone.Green, Glyph: "check"},
	enginehist.KindRestore: {Title: "放回原处", Tone: tone.Slate, Glyph: "undo"},
}

// KindMeta is the page a run's kind belongs to (its title, tile tone and glyph); 概览's cleans are titled 清理建议, as the page itself is 概览. A kind this build does not know, written by a newer version, shows as the history page's own, titled 其他操作.
func KindMeta(k enginehist.Kind) nav.Meta {
	if m, ok := kindMetas[k]; ok {
		return m
	}
	if r, ok := kindRoutes[k]; ok {
		m := r.Meta()
		if k == enginehist.KindSmartScan {
			m.Title = "清理建议"
		}
		return m
	}
	m := nav.History.Meta()
	m.Title = "其他操作"
	return m
}

// ruleTitles are the automatic rules' labels, as 设置 → 自动清理 shows them.
var ruleTitles = map[string]string{
	string(rules.AppCaches):           "旧的应用缓存",
	string(rules.Installers):          "旧安装包",
	string(rules.BuildOutput):         "闲置项目的构建产物",
	string(rules.IncompleteDownloads): "未完成的下载",
}

// RunTitle is a row's title: an automatic run's rule label, else its kind's title.
func RunTitle(r enginehist.Run) string {
	if r.Kind == enginehist.KindAuto {
		if t, ok := ruleTitles[r.Rule]; ok {
			return t
		}
	}
	return KindMeta(r.Kind).Title
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
	if r.Kind == enginehist.KindRestore {
		line := "放回 " + strconv.Itoa(s.Items) + " 项（" + format.Bytes(s.Trashed) + "）"
		if s.Failed > 0 {
			line += "，" + strconv.Itoa(s.Failed) + " 项没能放回"
		}
		return line
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
		if r.Summary == nil || r.Kind == enginehist.KindRestore {
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

// DetailPath returns the file or folder a detail line names, as a whole path with ~ for home: the path a line starts with, such as 「~/Library/Caches/x：文件正在使用，已跳过」, or the one it ends with, such as 「已移到废纸篓：~/Movies/a.mov」, where moved is set since the line names the place the item was moved from; "" when it names none.
func DetailPath(line, home string) (path string, moved bool) {
	before, after, ok := strings.Cut(line, "：")
	if !ok {
		return "", false
	}
	if p := wholePath(before, home); p != "" {
		return p, false
	}
	p := wholePath(after, home)
	return p, p != ""
}

// wholePath returns p, written with ~ for home, as a whole path; "" when p is not a path.
func wholePath(p, home string) string {
	if rest, found := strings.CutPrefix(p, "~"); found {
		if home == "" || rest != "" && !strings.HasPrefix(rest, "/") && !strings.HasPrefix(rest, `\`) {
			return ""
		}
		return filepath.Join(home, rest)
	}
	if filepath.IsAbs(p) {
		return p
	}
	return ""
}

// RestoredNote is what a run's summary line adds once 放回原处 put its items back, from the KindRestore runs among runs: 「 · 已放回原处」 when one left nothing in the Trash (a restore that stopped part-way counts what it did not get to as Failed), else 「 · 已放回 {a} 项」 with all they put back; "" before any.
func RestoredNote(run enginehist.Run, runs []enginehist.Run) string {
	restored, whole := 0, false
	for _, r := range runs {
		if r.Kind != enginehist.KindRestore || r.RestoreOf != run.ID || r.Summary == nil {
			continue
		}
		restored += r.Summary.Items
		if r.Summary.Failed == 0 && r.Summary.Items > 0 {
			whole = true
		}
	}
	switch {
	case whole:
		return " · 已放回原处"
	case restored > 0:
		return " · 已放回 " + strconv.Itoa(restored) + " 项"
	}
	return ""
}

// Restore toasts last 6 s, or 10 s with 查看.
const (
	toastShort = 6 * time.Second
	toastLong  = 10 * time.Second
)

// RestoreToast is the toast after 放回原处: 「已放回原处 {n} 项」, or 「已放回 {a} 项，{b} 项没能放回」 with 查看 (the 放回原处 run, whose details say why).
func RestoreToast(out app.RestoreOutcome) (text, viewRun string, d time.Duration) {
	if out.Failed == 0 {
		return "已放回原处 " + strconv.Itoa(out.Restored) + " 项", "", toastShort
	}
	return "已放回 " + strconv.Itoa(out.Restored) + " 项，" + strconv.Itoa(out.Failed) + " 项没能放回", out.RunID, toastLong
}
