package space_test

import (
	"context"
	"errors"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/growth"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/space/spacefake"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// undoable returns fakes with 概览's controller on macOS (moves can be undone) whose undo puts one item back.
func undoable(t *testing.T) *storefake.Services {
	fakes := newFakes(t)
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle, CanUndo: true})
	fakes.Overview.OnUndo = func(context.Context, string) (app.RestoreOutcome, error) {
		return app.RestoreOutcome{RunID: "restore-1", Restored: 1}, nil
	}
	return fakes
}

func trashed(svc *spacefake.Service) {
	root := spacefake.Root
	root.Size = 7e9
	svc.TrashResult = app.SpaceTrashResult{Trashed: 3e9, Items: 1, Failed: []app.SpaceFailure{}, Root: root, RunID: "run-1", Undoable: true}
}

// undo puts back runID through the store, as the toast's 撤销 does.
func (p *page) undo(runID string) {
	p.Post(func() { p.stores.Overview.Undo(p.root, runID, nil, nil) })
	p.Settle()
}

func TestOneClickMoveOffersUndoAndScansAgainAfterIt(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	p := start(t, svc, undoable(t))
	p.WaitFor(homeList, wait)
	trashGen := p.stores.Trash.Generation()
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	p.WaitUntil(wait, func() bool { return len(svc.Trashes()) == 1 })
	if p.Exists("id:trash-confirm") {
		t.Error("asked before a move that can be undone")
	}
	if got := p.waitToast(); got.Text != "已移到废纸篓 3 GB" || got.UndoRun != "run-1" || got.ViewRun != "" || got.Duration != 10*time.Second {
		t.Errorf("toast %+v", got)
	}
	if _, v := p.actionStat(true); v != "7 GB" {
		t.Errorf("stat %q", v)
	}
	if p.stores.Trash.Generation() == trashGen {
		t.Error("the Trash's generation did not change")
	}

	// An undo of another run leaves the page as it is.
	p.undo("elsewhere")
	p.WaitUntil(wait, func() bool { return p.stores.Overview.LastUndo().Seq == 1 })
	if len(svc.Scans()) != 0 {
		t.Errorf("scanned after another run's undo: %v", svc.Scans())
	}
	p.undo("run-1")
	p.WaitFor("text:正在分析「个人文件夹」…", wait)
	p.WaitUntil(wait, func() bool { return len(svc.Scans()) == 1 })
	if got := svc.Scans(); got[0] != spacefake.Home {
		t.Errorf("scans %v", got)
	}
	if _, _, _, undos, _ := p.fakes.Overview.Calls(); !slices.Equal(undos, []string{"elsewhere", "run-1"}) {
		t.Errorf("undos %v", undos)
	}
}

func TestUndoOverTheStartScreenScansOnReturn(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	p := start(t, svc, undoable(t))
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	p.waitToast()
	p.Press("id:change-location")
	p.WaitFor("id:volume-/", wait)
	p.undo("run-1")
	p.WaitUntil(wait, func() bool { return p.stores.Overview.LastUndo().Seq == 1 })
	if len(svc.Scans()) != 0 || !p.Exists("id:volume-/") {
		t.Fatalf("scanned over the start screen: %v", svc.Scans())
	}
	p.Press("id:return-results")
	p.WaitFor("text:正在分析「个人文件夹」…", wait)
}

// TestActionBarRisesOverTheToast: the shell's toast sits at the bottom of the page area, 24 points up and at least 44 tall, so while it shows the figures and 重新扫描 stand above it, and come back down once it goes.
func TestActionBarRisesOverTheToast(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	p := start(t, svc, undoable(t))
	p.WaitFor(homeList, wait)
	// bottom is where 重新扫描 ends, from the top of the window.
	bottom := func() float32 {
		v := p.Find("id:rescan")
		y := float32(v.Frame.Size.Height)
		for ; v != nil; v = v.Parent {
			y += float32(v.Frame.Origin.Y)
			if v.Props.TestID == "space-page" {
				y -= float32(v.Offset.Y)
			}
		}
		return y
	}
	without := bottom()
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	p.waitToast()
	p.Settle()
	if got := bottom(); got > 820-24-44-12 || got >= without {
		t.Errorf("重新扫描 ends at %v with the toast, %v without it", got, without)
	}
	if page := p.Find("id:space-page"); page.Content.Height > page.Frame.Size.Height {
		t.Errorf("the page scrolls with the toast: content %v in %v", page.Content.Height, page.Frame.Size.Height)
	}
	p.Post(p.stores.Toasts.Dismiss)
	p.Settle()
	if got := bottom(); got != without {
		t.Errorf("重新扫描 ends at %v after the toast, %v before it", got, without)
	}
}

