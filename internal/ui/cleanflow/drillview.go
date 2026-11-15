package cleanflow

import (
	"cmp"
	"context"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// DrillPage is a drill-down view that lists its source as one CategoryList (系统垃圾, 安装包): the header with its link back, the status of the check, the source's notices, the list with 清理 and the checked total, the confirmation, the cleaning overlay and, after a clean, the toast.
type DrillPage struct {
	Route nav.Route
	Toast Toaster
	// Notice is the line above the list, e.g. what the source looks at or where its items go.
	Notice func() string
	// ListLabel names the list for assistive technology; SizeLabel labels the checked size beside 清理.
	ListLabel, SizeLabel string
	// EmptyTitle and EmptyDescription are the view when the check found nothing of the source.
	EmptyTitle, EmptyDescription string
	// Empty, when set and returning a node, replaces that empty state (安装包 without folders to search); it is called again when what it reads changes.
	Empty func(cx *core.Scope) core.Node
	// Reveal shows an item in Finder or Explorer: Files.Reveal.
	Reveal func(ctx context.Context, path string) error
}

// Render implements core.Node.
func (p DrillPage) Render(cx *core.Scope) core.Node {
	d := NewDrill(cx, string(p.Route), p.Toast)
	cleanRef := new(core.Ref)
	meta := p.Route.Meta()
	has := func() bool { return d.Result() != nil }
	empty := core.NewMemo(cx, func() bool { return len(d.Categories()) == 0 })
	body := core.Show{When: has, Then: func(*core.Scope) core.Node {
		return core.Dynamic(func(*core.Scope) core.Node {
			if empty.Get() {
				def := kit.EmptyState{Title: p.EmptyTitle, Description: p.EmptyDescription, Tone: meta.Tone, Glyph: meta.Glyph, TestID: "drill-empty"}
				if p.Empty == nil {
					return core.Func(func(*core.Scope) core.Node { return def })
				}
				// What Empty reads (the settings) decides again when it changes.
				return core.Func(func(*core.Scope) core.Node {
					return core.Dynamic(func(cx *core.Scope) core.Node {
						n := p.Empty(cx)
						if n == nil {
							n = def
						}
						return core.Func(func(*core.Scope) core.Node { return n })
					})
				})
			}
			return core.Func(func(*core.Scope) core.Node {
				totals := func() Totals { return SelectedTotals(d.Categories(), d.Selection.Get()) }
				return core.Group{
					kit.Notice{Text: p.Notice},
					CategoryList{Categories: d.Categories, Selection: d.Selection.Get, OnChange: d.Change, Label: p.ListLabel, Reveal: p.Reveal,
						Disabled: d.Cleaning, Expand: d.Reveal, Scroll: d.ScrollTo, TestID: "category-list"},
					DrillBar(d, func() kit.Stat { return kit.Stat{Label: "已选择", Value: strconv.Itoa(totals().Count) + " 项"} },
						func() kit.Stat { return kit.Stat{Label: p.SizeLabel, Value: format.Bytes(totals().Bytes)} },
						func() int { return totals().Count }, func() {
							cats := d.cats.GetUntracked()
							d.RequestClean(cats, CleanPlanDialog, "共 "+strconv.Itoa(SelectedTotals(cats, d.Selection.GetUntracked()).Count)+" 项", cleanRef)
						}, cleanRef),
				}
			})
		})
	}}
	return core.Group{
		kit.Page{TestID: "drill-" + string(p.Route), Ref: d.Page, Children: []core.Node{
			kit.PageHeader{Route: p.Route}, DrillStatus(d), DrillNotices(d, DeniedText), body,
		}},
		DrillLayers(d),
	}
}

// DeniedText is the warning about the places a source could not read, "" when it read them all.
func DeniedText(denied int64) string {
	if denied <= 0 {
		return ""
	}
	return "有 " + strconv.FormatInt(denied, 10) + " 个位置没有读取权限，结果可能不完整"
}

// DrillBar is a drill-down view's action bar: the two figures beside 清理, which is disabled while count is 0 or CanClean says no.
func DrillBar(d *Drill, start, end func() kit.Stat, count func() int, clean func(), ref *core.Ref) core.Node {
	return kit.ActionBar{Start: start, End: end,
		Button: kit.ActionButton{Label: "清理", OnPress: clean, Ref: ref, TestID: "clean", Disabled: func() bool { return !d.CanClean(count()) }}}
}

// StaleText is why a drill-down view may not clean now while no check runs: the refused check, the failed one, no result yet, or a result to check again.
func StaleText(checkErr, err string, hasResult bool) string {
	switch {
	case checkErr != "":
		return checkErr
	case err != "":
		return "这次检查没有完成：" + err
	case !hasResult:
		return "还没有检查结果"
	}
	return "重新检查后才能清理"
}

// DrillStatus is the row under the header: 正在检查… while a check runs, else, while the result may not be cleaned, why and 重新检查.
func DrillStatus(d *Drill) core.Node {
	stale := func() bool { return !d.Checking() && !d.Actionable() }
	line := func(text func() string, testID string) core.Text {
		return core.Text{ContentFunc: text, TestID: testID, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5,
			Color: kit.ColorTextMuted}}
	}
	row := func(children ...core.Node) core.View {
		return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, ColumnGap: 8, RowGap: 4, FlexShrink: 0},
			Children: children}
	}
	return core.Group{
		core.Show{When: d.Checking, Then: func(*core.Scope) core.Node {
			r := row(kit.Spinner{Size: 14, Label: "正在检查"}, line(func() string { return "正在检查…" }, ""))
			r.TestID = "drill-checking"
			return r
		}},
		core.Show{When: stale, Then: func(*core.Scope) core.Node {
			r := row(line(func() string { return StaleText(d.CheckErr.Get(), d.State().Error, d.Result() != nil) }, "drill-stale-text"),
				kit.Button{Text: "重新检查", Variant: kit.ButtonLink, TestID: "drill-recheck", OnPress: d.CheckNow, Disabled: d.Busy})
			r.TestID = "drill-stale"
			return r
		}},
	}
}

