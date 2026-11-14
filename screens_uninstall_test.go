package main

import (
	"bytes"
	"context"
	"encoding/base64"
	"image"
	"image/color"
	"image/png"
	"math"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/pages/uninstall"
	"github.com/yldm-tech/spruce/internal/ui/pages/uninstall/uninstallfake"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// screenIcon is a 128-pixel app icon: a rounded square of c with a white disc, as a data: URL, so the screenshots show how sharp a measured icon draws.
func screenIcon(t *testing.T, c color.RGBA) string {
	t.Helper()
	const size = 128
	img := image.NewRGBA(image.Rect(0, 0, size, size))
	for y := range size {
		for x := range size {
			dx, dy := math.Max(math.Abs(float64(x)-63.5)-36, 0), math.Max(math.Abs(float64(y)-63.5)-36, 0)
			if math.Hypot(dx, dy) > 28 {
				continue
			}
			px := c
			if math.Hypot(float64(x)-63.5, float64(y)-63.5) < 26 {
				px = color.RGBA{255, 255, 255, 255}
			}
			img.SetRGBA(x, y, px)
		}
	}
	var buf bytes.Buffer
	if err := png.Encode(&buf, img); err != nil {
		t.Fatal(err)
	}
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(buf.Bytes())
}

// TestScreensUninstall shows the 应用卸载 page with its list, an app picked with its leftovers, a running app, the confirmation, the removal and the done card, for comparison with the design.
func TestScreensUninstall(t *testing.T) {
	now := time.Now()
	apps := []app.UninstallApp{
		{ID: "blender", Name: "Blender", Version: "4.2.1", BundleID: "org.blenderfoundation.blender", Path: "/Applications/Blender.app"},
		{ID: "code", Name: "Visual Studio Code", Version: "1.95.3", BundleID: "com.microsoft.VSCode", Path: "/Applications/Visual Studio Code.app", Running: true},
		{ID: "keynote", Name: "Keynote", Version: "14.2", BundleID: "com.apple.iWork.Keynote", Path: "/Applications/Keynote.app", AppStore: true},
		{ID: "note", Name: "便签助手", Version: "2.0.4", BundleID: "com.example.notes", Path: "/Applications/便签助手.app"},
		{ID: "music", Name: "音乐播放器", Version: "3.1", BundleID: "com.example.music", Path: "/Applications/音乐播放器.app"},
		{ID: "dict", Name: "词典", BundleID: "com.example.dict", Path: "/Applications/词典.app"},
		{ID: "arc", Name: "Arc", Version: "1.68.0", BundleID: "company.thebrowser.Browser", Path: "/Applications/Arc.app"},
		{ID: "figma", Name: "Figma", Version: "124.6.5", BundleID: "com.figma.Desktop", Path: "/Applications/Figma.app"},
		{ID: "zoom", Name: "zoom.us", Version: "6.2.5", BundleID: "us.zoom.xos", Path: "/Applications/zoom.us.app"},
	}
	icons := map[string]color.RGBA{"blender": {0xf5, 0x79, 0x2a, 255}, "code": {0x1f, 0x8a, 0xd2, 255}, "keynote": {0x2b, 0x7c, 0xf6, 255},
		"music": {0xe9, 0x3d, 0x5c, 255}, "arc": {0xd9, 0x4a, 0x8c, 255}, "figma": {0x1e, 0x1e, 0x1e, 255}, "zoom": {0x2d, 0x8c, 0xff, 255}}
	sizes := map[string]int64{"blender": 1_210_000_000, "code": 612_400_000, "keynote": 744_900_000, "note": 52_600_000, "music": 680_900_000,
		"dict": 28_100_000, "arc": 431_300_000, "figma": 389_000_000, "zoom": 221_300_000}
	var details []app.UninstallAppDetails
	for i, a := range apps {
		d := app.UninstallAppDetails{ID: a.ID, Size: uninstallfake.Size(sizes[a.ID]), Usage: app.UninstallUsed, LastUsed: now.Add(-time.Duration(i*9+2) * 24 * time.Hour)}
		if c, ok := icons[a.ID]; ok {
			d.Icon = screenIcon(t, c)
		}
		if a.ID == "dict" {
			d.Usage, d.LastUsed = app.UninstallNeverUsed, time.Time{}
		}
		details = append(details, d)
	}
	plan := app.UninstallPlan{AppID: "blender",
		Bundle: &app.UninstallLeftover{ID: "blender", Path: "/Applications/Blender.app", Kind: app.UninstallKindApp, Size: 1_210_000_000, Dir: true, Selected: true},
		Leftovers: []app.UninstallLeftover{
			{ID: "support", Path: "/Users/me/Library/Application Support/Blender", Kind: app.UninstallKindSupport, Size: 84_200_000, Dir: true, Selected: true},
			{ID: "caches", Path: "/Users/me/Library/Caches/org.blenderfoundation.blender", Kind: app.UninstallKindCaches, Size: 12_600_000, Dir: true, Selected: true},
			{ID: "prefs", Path: "/Users/me/Library/Preferences/org.blenderfoundation.blender.plist", Kind: app.UninstallKindPreferences, Size: 4_000, Selected: true},
			{ID: "state", Path: "/Users/me/Library/Saved Application State/org.blenderfoundation.blender.savedState", Kind: app.UninstallKindSavedState, Size: 81_000, Dir: true, Selected: true},
		}}
	release := make(chan struct{})
	svc := &uninstallfake.Service{List: apps, Details: details,
		OnLeftovers: func(_ context.Context, id string) (app.UninstallPlan, error) {
			if id == "code" {
				p := plan
				p.AppID, p.Running, p.Bundle, p.Leftovers = "code", true, &app.UninstallLeftover{ID: "code", Kind: app.UninstallKindApp, Size: 612_400_000}, plan.Leftovers[:2]
				return p, nil
			}
			return plan, nil
		},
		OnRemove: func(ctx context.Context, req app.UninstallRequest, progress func(cleanup.Progress)) (app.UninstallOutcome, error) {
			progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: "/Users/me/Library/Application Support/Blender", Done: 2, Total: 5, Bytes: 1_294_200_000})
			select {
			case <-release:
			case <-ctx.Done():
			}
			return app.UninstallOutcome{App: "Blender", BundleRemoved: true, Result: uninstallfake.Result(cleanup.CleanResult{Trashed: 1_306_885_000, Removed: 5,
				Categories: []cleanup.CategoryOutcome{
					{CategoryID: "app", Removed: 1, Trashed: 1_210_000_000},
					{CategoryID: "support", Removed: 4, Trashed: 96_885_000},
				}})}, nil
		}}
	fakes := storefake.New(t.TempDir())
	a := screensApp(t, fakes, shell.Deps{Uninstall: uninstall.Deps{Uninstall: svc, Files: &uninstallfake.Files{}, Platform: uninstall.PlatformMac}}, "uninstall")
	a.WaitFor("id:app-list", 10*time.Second)
	a.WaitUntil(10*time.Second, func() bool {
		if a.Exists("id:app-list >> text:1.2 GB") {
			return true
		}
		a.Advance(40 * time.Millisecond)
		return false
	})
	a.Screenshot("uninstall-idle")

	a.Press("id:app-list >> role:option Visual Studio Code")
	a.WaitFor("id:running", 10*time.Second)
	a.Screenshot("uninstall-running-app")

	a.Press("id:app-list >> role:option Blender")
	a.WaitFor("id:leftovers", 10*time.Second)
	a.Screenshot("uninstall-detail")

	a.Press("id:uninstall")
	a.WaitFor("id:confirm", 10*time.Second)
	a.Screenshot("uninstall-confirm")

	a.Press("id:confirm-ok")
	a.WaitUntil(10*time.Second, func() bool {
		if a.Exists("id:removing >> text:已处理 2/5 · 已释放 1.3 GB") {
			return true
		}
		a.Advance(40 * time.Millisecond)
		return false
	})
	a.Screenshot("uninstall-removing")

	close(release)
	a.WaitFor("id:done", 10*time.Second)
	a.Screenshot("uninstall-done")
	a.Press("id:done-close")
	a.WaitForGone("id:done", 10*time.Second)
}
