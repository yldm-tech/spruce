package main

import (
	"context"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
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
