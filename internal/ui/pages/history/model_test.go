package history

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	enginehist "github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit/tone"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

func at(y int, m time.Month, d, h, minute int) time.Time {
	return time.Date(y, m, d, h, minute, 0, 0, time.Local)
}

func run(id string, kind enginehist.Kind, started time.Time) enginehist.Run {
	return enginehist.Run{ID: id, Kind: kind, StartedAt: started, EndedAt: started}
}

func result(id string, o taskresult.Outcome, message string, partial bool) taskresult.Result {
	return taskresult.Result{TaskID: id, Outcome: o, Message: message, Partial: partial}
}

var now = at(2026, time.November, 14, 15, 0)

func TestGroupByDay(t *testing.T) {
	runs := []enginehist.Run{
		run("a", enginehist.KindMaintenance, at(2026, 11, 14, 14, 30)),
		run("b", enginehist.KindSystemJunk, at(2026, 11, 14, 0, 5)),
		run("c", enginehist.KindDevJunk, at(2026, 11, 13, 23, 50)),
		run("d", enginehist.KindInstallers, at(2026, 11, 2, 9, 30)),
		// Stored in UTC, as runs read back from the file are; grouping goes by the local day.
		run("e", enginehist.KindSpace, at(2025, 11, 2, 9, 30).UTC()),
	}
	var got []string
	keys := map[string]bool{}
	for _, g := range GroupByDay(runs, now) {
		ids := ""
		for _, r := range g.Runs {
			ids += r.ID
		}
		got = append(got, g.Label+":"+ids)
		keys[g.Key] = true
	}
	want := []string{"今天:ab", "昨天:c", "11月2日:d", "2025年11月2日:e"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("groups %q, want %q", got, want)
	}
	if len(keys) != 4 {
		t.Errorf("keys %v", keys)
	}
}

func TestDayLabelAcrossAMonth(t *testing.T) {
	first := at(2026, time.December, 1, 1, 0)
	if got := DayLabel(at(2026, time.November, 30, 22, 0), first); got != "昨天" {
		t.Errorf("Nov 30 = %q", got)
	}
	if got := DayLabel(at(2026, time.November, 29, 22, 0), first); got != "11月29日" {
		t.Errorf("Nov 29 = %q", got)
	}
}

func TestRunSummaryMaintenance(t *testing.T) {
	results := []taskresult.Result{
		result("dns", taskresult.Applied, "已刷新 DNS 缓存", false),
		result("icons", taskresult.Applied, "需要重新登录", true),
		result("quicklook", taskresult.Unchanged, "无需处理", false),
		result("databases", taskresult.Skipped, "Safari 仍在运行，已跳过", false),
		result("spotlight", taskresult.Failed, "失败了", false),
	}
	r := run("m", enginehist.KindMaintenance, now)
	r.Results = results
	if got := RunSummary(r); got != "完成 3 项，2 项未完成" {
		t.Errorf("all = %q", got)
	}
	r.Results = results[:3]
	if got := RunSummary(r); got != "完成 3 项" {
		t.Errorf("done = %q", got)
	}
}

func TestRunSummaryCleanUp(t *testing.T) {
	with := func(kind enginehist.Kind, s enginehist.Totals) enginehist.Run {
		r := run("x", kind, now)
		r.Summary = &s
		return r
	}
	trash := format.TrashName()
	for _, c := range []struct {
		run  enginehist.Run
		want string
	}{
		{with(enginehist.KindSystemJunk, enginehist.Totals{Freed: 1_234_000_000, Items: 34, Failed: 2}),
			"释放 " + format.Bytes(1_234_000_000) + "，34 项，2 项未能处理"},
		{with(enginehist.KindUninstall, enginehist.Totals{Trashed: 300_000_000, Items: 3}),
			"移到" + trash + " " + format.Bytes(300_000_000) + "，3 项"},
		{with(enginehist.KindSmartScan, enginehist.Totals{Freed: 5_000_000, Trashed: 2_000_000, Items: 9}),
			"释放 " + format.Bytes(5_000_000) + "，移到" + trash + " " + format.Bytes(2_000_000) + "，9 项"},
		{with(enginehist.KindSpace, enginehist.Totals{Failed: 1}), "释放 0 B，0 项，1 项未能处理"},
		{run("y", enginehist.KindDevJunk, now), "没有记录结果"},
	} {
		if got := RunSummary(c.run); got != c.want {
			t.Errorf("RunSummary = %q, want %q", got, c.want)
		}
	}
	// The design's figures, in Finder's units.
	if got := format.BytesIn(1_234_000_000, false); got != "1.2 GB" {
		t.Errorf("1.2 GB = %q", got)
	}
}

