package main

import (
	"context"
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/growth"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/ui/cleanflow/cleanflowfake"
	"spruce/apps/spruce/internal/ui/pages/overview"
	"spruce/apps/spruce/internal/ui/shell"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// overviewScan is a check as the design shows 概览: caches and logs deleted for good, a command, idle build output, installers and an incomplete download moved to the Trash, things left unchecked and the Trash.
func overviewScan() cleanup.ScanResult {
	now := time.Now()
	items := func(prefix string, sizes ...int64) []cleanup.Item {
		out := make([]cleanup.Item, len(sizes))
		for i, size := range sizes {
			p := fmt.Sprintf("%s-%02d", prefix, i)
			out[i] = cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: size, ModTime: now.Add(-40 * 24 * time.Hour), Dir: true, Selected: true}
		}
		return out
	}
	cat := func(id, title, desc, icon string, del cleanup.DeletionMode, its []cleanup.Item) cleanup.Category {
		src, _, _ := strings.Cut(id, ":")
		return cleanup.Category{ID: id, Title: title, Description: desc, Icon: icon, Kind: cleanup.KindCache, Deletion: del, Source: src, Items: its}
	}
	derived := cat("dev-junk:tool-xcode-derived-data", "Xcode 编译缓存", "DerivedData，下次编译时重新生成", "xcode", cleanup.DeletePermanent,
		[]cleanup.Item{{ID: cleanup.ItemID("/Users/me/Library/Developer/Xcode/DerivedData"), Path: "/Users/me/Library/Developer/Xcode/DerivedData",
			Size: 17_200_000_000, Dir: true, Selected: true, ModTime: now.Add(-40 * 24 * time.Hour)}})
	brew := cat("dev-junk:tool-homebrew", "Homebrew 缓存", "运行 brew cleanup，清理下载的安装包和已升级软件的旧版本", "package", cleanup.DeletePermanent,
		items("/Users/me/Library/Caches/Homebrew/downloads/pkg", 1_100_000_000, 840_000_000))
	brew.Command = "brew cleanup -s --prune=all"
	caches := cat("system-junk:app-caches", "应用缓存", "应用临时保存的数据，删除后会自动重建", "cache", cleanup.DeletePermanent,
		items("/Users/me/Library/Caches/com.spotify.client", 2_400_000_000, 610_000_000, 420_000_000, 180_000_000))
	running := cat("system-junk:app-caches-running", "正在运行的应用的缓存", "", "cache", cleanup.DeletePermanent, items("/Users/me/Library/Caches/Running/com.tinyspeck.slackmacgap", 640_000_000))
	running.Items[0].Selected, running.Warning = false, "这些应用正在运行，建议先退出再清理"
	logs := cat("system-junk:logs", "日志", "应用和系统的运行记录", "log", cleanup.DeletePermanent, items("/Users/me/Library/Logs/app", 140_000_000, 60_000_000))
	logs.Items[1].ModTime = now.Add(-200 * 24 * time.Hour)
	downloads := cat("system-junk:incomplete-downloads", "未完成的下载", "中断后留下的半截文件", "temp", cleanup.DeleteTrash, items("/Users/me/Downloads/big.crdownload", 310_000_000))
	trash := cat("system-junk:trash", "废纸篓", "废纸篓里的全部项目", "trash", cleanup.DeletePermanent, items("/Users/me/.Trash", 3_400_000_000))
	trash.Kind, trash.Items[0].Selected, trash.Warning = cleanup.KindTrash, false, "清空后无法恢复，默认不选"
	dmg := cat("installers:disk-images", "磁盘映像", "下载的 .dmg", "installer", cleanup.DeleteTrash,
		[]cleanup.Item{{ID: cleanup.ItemID("/Users/me/Downloads/Figma-124.5.dmg"), Path: "/Users/me/Downloads/Figma-124.5.dmg", Size: 512_000_000, Selected: true,
			ModTime: now.Add(-20 * 24 * time.Hour)}, {ID: cleanup.ItemID("/Users/me/Downloads/Docker.dmg"), Path: "/Users/me/Downloads/Docker.dmg", Size: 1_620_000_000,
			Selected: true, ModTime: now.Add(-12 * 24 * time.Hour)}})
	art := func(project string, size int64, git cleanup.GitState, hold string) cleanup.Item {
		p := project + "/node_modules"
		return cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: size, Dir: true, Selected: hold == "", Project: project, Git: git, Hold: hold,
			ModTime: now.Add(-120 * 24 * time.Hour)}
	}
	modules := cat("dev-junk:artifact-node-modules", "node_modules", "依赖", "node", cleanup.DeletePermanent, []cleanup.Item{
		art("/Users/me/code/old-site", 1_300_000_000, cleanup.GitClean, ""),
		art("/Users/me/code/prototype", 820_000_000, cleanup.GitNone, ""),
		art("/Users/me/code/spruce-web", 940_000_000, cleanup.GitDirty, "项目有未提交的改动"),
	})
	res := cleanup.NewResult([]cleanup.Category{caches, running, logs, downloads, trash, dmg, derived, brew, modules}, now.Add(-time.Minute))
	res.Sources = []cleanup.SourceState{
		{ID: "system-junk", Status: cleanup.SourceDone}, {ID: "dev-junk", Status: cleanup.SourceDone}, {ID: "installers", Status: cleanup.SourceDone},
	}
	return res
}

