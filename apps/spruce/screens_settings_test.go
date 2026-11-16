package main

import (
	"runtime"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/ui/pages/settings"
	"spruce/apps/spruce/internal/ui/pages/settings/settingsfake"
	"spruce/apps/spruce/internal/ui/shell"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// TestScreensSettings shows each tab of the 设置 page (自动清理 with its rules off and two of them on), the line under 空间不足时提醒我 when notifications are refused, a number out of range on 清理 and the reset dialog, for comparison with the design.
func TestScreensSettings(t *testing.T) {
	const wait = 10 * time.Second
	fakes := storefake.New(t.TempDir())
	fakes.Permission = maintenance.PermissionDenied
	// 概览's controller has a result: the rules say what they would take now, and one has run.
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{CheckedAt: time.Now()},
		Rules: []app.RuleStatus{
			{ID: "app-caches", Matching: 46, MatchingBytes: 3_400_000_000, LastRun: time.Now().Add(-26 * time.Hour), LastTrashed: 2_100_000_000},
			{ID: "installers", Matching: 3, MatchingBytes: 2_100_000_000},
			{ID: "build-output", Matching: 7, MatchingBytes: 11_800_000_000},
			{ID: "incomplete-downloads"},
		}})
	svc := settingsfake.New("/Users/me/Library/Application Support/Spruce")
	// The page asks a fake for the notification permission: on the target the system's own prompt would show for the test's bundle. Asked, the user refuses.
	permission := core.NotificationsNotAsked
	notify := func(_ *core.Scope, ask bool, done func(core.NotificationPermission)) {
		if ask {
			permission = core.NotificationsDenied
		}
		done(permission)
	}
	d := shell.Deps{Settings: settings.Deps{Files: &settingsfake.Files{}, GOOS: "darwin", NotificationPermission: notify}, Stores: svc.Use(fakes.Deps())}
	a := nsgotest.Start(t, core.AppRoot(shell.App(d)), nsgotest.Size(1280, 820), wallNow)
	a.WaitFor("id:sidebar", wait)
	a.Press("id:nav-settings")
	// Full Disk Access is macOS only: the 权限 group follows the system the page runs on, not GOOS, and Windows has none.
	if runtime.GOOS == "windows" {
		a.WaitFor("id:section-reset", wait)
		if a.Exists("id:section-permissions") {
			t.Fatal("Windows shows the 权限 group")
		}
	} else {
		a.WaitFor("id:section-permissions >> text:未授权", wait)
	}
	a.Screenshot("settings-general")

	// 空间不足时提醒我 asks for notifications as it is turned on; refused, the switch stays on and says how to allow them.
	a.Press("role:switch 空间不足时提醒我")
	a.WaitFor("id:notify-denied", wait)
	a.Screenshot("settings-notify-denied")
	a.Press("role:switch 空间不足时提醒我")
	a.WaitForGone("id:notify-denied", wait)

	a.Press("id:tab-auto")
	a.WaitFor("id:rule-app-caches-status", wait)
	a.Screenshot("settings-auto-off")
	a.Press("role:switch 旧的应用缓存")
	a.Press("role:switch 闲置项目的构建产物")
	a.WaitFor("text:还没有运行过，下次后台检查时运行", wait)
	a.Screenshot("settings-auto-on")

	// The page scrolls inside the window rather than growing it.
	if v := a.Find("id:settings-page"); v.Frame.Size.Height > 820 {
		t.Fatalf("the page is %v points tall in an 820-point window", v.Frame.Size.Height)
	}

	a.Press("id:tab-cleanup")
	a.WaitFor("id:exclusions-list", wait)
	a.Screenshot("settings-cleanup")
	a.Scroll("id:settings-page", 600)
	a.Clear("id:large-file-min-field")
	a.Type("id:large-file-min-field", "5")
	a.WaitFor("id:large-file-min-error", wait)
	a.Screenshot("settings-cleanup-invalid")

	a.Press("id:tab-health")
	a.WaitFor("id:section-health", wait)
	a.Screenshot("settings-health")

	a.Press("id:tab-history")
	a.WaitFor("id:section-history", wait)
	a.Screenshot("settings-history")

	a.Press("id:tab-about")
	a.WaitFor("id:license-list", wait)
	a.Screenshot("settings-about")

	a.Press("id:tab-general")
	a.WaitFor("id:reset", wait)
	a.Press("id:reset")
	a.WaitFor("id:reset-dialog", wait)
	a.Screenshot("settings-reset")
	a.Press("id:confirm-cancel")
	a.WaitForGone("id:reset-dialog", wait)

	// At the window's smallest size the rows keep their controls on the right and wrap their descriptions.
	a.Resize(960, 680)
	a.Press("id:tab-health")
	a.WaitFor("id:section-health", wait)
	a.Screenshot("settings-health-narrow")
	a.Press("id:tab-cleanup")
	a.WaitFor("id:exclusions-list", wait)
	a.Screenshot("settings-cleanup-narrow")
}