func TestTotalFreed(t *testing.T) {
	with := func(id string, kind enginehist.Kind, s enginehist.Totals) enginehist.Run {
		r := run(id, kind, now)
		r.Summary = &s
		return r
	}
	runs := []enginehist.Run{
		run("m", enginehist.KindMaintenance, now),
		with("a", enginehist.KindSystemJunk, enginehist.Totals{Freed: 1_000_000, Items: 1}),
		with("b", enginehist.KindUninstall, enginehist.Totals{Trashed: 9_000_000, Items: 1}),
		with("c", enginehist.KindDevJunk, enginehist.Totals{Freed: 2_000_000, Items: 1}),
	}
	if bytes, n := TotalFreed(runs); bytes != 3_000_000 || n != 3 {
		t.Errorf("TotalFreed = %d, %d", bytes, n)
	}
	if bytes, n := TotalFreed(runs[:1]); bytes != 0 || n != 0 {
		t.Errorf("TotalFreed of maintenance = %d, %d", bytes, n)
	}
}

func TestKindsIn(t *testing.T) {
	got := KindsIn([]enginehist.Run{
		run("1", enginehist.KindMaintenance, now),
		run("2", enginehist.KindSpace, now),
		run("3", "future", now),
		run("4", enginehist.KindSmartScan, now),
		run("5", enginehist.KindSpace, now),
	})
	want := []enginehist.Kind{enginehist.KindSmartScan, enginehist.KindSpace, enginehist.KindMaintenance, "future"}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("KindsIn = %v", got)
	}
}

func TestKindMeta(t *testing.T) {
	for kind, want := range map[enginehist.Kind]string{enginehist.KindDevJunk: "开发者", enginehist.KindMaintenance: "维护", "future": "其他操作"} {
		if got := KindMeta(kind).Title; got != want {
			t.Errorf("KindMeta(%s) = %q", kind, got)
		}
	}
	if KindMeta("future").Glyph != "page/history" {
		t.Error("an unknown kind does not show the history glyph")
	}
}

