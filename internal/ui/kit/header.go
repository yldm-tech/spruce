package kit

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// PageHeader is the top of every page, as the 维护 design draws it: a 48-point gradient tile with its glow, the title and the subtitle, from the page's nav.Meta, and Actions on the right. It reaches the top of the window and drags it; the actions stay clear of the Windows caption buttons.
type PageHeader struct {
	Route   nav.Route
	Actions []core.Node
}

// Render implements core.Node.
func (h PageHeader) Render(cx *core.Scope) core.Node {
	m := h.Route.Meta()
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
