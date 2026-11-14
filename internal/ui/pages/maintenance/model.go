package maintenance

import (
	"slices"
	"strconv"
	"strings"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// fullDiskAccessTasks are the tasks that read other apps' data under ~/Library and so need Full Disk Access on macOS. The Windows catalog has none of these IDs.
var fullDiskAccessTasks = map[string]bool{"databases": true}

// NeedsFullDiskAccess reports whether the task with ID id needs Full Disk Access.
func NeedsFullDiskAccess(id string) bool { return fullDiskAccessTasks[id] }

// FormatEstimate turns the summed EstimatedSeconds of the selected tasks into the footer's 预计用时.
func FormatEstimate(seconds int) string {
	switch {
	case seconds <= 0:
		return "0 分钟"
	case seconds < 60:
		return "不到 1 分钟"
	}
	return "约 " + strconv.Itoa(int(float64(seconds)/60+0.5)) + " 分钟"
}

// OKCount is the done card's big number: the tasks that applied a change or found nothing to change.
func OKCount(s app.RunSummary) int { return s.Applied + s.Unchanged }

// IssueLine is a line of the done card: the tasks that share one message (so a declined admin prompt reads as one line) and that message.
type IssueLine struct {
	Titles  []string
	Message string
}

// String renders the line, e.g. "优化应用数据库：Safari 仍在运行，已跳过".
func (l IssueLine) String() string { return strings.Join(l.Titles, "、") + "：" + l.Message }

// IsIssue reports whether a result needs the user's attention: it did not complete, or it completed only in part and its message says what is left.
func IsIssue(r taskresult.Result) bool {
	switch r.Outcome {
	case taskresult.Unchanged:
		return false
	case taskresult.Applied:
		return r.Partial
	}
	return true
}

// IssueLines lists the results that need attention, in catalog order, grouped by message.
func IssueLines(results []taskresult.Result, catalog []app.TaskView) []IssueLine {
	return groupLines(slices.DeleteFunc(slices.Clone(results), func(r taskresult.Result) bool { return !IsIssue(r) }), catalog)
}

// CompletedLines lists the results that completed with nothing left to do, in catalog order, grouped by message: some carry a hint such as logging out and in again.
func CompletedLines(results []taskresult.Result, catalog []app.TaskView) []IssueLine {
	return groupLines(slices.DeleteFunc(slices.Clone(results), IsIssue), catalog)
}

func groupLines(results []taskresult.Result, catalog []app.TaskView) []IssueLine {
	titles := map[string]string{}
	order := map[string]int{}
	for i, t := range catalog {
		titles[t.ID], order[t.ID] = t.Title, i
	}
	rank := func(r taskresult.Result) int {
		if i, ok := order[r.TaskID]; ok {
			return i
		}
		return len(catalog)
	}
	slices.SortStableFunc(results, func(a, b taskresult.Result) int { return rank(a) - rank(b) })
	var lines []IssueLine
	byMessage := map[string]int{}
	for _, r := range results {
		title := r.TaskID
		if t, ok := titles[r.TaskID]; ok {
			title = t
		}
		if i, ok := byMessage[r.Message]; ok {
			lines[i].Titles = append(lines[i].Titles, title)
			continue
		}
		byMessage[r.Message] = len(lines)
		lines = append(lines, IssueLine{Titles: []string{title}, Message: r.Message})
	}
	return lines
}

// DoneLines are the done card's lines for a run: the issues first, then every completed task's own message, and a note when the run stopped early.
func DoneLines(s app.RunSummary, catalog []app.TaskView) []string {
	var out []string
	for _, l := range append(IssueLines(s.Results, catalog), CompletedLines(s.Results, catalog)...) {
		out = append(out, l.String())
	}
	if s.Interrupted {
		out = append(out, "维护在中途停止，没有运行的任务不会改动系统")
	}
	return out
}

// DoneTitle is the done card's heading.
func DoneTitle(s app.RunSummary) string {
	switch {
	case s.Interrupted:
		return "维护已中断"
	case OKCount(s) == 0:
		return "维护未完成"
	}
	return "维护完成"
}

// FullDiskAccessBlocked reports whether a run left a task undone because Full Disk Access was missing, so the done card offers 去授权. A skip for another reason, such as Safari still running, does not count.
func FullDiskAccessBlocked(results []taskresult.Result) bool {
	return slices.ContainsFunc(results, func(r taskresult.Result) bool { return r.Reason == taskresult.ReasonFullDiskAccess })
}

// RunProgress is how far a run got.
type RunProgress struct {
	// Current is the task that started last, "" before the first.
	Current string
	// Finished holds the IDs of the tasks that finished.
	Finished    map[string]bool
	Done, Total int
}

// ApplyProgress returns p after ev.
func ApplyProgress(p RunProgress, ev app.ProgressEvent) RunProgress {
	if ev.Kind == app.ProgressStarted {
		p.Current, p.Done, p.Total = ev.TaskID, ev.Done, ev.Total
		return p
	}
	finished := make(map[string]bool, len(p.Finished)+1)
	for id := range p.Finished {
		finished[id] = true
	}
	finished[ev.TaskID] = true
	p.Finished, p.Done, p.Total = finished, ev.Done, ev.Total
	return p
}

// total is the run's task count: the progress's, or the selection's before the first event.
func total(tasks []app.TaskView, p RunProgress) int {
	if p.Total > 0 {
		return p.Total
	}
	return len(tasks)
}

// RunStatusText is the progress line of a run, e.g. "刷新 DNS 缓存 · 已完成 1/5", or "正在准备…" before the first task starts.
func RunStatusText(tasks []app.TaskView, p RunProgress) string {
	for _, t := range tasks {
		if t.ID == p.Current {
			return t.Title + " · 已完成 " + strconv.Itoa(p.Done) + "/" + strconv.Itoa(total(tasks, p))
		}
	}
	return "正在准备…"
}

// LiveText is what the page's status line announces while a run goes on.
func LiveText(tasks []app.TaskView, p RunProgress) string {
	if p.Current == "" {
		return "正在维护系统…，共 " + strconv.Itoa(total(tasks, p)) + " 项任务"
	}
	return RunStatusText(tasks, p)
}

// RunSub is the run overlay's second line.
func RunSub(tasks []app.TaskView, p RunProgress) string {
	return "共 " + strconv.Itoa(total(tasks, p)) + " 项任务，不会动你的文档和照片"
}

// AwaitingAdmin reports whether an admin task has yet to finish: admin tasks run first, together behind one system prompt, so until they finish the user may have a prompt to answer.
func AwaitingAdmin(tasks []app.TaskView, p RunProgress) bool {
	return slices.ContainsFunc(tasks, func(t app.TaskView) bool { return t.NeedsAdmin && !p.Finished[t.ID] })
}
