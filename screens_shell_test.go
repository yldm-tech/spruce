package main

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/pages/smartscan"
	"github.com/yldm-tech/spruce/internal/ui/pages/systemjunk"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// shellScreensApp starts the shell on fakes on start, with 概览 (still the 智能扫描 page) and 系统垃圾 idle, and returns the stores, so a test can show a toast.
func shellScreensApp(t *testing.T, start nav.Route) (*nsgotest.App, *showing) {
	t.Helper()
	fakes := storefake.New(t.TempDir())
	fakes.Locations = app.SpaceLocations{Volumes: []app.SpaceVolume{{Path: "/", Name: "Macintosh HD", Total: 994_660_000_000, Free: 128_400_000_000, Startup: true}}}
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	d := shell.Deps{Stores: fakes.Deps(), Start: start,
		SmartScan:  smartscan.Deps{Cleanup: &cleanflowfake.Cleaner{}, Files: &cleanflowfake.Files{}},
		SystemJunk: systemjunk.Deps{Cleanup: &cleanflowfake.Cleaner{}, Files: &cleanflowfake.Files{}}}
	stores := &showing{}
	a := nsgotest.Start(t, core.AppRoot(func(cx *core.Scope) {
		shell.App(d)(cx)
		stores.Stores, stores.app = store.Use(cx), cx
	}), nsgotest.Size(1280, 820))
	a.WaitFor("id:sidebar", 10*time.Second)
	return a, stores
}

// TestScreensShell shows the seven-page sidebar on 概览, a drill-down view with its link back and its parent lit, and the undo toast.
func TestScreensShell(t *testing.T) {
	a, stores := shellScreensApp(t, "")
	a.WaitFor("id:status-disk-note", 10*time.Second)
	a.Screenshot("sidebar")

	stores.show(store.Toast{Text: "已移到废纸篓 3.1 GB", UndoRun: "run-1", ViewRun: "run-1", Duration: time.Hour})
	a.WaitFor("id:toast", 10*time.Second)
	a.Screenshot("toast")

	d, _ := shellScreensApp(t, nav.SystemJunk)
	d.WaitFor("id:back", 10*time.Second)
	d.WaitFor("id:scan", 10*time.Second)
	d.Screenshot("drill-back")
}

// showing is the stores with the app's scope, to change them on the engine goroutine as a page would.
type showing struct {
	*store.Stores
	app *core.Scope
}

// show shows x as the toast.
func (s *showing) show(x store.Toast) { s.app.Post(func() { s.Toasts.Show(x) }) }