// overviewState is a result of overviewScan, checked minutes ago, on a disk with room.
func overviewState(res cleanup.ScanResult) app.OverviewState {
	recs := advice.Derive(res, advice.Inputs{Now: time.Now(), Home: "/Users/me", InstalledApps: []string{"Figma", "Docker"}, InstallerRoots: []string{"/Users/me/Downloads"}})
	return app.OverviewState{Phase: app.OverviewIdle, CanUndo: true,
		Result: &app.OverviewResult{CheckedAt: time.Now().Add(-12 * time.Minute), Mode: app.CheckBackground, Actionable: true, Scan: res, Recommendations: recs,
			Headline: advice.Headline(recs)},
		Disk: &app.DiskState{Volume: "Macintosh HD", Free: 128_400_000_000, Total: 994_660_000_000, At: time.Now()}}
}

// overviewApp opens the shell on 概览 with ov as its controller.
func overviewApp(t *testing.T, ov *storefake.Overview, permission maintenance.PermissionState) *nsgotest.App {
	t.Helper()
	fakes := storefake.New(t.TempDir())
	fakes.Permission = permission
	fakes.Overview = ov
	ov.GrowthList = []growth.Growth{
		{Path: "/Users/me/Library/Developer/Xcode/DerivedData", Label: "Xcode 编译缓存", Icon: "xcode", Before: 11_100_000_000, After: 17_200_000_000,
			Since: time.Now().Add(-4 * 24 * time.Hour)},
		{Path: "/Users/me/Downloads", Label: "下载", Icon: "downloads", Before: 4_000_000_000, After: 6_100_000_000, Since: time.Now().Add(-6 * 24 * time.Hour)},
	}
	return screensApp(t, fakes, shell.Deps{Overview: overview.Deps{Files: &cleanflowfake.Files{}, Home: "/Users/me"}}, "overview")
}