// DrillNotices are what the check left out of the source: that it failed (source-failed-<source>), and the places it could not read, worded by denied.
func DrillNotices(d *Drill, denied func(int64) string) core.Node {
	failed := func() string {
		if s, ok := d.SourceState(); ok && s.Status == cleanup.SourceFailed {
			return nav.Route(d.Source).Meta().Title + "没能检查完成：" + cmp.Or(s.Error, "没能检查完成")
		}
		return ""
	}
	deniedText := func() string {
		if s, ok := d.SourceState(); ok {
			return denied(s.Denied)
		}
		return ""
	}
	return core.Group{
		kit.Notice{Tone: kit.LevelWarning, TestID: "source-failed-" + d.Source, Text: failed},
		kit.Notice{Tone: kit.LevelWarning, TestID: "denied-notice", Text: deniedText},
	}
}

// DrillLayers are what a drill-down view draws over its page: the confirmation it asked and the cleaning overlay, with what the screen reader hears.
func DrillLayers(d *Drill) core.Node {
	dialog := core.Dynamic(func(*core.Scope) core.Node {
		q := d.Dialog.Get()
		if q == nil {
			return core.Group{}
		}
		return core.Func(func(*core.Scope) core.Node {
			c := *q
			c.Open = func() bool { return true }
			if c.TestID == "" {
				c.TestID = "confirm"
			}
			return c
		})
	})
	overlay := CleaningOverlay{Open: d.Cleaning, Progress: d.Progress.Get, OnCancel: d.Stop, Sub: d.Sub.Get, TestID: "clean-overlay"}
	live := kit.LiveStatus{Text: func() string {
		switch {
		case d.Cleaning():
			return "正在清理…"
		case d.Checking():
			return "正在检查…"
		}
		return ""
	}}
	return core.Group{live, dialog, overlay}
}
