package history_test

import (
	"context"
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	enginehist "github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/pages/history/historyfake"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// trashRun is a clean of installers today that moved 3 items (2.1 GB) to the Trash, all of which can be put back.
func trashRun(id string) enginehist.Run {
	r := span(id, enginehist.KindInstallers, daysAgo(0, 8, 30, 0), 2)
	r.Summary = &enginehist.Totals{Trashed: 2_100_000_000, Items: 3}
	r.Undo = &enginehist.UndoInfo{Items: 3, Bytes: 2_100_000_000}
	return r
}

func TestRestoreButtonOnlyOnRestorableRuns(t *testing.T) {
	svc := historyfake.New(trashRun("t1"), trashRun("t2"), junkRun())
	svc.SetRestorable("t1")
	p := startWith(t, svc, options{overview: storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})})
	p.WaitFor("id:run-t1-restore", wait)
	p.Find("id:run-t1-restore >> text:放回原处")
	// It sits beside the row's button, not inside it.
	if p.Exists("id:run-t1-head >> id:run-t1-restore") {
		t.Error("the restore button is inside the row's button")
	}
	if p.Exists("id:run-t2-restore") || p.Exists("id:run-j1-restore") {
		t.Error("a run that cannot be put back shows the button")
	}
	// Pressing the row elsewhere still opens it.
	p.Press("id:run-t1-head")
	p.WaitFor("id:run-t1 >> id:run-details", wait)
	if !p.Find("id:run-t1-head").Props.Expanded {
		t.Error("the row did not expand")
	}
	// Without restorable runs (Windows, or after 30 days, or put back already) no row has it.
	q := start(t, historyfake.New(trashRun("t1")))
	q.WaitFor("id:run-t1", wait)
	if q.Exists("id:run-t1-restore") {
		t.Error("the button shows for a run the service does not list")
	}
}

func TestRestorePutsBackAndShowsTheOutcome(t *testing.T) {
	svc := historyfake.New(trashRun("t1"))
	svc.SetRestorable("t1")
	ov := storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	ov.UndoRelease = make(chan struct{})
	ov.OnUndo = func(_ context.Context, runID string) (app.RestoreOutcome, error) {
		r := span("r1", enginehist.KindRestore, daysAgo(0, 9, 0, 0), 1)
		r.Summary = &enginehist.Totals{Items: 3, Trashed: 2_100_000_000}
		r.RestoreOf = runID
		svc.SetRestorable()
		svc.Record(r)
		return app.RestoreOutcome{RunID: "r1", Restored: 3, Bytes: 2_100_000_000}, nil
	}
	p := startWith(t, svc, options{overview: ov})
	p.WaitFor("id:run-t1-restore", wait)
	trashGen := p.stores.Trash.Generation()
	p.Press("id:run-t1-restore")
	p.WaitUntil(wait, func() bool { return p.Find("id:run-t1-restore").Props.Busy })
	p.Find("id:run-t1-restore >> text:正在放回…")
	if _, _, _, undos, _ := ov.Calls(); !reflect.DeepEqual(undos, []string{"t1"}) {
		t.Errorf("Undo calls %v", undos)
	}
	close(ov.UndoRelease)
	p.WaitForGone("id:run-t1-restore", wait)
	p.WaitFor("id:run-t1-restored", wait)
	if got := p.Find("id:run-t1-restored").Text; got != " · 已放回原处" {
		t.Errorf("summary note %q", got)
	}
	toast := p.stores.Toasts.Current()
	if toast == nil || toast.Text != "已放回原处 3 项" || toast.ViewRun != "" || toast.UndoRun != "" || toast.Duration != 6*time.Second {
		t.Errorf("toast %+v", toast)
	}
	if p.stores.Trash.Generation() == trashGen {
		t.Error("the Trash was not measured again")
	}
	// The restore is a row of its own.
	p.WaitFor("id:run-r1", wait)
	p.Find("id:run-r1 >> text:放回原处")
	p.Find("id:run-r1 >> text:放回 3 项（" + format.Bytes(2_100_000_000) + "）")
}

