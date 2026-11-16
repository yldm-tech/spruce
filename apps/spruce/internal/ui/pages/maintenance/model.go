package maintenance

import (
	"slices"
	"strconv"
	"strings"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/engine/taskresult"
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

// Unfinished returns the IDs of the tasks a run did not finish and that running again may finish: those needing attention, but for a task this computer does not have and for an admin task stopped while it may still be running as root.
func Unfinished(results []taskresult.Result) []string {
	var ids []string
	for _, r := range results {
		if IsIssue(r) && r.Outcome != taskresult.Unavailable && !maintenance.MayStillRun(r) {
			ids = append(ids, r.TaskID)
		}
	}
	return ids
}

// TaskLine is one task of a run, as the done card and 操作历史 list it.
type TaskLine struct {
	TaskID string
	Title  string
	// Status is 已完成, 部分完成, 无需处理, 已跳过, 不可用 or 失败.
	Status string
	// Message is the task's own sentence without a closing 「，{Status}」 that repeats the status.
	Message string
	// Attention is set when the task needs the user's attention (IsIssue).
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

// TaskLines lists a run's tasks with those needing attention first, each part in catalog order. A task that is not in this build's catalog shows its ID.
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
		if IsIssue(r) {
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
		status := StatusLabel(r)
		// 「Safari 仍在运行，已跳过」 under 已跳过 says it twice; only an ending that is the status itself goes.
		lines[i] = TaskLine{TaskID: r.TaskID, Title: title, Status: status, Message: strings.TrimSuffix(r.Message, "，"+status), Attention: IsIssue(r)}
	}
	return lines
}

// InterruptedText is the done card's line for a run that was stopped.
const InterruptedText = "维护在中途停止，没有运行的任务不会改动系统"

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