// TestUndoWhileAwayScansOnReturn: the toast's 撤销 outlives the page, so a move undone while another page shows scans its location again when 空间透视 opens again, and only once.
func TestUndoWhileAwayScansOnReturn(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	kept := *svc.Current()
	svc.OnScan = func(context.Context, string, func(app.SpaceProgress)) (app.SpaceSummary, error) { return kept, nil }
	p := start(t, svc, undoable(t))
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	p.waitToast()
	p.Post(func() { p.shown.Set(false) })
	p.WaitForGone("id:space-page", wait)
	p.undo("run-1")
	p.WaitUntil(wait, func() bool { return p.stores.Overview.LastUndo().Seq == 1 })
	p.Post(func() { p.shown.Set(true) })
	p.WaitUntil(wait, func() bool { return len(svc.Scans()) == 1 })
	if got := svc.Scans(); got[0] != spacefake.Home {
		t.Errorf("scans %v", got)
	}
	p.WaitFor(homeList, wait)

	// Opening the page again after that scan does not scan once more.
	p.Post(func() { p.shown.Set(false) })
	p.WaitForGone("id:space-page", wait)
	p.Post(func() { p.shown.Set(true) })
	p.WaitFor(homeList, wait)
	if got := svc.Scans(); len(got) != 1 {
		t.Errorf("scanned again: %v", got)
	}
}

// TestUndoAfterLeavingAndComingBack: a move made on an earlier showing of the page is still the page's when 撤销 comes after it opened again.
func TestUndoAfterLeavingAndComingBack(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	p := start(t, svc, undoable(t))
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	p.waitToast()
	p.Post(func() { p.shown.Set(false) })
	p.WaitForGone("id:space-page", wait)
	p.Post(func() { p.shown.Set(true) })
	p.WaitFor(homeList, wait)
	if len(svc.Scans()) != 0 {
		t.Fatalf("scanned without an undo: %v", svc.Scans())
	}
	p.undo("run-1")
	p.WaitUntil(wait, func() bool { return len(svc.Scans()) == 1 })
}

func TestOneClickMoveSaysWhatCouldNotBeUndone(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	svc.TrashResult.Undoable, svc.TrashResult.NotUndoable = false, 1
	p := start(t, svc, undoable(t))
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	if got := p.waitToast(); got.Text != "已移到废纸篓 3 GB，其中 1 项不能撤销" || got.UndoRun != "" {
		t.Errorf("toast %+v", got)
	}
}

// TestMoveConfirmsWhereItCannotBeUndone is the Windows case (OverviewState.CanUndo false): the confirmation stays and the toast names the Recycle Bin's restore.
func TestMoveConfirmsWhereItCannotBeUndone(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	svc.TrashResult.Undoable = false
	fakes := newFakes(t)
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle, CanUndo: false})
	p := start(t, svc, fakes)
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	p.WaitFor("id:trash-confirm", wait)
	if len(svc.Trashes()) != 0 {
		t.Fatal("moved before the confirmation")
	}
	p.Press("id:confirm-ok")
	if got := p.waitToast(); got.Text != "已移到废纸篓 3 GB，需要时可以在废纸篓里还原" || got.UndoRun != "" {
		t.Errorf("toast %+v", got)
	}
}

func TestOneClickMoveThatMovedNothingSaysWhy(t *testing.T) {
	svc := spacefake.New(time.Now())
	svc.TrashResult = app.SpaceTrashResult{Failed: []app.SpaceFailure{{Path: spacefake.ISO.Path, Reason: "没有权限移动这个项目"}}, Root: spacefake.Root, RunID: "run-2"}
	p := start(t, svc, undoable(t))
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	if got := p.WaitFor("id:space-notice-error", wait).Text; got != "「big.iso」没有移走：没有权限移动这个项目" {
		t.Errorf("alert %q", got)
	}
	if p.stores.Toasts.Current() != nil {
		t.Error("a toast for a move that moved nothing")
	}
}

