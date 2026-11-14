package main

import (
	"testing"
	"time"

	enginehist "github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
	"github.com/yldm-tech/spruce/internal/ui/pages/history"
	"github.com/yldm-tech/spruce/internal/ui/pages/history/historyfake"
	"github.com/yldm-tech/spruce/internal/ui/pages/maintenance/maintenancefake"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// today is a local time today, days back.
func today(days, h, m, s int) time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day()-days, h, m, s, 0, time.Local)
}

// screenRun is a run of kind starting at start and lasting seconds.
func screenRun(id string, kind enginehist.Kind, start time.Time, seconds int) enginehist.Run {
	return enginehist.Run{ID: id, Kind: kind, StartedAt: start, EndedAt: start.Add(time.Duration(seconds) * time.Second)}
}

// maintenanceRuns are the reference screenshot's three maintenance runs of today.
func maintenanceRuns() []enginehist.Run {
	ok := func(id, msg string) taskresult.Result {
		return taskresult.Result{TaskID: id, Outcome: taskresult.Applied, Message: msg}
	}
	a := screenRun("m3", enginehist.KindMaintenance, today(0, 9, 50, 12), 31)
	a.Results = []taskresult.Result{ok("dns", "已刷新 DNS 缓存"), ok("quicklook", "已刷新 QuickLook"),
		{TaskID: "launchservices", Outcome: taskresult.Unchanged, Message: "没有需要清理的登记"},
		{TaskID: "databases", Outcome: taskresult.Skipped, Message: "Safari 仍在运行，已跳过"}}
	b := screenRun("m2", enginehist.KindMaintenance, today(0, 2, 20, 0), 4)
	b.Results = []taskresult.Result{ok("dns", "已刷新 DNS 缓存")}
	c := screenRun("m1", enginehist.KindMaintenance, today(0, 1, 38, 0), 4)
	c.Results = []taskresult.Result{ok("dns", "已刷新 DNS 缓存")}
	return []enginehist.Run{a, b, c}
}

// mixedRuns add clean-ups of other kinds and days to the maintenance runs.
func mixedRuns() []enginehist.Run {
	runs := maintenanceRuns()
	junk := screenRun("j1", enginehist.KindSystemJunk, today(0, 8, 12, 0), 6)
	junk.Summary = &enginehist.Totals{Freed: 1_234_000_000, Items: 34, Failed: 2}
	junk.Details = []string{"系统缓存：已清理 30 项", "系统日志：已清理 4 项", "无法删除 ~/Library/Caches/com.example.app：文件正在使用，已跳过"}
	dev := screenRun("d1", enginehist.KindDevJunk, today(1, 21, 5, 0), 12)
	dev.Summary = &enginehist.Totals{Freed: 3_480_000_000, Items: 1260}
	dev.Details = []string{"Xcode DerivedData：已清理 1200 项", "npm 缓存：已清理 60 项"}
	app := screenRun("u1", enginehist.KindUninstall, today(1, 17, 30, 0), 3)
	app.Summary = &enginehist.Totals{Freed: 120_000_000, Trashed: 2_100_000_000, Items: 3}
	app.Details = []string{"Xcode：已移到废纸篓 3 项"}
	return []enginehist.Run{runs[0], junk, runs[1], runs[2], dev, app}
}

// TestScreensHistory shows the 操作历史 page as the reference does: three maintenance runs of today.
func TestScreensHistory(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Runs = maintenanceRuns()
	a := screensApp(t, fakes, shell.Deps{History: history.Deps{Catalog: historyfake.Catalog(maintenancefake.MacCatalog())}}, "history")
	a.WaitFor("id:run-m1", 10*time.Second)
	a.WaitUntil(10*time.Second, func() bool { return a.Find("id:history-count").Text == "共 3 条记录，保留最近 180 天" })
	a.Screenshot("history-idle")
}

// TestScreensHistoryFilled shows runs of several kinds over two days with two rows open, a filter, the clear dialog and the empty page after it.
func TestScreensHistoryFilled(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Runs = mixedRuns()
	a := screensApp(t, fakes, shell.Deps{History: history.Deps{Catalog: historyfake.Catalog(maintenancefake.MacCatalog())}}, "history")
	a.WaitFor("id:chip-all", 10*time.Second)
	a.Press("id:run-m3-head")
	a.Press("id:run-j1-head")
	a.WaitFor("id:run-j1 >> id:run-details", 10*time.Second)
	a.Screenshot("history-filled")

	a.Press("id:chip-devJunk")
	a.WaitForGone("id:run-m3", 10*time.Second)
	a.Screenshot("history-filtered")
	a.Press("id:chip-all")
	a.WaitFor("id:run-m3", 10*time.Second)

	a.Press("id:clear-history")
	a.WaitFor("id:clear-dialog", 10*time.Second)
	a.Screenshot("history-clear")
	a.Press("id:confirm-ok")
	a.WaitFor("id:history-empty", 10*time.Second)
	a.WaitForGone("id:clear-dialog", 10*time.Second)
	a.Screenshot("history-empty")
}