// TestScreensOverview shows 概览 in each of its states, a card expanded, its confirmations, a card at work and the undo toast, for comparison with the design.
func TestScreensOverview(t *testing.T) {
	const wait = 10 * time.Second
	res := overviewScan()

	// A: the first check.
	first := app.OverviewState{Phase: app.OverviewChecking, CanUndo: true, Disk: overviewState(res).Disk,
		Progress: &cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 47, Total: 100, Sources: []cleanup.SourceState{
			{ID: "system-junk", Status: cleanup.SourceScanning, Done: 6, Total: 19, Bytes: 1_200_000_000},
			{ID: "dev-junk", Status: cleanup.SourceScanning, Done: 1, Total: 13, Bytes: 54_600_000},
			{ID: "installers", Status: cleanup.SourceDone, Bytes: 2_132_000_000, Items: 2},
		}}}
	ov := storefake.NewOverview(first)
	a := overviewApp(t, ov, maintenance.PermissionGranted)
	a.WaitFor("id:check-progress", wait)
	a.WaitFor("text:正在检查… 已找到 54.6 MB", wait)
	a.Screenshot("overview-first-check")

	// B: the cached result while a check updates it.
	cached := overviewState(res)
	cached.Phase, cached.Result.FromCache, cached.Result.Actionable = app.OverviewChecking, true, false
	cached.Result.CheckedAt = time.Now().Add(-3 * time.Hour)
	cached.Progress = &cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 62, Total: 100}
	ov.Set(cached)
	a.WaitFor("id:rec-app-caches-act >> text:正在核对…", wait)
	a.WaitFor("id:recheck >> text:停止检查", wait)
	a.Screenshot("overview-cached-updating")

	// C: ready, and a card expanded.
	ov.Set(overviewState(res))
	a.WaitFor("id:conclusion-value", wait)
	a.WaitFor("id:rec-app-caches-act >> text:删除…", wait)
	a.WaitFor("id:growth-row-0", wait)
	a.Screenshot("overview-ready")
	a.Press("id:rec-app-caches-head")
	a.WaitFor("id:rec-app-caches-items", wait)
	a.Scroll("id:overview-page", 240)
	a.Screenshot("overview-expanded")
	a.Press("id:rec-app-caches-expand")
	a.Scroll("id:overview-page", -2000)

	// The confirmations of deleting for good and of a command.
	a.Press("id:rec-tool-xcode-derived-data-act")
	a.WaitFor("id:rec-confirm", wait)
	a.Screenshot("overview-confirm-delete")
	a.Press("id:confirm-cancel")
	a.WaitForGone("id:rec-confirm", wait)
	a.Press("id:rec-tool-homebrew-act")
	a.WaitFor("id:rec-confirm", wait)
	a.Screenshot("overview-confirm-command")
	a.Press("id:confirm-cancel")
	a.WaitForGone("id:rec-confirm", wait)

	// A card at work: its progress in place of its button, the other cards waiting.
	release := make(chan struct{})
	ov.OnClean = func(ctx context.Context, req cleanup.CleanRequest, origin app.CleanOrigin, progress func(cleanup.Progress)) (app.OverviewOutcome, error) {
		if origin.Recommendation == "tool-xcode-derived-data" {
			progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: "/Users/me/Library/Developer/Xcode/DerivedData", Done: 0, Total: 1})
			select {
			case <-release:
			case <-ctx.Done():
			}
			return app.OverviewOutcome{RunID: "run-del", Result: cleanup.CleanResult{Freed: 17_200_000_000, Removed: 1}}, nil
		}
		return app.OverviewOutcome{RunID: "run-trash", Undoable: true, Result: cleanup.CleanResult{Trashed: 2_132_000_000, Removed: 2}}, nil
	}
	a.Press("id:rec-tool-xcode-derived-data-act")
	a.WaitFor("id:rec-confirm", wait)
	a.Press("id:confirm-ok")
	a.WaitFor("id:rec-tool-xcode-derived-data-progress", wait)
	a.Screenshot("overview-cleaning")
	close(release)
	a.WaitFor("id:toast", wait)

	// A move to the Trash: at once, with 撤销.
	a.Press("id:rec-installers-act")
	a.WaitFor("id:toast-undo", wait)
	a.Screenshot("overview-toast-undo")
	a.Press("id:toast-close")
	a.WaitForGone("id:toast", wait)

	// D: outdated.
	stale := overviewState(res)
	stale.Result.Stale, stale.Result.Actionable = true, false
	stale.Result.CheckedAt = time.Now().Add(-30 * time.Hour)
	ov.Set(stale)
	a.WaitFor("id:rec-app-caches-act >> text:重新检查", wait)
	a.Screenshot("overview-stale")

	// The folders a background check skipped, and a scheduled check waiting for power.
	skipped := overviewState(res)
	skipped.Result.Skipped = []string{"/Users/me/Downloads", "/Users/me/Desktop", "/Users/me/Documents"}
	skipped.Waiting = "正在使用电池，接通电源后再检查"
	ov.Set(skipped)
	a.WaitFor("id:skipped-notice", wait)
	a.Screenshot("overview-skipped")

	// E: nothing to clean.
	empty := overviewState(cleanup.NewResult([]cleanup.Category{}, time.Now()))
	ov.Set(empty)
	a.WaitFor("id:overview-empty", wait)
	a.Screenshot("overview-empty")

	// F: the check failed, with no result.
	ov.Set(app.OverviewState{Phase: app.OverviewIdle, CanUndo: true, Disk: empty.Disk, Error: "无法读取应用支持文件夹"})
	a.WaitFor("id:check-retry", wait)
	a.Screenshot("overview-failed")

	// G: stopped, with no result.
	ov.Set(app.OverviewState{Phase: app.OverviewIdle, CanUndo: true, Disk: empty.Disk})
	a.WaitFor("id:check-start", wait)
	a.Screenshot("overview-stopped")

	// I: little space left.
	low := overviewState(res)
	low.Disk = &app.DiskState{Volume: "Macintosh HD", Free: 12_600_000_000, Total: 494_380_000_000, Low: true}
	ov.Set(low)
	a.WaitFor("id:low-disk-offer", wait)
	a.Screenshot("overview-low-disk")
}

// TestScreensOverviewDenied shows 概览 without Full Disk Access (H): what it could read, and the guide.
func TestScreensOverviewDenied(t *testing.T) {
	st := overviewState(overviewScan())
	st.Result.Scan.Denied = 4
	a := overviewApp(t, storefake.NewOverview(st), maintenance.PermissionDenied)
	a.WaitFor("id:denied-notice", 10*time.Second)
	a.Screenshot("overview-denied")
}
