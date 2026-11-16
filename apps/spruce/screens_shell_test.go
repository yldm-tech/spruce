package main

import (
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/schedule"
	"spruce/apps/spruce/internal/ui/cleanflow/cleanflowfake"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/overview"
	"spruce/apps/spruce/internal/ui/pages/systemjunk"
	"spruce/apps/spruce/internal/ui/shell"
	"spruce/apps/spruce/internal/ui/store"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// shellScreensApp starts the shell on fakes on start, with 概览 stopped and 系统垃圾 idle, and activity as the clean gate (nil for none), and returns the stores, so a test can show a toast.
func shellScreensApp(t *testing.T, start nav.Route, activity *schedule.Activity) (*nsgotest.App, *showing) {
	t.Helper()
	fakes := storefake.New(t.TempDir())
	fakes.Locations = app.SpaceLocations{Volumes: []app.SpaceVolume{{Path: "/", Name: "Macintosh HD", Total: 994_660_000_000, Free: 128_400_000_000, Startup: true}}}
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	d := shell.Deps{Stores: fakes.Deps(), Start: start, Activity: activity,
		Overview:   overview.Deps{Files: &cleanflowfake.Files{}},
		SystemJunk: systemjunk.Deps{Files: &cleanflowfake.Files{}}}
	stores := &showing{}
	a := nsgotest.Start(t, core.AppRoot(func(cx *core.Scope) {
		shell.App(d)(cx)
		stores.Stores, stores.app = store.Use(cx), cx
	}), nsgotest.Size(1280, 820), wallNow)
	a.WaitFor("id:sidebar", 10*time.Second)
	return a, stores
}

// TestScreensShell shows the seven-page sidebar on 概览, a drill-down view with its link back and its parent lit, the undo toast, and the question before quitting during a clean.
func TestScreensShell(t *testing.T) {
	a, stores := shellScreensApp(t, "", nil)
	a.WaitFor("id:check-start", 10*time.Second)
	a.Screenshot("sidebar")

	stores.show(store.Toast{Text: "已移到废纸篓 3.1 GB", UndoRun: "run-1", ViewRun: "run-1", Duration: time.Hour})
	a.WaitFor("id:toast", 10*time.Second)
	a.Screenshot("toast")

	d, _ := shellScreensApp(t, nav.SystemJunk, nil)
	d.WaitFor("id:back", 10*time.Second)
	d.WaitFor("id:drill-system-junk", 10*time.Second)
	d.Screenshot("drill-back")

	activity := schedule.NewActivity(nil)
	_, end, err := activity.BeginClean(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	q, _ := shellScreensApp(t, "", activity)
	q.WaitFor("id:check-start", 10*time.Second)
	q.ChooseMenu("Spruce", "退出 Spruce")
	q.WaitFor("id:quit-confirm", 10*time.Second)
	q.Advance(time.Second)
	q.Screenshot("quit-confirm")
}

// showing is the stores with the app's scope, to change them on the engine goroutine as a page would.
type showing struct {
	*store.Stores
	app *core.Scope
}

// show shows x as the toast.
func (s *showing) show(x store.Toast) { s.app.Post(func() { s.Toasts.Show(x) }) }