func TestTaskLines(t *testing.T) {
	catalog := []app.TaskView{{ID: "dns", Title: "刷新 DNS 缓存"}, {ID: "icons", Title: "重建图标缓存"}, {ID: "databases", Title: "优化应用数据库"}}
	lines := TaskLines([]taskresult.Result{
		result("dns", taskresult.Applied, "已刷新 DNS 缓存", false),
		result("icons", taskresult.Applied, "注销后生效", true),
		result("databases", taskresult.Skipped, "Safari 仍在运行，已跳过", false),
		result("gone", taskresult.Unavailable, "这台电脑上没有这项", false),
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

func TestTimes(t *testing.T) {
	start := time.Date(2026, time.November, 14, 9, 5, 7, 0, time.Local)
	r := enginehist.Run{StartedAt: start.UTC(), EndedAt: start.Add(28 * time.Second).UTC()}
	if got := Clock(r.StartedAt, false); got != "09:05" {
		t.Errorf("clock %q", got)
	}
	if got := Clock(r.StartedAt, true); got != "09:05:07" {
		t.Errorf("clock with seconds %q", got)
	}
	if got := RunDuration(r); got != "28 秒" {
		t.Errorf("duration %q", got)
	}
}

func TestCountText(t *testing.T) {
	if got := CountText(3, 180, true); got != "共 3 条记录，保留最近 180 天" {
		t.Errorf("known %q", got)
	}
	if got := CountText(3, 0, false); got != "共 3 条记录" {
		t.Errorf("unknown %q", got)
	}
}

func TestNewKindsMetaTitlesAndOrder(t *testing.T) {
	for kind, want := range map[enginehist.Kind]nav.Meta{
		enginehist.KindAuto:    {Title: "自动清理", Tone: tone.Green, Glyph: "check"},
		enginehist.KindRestore: {Title: "放回原处", Tone: tone.Slate, Glyph: "undo"},
	} {
		if got := KindMeta(kind); got.Title != want.Title || got.Tone != want.Tone || got.Glyph != want.Glyph {
			t.Errorf("KindMeta(%s) = %+v", kind, got)
		}
	}
	if m := KindMeta(enginehist.KindSmartScan); m.Title != "清理建议" || m.Glyph != "page/overview" {
		t.Errorf("smartScan meta %+v", m)
	}
	got := KindsIn([]enginehist.Run{run("1", enginehist.KindRestore, now), run("2", enginehist.KindSystemJunk, now), run("3", enginehist.KindAuto, now),
		run("4", enginehist.KindSmartScan, now)})
	want := []enginehist.Kind{enginehist.KindSmartScan, enginehist.KindAuto, enginehist.KindSystemJunk, enginehist.KindRestore}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("KindsIn = %v", got)
	}
	auto := run("a", enginehist.KindAuto, now)
	for rule, title := range map[string]string{"app-caches": "旧的应用缓存", "installers": "旧安装包", "build-output": "闲置项目的构建产物",
		"incomplete-downloads": "未完成的下载", "": "自动清理", "future": "自动清理"} {
		auto.Rule = rule
		if got := RunTitle(auto); got != title {
			t.Errorf("RunTitle(rule %q) = %q", rule, got)
		}
	}
	if got := RunTitle(run("j", enginehist.KindSystemJunk, now)); got != "系统垃圾" {
		t.Errorf("RunTitle = %q", got)
	}
}

func TestRestoreSummaryNoteAndToast(t *testing.T) {
	r := run("r", enginehist.KindRestore, now)
	r.Summary = &enginehist.Totals{Items: 3, Trashed: 3_100_000_000}
	if got := RunSummary(r); got != "放回 3 项（"+format.Bytes(3_100_000_000)+"）" {
		t.Errorf("summary %q", got)
	}
	r.Summary.Failed = 2
	if got := RunSummary(r); got != "放回 3 项（"+format.Bytes(3_100_000_000)+"），2 项没能放回" {
		t.Errorf("summary %q", got)
	}

	moved := run("m", enginehist.KindInstallers, now)
	partial := run("p1", enginehist.KindRestore, now)
	partial.RestoreOf, partial.Summary = "m", &enginehist.Totals{Items: 1, Failed: 2}
	other := run("p2", enginehist.KindRestore, now)
	other.RestoreOf, other.Summary = "elsewhere", &enginehist.Totals{Items: 5}
	if got := RestoredNote(moved, []enginehist.Run{other}); got != "" {
		t.Errorf("note before any restore %q", got)
	}
	if got := RestoredNote(moved, []enginehist.Run{partial, other}); got != " · 已放回 1 项" {
		t.Errorf("partial note %q", got)
	}
	whole := run("p3", enginehist.KindRestore, now)
	whole.RestoreOf, whole.Summary = "m", &enginehist.Totals{Items: 2}
	if got := RestoredNote(moved, []enginehist.Run{whole, partial}); got != " · 已放回原处" {
		t.Errorf("whole note %q", got)
	}

	if text, view, d := RestoreToast(app.RestoreOutcome{RunID: "x", Restored: 4}); text != "已放回原处 4 项" || view != "" || d != 6*time.Second {
		t.Errorf("toast %q %q %v", text, view, d)
	}
	if text, view, d := RestoreToast(app.RestoreOutcome{RunID: "x", Restored: 1, Failed: 2}); text != "已放回 1 项，2 项没能放回" || view != "x" || d != 10*time.Second {
		t.Errorf("toast %q %q %v", text, view, d)
	}
}
