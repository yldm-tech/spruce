package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/cleanflow/cleanflowfake"
	"spruce/apps/spruce/internal/ui/pages/systemjunk"
	"spruce/apps/spruce/internal/ui/shell"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// drillState is 概览's controller with an actionable check over cats, all of source, a few minutes old, on macOS.
func drillState(source string, cats ...cleanup.Category) app.OverviewState {
	for i := range cats {
		cats[i].ID, cats[i].Source = source+":"+cats[i].ID, source
	}
	res := cleanflowfake.Result(cats...)
	res.Sources = []cleanup.SourceState{{ID: source, Status: cleanup.SourceDone}}
	return app.OverviewState{Phase: app.OverviewIdle, CanUndo: true,
		Result: &app.OverviewResult{CheckedAt: time.Now().Add(-4 * time.Minute), Mode: app.CheckForeground, Actionable: true, Scan: res},
		Disk:   &app.DiskState{Volume: "Macintosh HD", Free: 128_400_000_000, Total: 994_660_000_000, At: time.Now()}}
}

// junkCategories is a 系统垃圾 check as the design shows it: caches with many items, logs, a category with a warning, downloads that go to the Trash, a tool's own clean command and the Trash.
func junkCategories() []cleanup.Category {
	mod := time.Now().Add(-26 * time.Hour)
	items := func(prefix string, n int, size int64) []cleanup.Item {
		out := make([]cleanup.Item, n)
		for i := range out {
			p := fmt.Sprintf("%s/com.example.app%02d", prefix, i)
			out[i] = cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: size - int64(i)*3_100_000, ModTime: mod, Dir: true}
		}
		return out
	}
	cat := func(id, title, desc, icon string, d cleanup.DeletionMode, selected bool, its []cleanup.Item) cleanup.Category {
		c := cleanup.Category{ID: id, Title: title, Description: desc, Icon: icon, Kind: cleanup.KindCache, Deletion: d, Selected: selected, Items: its}
		for i := range c.Items {
			c.Items[i].Selected = selected
			c.Size += c.Items[i].Size
		}
		return c
	}
	caches := cat("app-caches", "应用缓存", "应用临时保存的数据，删除后会自动重建", "cache", cleanup.DeletePermanent, true, items("/Users/me/Library/Caches", 12, 180_000_000))
	running := cat("app-caches-running", "正在运行的应用的缓存", "", "cache", cleanup.DeletePermanent, false, items("/Users/me/Library/Caches/Running", 3, 40_000_000))
	running.Warning = "这些应用正在运行，建议先退出再清理"
	logs := cat("logs", "日志", "应用和系统的运行记录", "log", cleanup.DeletePermanent, true, items("/Users/me/Library/Logs", 6, 24_000_000))
	browser := cat("browser-caches", "浏览器缓存", "网页的临时文件，下次访问会重新下载", "browser", cleanup.DeletePermanent, true, items("/Users/me/Library/Caches/Browser", 4, 90_000_000))
	downloads := cat("incomplete-downloads", "未完成的下载", "中断后留下的半截文件", "temp", cleanup.DeleteTrash, true, items("/Users/me/Downloads", 2, 12_000_000))
	trash := cat("trash", "废纸篓", "已删除但还没清空的文件", "trash", cleanup.DeletePermanent, false, items("/Users/me/.Trash", 1, 900_000_000))
	trash.Kind, trash.Warning = cleanup.KindTrash, "清空后无法恢复，默认不选"
	return []cleanup.Category{caches, running, logs, browser, downloads, trash}
}

// TestScreensSystemJunk shows the 系统垃圾 drill-down view with 概览's check, a category expanded, its confirmation, cleaning and the toast after it, for comparison with the design.
func TestScreensSystemJunk(t *testing.T) {
	const wait = 10 * time.Second
	fakes := storefake.New(t.TempDir())
	ov := storefake.NewOverview(drillState("system-junk", junkCategories()...))
	release := make(chan struct{})
	ov.CleanRelease = release
	ov.TrashInfoValue = app.TrashInfo{Items: 214, Bytes: 1_240_000_000, Restorable: 24}
	ov.OnClean = func(_ context.Context, _ cleanup.CleanRequest, _ app.CleanOrigin, _ func(cleanup.Progress)) (app.OverviewOutcome, error) {
		return app.OverviewOutcome{Result: cleanup.CleanResult{Freed: 2_390_000_000, Trashed: 24_000_000, Removed: 24}, RunID: "run-1", Undoable: true}, nil
	}
	fakes.Overview = ov
	a := screensApp(t, fakes, shell.Deps{SystemJunk: systemjunk.Deps{Files: &cleanflowfake.Files{}}}, "system-junk")
	a.WaitFor("id:category-list", wait)
	a.Screenshot("system-junk")
	a.Press("id:expand-system-junk:app-caches")
	a.Press("role:checkbox com.example.app01")
	a.Screenshot("system-junk-expanded")

	a.Press("id:clean")
	a.WaitFor("id:confirm", wait)
	a.Screenshot("system-junk-confirm")
	a.Press("id:confirm-ok")
	a.WaitFor("id:clean-overlay", wait)
	a.Screenshot("system-junk-cleaning")
	close(release)
	a.WaitFor("id:toast", wait)
	a.Screenshot("system-junk-toast")

	// Emptying the Trash asks with the Trash measured now.
	a.Press("id:toast-close")
	a.WaitForGone("id:toast", wait)
	a.Press("id:select-none")
	a.Press("id:check-system-junk:trash")
	a.Press("id:clean")
	a.WaitFor("id:confirm-trash", wait)
	a.Screenshot("system-junk-trash-confirm")
	a.Press("id:confirm-cancel")
	a.WaitForGone("id:confirm", wait)

	// A source that failed and found nothing is not called clean.
	failed := drillState("system-junk")
	failed.Result.Scan.Sources[0] = cleanup.SourceState{ID: "system-junk", Status: cleanup.SourceFailed, Error: "读取超时"}
	fb := storefake.New(t.TempDir())
	fb.Overview = storefake.NewOverview(failed)
	b := screensApp(t, fb, shell.Deps{SystemJunk: systemjunk.Deps{Files: &cleanflowfake.Files{}}}, "system-junk")
	b.WaitFor("id:drill-failed", wait)
	b.Screenshot("system-junk-failed")
}

