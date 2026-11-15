package main

import (
	"context"
	"slices"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/advice"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/growth"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/ui/pages/space"
	"github.com/yldm-tech/spruce/internal/ui/pages/space/spacefake"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// TestScreensSpace shows the 空间透视 page before a scan, scanning, with the results of a home folder of 302 cells, on the 大文件 tab, asking before a move to the Trash and with folders it could not read, for comparison with the design.
func TestScreensSpace(t *testing.T) {
	const wait = 10 * time.Second
	svc := spacefake.Crowded(time.Now().Add(-3 * time.Minute))
	crowded := *svc.Kept
	svc.Kept = nil
	release := make(chan struct{})
	svc.OnScan = func(ctx context.Context, path string, progress func(app.SpaceProgress)) (app.SpaceSummary, error) {
		progress(app.SpaceProgress{Files: 182_406, Dirs: 21_380, Bytes: 61.7e9, Path: spacefake.Home + "/Library/Developer/CoreSimulator/Devices"})
		select {
		case <-release:
		case <-ctx.Done():
			return app.SpaceSummary{}, ctx.Err()
		}
		return crowded, nil
	}
	fakes := storefake.New(t.TempDir())
	fakes.Locations = app.SpaceLocations{
		Volumes: []app.SpaceVolume{{Path: "/", Name: "Macintosh HD", Total: 494.4e9, Free: 44.8e9, Startup: true}},
		Targets: []app.SpaceTarget{
			{ID: "home", Name: "个人文件夹", Path: spacefake.Home},
			{ID: "downloads", Name: "下载", Path: spacefake.Home + "/Downloads"},
			{ID: "applications", Name: "应用程序", Path: "/Applications"},
		},
	}
	a := screensApp(t, fakes, shell.Deps{Space: space.Deps{Space: svc, Files: &spacefake.Files{}}}, "space")
	a.WaitFor("id:volume-/", wait)
	a.Screenshot("space-idle")

	a.Press("id:target-home")
	a.WaitUntil(wait, func() bool {
		if a.Exists("text:已扫描 182,406 个文件 · 61.7 GB") {
			return true
		}
		a.Advance(20 * time.Millisecond)
		return false
	})
	a.Screenshot("space-scanning")

	close(release)
	a.WaitFor("id:treemap", wait)
	a.WaitUntil(wait, func() bool { return len(a.Query("id:treemap >> role:button")) > 20 })
	a.Screenshot("space-results")

	a.Press("id:tab-large")
	a.WaitFor(`role:group "大文件"`, wait)
	a.Screenshot("space-large")
	a.Press("id:tab-map")
	a.WaitFor("id:treemap", wait)

	a.Press(`id:space-list >> role:button "将 archive-004.zip 移到废纸篓"`)
	a.WaitFor("id:trash-confirm", wait)
	a.Screenshot("space-confirm")
	a.Press("id:confirm-cancel")
	a.WaitForGone("id:trash-confirm", wait)
}

// TestScreensSpaceDenied shows the results of a scan that could not read some folders, without Full Disk Access.
func TestScreensSpaceDenied(t *testing.T) {
	svc := spacefake.New(time.Now().Add(-2 * time.Hour))
	svc.Kept.Denied, svc.Kept.DeniedPaths = 12, []string{spacefake.Home + "/Library/Mail", spacefake.Home + "/Library/Safari"}
	fakes := storefake.New(t.TempDir())
	fakes.Permission = maintenance.PermissionDenied
	a := screensApp(t, fakes, shell.Deps{Space: space.Deps{Space: svc, Files: &spacefake.Files{}}}, "space")
	a.WaitFor("id:denied-banner", 10*time.Second)
	a.WaitFor("id:grant-full-disk-access", 10*time.Second)
	a.Screenshot("space-denied")
}

// spaceGrowthFakes are store fakes for the 最近变化 screens: the startup disk and the quick folders, and 概览's controller on macOS, whose 应用缓存 card covers the first location that grew.
func spaceGrowthFakes(t *testing.T, list []growth.Growth, err error) *storefake.Services {
	fakes := storefake.New(t.TempDir())
	fakes.Locations = app.SpaceLocations{
		Volumes: []app.SpaceVolume{{Path: "/", Name: "Macintosh HD", Total: 494.4e9, Free: 44.8e9, Startup: true}},
		Targets: []app.SpaceTarget{
			{ID: "home", Name: "个人文件夹", Path: spacefake.Home},
			{ID: "downloads", Name: "下载", Path: spacefake.Home + "/Downloads"},
			{ID: "applications", Name: "应用程序", Path: "/Applications"},
		},
	}
	safari := spacefake.Home + "/Library/Caches/com.apple.Safari"
	res := cleanup.ScanResult{Categories: []cleanup.Category{{ID: "system-junk:app-caches", Items: []cleanup.Item{{ID: "safari", Path: safari, Size: 7e9, Selected: true}}, Size: 7e9}}, Items: 1, Size: 7e9}
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle, CanUndo: true, Result: &app.OverviewResult{Scan: res,
		Recommendations: []advice.Recommendation{{ID: "app-caches", Title: "应用缓存", Items: []advice.ItemRef{{CategoryID: "system-junk:app-caches", ItemID: "safari"}}}}}})
	fakes.Overview.GrowthList, fakes.Overview.GrowthErr = list, err
	fakes.Overview.OnUndo = func(context.Context, string) (app.RestoreOutcome, error) {
		return app.RestoreOutcome{RunID: "restore-1", Restored: 1, Bytes: 3e9}, nil
	}
	return fakes
}