func TestGoSpaceScansTheFolderAsThePageOpens(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := &page{svc: svc, files: &spacefake.Files{}, fakes: newFakes(t)}
	p.fakes.Locations = spacefake.Locations()
	mount(t, p, false)
	caches := filepath.Join(spacefake.Home, "Library", "Caches")
	p.Post(func() {
		p.nav.GoSpace(caches)
		p.shown.Set(true)
	})
	p.WaitFor("text:正在分析「Caches」…", wait)
	p.WaitUntil(wait, func() bool { return len(svc.Scans()) == 1 })
	if got := svc.Scans(); got[0] != caches {
		t.Errorf("scans %v", got)
	}
	if p.nav.TakeSpacePath() != "" {
		t.Error("the folder was not taken")
	}
}

// growthFakes are fakes whose controller lists two locations that grew, the first covered by the 应用缓存 card.
func growthFakes(t *testing.T, now time.Time) *storefake.Services {
	fakes := newFakes(t)
	spotify := spacefake.Home + "/Library/Caches/com.spotify.client"
	res := cleanup.ScanResult{Categories: []cleanup.Category{{ID: "system-junk:app-caches", Items: []cleanup.Item{{ID: "i1", Path: spotify, Size: 6e9, Selected: true}}, Size: 6e9}}, Items: 1, Size: 6e9}
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle, CanUndo: true, Result: &app.OverviewResult{Scan: res,
		Recommendations: []advice.Recommendation{{ID: "app-caches", Title: "应用缓存", Items: []advice.ItemRef{{CategoryID: "system-junk:app-caches", ItemID: "i1"}}}}}})
	fakes.Overview.GrowthList = []growth.Growth{
		{Path: spotify, Icon: "cache", Before: 1e9, After: 7e9, Since: now.Add(-3 * 24 * time.Hour)},
		{Path: spacefake.Home + "/Movies", Label: "影片", Icon: "video", Before: 10e9, After: 12e9, Since: now.Add(-6 * time.Hour), AtLeast: true},
	}
	return fakes
}

var weekdays = []string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

func TestGrowthRowsAndTabs(t *testing.T) {
	now := time.Now()
	svc := &spacefake.Service{Listings: spacefake.Listings()}
	fakes := growthFakes(t, now)
	p := start(t, svc, fakes)
	p.WaitFor("id:growth-row-0", wait)
	p.Find(`id:space-growth >> role:header "最近变化"`)
	if !p.Find("id:growth-7d").Props.Selected {
		t.Error("7 天 is not the first tab shown")
	}
	p.Find("id:space-growth >> text:每天记录一次，保留 90 天。")
	if got := p.Find("id:growth-row-0-text").Text; got != "从"+weekdays[now.Add(-3*24*time.Hour).Weekday()]+"到现在涨了 6 GB" {
		t.Errorf("row 0 %q", got)
	}
	p.Find("id:growth-row-0 >> text:~/Library/Caches/com.spotify.client")
	p.Find("id:growth-row-0 >> text:+6 GB")
	p.Find("id:growth-row-0 >> text:1 GB → 7 GB")
	if got := p.Find("id:growth-row-1-text").Text; got != "从6 小时前到现在至少涨了 2 GB" {
		t.Errorf("row 1 %q", got)
	}
	p.Find("id:growth-row-1 >> text:影片")
	if _, _, _, _, windows := fakes.Overview.Calls(); !slices.Equal(windows, []time.Duration{7 * 24 * time.Hour}) {
		t.Errorf("windows %v", windows)
	}

	p.Press("id:growth-24h")
	p.WaitUntil(wait, func() bool {
		_, _, _, _, w := fakes.Overview.Calls()
		return len(w) == 2
	})
	p.Press("id:growth-30d")
	p.WaitUntil(wait, func() bool {
		_, _, _, _, w := fakes.Overview.Calls()
		return len(w) == 3
	})
	if _, _, _, _, w := fakes.Overview.Calls(); !slices.Equal(w, []time.Duration{7 * 24 * time.Hour, 24 * time.Hour, 30 * 24 * time.Hour}) {
		t.Errorf("windows %v", w)
	}
	if !p.Find("id:growth-30d").Props.Selected {
		t.Error("30 天 is not selected")
	}
}

