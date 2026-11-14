package smartscan

import (
	"context"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// results is the scan's outcome: the 「可清理」 total of what starts checked, a card per source, the denied locations, the other checks and every category grouped by source, ready to adjust. It stays through the clean and its done card, disabled.
func results(cx *core.Scope, f *cleanflow.Flow, cleanRef *core.Ref, reveal func(context.Context, string) error) core.Node {
	res := f.Result.GetUntracked()
	if res == nil {
		return core.Group{}
	}
	scan := *res
	summaries := Summarize(scan)
	found := scan.Items > 0
	notResults := func() bool { return f.Phase.Get() != cleanflow.PhaseResults }

	var children []core.Node
	if found {
		children = append(children, core.View{Role: core.RoleRegion, Label: "扫描结果", TestID: "total",
			Style: core.Style{AlignItems: core.AlignCenter, Gap: 4, Padding: core.Edges{Top: 12, Bottom: 4}, TextAlign: core.TextAlignCenter},
			Children: []core.Node{
				kit.Text("可清理", kit.FontCardTitle, kit.Regular, kit.ColorTextSecondary),
				gradientFigure(fixed(format.Bytes(RecommendedTotals(scan).Bytes)), "total-value"),
				core.Text{Content: TotalSub(scan), TestID: "total-sub", Style: core.Style{MaxWidth: core.Pt(560), FontFamily: kit.FontSans,
					FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: kit.ColorTextMuted, TextAlign: core.TextAlignCenter}},
			}})
	} else {
		meta := nav.Overview.Meta()
		children = append(children, kit.EmptyState{Title: "没有找到需要清理的内容", Description: "系统垃圾、开发者垃圾和安装包都很干净，过段时间再来看看。",
			Tone: meta.Tone, Glyph: meta.Glyph, TestID: "empty"})
	}
	cards := make([]core.Node, len(summaries))
	for i, s := range summaries {
		cards[i] = sourceCard(s)
	}
	children = append(children, cardGrid("各来源的结果", "sources", cards...))
	if scan.Denied > 0 {
		denied := "有 " + strconv.FormatInt(scan.Denied, 10) + " 个位置没有读取权限，结果可能不完整"
		children = append(children, kit.Notice{Text: fixed(denied), Tone: kit.LevelWarning})
	}
	children = append(children, statusCards{})
	if found {
		list := []core.Node{kit.SectionTitle("全部项目")}
		for _, s := range summaries {
			if s.Items > 0 {
				list = append(list, sourceSection{Summary: s, Flow: f, Disabled: notResults, Reveal: reveal})
			}
		}
		list = append(list, kit.ActionBar{
			Start: func() kit.Stat { return kit.Stat{Label: "已选择", Value: strconv.Itoa(f.Totals().Count) + " 项"} },
			End:   func() kit.Stat { return kit.Stat{Label: "已选大小", Value: format.Bytes(f.Totals().Bytes)} },
			Button: kit.ActionButton{Label: "清理", OnPress: f.RequestClean, Ref: cleanRef, TestID: "clean",
				Disabled: func() bool { return notResults() || f.Totals().Count == 0 }},
		})
		children = append(children, core.View{Role: core.RoleRegion, Label: "全部项目", TestID: "all-items", Style: core.Style{Gap: 14}, Children: list})
	}
	return core.Group(children)
}

// sourceCard sums up one source's share of the results (.smart-source-card), with 查看详情 to its page; a source whose scan failed says why.
func sourceCard(s SourceSummary) core.Node {
	route := nav.Route(s.ID)
	meta := route.Meta()
	id := string(s.ID)
	children := []core.Node{cardHead(meta.Tone, meta.Glyph, 36, 18, meta.Title)}
	if s.Failed() {
		msg := s.State.Error
		if msg == "" {
			msg = "没能扫描完成"
		}
		children = append(children, mutedText(fixed(msg), func() bool { return true }, "source-error-"+id))
	} else {
		children = append(children,
			core.Text{Content: format.Bytes(s.Bytes), TestID: "source-size-" + id, Style: core.Style{FontFamily: kit.FontFace(kit.Bold),
				FontWeight: kit.FaceWeight(kit.Bold), FontSize: kit.FontStat, LetterSpacingEm: kit.LetterSpacingStat, FontVariant: core.FontTabularNums,
				Color: kit.ColorText}},
			mutedText(fixed(SourceNote(s)), nil, "source-note-"+id))
	}
	children = append(children, core.View{Style: core.Style{AutoMargin: core.SideTop, AlignItems: core.AlignStart}, Children: []core.Node{
		kit.Link{Start: true, Text: fixed("查看详情"), To: to(route), TestID: "source-link-" + id},
	}})
	return core.View{Role: core.RoleRegion, Label: meta.Title, TestID: "source-" + id, Style: cardStyle(), Children: children}
}

// sourceSection is one source's categories in the full list, under a heading that folds them away (.smart-list-section); its 全选 and 全不选 act on the source only.
type sourceSection struct {
	Summary  SourceSummary
	Flow     *cleanflow.Flow
	Disabled func() bool
	Reveal   func(context.Context, string) error
}

// Render implements core.Node.
func (s sourceSection) Render(cx *core.Scope) core.Node {
	open := core.NewSignal(cx, true)
	meta := nav.Route(s.Summary.ID).Meta()
	id := string(s.Summary.ID)
	figure := strconv.Itoa(s.Summary.Items) + " 项 · " + format.Bytes(s.Summary.Bytes)
	f, group := s.Flow, s.Summary.Categories
	toggle := core.View{Role: core.RoleButton, Label: meta.Title + " " + figure, TestID: "toggle-" + id,
		Expandable: func() bool { return true }, Expanded: open.Get,
		OnPress: func(core.PressEvent) { open.Set(!open.GetUntracked()) },
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, Padding: core.Symmetric(4, 0), Cursor: core.CursorPointer,
			FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText}, CornerRadius: kit.RadiusCheckbox},
		Children: []core.Node{
			core.Dynamic(func(*core.Scope) core.Node {
				glyph := "chevron-right"
				if open.Get() {
					glyph = "chevron-down"
				}
				return core.Func(func(*core.Scope) core.Node { return kit.Glyph{Name: glyph, Size: 16} })
			}),
			kit.Text(meta.Title, kit.FontCardTitle, kit.Semibold, kit.ColorText),
			core.View{Style: core.Style{AutoMargin: core.SideLeft}, Children: []core.Node{
				kit.Tabular(kit.Text(figure, kit.FontBody, kit.Regular, kit.ColorTextSecondary)),
			}},
		}}
	return core.View{TestID: "section-" + id, Style: core.Style{Gap: 10}, Children: []core.Node{
		toggle,
		core.Show{When: open.Get, Then: func(*core.Scope) core.Node {
			return cleanflow.CategoryList{Categories: func() []cleanup.Category { return group }, Selection: f.Selection.Get,
				OnChange: func(next cleanflow.Selection) {
					f.SetSelection(cleanflow.WithinGroup(f.Selection.GetUntracked(), group, next))
				},
				Label: meta.Title + "的项目", Reveal: s.Reveal, Disabled: s.Disabled, TestID: "list-" + id}
		}},
	}}
}
