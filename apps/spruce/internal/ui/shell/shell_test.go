package shell_test

import (
	"context"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/schedule"
	"spruce/apps/spruce/internal/ui/cleanflow/cleanflowfake"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/history"
	"spruce/apps/spruce/internal/ui/pages/history/historyfake"
	"spruce/apps/spruce/internal/ui/pages/maintenance/maintenancefake"
	"spruce/apps/spruce/internal/ui/pages/systemjunk"
	"spruce/apps/spruce/internal/ui/shell"
	"spruce/apps/spruce/internal/ui/store"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

func startShell(t *testing.T, fakes *storefake.Services, start nav.Route) (*nsgotest.App, *showing) {
	t.Helper()
	d := shell.Deps{Stores: fakes.Deps(), Start: start, SystemJunk: systemjunk.Deps{Files: &cleanflowfake.Files{}},
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

func TestEscapeLeavesADrillDownView(t *testing.T) {
	a, _ := startShell(t, storefake.New(t.TempDir()), nav.SystemJunk)
	a.WaitFor("id:back", wait)
	a.Key("escape")
	a.WaitFor("role:header 概览", wait)
	// On a sidebar page Escape does nothing.
	a.Key("escape")
	a.Find("role:header 概览")
}

// drillState is a finished check whose 系统垃圾 holds one selected category that goes to the Trash, so 清理 asks a question that is not a danger one.
func drillState() app.OverviewState {
	logs := cleanup.Category{ID: "system-junk:logs", Source: "system-junk", Title: "日志", Kind: cleanup.KindCache, Deletion: cleanup.DeleteTrash, Selected: true,
		Items: []cleanup.Item{{ID: "/Users/me/Library/Logs/a.log", Path: "/Users/me/Library/Logs/a.log", Size: 24_000_000, Selected: true}}, Size: 24_000_000}
	res := cleanflowfake.Result(logs)
	res.Sources = []cleanup.SourceState{{ID: "system-junk", Status: cleanup.SourceDone}}
	return app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{CheckedAt: time.Now(), Mode: app.CheckForeground, Actionable: true, Scan: res}}
}

// On a drill-down view with a dialog open, Escape answers the dialog and leaves the view only once it is gone; the menu's 返回 waits for it too.
func TestEscapeClosesADialogBeforeLeavingADrillDown(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Overview = storefake.NewOverview(drillState())
	a, _ := startShell(t, fakes, nav.SystemJunk)
	a.WaitFor("id:clean", wait)
	a.Press("id:clean")
	a.WaitFor("id:confirm", wait)
	for _, m := range a.Menu {
		if m.Label == "前往" && !m.Items[len(m.Items)-1].Disabled {
			t.Error("返回 is enabled under the dialog")
		}
	}
	a.Key("escape")
	a.WaitForGone("id:confirm", wait)
	if !a.Exists("id:clean") {
		t.Fatal("Escape left 系统垃圾 along with its dialog")
	}
	a.Key("escape")
	a.WaitFor("role:header 概览", wait)
}

// The question before quitting, opened over a page's confirmation, takes Return: it must not start the clean under it.
func TestReturnUnderTheQuitQuestionLeavesThePageDialogAlone(t *testing.T) {
	activity := schedule.NewActivity(nil)
	_, end, err := activity.BeginClean(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	fakes := storefake.New(t.TempDir())
	ov := storefake.NewOverview(drillState())
	cleans := 0
	ov.OnClean = func(context.Context, cleanup.CleanRequest, app.CleanOrigin, func(cleanup.Progress)) (app.OverviewOutcome, error) {
		cleans++
		return app.OverviewOutcome{}, nil
	}
	fakes.Overview = ov
	d := shell.Deps{Stores: fakes.Deps(), Start: nav.SystemJunk, Activity: activity, SystemJunk: systemjunk.Deps{Files: &cleanflowfake.Files{}}}
	a := nsgotest.Start(t, core.AppRoot(shell.App(d)), nsgotest.Size(1280, 820), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	a.WaitFor("id:clean", wait)
	a.Press("id:clean")
	a.WaitFor("id:confirm", wait)
	a.ChooseMenu("Spruce", "退出 Spruce")
	a.WaitFor("id:quit-confirm", wait)
	a.Key("enter")
	a.Advance(time.Second)
	if cleans != 0 || !a.Exists("id:confirm >> id:confirm-ok") {
		t.Errorf("Return under the question confirmed the page's dialog (%d cleans)", cleans)
	}
	if a.Quits != 0 {
		t.Errorf("Return quit %d times", a.Quits)
	}
}

func TestMenuGoesToPages(t *testing.T) {
	a, _ := startShell(t, storefake.New(t.TempDir()), "")
	a.ChooseMenu("前往", "操作历史")
	a.WaitFor("role:header 操作历史", wait)
	if !a.Find("id:nav-history").Props.Selected {
		t.Error("the sidebar does not light 操作历史")
	}
	a.ChooseMenu("Spruce", "设置…")
	a.WaitFor("role:header 设置", wait)
}

// While a clean runs, 退出 Spruce asks first and keeps Spruce up on 继续清理; the question holds the sidebar and the menu's pages like any dialog.
func TestQuitDuringACleanAsks(t *testing.T) {
	activity := schedule.NewActivity(nil)
	_, end, err := activity.BeginClean(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	d := shell.Deps{Stores: storefake.New(t.TempDir()).Deps(), Start: nav.History, Activity: activity,
		History: history.Deps{Catalog: historyfake.Catalog(maintenancefake.MacCatalog())}}
	a := nsgotest.Start(t, core.AppRoot(shell.App(d)), nsgotest.Size(1280, 820))
	a.WaitFor("id:sidebar", wait)

	a.ChooseMenu("Spruce", "退出 Spruce")
	a.WaitFor("id:quit-confirm", wait)
	a.Find("id:quit-confirm >> text:正在清理")
	if a.Quits != 0 {
		t.Fatal("quit while a clean runs without asking")
	}
	if a.Enabled("id:nav-overview") {
		t.Error("the sidebar is reachable under the question")
	}
	for _, m := range a.Menu {
		if m.Label == "前往" && !m.Items[0].Disabled {
			t.Error("前往 is enabled under the question")
		}
	}
	a.Press("id:confirm-cancel")
	a.WaitForGone("id:quit-confirm", wait)
	if a.Quits != 0 {
		t.Error("继续清理 quit")
	}

	a.ChooseMenu("Spruce", "退出 Spruce")
	a.WaitFor("id:quit-confirm", wait)
	a.Press("id:confirm-ok")
	if a.Quits != 1 {
		t.Errorf("停止并退出 quit %d times", a.Quits)
	}

	// With nothing running it quits at once.
	end()
	a.ChooseMenu("Spruce", "退出 Spruce")
	if a.Quits != 2 {
		t.Errorf("退出 with nothing running: %d quits", a.Quits)
	}

	// A run that ends before the question is answered leaves nothing to ask about: Spruce quits, as asked.
	_, end, err = activity.BeginClean(context.Background())
	if err != nil {
		t.Fatal(err)
	}
	a.ChooseMenu("Spruce", "退出 Spruce")
	a.WaitFor("id:quit-confirm", wait)
	a.Advance(2 * time.Second)
	if a.Quits != 2 {
		t.Fatalf("quit while the clean still runs: %d quits", a.Quits)
	}
	end()
	a.Advance(time.Second)
	a.WaitForGone("id:quit-confirm", wait)
	if a.Quits != 3 {
		t.Errorf("the question outlived its run: %d quits", a.Quits)
	}
}

// 设置 opens on 通用 again, so where another tab was scrolled to means nothing there: it opens at the top until the tab it was on is kept as well.
func TestSettingsComesBackAtTheTop(t *testing.T) {
	a, _ := startShell(t, storefake.New(t.TempDir()), nav.Settings)
	a.Resize(960, 420)
	a.WaitFor("id:tab-auto", wait)
	a.Press("id:tab-auto")
	a.WaitFor("id:settings-tab-auto", wait)
	page := func() *nsgotest.View { return a.Find("id:settings-page") }
	end := page().Content.Height - page().Frame.Size.Height
	if end < 40 {
		t.Fatalf("自动清理 does not scroll at 960x420 (%v)", end)
	}
	a.Scroll("id:settings-page", end)
	a.Advance(200 * time.Millisecond)
	a.Press("id:nav-history")
	a.WaitFor("role:header 操作历史", wait)
	a.ChooseMenu("前往", "设置")
	a.WaitFor("id:settings-tab-general", wait)
	a.Advance(time.Second)
	if y := page().Offset.Y; y != 0 {
		t.Errorf("通用 at %v, scrolled to where 自动清理 was left", y)
	}
}

// Going to another page and back lands 概览 where it was left.
func TestOverviewComesBackWhereItWasLeft(t *testing.T) {
	a, _ := startShell(t, storefake.New(t.TempDir()), "")
	a.Resize(960, 420)
	a.WaitFor("id:check-start", wait)
	page := func() *nsgotest.View { return a.Find("id:overview-page") }
	end := page().Content.Height - page().Frame.Size.Height
	if end < 40 {
		t.Fatalf("概览 does not scroll at 960x420 (%v)", end)
	}
	a.Scroll("id:overview-page", end)
	a.Advance(200 * time.Millisecond)
	a.Press("id:nav-history")
	a.WaitFor("role:header 操作历史", wait)
	a.Press("id:nav-overview")
	a.WaitFor("id:check-start", wait)
	a.Advance(time.Second)
	if y := page().Offset.Y; y != end {
		t.Errorf("概览 at %v after going back, want %v", y, end)
	}
	// The header stayed in view all along.
	if h := a.Find("role:header 概览"); h.Frame.Size.Height == 0 {
		t.Error("the header is gone")
	}
}