func TestGrowthRowActions(t *testing.T) {
	now := time.Now()
	svc := &spacefake.Service{Listings: spacefake.Listings()}
	p := start(t, svc, growthFakes(t, now))
	p.WaitFor("id:growth-row-1-space", wait)
	if p.Exists("id:growth-row-0-space") || p.Exists("id:growth-row-1-clean") {
		t.Error("the rows offer the wrong action")
	}
	p.Find("id:growth-row-1-space >> text:扫描")
	p.Press(`id:growth-row-1-reveal`)
	p.WaitUntil(wait, func() bool { return len(p.files.Reveals()) == 1 })
	if got := p.files.Reveals()[0]; got != spacefake.Home+"/Movies" {
		t.Errorf("reveal %q", got)
	}
	if got := p.Find("id:growth-row-1-reveal").Props.Label; got != "在访达中显示 影片" {
		t.Errorf("reveal label %q", got)
	}

	p.Press("id:growth-row-1-space")
	p.WaitFor("text:正在分析「Movies」…", wait)
	p.WaitUntil(wait, func() bool { return len(svc.Scans()) == 1 })
	if got := svc.Scans()[0]; got != spacefake.Home+"/Movies" {
		t.Errorf("scan %q", got)
	}
	p.Press("id:space-stop")
	p.WaitFor("id:growth-row-0-clean", wait)
	p.Press("id:growth-row-0-clean")
	p.Settle()
	if p.nav.Current() != nav.Overview || p.nav.TakeOverviewCard() != "app-caches" {
		t.Errorf("去清理 went to %q", p.nav.Current())
	}
}

func TestGrowthEmpty(t *testing.T) {
	// Fewer than two snapshots: the controller answers growth.ErrNoHistory.
	fakes := newFakes(t)
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	fakes.Overview.GrowthErr = growth.ErrNoHistory
	p := start(t, &spacefake.Service{Listings: spacefake.Listings()}, fakes)
	if got := p.WaitFor("id:growth-empty", wait).Text; got != "Spruce 会定期记下主要位置的大小，过一两天这里就会显示哪些位置在变大。" {
		t.Errorf("empty %q", got)
	}

	// Without the controller it says the same.
	q := start(t, &spacefake.Service{Listings: spacefake.Listings()}, newFakes(t))
	if got := q.WaitFor("id:growth-empty", wait).Text; got != "Spruce 会定期记下主要位置的大小，过一两天这里就会显示哪些位置在变大。" {
		t.Errorf("empty without a controller %q", got)
	}

	// The records cannot be read: it says so rather than that they are coming.
	fakes = newFakes(t)
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	fakes.Overview.GrowthErr = errors.New("growth: permission denied")
	e := start(t, &spacefake.Service{Listings: spacefake.Listings()}, fakes)
	if got := e.WaitFor("id:growth-error", wait).Text; got != "无法读取空间变化的记录" {
		t.Errorf("error %q", got)
	}
	if e.Exists("id:growth-empty") {
		t.Error("an error read as no history")
	}

	// Nothing grew enough: the sentence follows the tab.
	fakes = newFakes(t)
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	r := start(t, &spacefake.Service{Listings: spacefake.Listings()}, fakes)
	if got := r.WaitFor("id:growth-empty", wait).Text; got != "最近 7 天没有明显变大的位置。" {
		t.Errorf("none %q", got)
	}
	r.Press("id:growth-24h")
	r.WaitUntil(wait, func() bool { return r.Find("id:growth-empty").Text == "最近 24 小时没有明显变大的位置。" })
	r.Press("id:growth-30d")
	r.WaitUntil(wait, func() bool { return r.Find("id:growth-empty").Text == "最近 30 天没有明显变大的位置。" })
	if r.Exists("id:growth-row-0") {
		t.Error("a row without growth")
	}
}

func TestGrowthListsAtMostTwenty(t *testing.T) {
	now := time.Now()
	fakes := newFakes(t)
	fakes.Overview = storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	for i := range 25 {
		fakes.Overview.GrowthList = append(fakes.Overview.GrowthList, growth.Growth{Path: spacefake.Home + "/f" + string(rune('a'+i)), Before: 1e9, After: int64(30-i) * 1e9, Since: now.Add(-48 * time.Hour)})
	}
	p := start(t, &spacefake.Service{Listings: spacefake.Listings()}, fakes)
	p.WaitFor("id:growth-row-19", wait)
	if p.Exists("id:growth-row-20") {
		t.Error("more than 20 rows")
	}
}
