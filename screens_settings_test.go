package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/ui/pages/settings"
	"github.com/yldm-tech/spruce/internal/ui/pages/settings/settingsfake"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// TestScreensSettings shows the 设置 page at its top, scrolled to the folder lists and to 关于, with a number out of range and with the reset dialog, for comparison with the design.
func TestScreensSettings(t *testing.T) {
	const wait = 10 * time.Second
	fakes := storefake.New(t.TempDir())
	fakes.Permission = maintenance.PermissionDenied
	svc := settingsfake.New("/Users/me/Library/Application Support/Spruce")
	d := shell.Deps{Settings: settings.Deps{Files: &settingsfake.Files{}}, Stores: svc.Use(fakes.Deps())}
	a := nsgotest.Start(t, core.AppRoot(shell.App(d)), nsgotest.Size(1280, 820))
	a.WaitFor("id:sidebar", wait)
	a.Press("id:nav-settings")
	a.WaitFor("id:section-about", wait)
	a.Screenshot("settings-idle")

	// The page scrolls inside the window rather than growing it.
	if v := a.Find("id:settings-page"); v.Frame.Size.Height > 820 {
		t.Fatalf("the page is %v points tall in an 820-point window", v.Frame.Size.Height)
	}
	a.Scroll("id:settings-page", 560)
	a.Clear("id:large-file-min-field")
	a.Type("id:large-file-min-field", "5")
	a.WaitFor("id:large-file-min-error", wait)
	a.Screenshot("settings-cleanup")

	a.Scroll("id:settings-page", 2000)
	a.WaitFor("id:section-permissions >> text:未授权", wait)
	a.Screenshot("settings-about")

	a.Press("id:reset")
	a.WaitFor("id:reset-dialog", wait)
	a.Screenshot("settings-reset")
	a.Press("id:confirm-cancel")
	a.WaitForGone("id:reset-dialog", wait)
}
