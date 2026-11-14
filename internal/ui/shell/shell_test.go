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

func startShell(t *testing.T, fakes *storefake.Services, start nav.Route) (*nsgotest.App, *store.Stores) {
	t.Helper()
	d := shell.Deps{Stores: fakes.Deps(), Start: start, SystemJunk: systemjunk.Deps{Cleanup: &cleanflowfake.Cleaner{}, Files: &cleanflowfake.Files{}},
		History: history.Deps{Catalog: historyfake.Catalog(maintenancefake.MacCatalog())}}
	var stores *store.Stores
	a := nsgotest.Start(t, core.AppRoot(func(cx *core.Scope) {
		shell.App(d)(cx)
		stores = store.Use(cx)
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
	a.Post(func() {
		stores.Toasts.Show(store.Toast{Text: "已移到废纸篓 3.1 GB", UndoRun: "run-1", Duration: 10 * time.Second})
	})
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
	a.Post(func() {
		stores.Toasts.Show(store.Toast{Text: "已移到废纸篓 1 GB", UndoRun: "run-1", Duration: 10 * time.Second})
	})
	a.WaitFor("id:toast-undo", wait)
	a.Press("id:toast-undo")
	a.WaitFor("id:toast >> text:概览还没有接入", wait)
	if a.Exists("id:toast-undo") {
		t.Error("the error toast offers 撤销")
	}
}