func TestRestorePartlyShowsWhatStayed(t *testing.T) {
	svc := historyfake.New(trashRun("t1"))
	svc.SetRestorable("t1")
	ov := storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	ov.OnUndo = func(_ context.Context, runID string) (app.RestoreOutcome, error) {
		r := span("r1", enginehist.KindRestore, daysAgo(0, 9, 0, 0), 1)
		r.Summary = &enginehist.Totals{Items: 2, Trashed: 100, Failed: 1}
		r.RestoreOf = runID
		r.Details = []string{"~/Downloads/a.dmg：原位置已有同名项目"}
		// The restore tried every item: the run is no longer offered (2.11), even though one stayed in the Trash.
		svc.SetRestorable()
		svc.Record(r)
		return app.RestoreOutcome{RunID: "r1", Restored: 2, Failed: 1}, nil
	}
	p := startWith(t, svc, options{overview: ov})
	p.WaitFor("id:run-t1-restore", wait)
	p.Press("id:run-t1-restore")
	p.WaitFor("id:run-t1-restored", wait)
	if got := p.Find("id:run-t1-restored").Text; got != " · 已放回 2 项" {
		t.Errorf("summary note %q", got)
	}
	toast := p.stores.Toasts.Current()
	if toast == nil || toast.Text != "已放回 2 项，1 项没能放回" || toast.ViewRun != "r1" || toast.Duration != 10*time.Second {
		t.Errorf("toast %+v", toast)
	}
	p.WaitFor("id:run-r1", wait)
	p.Find("id:run-r1 >> text:放回 2 项（" + format.Bytes(100) + "），1 项没能放回")
	p.WaitForGone("id:run-t1-restore", wait)
	// 查看 in the toast, pressed while the page is open, opens the restore's record.
	if p.Exists("id:run-r1 >> id:run-details") {
		t.Fatal("the restore's record is open already")
	}
	p.nav.GoHistory("", toast.ViewRun)
	p.WaitFor("id:run-r1 >> id:run-details", wait)
	p.Find("id:run-r1 >> text:~/Downloads/a.dmg：原位置已有同名项目")
}

func TestRestoreWithoutTheControllerSaysSo(t *testing.T) {
	svc := historyfake.New(trashRun("t1"))
	svc.SetRestorable("t1")
	p := start(t, svc)
	p.WaitFor("id:run-t1-restore", wait)
	p.Press("id:run-t1-restore")
	p.WaitUntil(wait, func() bool { t := p.stores.Toasts.Current(); return t != nil && t.Text == "概览还没有接入" })
	if !p.Enabled("id:run-t1-restore") {
		t.Error("the button stayed busy")
	}
}

func TestRestoreWaitsWhileACleanRuns(t *testing.T) {
	svc := historyfake.New(trashRun("t1"), trashRun("t2"))
	svc.SetRestorable("t1", "t2")
	ov := storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	ov.UndoRelease = make(chan struct{})
	p := startWith(t, svc, options{overview: ov})
	p.WaitFor("id:run-t2-restore", wait)
	p.Press("id:run-t1-restore")
	p.WaitUntil(wait, func() bool { return !p.Enabled("id:run-t2-restore") })
	close(ov.UndoRelease)
	p.WaitUntil(wait, func() bool { return p.Enabled("id:run-t2-restore") })
}

