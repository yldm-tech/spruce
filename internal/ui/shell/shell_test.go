package shell_test

import (
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/pages/history"
	"github.com/yldm-tech/spruce/internal/ui/pages/history/historyfake"
	"github.com/yldm-tech/spruce/internal/ui/pages/maintenance/maintenancefake"
	"github.com/yldm-tech/spruce/internal/ui/pages/systemjunk"
	"github.com/yldm-tech/spruce/internal/ui/shell"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

func startShell(t *testing.T, fakes *storefake.Services, start nav.Route) (*nsgotest.App, *showing) {
	t.Helper()
	d := shell.Deps{Stores: fakes.Deps(), Start: start, SystemJunk: systemjunk.Deps{Cleanup: &cleanflowfake.Cleaner{}, Files: &cleanflowfake.Files{}},
		History: history.Deps{Catalog: historyfake.Catalog(maintenancefake.MacCatalog())}}
	stores := &showing{}
	a := nsgotest.Start(t, core.AppRoot(func(cx *core.Scope) {
		shell.App(d)(cx)
		stores.Stores, stores.app = store.Use(cx), cx
	}), nsgotest.Size(1280, 820))
	a.WaitFor("id:sidebar", wait)
	return a, stores
}

func TestSidebarHasSevenPages(t *testing.T) {
	a, _ := startShell(t, storefake.New(t.TempDir()), "")
	links := a.Query("id:sidebar >> role:link")
	want := []string{"nav-overview", "nav-uninstall", "nav-space", "nav-maintenance", "nav-health", "nav-history", "nav-settings"}
	if len(links) != len(want) {
		t.Fatalf("sidebar has %d entries, want %d", len(links), len(want))
	}
	for i, l := range links {
		if l.Props.TestID != want[i] {
			t.Errorf("entry %d is %q, want %q", i, l.Props.TestID, want[i])
		}
	}
	if !a.Find("id:nav-overview").Props.Selected {
		t.Error("概览 not lit on start")
	}
	for _, gone := range []string{"id:nav-smart-scan", "id:nav-system-junk", "id:nav-dev-junk", "id:nav-installers", "text:清理"} {
		if a.Exists("id:sidebar >> " + gone) {
			t.Errorf("sidebar still has %s", gone)
		}
	}
}

func TestDrillDownLightsItsParentAndGoesBack(t *testing.T) {
	a, _ := startShell(t, storefake.New(t.TempDir()), nav.SystemJunk)
	a.WaitFor("id:back", wait)
	if got := a.Find("id:back").Children[0].Text; got != "‹ 概览" {
		t.Errorf("back link %q", got)
	}
	if !a.Find("id:nav-overview").Props.Selected {
		t.Error("概览 not lit on its drill-down view")
	}
	a.Press("id:back")
	a.WaitForGone("id:back", wait)
	a.Find("role:header 概览")

	// A sidebar page has no link back.
	a.Press("id:nav-history")
	a.WaitFor("role:header 操作历史", wait)
	if a.Exists("id:back") || a.Find("id:nav-overview").Props.Selected {
		t.Error("a sidebar page shows a link back or lights 概览")
	}
}

func TestToastUndoAndView(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	ov := storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	ov.UndoRelease = make(chan struct{})
	ov.OnUndo = func(context.Context, string) (app.RestoreOutcome, error) {
		return app.RestoreOutcome{RunID: "restore-1", Restored: 2, Failed: 1}, nil
	}
	fakes.Overview = ov
	a, stores := startShell(t, fakes, "")
	stores.show(store.Toast{Text: "已移到废纸篓 3.1 GB", UndoRun: "run-1", Duration: 10 * time.Second})
	a.WaitFor("id:toast", wait)
	if got := a.Find("id:toast-text").Text; got != "已移到废纸篓 3.1 GB" {
		t.Errorf("toast %q", got)
	}
	a.Press("id:toast-undo")
	a.WaitFor("id:toast >> text:正在放回…", wait)
	if !a.Find("id:toast-undo").Props.Busy {
		t.Error("撤销 not busy while it puts back")
	}
	close(ov.UndoRelease)
	a.WaitFor("id:toast >> text:已放回 2 项，1 项没能放回", wait)
	if _, _, _, undos, _ := ov.Calls(); len(undos) != 1 || undos[0] != "run-1" {
		t.Errorf("undos %v", undos)
	}
	a.Press("id:toast-details")
	a.WaitFor("role:header 操作历史", wait)

	a.Press("id:toast-close")
	a.WaitForGone("id:toast", wait)
}

func TestToastWithoutAController(t *testing.T) {
	a, stores := startShell(t, storefake.New(t.TempDir()), "")
	stores.show(store.Toast{Text: "已移到废纸篓 1 GB", UndoRun: "run-1", Duration: 10 * time.Second})
	a.WaitFor("id:toast-undo", wait)
	a.Press("id:toast-undo")
	a.WaitFor("id:toast >> text:概览还没有接入", wait)
	if a.Exists("id:toast-undo") {
		t.Error("the error toast offers 撤销")
	}
}

// showing is the stores with the app's scope, to change them on the engine goroutine as a page would.
type showing struct {
	*store.Stores
	app *core.Scope
}

// show shows x as the toast.
func (s *showing) show(x store.Toast) { s.app.Post(func() { s.Toasts.Show(x) }) }

// startLaunch starts the shell on fakes as the system launched it (launched, nil for the user), counting the main views it shows.
func startLaunch(t *testing.T, launched func() (bool, bool), keepInTray bool) (*nsgotest.App, *int) {
	t.Helper()
	fakes := storefake.New(t.TempDir())
	fakes.Settings.General.KeepInTray = keepInTray
	views := new(int)
	d := shell.Deps{Stores: fakes.Deps(), LaunchedAtLogin: launched,
		SetMainView: func(cx *core.Scope, view func(cx *core.Scope) core.Node) {
			*views++
			cx.SetMainView(view)
		}}
	a := nsgotest.Start(t, core.AppRoot(shell.App(d)), nsgotest.Size(1280, 820))
	return a, views
}

func TestLoginLaunchStaysInTheMenuBar(t *testing.T) {
	a, views := startLaunch(t, func() (bool, bool) { return true, true }, true)
	// The launch is known to be at login and the window is to stay in the menu bar: it never opens, and two seconds later still has not.
	a.Advance(2 * time.Second)
	if *views != 0 || a.Exists("id:sidebar") {
		t.Fatalf("the window opened at a login start (%d views)", *views)
	}
}

func TestLaunchesThatOpenTheWindow(t *testing.T) {
	for _, c := range []struct {
		name     string
		launched func() (bool, bool)
		keep     bool
	}{
		{"the user opened it", nil, true},
		{"at login, closing the window quits", func() (bool, bool) { return true, true }, false},
		{"never known", func() (bool, bool) { return false, false }, true},
	} {
		a, views := startLaunch(t, c.launched, c.keep)
		a.Advance(2 * time.Second)
		a.WaitFor("id:sidebar", wait)
		if *views != 1 {
			t.Errorf("%s: %d main views, want 1", c.name, *views)
		}
	}
}

func TestNotificationsNeedThePermissionAndOpenWhatTheyAreAbout(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	ov := storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{Headline: 18 << 30}})
	fakes.Overview = ov
	a, _ := startShell(t, fakes, nav.History)

	// Without the permission nothing shows, and background work never asks for it.
	ov.Notify(app.Notice{Kind: app.NoticeLowDisk, ID: "low-disk", Volume: "Macintosh HD", Free: 8 << 30, Recommended: 18 << 30})
	a.Advance(time.Second)
	if len(a.Notifications) != 0 || a.NotificationPermission != core.NotificationsNotAsked {
		t.Fatalf("notifications %v, permission %v without asking", a.Notifications, a.NotificationPermission)
	}

	a.NotificationPermission = core.NotificationsGranted
	ov.Notify(app.Notice{Kind: app.NoticeLowDisk, ID: "low-disk", Volume: "Macintosh HD", Free: 8 << 30, Recommended: 18 << 30})
	a.WaitUntil(wait, func() bool { return len(a.Notifications) == 1 })
	if n := a.Notifications[0]; n.ID != "low-disk" || n.Title != "磁盘空间不多了" {
		t.Errorf("notification %+v", n)
	}
	a.PressNotification("low-disk")
	a.WaitFor("role:header 概览", wait)

	// The weekly summary replaces nothing and opens 操作历史.
	ov.Notify(app.Notice{Kind: app.NoticeAutoSummary, ID: "auto-summary:2026-W46", Trashed: 3 << 30})
	a.WaitUntil(wait, func() bool { return len(a.Notifications) == 1 && a.Notifications[0].ID == "auto-summary:2026-W46" })
	a.PressNotification("auto-summary:2026-W46")
	a.WaitFor("role:header 操作历史", wait)
}
