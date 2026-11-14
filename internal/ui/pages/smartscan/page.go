// Package smartscan is the 智能扫描 page, the app's home: one scan over 系统垃圾, 开发者垃圾 and 安装包 (the smart-scan cleaner) with the recommended items checked, cleaned through the same confirmation, overlay and done card as the cleaner pages (package cleanflow), beside informational cards about the disk, the CPU, Full Disk Access and 维护.
package smartscan

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/shared"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// mode is which body the page shows: the results stay through the clean and its done card.
type mode uint8

const (
	modeIdle mode = iota
	modeScanning
	modeResults
)

// Page is the 智能扫描 page.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	f := cleanflow.NewFlow(cx, deps.Cleanup, app.CleanerSmartScan)
	stores := store.Use(cx)
	scanRef, cleanRef := new(core.Ref), new(core.Ref)
	phase := f.Phase.Get
	m := core.NewMemo(cx, func() mode {
		switch phase() {
		case cleanflow.PhaseIdle:
			return modeIdle
		case cleanflow.PhaseScanning:
			return modeScanning
		}
		return modeResults
	})

	rescan := kit.Button{Text: "重新扫描", Variant: kit.ButtonGhost, OnPress: f.Scan, TestID: "rescan"}
	header := kit.PageHeader{Route: nav.Overview, Actions: []core.Node{
		core.Show{When: func() bool { return phase() == cleanflow.PhaseResults }, Then: func(*core.Scope) core.Node { return rescan }},
	}}

	body := core.Dynamic(func(*core.Scope) core.Node {
		switch m.Get() {
		case modeIdle:
			return core.Func(func(cx *core.Scope) core.Node {
				if f.TakeFocusRequest() {
					cx.Effect(scanRef.Focus)
				}
				return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 0, Gap: 18}, Children: []core.Node{hero(cx, f, scanRef), statusCards{}}}
			})
		case modeScanning:
			return core.Func(func(*core.Scope) core.Node { return scanning(f) })
		}
		return core.Func(func(cx *core.Scope) core.Node { return results(cx, f, cleanRef, revealer(deps.Files)) })
	})

	live := kit.LiveStatus{Text: func() string {
		switch phase() {
		case cleanflow.PhaseScanning:
			return "正在扫描…"
		case cleanflow.PhaseResults:
			if r := f.Result.Get(); r != nil {
				return "扫描完成，可清理 " + format.Bytes(RecommendedTotals(*r).Bytes)
			}
		case cleanflow.PhaseCleaning:
			return "正在清理…"
		}
		return ""
	}}

	confirm := core.Dynamic(func(*core.Scope) core.Node {
		if phase() != cleanflow.PhaseResults || !f.Confirming.Get() {
			return core.Group{}
		}
		return core.Func(func(cx *core.Scope) core.Node {
			d := core.Untracked(cx, func() kit.ConfirmDialog { return cleanflow.CleanPlanDialog(f.Categories(), f.Selection.Get()) })
			d.Open = func() bool { return true }
			// The dialog goes with Confirming, before it could hand the focus back itself.
			d.OnConfirm = func() {
				f.Confirm()
				cleanRef.Focus()
			}
			d.OnCancel = func() {
				f.Cancel()
				cleanRef.Focus()
			}
			return d
		})
	})

	overlay := cleanflow.CleaningOverlay{Open: func() bool { return phase() == cleanflow.PhaseCleaning }, Progress: f.Progress.Get, OnCancel: f.Stop,
		TestID: "clean-overlay", Sub: func() string { return "共 " + strconv.Itoa(f.Totals().Count) + " 项" }}

	finish := func() {
		f.Finish()
		// The clean gave space back: the disk card reads the disks again.
		stores.Locations.Bump()
	}
	done := core.Dynamic(func(*core.Scope) core.Node {
		if phase() != cleanflow.PhaseDone {
			return core.Group{}
		}
		return core.Func(func(*core.Scope) core.Node {
			o := f.Outcome.GetUntracked()
			if o == nil {
				o = &cleanflow.CleanOutcome{Err: "清理没有完成"}
			}
			out, inUse := *o, 0
			if o.Result != nil {
				r, n := cleanflow.SetInUseAside(*o.Result)
				out, inUse = cleanflow.CleanOutcome{Result: &r}, n
			}
			return cleanflow.CleanDoneDialog{Open: func() bool { return true }, Outcome: out, InUse: inUse, OnClose: finish, TestID: "done"}
		})
	})

	return core.Group{
		kit.Page{TestID: "smart-scan-page", Children: []core.Node{header, body}},
		live, confirm, overlay, done,
	}
}

// hero is the page before a scan (.smart-hero): the big round 扫描, what 智能扫描 does, the last smart clean and why the last scan failed.
func hero(cx *core.Scope, f *cleanflow.Flow, scanRef *core.Ref) core.Node {
	const title = "一次检查系统垃圾、开发者垃圾和安装包"
	last := shared.LastClean(cx, history.KindSmartScan)
	sub := func(s func() string, testID string) core.Node {
		return core.Text{ContentFunc: s, TestID: testID, Style: core.Style{MaxWidth: core.Pt(520), FontFamily: kit.FontSans, FontSize: kit.FontBody,
			LineHeightMultiple: 1.5, Color: kit.ColorTextMuted, TextAlign: core.TextAlignCenter}}
	}
	lastText := func() string {
		if run := last(); run != nil {
			return shared.LastCleanText(*run, timeNow())
		}
		return ""
	}
	meta := nav.Overview.Meta()
	return core.View{Role: core.RoleRegion, Label: title, TestID: "hero",
		Style: core.Style{FlexGrow: 1, FlexShrink: 0, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 12,
			Padding: core.Edges{Top: 40, Bottom: 24}, TextAlign: core.TextAlignCenter},
		Children: []core.Node{
			core.View{Style: core.Style{Margin: core.Edges{Bottom: 30}}, Children: []core.Node{
				heroButton{Text: "扫描", Glyph: meta.Glyph, OnPress: f.Scan, Ref: scanRef, TestID: "scan"},
			}},
			kit.Text(title, kit.FontTitle, kit.Semibold, kit.ColorText),
			sub(fixed("扫描只读取，不会删除任何东西。结果会按推荐勾选可以放心清理的项目，你确认后才会清理。"), ""),
			// Only there while there is something to say, so the gaps stay the design's.
			core.Show{When: func() bool { return lastText() != "" }, Then: func(*core.Scope) core.Node { return sub(lastText, "last-clean") }},
			core.Show{When: func() bool { return f.ScanErr.Get() != "" }, Then: func(*core.Scope) core.Node {
				return core.Text{ContentFunc: f.ScanErr.Get, LiveRegion: core.LiveRegionPolite, Role: core.RoleAlert, TestID: "scan-error",
					Style: core.Style{MaxWidth: core.Pt(520), FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5,
						Color: kit.ColorWarning, TextAlign: core.TextAlignCenter}}
			}},
		}}
}