func TestAutoAndRestoreRunsAndTheirChips(t *testing.T) {
	auto := span("a1", enginehist.KindAuto, daysAgo(0, 3, 0, 0), 4)
	auto.Rule = "app-caches"
	auto.Summary = &enginehist.Totals{Trashed: 4_200_000_000, Items: 12}
	auto.Details = []string{"旧的应用缓存：12 项（4.2 GB）"}
	unknownRule := span("a2", enginehist.KindAuto, daysAgo(0, 2, 0, 0), 1)
	unknownRule.Rule = "future-rule"
	restore := span("r1", enginehist.KindRestore, daysAgo(0, 4, 0, 0), 1)
	restore.Summary = &enginehist.Totals{Items: 2, Trashed: 1_000_000}
	smart := span("s1", enginehist.KindSmartScan, daysAgo(0, 5, 0, 0), 3)
	smart.Summary = &enginehist.Totals{Freed: 500_000_000, Items: 4}
	p := start(t, historyfake.New(smart, restore, auto, unknownRule, junkRun()))
	p.WaitFor("id:chip-all", wait)
	var labels []string
	for _, v := range p.Query("role:toolbar 按类型筛选 >> role:button") {
		labels = append(labels, v.Props.Label)
	}
	if want := []string{"全部", "清理建议", "自动清理", "系统垃圾", "放回原处"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("chips %q", labels)
	}
	p.Find("id:run-a1 >> text:旧的应用缓存")
	p.Find("id:run-a1 >> text:移到" + format.TrashName() + " " + format.Bytes(4_200_000_000) + "，12 项")
	p.Find("id:run-a2 >> text:自动清理")
	p.Find("id:run-s1 >> text:清理建议")
	p.Find("id:run-r1 >> text:放回 2 项（" + format.Bytes(1_000_000) + "）")
	// What a restore put back is not counted as freed.
	if got := strings.Join(p.texts("id:history-total >> text:/./"), ""); got != "累计释放 "+format.Bytes(1_700_000_000) {
		t.Errorf("total %q", got)
	}
	p.Press("id:chip-auto")
	p.WaitUntil(wait, func() bool { return reflect.DeepEqual(p.rows("role:region"), []string{"a1", "a2"}) })
	p.Press("id:chip-restore")
	p.WaitUntil(wait, func() bool { return reflect.DeepEqual(p.rows("role:region"), []string{"r1"}) })
}

func TestOpensAsGoHistoryAsked(t *testing.T) {
	auto := span("a1", enginehist.KindAuto, daysAgo(0, 3, 0, 0), 4)
	auto.Rule = "installers"
	auto.Details = []string{"旧安装包：2 项（1 GB）"}
	p := startWith(t, historyfake.New(auto, junkRun()), options{before: func(n *nav.Navigator) { n.GoHistory(enginehist.KindAuto, "a1") }})
	p.WaitFor("id:run-a1 >> id:run-details", wait)
	if !p.Find("id:chip-auto").Props.Pressed {
		t.Error("the 自动清理 filter is not chosen")
	}
	if p.Exists("id:run-j1") {
		t.Error("other kinds show")
	}
	p.Find("id:run-a1 >> text:旧安装包：2 项（1 GB）")
	// The request is read once.
	if k, r := p.nav.TakeHistoryFilter(); k != "" || r != "" {
		t.Errorf("left %q %q", k, r)
	}
}

func TestFollowsGoHistoryWhileOpen(t *testing.T) {
	auto := span("a1", enginehist.KindAuto, daysAgo(0, 3, 0, 0), 4)
	auto.Rule = "installers"
	auto.Details = []string{"旧安装包：2 项（1 GB）"}
	p := start(t, historyfake.New(auto, junkRun()))
	p.WaitFor("id:chip-all", wait)
	p.Press("id:chip-systemJunk")
	p.WaitUntil(wait, func() bool { return !p.Exists("id:run-a1") })
	// A request made while the page is open (查看 in a toast over it) is taken at once: the filter it names and its run expanded.
	p.nav.GoHistory(enginehist.KindAuto, "a1")
	p.WaitFor("id:run-a1 >> id:run-details", wait)
	if !p.Find("id:chip-auto").Props.Pressed || p.Exists("id:run-j1") {
		t.Error("the 自动清理 filter is not chosen")
	}
	if k, r := p.nav.TakeHistoryFilter(); k != "" || r != "" {
		t.Errorf("the request was left for a later visit: %q %q", k, r)
	}
	// Closed by hand, the same request opens it again.
	p.Press("id:run-a1-head")
	p.WaitForGone("id:run-a1 >> id:run-details", wait)
	p.nav.GoHistory("", "a1")
	p.WaitFor("id:run-a1 >> id:run-details", wait)
	p.WaitFor("id:run-j1", wait)
}
