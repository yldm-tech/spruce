package main

import (
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/health"
	"spruce/apps/spruce/internal/engine/taskresult"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/maintenance"
	"spruce/apps/spruce/internal/ui/pages/maintenance/maintenancefake"
	"spruce/apps/spruce/internal/ui/shell"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// screensApp starts the shell on fakes and goes to page: through the sidebar for its pages, and by opening on it for a drill-down view, which has no entry there.
// wallNow sets the wall time the pages read (core.Scope.WallTime) to the real time, which the fakes' states are made relative to; the test clock itself stays manual.
func wallNow(a *nsgotest.App) { a.Clock.SetWallTime(time.Now()) }

func screensApp(t *testing.T, fakes *storefake.Services, d shell.Deps, page string) *nsgotest.App {
	t.Helper()
	d.Stores = fakes.Deps()
	r := nav.Route(page)
	if !r.InSidebar() {
		d.Start = r
	}
	a := nsgotest.Start(t, core.AppRoot(shell.App(d)), nsgotest.Size(1280, 820), wallNow)
	a.WaitFor("id:sidebar", 10*time.Second)
	if r.InSidebar() {
		a.Press("id:nav-" + page)
	}
	return a
}

// TestScreensMaintenance shows the 维护 page idle, with the CPU alert, running and done, for comparison with the design.
func TestScreensMaintenance(t *testing.T) {
	release := make(chan struct{})
	svc := &maintenancefake.Service{Tasks: maintenancefake.MacCatalog(),
		OnRun: func(ctx context.Context, ids []string, progress func(app.ProgressEvent)) (app.RunSummary, error) {
			progress(app.ProgressEvent{Kind: app.ProgressStarted, TaskID: "dns", Done: 0, Total: len(ids)})
			select {
			case <-release:
			case <-ctx.Done():
			}
			return maintenancefake.Summary(
				maintenancefake.Result("dns", taskresult.Applied, "已刷新 DNS 缓存"),
				maintenancefake.Result("quicklook", taskresult.Applied, "已刷新 QuickLook"),
				maintenancefake.Result("icons", taskresult.Applied, "已重建图标缓存"),
				maintenancefake.Result("launchservices", taskresult.Unchanged, "没有需要清理的登记"),
			), nil
		}}
	fakes := storefake.New(t.TempDir())
	a := screensApp(t, fakes, shell.Deps{Maintenance: maintenance.Deps{Maintenance: svc}}, "maintenance")
	a.WaitUntil(10*time.Second, func() bool { return len(a.Query("role:switch")) == 6 })
	a.Screenshot("maintenance-idle")

	fakes.Alert([]health.Alert{{PID: 4242, Name: "Xcode", CPUPercent: 48.3, Duration: 12 * time.Minute, Threshold: 40}})
	a.WaitFor("id:alert-banner", 10*time.Second)
	a.Screenshot("maintenance-alert")
	fakes.Alert(nil)
	a.WaitForGone("id:alert-banner", 10*time.Second)

	a.Press("id:run")
	a.WaitFor("id:run-overlay >> text:刷新 DNS 缓存 · 已完成 0/4", 10*time.Second)
	a.Screenshot("maintenance-running")

	close(release)
	a.WaitFor("id:done", 10*time.Second)
	a.Screenshot("maintenance-done")
	a.Press("id:done-close")
	a.WaitForGone("id:done", 10*time.Second)
}
