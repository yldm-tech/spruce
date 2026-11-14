package cleanflow

import (
	"context"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// ViewConfig is what a cleaner page puts into View.
type ViewConfig struct {
	// Page is the page, for its header and tile.
	Page nav.Route
	// IdleTitle is what the idle page offers to do, e.g. 扫描系统垃圾; IdleDescription what the scan looks at.
	IdleTitle, IdleDescription string
	// IdleDetails, when set, builds what sits under the description before a scan, such as the last clean (IdleDetails).
	IdleDetails func(cx *core.Scope) core.Node
	// ScanDisabled disables 扫描, e.g. when there is nowhere to search.
	ScanDisabled func() bool
	// ListLabel is the result list's accessible name; Notice a line above the results, such as where the items go.
	ListLabel, Notice string
	// SizeLabel labels the checked size beside 清理: 可释放 when it is freed at once, otherwise what it is.
	SizeLabel string
	// EmptyTitle and EmptyDescription are the page when the scan found nothing.
	EmptyTitle, EmptyDescription string
	// Results, when set, builds the results in place of the notices and the one CategoryList (开发者垃圾's two sections); the action bar stays.
	Results func(cx *core.Scope, f *Flow) core.Node
	// Confirm, when set, builds the question asked before a clean; View sets its Open, OnConfirm and OnCancel. CleanPlanDialog when nil.
	Confirm func(f *Flow) kit.ConfirmDialog
	// OverlaySub, when set, is the line under the cleaning overlay's title; 共 N 项 when nil.
	OverlaySub func(f *Flow) string
	// Reveal shows an item in Finder or Explorer: Files.Reveal.
	Reveal func(ctx context.Context, path string) error
}

// mode is which body View shows: the results stay through the clean and its done card.
type mode uint8

const (
	modeIdle mode = iota
	modeScanning
	modeResults
)

// View is the body of a cleaner page, driven by f: the idle page with 扫描, the scan's progress, the results in a CategoryList with 清理 and the checked total, the confirmation that lists what is deleted for good and what goes to the Trash, the cleaning overlay and the done card. Files another program held open are summed up in one line on the done card rather than listed as failures.
func View(cx *core.Scope, f *Flow, cfg ViewConfig) core.Node {
	meta := cfg.Page.Meta()
	scanRef, cleanRef := new(core.Ref), new(core.Ref)
	phase := f.Phase.Get
	m := core.NewMemo(cx, func() mode {
		switch phase() {
		case PhaseIdle:
			return modeIdle
		case PhaseScanning:
			return modeScanning
		}
		return modeResults
	})
	hasResults := func() bool { return len(f.Categories()) > 0 }

	rescan := kit.Button{Text: "重新扫描", Variant: kit.ButtonGhost, OnPress: f.Scan, TestID: "rescan"}
	header := kit.PageHeader{Route: cfg.Page, Actions: []core.Node{
		core.Show{When: func() bool { return phase() == PhaseResults && hasResults() }, Then: func(*core.Scope) core.Node { return rescan }},
	}}

	body := core.Dynamic(func(*core.Scope) core.Node {
		switch m.Get() {
		case modeIdle:
			return core.Func(func(cx *core.Scope) core.Node {
				if f.TakeFocusRequest() {
					cx.Effect(scanRef.Focus)
				}
				var details core.Node
				if cfg.IdleDetails != nil {
					details = cfg.IdleDetails(cx)
				}
				return ScanIdle{Tone: meta.Tone, Glyph: meta.Glyph, Title: cfg.IdleTitle, Description: cfg.IdleDescription, Details: details,
					OnStart: f.Scan, Disabled: cfg.ScanDisabled, Error: f.ScanErr.Get, Ref: scanRef, TestID: "scan-idle"}
			})
		case modeScanning:
			return core.Func(func(*core.Scope) core.Node {
				return ScanProgress{Progress: f.Progress.Get, OnCancel: f.Stop, TestID: "scan-progress"}
			})
		}
		return core.Func(func(cx *core.Scope) core.Node { return results(cx, f, cfg, meta, cleanRef) })
	})

	live := kit.LiveStatus{Text: func() string {
		switch phase() {
		case PhaseScanning:
			return "正在扫描…"
		case PhaseResults:
			if r := f.Result.Get(); r != nil {
				return "扫描完成，找到 " + format.Bytes(r.Size)
			}
		case PhaseCleaning:
			return "正在清理…"
		}
		return ""
	}}

	confirm := core.Dynamic(func(*core.Scope) core.Node {
		if phase() != PhaseResults || !f.Confirming.Get() {
			return core.Group{}
		}
		return core.Func(func(cx *core.Scope) core.Node {
			var d kit.ConfirmDialog
			if cfg.Confirm != nil {
				d = cfg.Confirm(f)
			} else {
				d = core.Untracked(cx, func() kit.ConfirmDialog { return CleanPlanDialog(f.Categories(), f.Selection.Get()) })
			}
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
			if d.TestID == "" {
				d.TestID = "confirm"
			}
			return d
		})
	})

	overlay := CleaningOverlay{Open: func() bool { return phase() == PhaseCleaning }, Progress: f.Progress.Get, OnCancel: f.Stop, TestID: "clean-overlay",
		Sub: func() string {
			if cfg.OverlaySub != nil {
				return cfg.OverlaySub(f)
			}
			return "共 " + strconv.Itoa(f.Totals().Count) + " 项"
		}}

	done := core.Dynamic(func(*core.Scope) core.Node {
		if phase() != PhaseDone {
			return core.Group{}
		}
		return core.Func(func(cx *core.Scope) core.Node {
			o := f.Outcome.GetUntracked()
			if o == nil {
				o = &CleanOutcome{Err: "清理没有完成"}
			}
			out, inUse := *o, 0
			if o.Result != nil {
				r, n := SetInUseAside(*o.Result)
				out, inUse = CleanOutcome{Result: &r}, n
			}
			return CleanDoneDialog{Open: func() bool { return true }, Outcome: out, InUse: inUse, OnClose: f.Finish, TestID: "done"}
		})
	})

	return core.Group{
		kit.Page{Children: []core.Node{header, body}},
		live, confirm, overlay, done,
	}
}

// results is the body after a scan: the empty state, or the notices, the list and the action bar.
func results(cx *core.Scope, f *Flow, cfg ViewConfig, meta nav.Meta, cleanRef *core.Ref) core.Node {
	res := f.Result.GetUntracked()
	if res == nil || len(res.Categories) == 0 {
		return kit.EmptyState{Title: cfg.EmptyTitle, Description: cfg.EmptyDescription, Tone: meta.Tone, Glyph: meta.Glyph, TestID: "empty",
			Action: kit.Button{Text: "重新扫描", Variant: kit.ButtonGhost, OnPress: f.Scan, TestID: "empty-rescan"}}
	}
	notResults := func() bool { return f.Phase.Get() != PhaseResults }
	var children []core.Node
	if cfg.Results != nil {
		children = append(children, cfg.Results(cx, f))
	} else {
		if cfg.Notice != "" {
			children = append(children, kit.Notice{Text: func() string { return cfg.Notice }})
		}
		if res.Denied > 0 {
			denied := "有 " + strconv.FormatInt(res.Denied, 10) + " 个位置没有读取权限，结果可能不完整"
			children = append(children, kit.Notice{Text: func() string { return denied }, Tone: kit.LevelWarning})
		}
		children = append(children, CategoryList{Categories: f.Categories, Selection: f.Selection.Get, OnChange: f.SetSelection,
			Label: cfg.ListLabel, Reveal: cfg.Reveal, Disabled: notResults, TestID: "category-list"})
	}
	children = append(children, kit.ActionBar{
		Start: func() kit.Stat { return kit.Stat{Label: "已选择", Value: strconv.Itoa(f.Totals().Count) + " 项"} },
		End:   func() kit.Stat { return kit.Stat{Label: cfg.SizeLabel, Value: format.Bytes(f.Totals().Bytes)} },
		Button: kit.ActionButton{Label: "清理", OnPress: f.RequestClean, Ref: cleanRef, TestID: "clean",
			Disabled: func() bool { return notResults() || f.Totals().Count == 0 }},
	})
	return core.Group(children)
}