// TestScreensSpaceGrowth shows 最近变化 over 7 days on the start screen, then a scan's results right after a move to the Trash with the toast offering 撤销, and after 撤销 put it back and the page scanned again.
func TestScreensSpaceGrowth(t *testing.T) {
	const wait = 10 * time.Second
	now := time.Now()
	home := spacefake.Home
	list := []growth.Growth{
		{Path: home + "/Library/Caches/com.apple.Safari", Icon: "cache", Before: 1.2e9, After: 7.4e9, Since: now.Add(-5 * 24 * time.Hour)},
		{Path: home + "/Library/Developer/Xcode/DerivedData", Label: "Xcode 编译缓存", Icon: "xcode", Before: 9.8e9, After: 14.1e9, Since: now.Add(-5 * 24 * time.Hour)},
		{Path: home + "/Downloads", Label: "下载", Icon: "installer", Before: 6.3e9, After: 9.1e9, Since: now.Add(-5 * 24 * time.Hour)},
		{Path: home + "/code/web-app", Icon: "build", Before: 20 << 20, After: 1.6e9, Since: now.Add(-5 * 24 * time.Hour), AtLeast: true},
		{Path: home + "/Movies", Label: "影片", Icon: "video", Before: 8.2e9, After: 9.4e9, Since: now.Add(-5 * 24 * time.Hour)},
	}
	fakes := spaceGrowthFakes(t, list, nil)
	svc := spacefake.New(now.Add(-time.Minute))
	listing := svc.Listings[0]
	svc.Kept = nil
	svc.OnScan = func(context.Context, string, func(app.SpaceProgress)) (app.SpaceSummary, error) {
		return spacefake.Summary(time.Now()), nil
	}
	a := screensApp(t, fakes, shell.Deps{Space: space.Deps{Space: svc, Files: &spacefake.Files{}}}, "space")
	a.WaitFor("id:growth-row-4", wait)
	a.Screenshot("space-growth-7d")

	a.Press("id:target-home")
	a.WaitFor(`role:group "个人文件夹 的内容"`, wait)
	// The move takes big.iso out of the listing and the scan's size, as the service does.
	root := spacefake.Root
	root.Size = 7e9
	without := listing
	without.Entry = root
	without.Ancestors = []app.SpaceEntry{root}
	without.Children = slices.DeleteFunc(slices.Clone(listing.Children), func(e app.SpaceEntry) bool { return e.ID == spacefake.ISO.ID })
	svc.SetListing(0, without)
	svc.TrashResult = app.SpaceTrashResult{Trashed: 3e9, Items: 1, Failed: []app.SpaceFailure{}, Root: root, RunID: "run-1", Undoable: true}
	a.Press(`id:space-list >> role:button "将 big.iso 移到废纸篓"`)
	a.WaitFor("id:toast-undo", wait)
	a.WaitForGone(`id:space-list >> text:big.iso`, wait)
	a.Screenshot("space-trash-toast")

	svc.SetListing(0, listing)
	a.Press("id:toast-undo")
	a.WaitUntil(wait, func() bool { return a.Exists("id:toast-text") && a.Find("id:toast-text").Text == "已放回原处 1 项" })
	a.WaitFor(`id:space-list >> text:big.iso`, wait)
	a.Screenshot("space-undo-done")
}

// TestScreensSpaceGrowthEmpty shows 最近变化 before two snapshots exist.
func TestScreensSpaceGrowthEmpty(t *testing.T) {
	fakes := spaceGrowthFakes(t, nil, growth.ErrNoHistory)
	a := screensApp(t, fakes, shell.Deps{Space: space.Deps{Space: &spacefake.Service{Listings: spacefake.Listings()}, Files: &spacefake.Files{}}}, "space")
	a.WaitFor("id:growth-empty", 10*time.Second)
	a.Screenshot("space-growth-empty")
}
