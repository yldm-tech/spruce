package kit

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"spruce/apps/spruce/internal/ui/nav"
)

// PageHeader is the top of every page, as the 维护 design draws it: a 48-point gradient tile with its glow, the title and the subtitle, from the page's nav.Meta, and Actions on the right. It reaches the top of the window and drags it; the actions stay clear of the Windows caption buttons. A drill-down view (a route with a Parent) has a link back above the title, 「‹ 概览」, which calls Navigator.Back; it needs the shell's Navigator (nav.Key) and is left out without one.
type PageHeader struct {
	Route   nav.Route
	Actions []core.Node
}

// Render implements core.Node.
func (h PageHeader) Render(cx *core.Scope) core.Node {
	m := h.Route.Meta()
	row := h.row(cx, m)
	navigator := nav.Use(cx)
	if m.Parent == "" || navigator == nil {
		return row
	}
	back := core.View{Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff}, Style: core.Style{AlignSelf: core.AlignStart, Margin: core.Edges{Left: -4}},
		Children: []core.Node{Button{Variant: ButtonLink, Text: "‹ " + m.Parent.Meta().Title, TestID: "back", OnPress: navigator.Back}}}
	row.StyleFunc = func() core.Style {
		return core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween,
			Gap: 12, Padding: core.Edges{Right: desktop.TitleBar(cx).Right}}
	}
	return core.View{Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn}, Style: core.Style{Padding: core.Edges{Top: 4}},
		Children: []core.Node{back, row}}
}

// row is the tile, the title and the subtitle, and the actions.
func (h PageHeader) row(cx *core.Scope, m nav.Meta) core.View {
	title := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14, FlexShrink: 1, MinWidth: core.Pt(0)},
		Children: []core.Node{
			IconTile{Tone: m.Tone, Size: 48, GlyphSize: 24, Glyph: m.Glyph, Glow: true},
			core.View{Style: core.Style{Gap: 2, FlexShrink: 1}, Children: []core.Node{
				core.Text{Content: m.Title, Role: core.RoleHeader, Style: textStyle(FontTitle, Bold, ColorText)},
				Text(m.Subtitle, FontBody, Regular, ColorTextSecondary),
			}},
		}}
	children := []core.Node{title}
	if len(h.Actions) > 0 {
		children = append(children, core.View{Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOff},
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10}, Children: h.Actions})
	}
	return core.View{
		Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn},
		StyleFunc: func() core.Style {
			return core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween,
				Gap: 12, Padding: core.Edges{Top: 20, Right: desktop.TitleBar(cx).Right}}
		},
		Children: children,
	}
}