// TestScreensSystemJunkToastAtTheEnd cleans a long list scrolled to its end: the toast with 撤销 that follows must leave the action bar in view above it.
func TestScreensSystemJunkToastAtTheEnd(t *testing.T) {
	const wait = 10 * time.Second
	cats := make([]cleanup.Category, 14)
	for i := range cats {
		p := fmt.Sprintf("/Users/me/Downloads/part-%02d.crdownload", i)
		cats[i] = cleanup.Category{ID: fmt.Sprintf("downloads-%02d", i), Title: fmt.Sprintf("未完成的下载 %d", i+1), Description: "中断后留下的半截文件", Icon: "temp",
			Kind: cleanup.KindCache, Deletion: cleanup.DeleteTrash, Selected: true, Size: 12_000_000,
			Items: []cleanup.Item{{ID: cleanup.ItemID(p), Path: p, Size: 12_000_000, Selected: true, ModTime: time.Now().Add(-50 * time.Hour)}}}
	}
	fakes := storefake.New(t.TempDir())
	ov := storefake.NewOverview(drillState("system-junk", cats...))
	ov.OnClean = func(_ context.Context, _ cleanup.CleanRequest, _ app.CleanOrigin, _ func(cleanup.Progress)) (app.OverviewOutcome, error) {
		return app.OverviewOutcome{Result: cleanup.CleanResult{Trashed: 168_000_000, Removed: 14}, RunID: "run-1", Undoable: true}, nil
	}
	fakes.Overview = ov
	a := screensApp(t, fakes, shell.Deps{SystemJunk: systemjunk.Deps{Files: &cleanflowfake.Files{}}}, "system-junk")
	a.WaitFor("id:clean", wait)
	a.Scroll("id:drill-system-junk", 100000)
	a.Settle()
	// bottom is where 清理 ends in the window.
	bottom := func() float32 {
		v := a.Find("id:clean")
		y := float32(v.Frame.Size.Height)
		for ; v != nil; v = v.Parent {
			y += float32(v.Frame.Origin.Y)
			if v.Props.TestID == "drill-system-junk" {
				y -= float32(v.Offset.Y)
			}
		}
		return y
	}
	a.Press("id:clean")
	a.WaitFor("id:toast", wait)
	a.WaitUntil(wait, func() bool { return bottom() <= 820-24-44-12 })
	a.Screenshot("system-junk-toast-end")
}

// TestScreensSystemJunkLongList expands a category of 3,000 items and scrolls through it: the design's largest list, for its look and, under -trace, its commit times.
func TestScreensSystemJunkLongList(t *testing.T) {
	if raceEnabled {
		t.Skip("the 3,000-row scroll is about checking look and commit times, which the headless and target runs without the race detector cover; under -race it alone takes up to 25 minutes on a loaded machine")
	}
	mod := time.Now().Add(-50 * time.Hour)
	items := make([]cleanup.Item, 3000)
	var size int64
	for i := range items {
		p := fmt.Sprintf("/Users/me/Library/Caches/com.example.big/entry-%04d", i)
		items[i] = cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: int64(3000-i) * 41_000, ModTime: mod, Selected: true}
		size += items[i].Size
	}
	big := cleanup.Category{ID: "big", Title: "应用缓存", Description: "应用临时保存的数据，删除后会自动重建", Icon: "cache", Kind: cleanup.KindCache,
		Deletion: cleanup.DeletePermanent, Selected: true, Items: items, Size: size}
	fakes := storefake.New(t.TempDir())
	fakes.Overview = storefake.NewOverview(drillState("system-junk", big))
	a := screensApp(t, fakes, shell.Deps{SystemJunk: systemjunk.Deps{Files: &cleanflowfake.Files{}}}, "system-junk")
	a.WaitFor("id:category-list", 10*time.Second)
	a.Press("id:expand-system-junk:big")
	a.WaitFor("id:items-system-junk:big", 10*time.Second)
	a.Mark("scroll")
	for i := 1; i <= 20; i++ {
		a.Scroll("id:items-system-junk:big", 2400)
	}
	a.Mark("scrolled")
	a.Screenshot("system-junk-long-list")
}
