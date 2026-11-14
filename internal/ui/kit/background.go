package kit

import "github.com/nsgo-dev/nsgo/core"

// PageBackground is the window's background (--page-background): a 160° linear gradient from rust to plum to the deepest violet, a warm glow at the top right and a pink one at the bottom left. Its children lie over it in a row. Only the shell draws it.
type PageBackground struct {
	Children []core.Node
}

// radialGlow is one of the background's radial-gradient(<w>px <h>px at <x> <y>, c, transparent 60%) layers: CSS sizes the ellipse explicitly, which core.Gradient cannot, so it is a box twice the radii, centered on the point, whose closest side is the radii.
func radialGlow(rx, ry core.Points, x, y core.Length, c core.Color) core.Node {
	clear := c &^ 0xff // the same color, transparent: mixes like CSS's premultiplied transparent
	return core.View{PointerEvents: core.PointerEventsNone, Style: core.Style{
		Position: core.Absolute, Left: x, Top: y, Width: core.Pt(2 * rx), Height: core.Pt(2 * ry),
		Margin: core.Edges{Left: -rx, Top: -ry},
		Gradients: []core.Gradient{{Kind: core.GradientRadial, Extent: core.ExtentClosestSide, Space: core.ColorSpaceSRGB,
			Stops: []core.ColorStop{{Color: c, At: core.Pct(0)}, {Color: core.SetColor(clear), At: core.Pct(60)}}}},
	}}
}

// Render implements core.Node.
func (b PageBackground) Render(*core.Scope) core.Node {
	fill := core.Style{Position: core.Absolute, Left: core.Pt(0), Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0)}
	base := fill
	base.Gradients = []core.Gradient{Linear(160, Stop(core.RGB(0x8a4a12), 0), Stop(core.RGB(0x4a2238), 45), Stop(ColorBgDeep, 100))}
	content := core.View{Style: core.Style{Position: core.Relative, FlexGrow: 1, Direction: core.Row, MinHeight: core.Pt(0)}, Children: b.Children}
	return core.View{
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, Direction: core.Row, Background: ColorBgDeep, Overflow: core.Hidden,
			FontFamily: FontSans, Color: ColorText},
		Children: []core.Node{
			core.View{Style: base, PointerEvents: core.PointerEventsNone},
			radialGlow(700, 600, core.Pct(10), core.Pct(100), core.RGBA(0xd64678, 0.4)),
			radialGlow(900, 600, core.Pct(80), core.Pct(0), core.RGBA(0xffc83c, 0.42)),
			content,
		},
	}
}
