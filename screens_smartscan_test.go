package main

import (
	"context"
	"fmt"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/pages/smartscan"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// smartCategories is a 智能扫描 scan as the design shows it: the 系统垃圾 scan of the system-junk screens, a few developer caches and build folders, and two installers.
func smartCategories() []cleanup.Category {
	mod := time.Now().Add(-40 * 24 * time.Hour)
	cat := func(source, id, title, desc, icon string, selected bool, command string, sizes ...int64) cleanup.Category {
		c := cleanup.Category{ID: source + ":" + id, Title: title, Description: desc, Icon: icon, Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent,
			Selected: selected, Source: source, Command: command}
		for i, size := range sizes {
			p := fmt.Sprintf("/Users/me/%s/%s-%02d", id, id, i)
			c.Items = append(c.Items, cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: size, ModTime: mod, Dir: true, Selected: selected})
			c.Size += size
		}
		return c
	}
	var out []cleanup.Category
	for _, c := range junkCategories() {
		c.ID, c.Source = "system-junk:"+c.ID, "system-junk"
		out = append(out, c)
	}
	installer := cat("installers", "downloads", "下载的安装包", "装完之后通常不再需要", "installer", false, "", 412_000_000, 96_000_000)
	installer.Deletion = cleanup.DeleteTrash
	return append(out,
		cat("dev-junk", "npm", "npm 缓存", "下载过的包，需要时会重新下载", "node", true, "npm cache clean --force", 1_340_000_000),
		cat("dev-junk", "xcode-derived", "Xcode 编译缓存", "DerivedData，下次编译时重新生成", "xcode", true, "", 3_210_000_000, 1_120_000_000, 640_000_000),
		cat("dev-junk", "node-modules", "旧项目的 node_modules", "30 天没有改动的项目", "build", false, "", 820_000_000, 310_000_000),
		installer,
	)
}

// TestScreensSmartScan shows the 智能扫描 page idle, scanning, with results, its full list, asking, cleaning and done, for comparison with the design.
func TestScreensSmartScan(t *testing.T) {
	scanRelease, cleanRelease := make(chan struct{}), make(chan struct{})
	result := cleanflowfake.Result(smartCategories()...)
	result.Sources = []cleanup.SourceState{
		{ID: "system-junk", Status: cleanup.SourceDone},
		{ID: "dev-junk", Status: cleanup.SourceDone},
		{ID: "installers", Status: cleanup.SourceDone},
	}
	c := &cleanflowfake.Cleaner{
		OnScan: func(ctx context.Context, _ app.CleanerID, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
			// The state of the design's reference screenshot.
			progress(cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 47, Total: 100, Bytes: 1_629_400_000, Sources: []cleanup.SourceState{
				{ID: "system-junk", Status: cleanup.SourceScanning, Done: 6, Total: 19, Bytes: 1_200_000_000},
				{ID: "dev-junk", Status: cleanup.SourceScanning, Done: 1, Total: 13, Bytes: 54_600_000},
				{ID: "installers", Status: cleanup.SourceDone, Bytes: 374_800_000, Items: 5},
			}})
			select {
			case <-scanRelease:
			case <-ctx.Done():
				return cleanup.ScanResult{}, ctx.Err()
			}
			return result, nil
		},
		OnClean: func(ctx context.Context, _ app.CleanerID, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
			progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: "/Users/me/xcode-derived/xcode-derived-01", Done: 18, Total: 33, Bytes: 3_920_000_000})
			select {
			case <-cleanRelease:
			case <-ctx.Done():
			}
			return cleanup.CleanResult{Freed: 7_890_000_000, Removed: 33, Categories: []cleanup.CategoryOutcome{
				{CategoryID: "system-junk:user-caches", Title: "应用缓存", Source: "system-junk", Removed: 12, Freed: 1_950_000_000},
				{CategoryID: "system-junk:logs", Title: "日志", Source: "system-junk", Removed: 6, Freed: 97_000_000},
				{CategoryID: "dev-junk:npm", Title: "npm 缓存", Source: "dev-junk", Removed: 1, Freed: 1_340_000_000},
				{CategoryID: "dev-junk:xcode-derived", Title: "Xcode 编译缓存", Source: "dev-junk", Removed: 3, Freed: 4_970_000_000},
			}}, nil
		}}
	fakes := storefake.New(t.TempDir())
	fakes.Permission = maintenance.PermissionDenied
	fakes.Locations = app.SpaceLocations{Volumes: []app.SpaceVolume{{Path: "/", Name: "Macintosh HD", Total: 994_660_000_000, Free: 128_400_000_000, Startup: true}}}
	ended := time.Now().Add(-9 * 24 * time.Hour)
	fakes.Runs = []history.Run{{ID: "s", Kind: history.KindSmartScan, StartedAt: ended, EndedAt: ended, Summary: &history.Totals{Freed: 4_200_000_000, Items: 41}}}
	a := screensApp(t, fakes, shell.Deps{SmartScan: smartscan.Deps{Cleanup: c, Files: &cleanflowfake.Files{}}}, "overview")
	a.WaitFor("id:last-clean", 10*time.Second)
	a.WaitFor("id:status-disk-note", 10*time.Second)
	a.Screenshot("smart-scan-idle")

	a.Press("id:scan")
	a.WaitFor("text:正在扫描… 已找到 54.6 MB", 10*time.Second)
	a.Screenshot("smart-scan-scanning")

	close(scanRelease)
	a.WaitFor("id:total", 10*time.Second)
	a.Screenshot("smart-scan-results")
	a.Press("id:list-dev-junk >> id:expand-dev-junk:xcode-derived")
	a.Scroll("id:smart-scan-page", 760)
	a.Screenshot("smart-scan-results-list")

	a.Press("id:clean")
	a.WaitFor("id:confirm", 10*time.Second)
	a.Screenshot("smart-scan-confirm")

	a.Press("id:confirm-ok")
	a.WaitFor("id:clean-overlay >> text:已处理 18/33 · 已释放 3.9 GB", 10*time.Second)
	a.Screenshot("smart-scan-cleaning")

	close(cleanRelease)
	a.WaitFor("id:done", 10*time.Second)
	a.Screenshot("smart-scan-done")
	a.Press("id:done-close")
	a.WaitForGone("id:done", 10*time.Second)
}

// TestScreensSmartScanEmpty shows the results of a scan that found nothing, beside a CPU alert and a low disk.
func TestScreensSmartScanEmpty(t *testing.T) {
	result := cleanflowfake.Result()
	c := &cleanflowfake.Cleaner{OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
		return result, nil
	}}
	fakes := storefake.New(t.TempDir())
	fakes.Alerts = []health.Alert{{PID: 4242, Name: "Xcode", CPUPercent: 148.6, Duration: 14 * time.Minute, Threshold: 40}}
	fakes.Locations = app.SpaceLocations{Volumes: []app.SpaceVolume{{Path: "/", Name: "Macintosh HD", Total: 494_380_000_000, Free: 12_600_000_000, Startup: true}}}
	a := screensApp(t, fakes, shell.Deps{SmartScan: smartscan.Deps{Cleanup: c, Files: &cleanflowfake.Files{}}}, "overview")
	a.WaitFor("id:scan", 10*time.Second)
	a.Press("id:scan")
	a.WaitFor("id:empty", 10*time.Second)
	a.Screenshot("smart-scan-empty")
}
