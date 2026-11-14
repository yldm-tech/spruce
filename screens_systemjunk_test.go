package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/pages/systemjunk"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// junkCategories is a 系统垃圾 scan as the design shows it: caches with many items, logs, a category with a warning, downloads that go to the Trash, a tool's own clean command and the Trash.
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
	caches := cat("user-caches", "应用缓存", "应用临时保存的数据，删除后会自动重建", "cache", cleanup.DeletePermanent, true, items("/Users/me/Library/Caches", 12, 180_000_000))
	running := cat("running-caches", "正在运行的应用的缓存", "", "cache", cleanup.DeletePermanent, false, items("/Users/me/Library/Caches/Running", 3, 40_000_000))
	running.Warning = "这些应用正在运行，建议先退出再清理"
	logs := cat("logs", "日志", "应用和系统的运行记录", "log", cleanup.DeletePermanent, true, items("/Users/me/Library/Logs", 6, 24_000_000))
	browser := cat("browser", "浏览器缓存", "网页的临时文件，下次访问会重新下载", "browser", cleanup.DeletePermanent, true, items("/Users/me/Library/Caches/Browser", 4, 90_000_000))
	brew := cat("brew", "Homebrew 缓存", "旧版本的安装包", "package", cleanup.DeletePermanent, true, items("/Users/me/Library/Caches/Homebrew", 2, 60_000_000))
	brew.Command = "brew cleanup -s --prune=all"
	downloads := cat("downloads", "未完成的下载", "中断后留下的半截文件", "temp", cleanup.DeleteTrash, true, items("/Users/me/Downloads", 2, 12_000_000))
	trash := cat("trash", "废纸篓", "已删除但还没清空的文件", "trash", cleanup.DeletePermanent, false, items("/Users/me/.Trash", 1, 900_000_000))
	trash.Warning = "清空后无法恢复，默认不选"
	return []cleanup.Category{caches, running, logs, browser, brew, downloads, trash}
}

// TestScreensSystemJunk shows the 系统垃圾 page idle, scanning, with results, asking, cleaning and done, for comparison with the design.
func TestScreensSystemJunk(t *testing.T) {
	scanRelease, cleanRelease := make(chan struct{}), make(chan struct{})
	result := cleanflowfake.Result(junkCategories()...)
	c := &cleanflowfake.Cleaner{
		OnScan: func(ctx context.Context, _ app.CleanerID, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
			progress(cleanup.Progress{Phase: cleanup.PhaseScanning, Path: "/Users/me/Library/Caches/com.example.app03", Done: 5, Total: 8, Bytes: 742_000_000})
			select {
			case <-scanRelease:
			case <-ctx.Done():
				return cleanup.ScanResult{}, ctx.Err()
			}
			return result, nil
		},
		OnClean: func(ctx context.Context, _ app.CleanerID, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
			progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: "/Users/me/Library/Caches/com.example.app07", Done: 14, Total: 31, Bytes: 1_240_000_000})
			select {
			case <-cleanRelease:
			case <-ctx.Done():
			}
			return cleanup.CleanResult{Freed: 2_480_000_000, Trashed: 21_000_000, Removed: 30, Partial: true,
				Failed: []cleanup.Failure{{ItemID: "x", CategoryID: "user-caches", Path: "/Users/me/Library/Caches/com.example.app05", Reason: "有 1 个文件未能删除，可能正在使用", InUse: true}},
				Categories: []cleanup.CategoryOutcome{
					{CategoryID: "user-caches", Title: "应用缓存", Removed: 11, Freed: 1_950_000_000, Failed: 1},
					{CategoryID: "logs", Title: "日志", Removed: 6, Freed: 97_000_000},
					{CategoryID: "browser", Title: "浏览器缓存", Removed: 4, Freed: 341_000_000},
					{CategoryID: "brew", Title: "Homebrew 缓存", Removed: 2, Freed: 117_000_000},
					{CategoryID: "downloads", Title: "未完成的下载", Removed: 2, Trashed: 21_000_000},
				}}, nil
		}}
	fakes := storefake.New(t.TempDir())
	a := screensApp(t, fakes, shell.Deps{SystemJunk: systemjunk.Deps{Cleanup: c, Files: &cleanflowfake.Files{}}}, "system-junk")
	a.WaitFor("id:scan", 10*time.Second)
	a.Screenshot("system-junk-idle")

	a.Press("id:scan")
	a.WaitFor("text:已找到 742 MB", 10*time.Second)
	a.Screenshot("system-junk-scanning")

	close(scanRelease)
	a.WaitFor("id:category-list", 10*time.Second)
	a.Press("id:expand-user-caches")
	a.Press("role:checkbox com.example.app01")
	a.Screenshot("system-junk-results")

	a.Press("id:check-trash")
	a.Press("id:clean")
	a.WaitFor("id:confirm", 10*time.Second)
	a.Screenshot("system-junk-confirm")

	a.Press("id:confirm-ok")
	a.WaitFor("id:clean-overlay >> text:已处理 14/31 · 已释放 1.2 GB", 10*time.Second)
	a.Screenshot("system-junk-cleaning")

	close(cleanRelease)
	a.WaitFor("id:done", 10*time.Second)
	a.Screenshot("system-junk-done")
	a.Press("id:done-close")
	a.WaitForGone("id:done", 10*time.Second)
}
