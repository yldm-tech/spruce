package main

import (
	"context"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
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

// undoRuns are today's clean-ups that moved items to the Trash, an automatic rule's run among them, with one earlier 放回原处.
func undoRuns() []enginehist.Run {
	inst := screenRun("t1", enginehist.KindInstallers, today(0, 10, 4, 0), 3)
	inst.Summary = &enginehist.Totals{Trashed: 2_140_000_000, Items: 3}
	inst.Undo = &enginehist.UndoInfo{Items: 3, Bytes: 2_140_000_000}
	inst.Details = []string{"安装包：已移到废纸篓 3 项"}
	smart := screenRun("s1", enginehist.KindSmartScan, today(0, 9, 31, 0), 14)
	smart.Summary = &enginehist.Totals{Freed: 17_200_000_000, Trashed: 3_100_000_000, Items: 41}
	smart.Undo = &enginehist.UndoInfo{Items: 6, Bytes: 3_100_000_000}
	smart.Details = []string{"Xcode 编译缓存：已清理 1 项，释放 17.2 GB", "安装包：已移到废纸篓 6 项", "这次清理里直接删除的 35 项无法放回"}
	auto := screenRun("a1", enginehist.KindAuto, today(0, 3, 12, 0), 21)
	auto.Rule = "app-caches"
	auto.Summary = &enginehist.Totals{Trashed: 4_200_000_000, Items: 12}
	auto.Undo = &enginehist.UndoInfo{Items: 12, Bytes: 4_200_000_000}
	auto.Details = []string{"旧的应用缓存：12 项（4.2 GB）"}
	space := screenRun("p1", enginehist.KindSpace, today(1, 18, 40, 0), 1)
	space.Summary = &enginehist.Totals{Trashed: 820_000_000, Items: 1}
	space.Undo = &enginehist.UndoInfo{Items: 1, Bytes: 820_000_000}
	space.Details = []string{"已移到废纸篓：~/Movies/old-export.mov"}
	back := screenRun("r0", enginehist.KindRestore, today(1, 18, 45, 0), 1)
	back.RestoreOf = "p1"
	back.Summary = &enginehist.Totals{Items: 1, Trashed: 820_000_000}
	return []enginehist.Run{inst, smart, auto, back, space}
}

// TestScreensHistoryRestore shows 放回原处 on recent moves to the Trash, the row and toast after putting one back, the 自动清理 filter and a 放回原处 record that left items in the Trash.
func TestScreensHistoryRestore(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Runs = undoRuns()
	fakes.SetRestorable("t1", "s1", "a1")
	ov := storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	ov.OnUndo = func(_ context.Context, runID string) (app.RestoreOutcome, error) {
		r := screenRun("r1", enginehist.KindRestore, time.Now(), 1)
		r.RestoreOf = runID
		r.Summary = &enginehist.Totals{Items: 2, Trashed: 1_400_000_000, Failed: 1}
		r.Details = []string{"~/Downloads/Figma.dmg：原位置已有同名项目"}
		// The restore tried every item, so the run is no longer offered, though one stayed in the Trash.
		fakes.SetRestorable("s1", "a1")
		fakes.Record(r)
		return app.RestoreOutcome{RunID: "r1", Restored: 2, Failed: 1, Bytes: 1_400_000_000}, nil
	}
	fakes.Overview = ov
	a := screensApp(t, fakes, shell.Deps{History: history.Deps{Catalog: historyfake.Catalog(maintenancefake.MacCatalog())}}, "history")
	a.WaitFor("id:run-t1-restore", 10*time.Second)
	a.WaitFor("id:run-p1-restored", 10*time.Second)
	a.Screenshot("history-restore")

	a.Press("id:run-t1-restore")
	a.WaitFor("id:run-t1-restored", 10*time.Second)
	a.WaitFor("id:toast", 10*time.Second)
	a.WaitFor("id:run-r1", 10*time.Second)
	a.WaitForGone("id:run-t1-restore", 10*time.Second)
	a.Screenshot("history-restored")

	// 查看 in the toast, pressed while 操作历史 is open, expands the restore's record.
	a.Press("id:toast-details")
	a.WaitFor("id:run-r1 >> id:run-details", 10*time.Second)
	a.Find("id:run-r1 >> text:~/Downloads/Figma.dmg：原位置已有同名项目")

	a.Press("id:chip-auto")
	a.WaitForGone("id:run-t1", 10*time.Second)
	a.Press("id:run-a1-head")
	a.WaitFor("id:run-a1 >> id:run-details", 10*time.Second)
	a.Screenshot("history-auto")

	a.Press("id:chip-restore")
	a.WaitFor("id:run-r1", 10*time.Second)
	// 查看 asked for it, so it opens again with the filter.
	a.WaitFor("id:run-r1 >> id:run-details", 10*time.Second)
	a.Screenshot("history-restore-kind")
}
