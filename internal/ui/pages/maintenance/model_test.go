package maintenance

import (
	"reflect"
	"testing"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
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

func TestIssueLines(t *testing.T) {
	got := IssueLines([]taskresult.Result{
		r("a", taskresult.Skipped, "取消"), r("b", taskresult.Applied, "好"), r("c", taskresult.Skipped, "取消"), r("x", taskresult.Failed, "坏"),
	}, testCatalog)
	want := []IssueLine{{Titles: []string{"甲", "丙"}, Message: "取消"}, {Titles: []string{"x"}, Message: "坏"}}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("IssueLines = %v, want %v", got, want)
	}
}

func TestPartialIsAnIssueAndCompletedKeepCatalogOrder(t *testing.T) {
	partial := r("b", taskresult.Applied, "已清除，另有 2 个未能清除")
	partial.Partial = true
	results := []taskresult.Result{r("c", taskresult.Unchanged, "无需处理"), partial, r("a", taskresult.Applied, "已完成；注销后生效")}
	if got, want := IssueLines(results, testCatalog), []IssueLine{{Titles: []string{"乙"}, Message: "已清除，另有 2 个未能清除"}}; !reflect.DeepEqual(got, want) {
		t.Errorf("IssueLines = %v", got)
	}
	want := []IssueLine{{Titles: []string{"甲"}, Message: "已完成；注销后生效"}, {Titles: []string{"丙"}, Message: "无需处理"}}
	if got := CompletedLines(results, testCatalog); !reflect.DeepEqual(got, want) {
		t.Errorf("CompletedLines = %v", got)
	}
	if got := CompletedLines([]taskresult.Result{r("a", taskresult.Applied, "好"), r("b", taskresult.Skipped, "取消"), r("c", taskresult.Applied, "好")}, testCatalog); !reflect.DeepEqual(got, []IssueLine{{Titles: []string{"甲", "丙"}, Message: "好"}}) {
		t.Errorf("grouped CompletedLines = %v", got)
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

func TestDoneTitleAndLines(t *testing.T) {
	s := app.RunSummary{Results: []taskresult.Result{r("a", taskresult.Applied, "好"), r("b", taskresult.Skipped, "取消")}, Applied: 1, Skipped: 1}
	if DoneTitle(s) != "维护完成" || OKCount(s) != 1 {
		t.Errorf("title %q, ok %d", DoneTitle(s), OKCount(s))
	}
	if got := DoneLines(s, testCatalog); !reflect.DeepEqual(got, []string{"乙：取消", "甲：好"}) {
		t.Errorf("DoneLines = %q", got)
	}
	s.Interrupted = true
	if DoneTitle(s) != "维护已中断" || DoneLines(s, testCatalog)[2] != "维护在中途停止，没有运行的任务不会改动系统" {
		t.Errorf("interrupted: %q %q", DoneTitle(s), DoneLines(s, testCatalog))
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
