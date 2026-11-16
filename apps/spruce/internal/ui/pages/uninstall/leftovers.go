package uninstall

import (
	"fmt"
	"strings"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
)

// kindText is the color of a leftover's kind before its name.
var kindText = kit.ColorAccentBright

// LeftoverList lists what an app left behind under 全选 and 全不选, each with its checkbox, kind, place, size and a button that shows it in Finder or Explorer.
type LeftoverList struct {
	Leftovers func() []app.UninstallLeftover
	Checked   func() map[string]bool
	OnToggle  func(id string, on bool)
	// OnSet replaces the checked set (全选, 全不选).
	OnSet    func(map[string]bool)
	OnReveal func(path string)
	Disabled func() bool
	Windows  bool
}

// Render implements core.Node.
func (l LeftoverList) Render(*core.Scope) core.Node {
	totals := func() string {
		n, size := CheckedTotals(l.Leftovers(), l.Checked())
		return fmt.Sprintf("已选 %d/%d 项，共 %s", n, len(l.Leftovers()), format.BytesIn(size, l.Windows))
	}
	toolbar := core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, RowGap: 8, ColumnGap: 12},
		Children: []core.Node{
			kit.Tabular(core.Text{ContentFunc: totals, TestID: "leftover-totals", Style: core.Style{AutoMargin: core.SideRight, FontFamily: kit.FontSans,
				FontSize: kit.FontBody, Color: kit.ColorTextSecondary}}),
			kit.Button{Text: "全选", Variant: kit.ButtonLink, Disabled: l.Disabled, TestID: "leftovers-all",
				OnPress: func() { l.OnSet(all(l.Leftovers())) }},
			kit.Button{Text: "全不选", Variant: kit.ButtonLink, Disabled: l.Disabled, TestID: "leftovers-none",
				OnPress: func() { l.OnSet(map[string]bool{}) }},
		}}
	rows := core.Each(l.Leftovers, func(x app.UninstallLeftover) string { return x.ID },
		func(_ *core.Scope, item func() app.UninstallLeftover, _ func() int) core.Node { return l.row(item) })
	list := core.View{Role: core.RoleGroup, Label: "相关文件", TestID: "leftovers",
		Style: core.Style{Padding: core.Symmetric(4, 0), CornerRadius: 14, BorderWidth: 1, BorderColor: kit.BorderSubtle, Overflow: core.Hidden,
			Background: kit.SurfaceList, TextAlign: core.TextAlignStart},
		Children: []core.Node{rows}}
	return core.View{Style: core.Style{Gap: 8, AlignSelf: core.AlignStretch}, Children: []core.Node{toolbar, list}}
}

// row is one leftover.
func (l LeftoverList) row(item func() app.UninstallLeftover) core.Node {
	x := item()
	name := format.BaseName(x.Path)
	label := KindLabels[x.Kind]
	place := func() string {
		x := item()
		parts := []string{format.TruncateMiddle(format.ParentPath(x.Path), 60)}
		if x.Note != "" {
			parts = append(parts, x.Note)
		}
		return strings.Join(parts, " · ")
	}
	return core.View{Tooltip: kit.Tooltip(x.Path),
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, MinHeight: core.Pt(48), Padding: core.Edges{Right: 10, Left: 14},
			Hover: &core.Style{Background: kit.SurfaceRowHover}},
		Children: []core.Node{
			kit.Checkbox{Label: label + "：" + name, TestID: "leftover-" + x.ID, Disabled: l.Disabled,
				Checked:  func() bool { return l.Checked()[item().ID] },
				OnChange: func(on bool) { l.OnToggle(item().ID, on) }},
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
				core.Text{MaxLines: 1, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorText},
					Spans: []core.Span{
						{Content: label + "  ", Style: core.Style{FontFamily: kit.FontFace(kit.Semibold), FontSize: 11, FontWeight: kit.FaceWeight(kit.Semibold),
							Color: kindText}},
						{Content: name},
					}},
				core.Text{ContentFunc: place, MaxLines: 1, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, Color: kit.ColorTextMuted}},
			}},
			kit.Tabular(core.Text{ContentFunc: func() string { return format.BytesIn(item().Size, l.Windows) },
				Style: core.Style{FlexShrink: 0, MinWidth: core.Pt(64), TextAlign: core.TextAlignEnd, FontFamily: kit.FontSans, FontSize: kit.FontBody,
					Color: kit.ColorText}}),
			kit.IconButton{Glyph: "reveal", Label: "在" + fileManagerName(l.Windows) + "中显示 " + name, TestID: "reveal-" + x.ID,
				OnPress: func() { l.OnReveal(item().Path) }},
		}}
}
