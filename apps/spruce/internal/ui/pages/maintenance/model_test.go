package maintenance

import (
	"reflect"
	"strings"
	"testing"

	"spruce/apps/spruce/internal/app"
	enginemaint "spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/engine/taskresult"
)

func r(id string, o taskresult.Outcome, msg string) taskresult.Result {
	return taskresult.Result{TaskID: id, Outcome: o, Message: msg}
}

var testCatalog = []app.TaskView{{ID: "a", Title: "甲"}, {ID: "b", Title: "乙"}, {ID: "c", Title: "丙"}}

func TestFormatEstimate(t *testing.T) {
	for s, want := range map[int]string{0: "0 分钟", 59: "不到 1 分钟", 60: "约 1 分钟", 89: "约 1 分钟", 90: "约 2 分钟"} {
		if got := FormatEstimate(s); got != want {
			t.Errorf("FormatEstimate(%d) = %q, want %q", s, got, want)
		}
	}
}

func TestTaskLines(t *testing.T) {
	catalog := []app.TaskView{{ID: "dns", Title: "刷新 DNS 缓存"}, {ID: "icons", Title: "重建图标缓存"}, {ID: "databases", Title: "优化应用数据库"}}
	partial := r("icons", taskresult.Applied, "注销后生效")
	partial.Partial = true
	lines := TaskLines([]taskresult.Result{
		r("dns", taskresult.Applied, "已刷新 DNS 缓存"),
		partial,
		r("databases", taskresult.Skipped, "Safari 仍在运行，已跳过"),
		r("gone", taskresult.Unavailable, "这台电脑上没有这项"),
	}, catalog)
	var got []string
	for _, l := range lines {
		got = append(got, strings.Join([]string{l.Title, l.Status, l.Message}, "|")+map[bool]string{true: "|!", false: ""}[l.Attention])
	}
	want := []string{
		"重建图标缓存|部分完成|注销后生效|!",
		"优化应用数据库|已跳过|Safari 仍在运行|!",
		"gone|不可用|这台电脑上没有这项|!",
		"刷新 DNS 缓存|已完成|已刷新 DNS 缓存",
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("TaskLines = %q", got)
	}
}

func TestStatusLabels(t *testing.T) {
	for o, want := range map[taskresult.Outcome]string{taskresult.Unchanged: "无需处理", taskresult.Failed: "失败", "odd": "odd"} {
		if got := StatusLabel(taskresult.Result{Outcome: o}); got != want {
			t.Errorf("StatusLabel(%s) = %q", o, got)
		}
	}
}

func TestUnfinished(t *testing.T) {
	partial := r("b", taskresult.Applied, "另有 2 个未能清除")
	partial.Partial = true
	got := Unfinished([]taskresult.Result{r("a", taskresult.Applied, "好"), partial, r("c", taskresult.Unchanged, ""), r("d", taskresult.Skipped, "取消"),
		r("e", taskresult.Unavailable, "没有"), r("f", taskresult.Failed, "坏"), r("g", taskresult.Skipped, enginemaint.StoppedUncertain)})
	if !reflect.DeepEqual(got, []string{"b", "d", "f"}) {
		t.Errorf("Unfinished = %q", got)
	}
}

func TestFullDiskAccessBlocked(t *testing.T) {
	fda := r("databases", taskresult.Skipped, "")
	fda.Reason = taskresult.ReasonFullDiskAccess
	if !FullDiskAccessBlocked([]taskresult.Result{fda}) {
		t.Error("a skip for the permission does not count")
	}
	for _, other := range []taskresult.Result{
		r("databases", taskresult.Skipped, "Safari 仍在运行，已跳过"), r("databases", taskresult.Unavailable, ""),
		r("databases", taskresult.Failed, ""), r("dns", taskresult.Skipped, ""),
	} {
		if FullDiskAccessBlocked([]taskresult.Result{other}) {
			t.Errorf("%+v counts as blocked by the permission", other)
		}
	}
}

func TestDoneTitle(t *testing.T) {
	s := app.RunSummary{Results: []taskresult.Result{r("a", taskresult.Applied, "好"), r("b", taskresult.Skipped, "取消")}, Applied: 1, Skipped: 1}
	if DoneTitle(s) != "维护完成" || OKCount(s) != 1 {
		t.Errorf("title %q, ok %d", DoneTitle(s), OKCount(s))
	}
	s.Interrupted = true
	if DoneTitle(s) != "维护已中断" {
		t.Errorf("interrupted: %q", DoneTitle(s))
	}
	if DoneTitle(app.RunSummary{Skipped: 1}) != "维护未完成" {
		t.Error("nothing done")
	}
}

func TestProgress(t *testing.T) {
	tasks := []app.TaskView{{ID: "dns", Title: "刷新 DNS 缓存", NeedsAdmin: true}, {ID: "icons", Title: "重建图标缓存", NeedsAdmin: true}, {ID: "ql", Title: "刷新 QuickLook 预览"}}
	p := RunProgress{Total: 3}
	if LiveText(tasks, p) != "正在维护系统…，共 3 项任务" || RunStatusText(tasks, p) != "正在准备…" || !AwaitingAdmin(tasks, p) {
		t.Errorf("before: %q %q", LiveText(tasks, p), RunStatusText(tasks, p))
	}
	p = ApplyProgress(p, app.ProgressEvent{Kind: app.ProgressStarted, TaskID: "dns", Done: 0, Total: 3})
	if RunStatusText(tasks, p) != "刷新 DNS 缓存 · 已完成 0/3" || LiveText(tasks, p) != RunStatusText(tasks, p) {
		t.Errorf("started: %q", RunStatusText(tasks, p))
	}
	before := p
	p = ApplyProgress(p, app.ProgressEvent{Kind: app.ProgressFinished, TaskID: "dns", Done: 1, Total: 3})
	p = ApplyProgress(p, app.ProgressEvent{Kind: app.ProgressFinished, TaskID: "icons", Done: 2, Total: 3})
	if AwaitingAdmin(tasks, p) || len(before.Finished) != 0 {
		t.Errorf("admin tasks finished but still awaiting, or the old progress changed: %+v %+v", p, before)
	}
	if RunSub(tasks, p) != "共 3 项任务，不会动你的文档和照片" {
		t.Errorf("sub %q", RunSub(tasks, p))
	}
}
