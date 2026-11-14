package overview

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// The parts below are used by this page only, so they stay here rather than in kit.

// gradientFigure is the conclusion's figure: 56-point bold tabular digits filled with the accent gradient (--gradient-accent, 135deg from #ffe066 to #f5a623, clipped to the text). NSGo cannot clip a gradient to text, so it draws the text in the gradient's end color and the same text in its start color over it, masked from opaque to clear along the same 135 degrees: the blend of the two is the gradient. Assistive technology and tests see the lower text only (TestID).
func gradientFigure(text func() string, testID string) core.Node {
	st := core.Style{FontFamily: kit.FontFace(kit.Bold), FontSize: kit.FontHero, FontWeight: kit.FaceWeight(kit.Bold),
		LetterSpacingEm: kit.LetterSpacingStat, FontVariant: core.FontTabularNums, Color: core.RGB(0xf5a623), WhiteSpace: core.WhiteSpaceNoWrap}
	top := st
	top.Color = core.RGB(0xffe066)
	mask := kit.Linear(135, kit.Stop(core.RGBA(0xffffff, 1), 0), kit.Stop(core.RGBA(0xffffff, 0), 100))
	return core.View{Children: []core.Node{
		core.Text{ContentFunc: text, TestID: testID, Style: st},
		core.View{AccessibilityElementsHidden: func() bool { return true }, PointerEvents: core.PointerEventsNone,
			Style:    core.Style{Position: core.Absolute, Left: core.Pt(0), Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Mask: &mask},
			Children: []core.Node{core.Text{ContentFunc: text, Style: top}}},
	}}
}

// to is a fixed link target.
func to(r nav.Route) func() nav.Route { return func() nav.Route { return r } }

// fixed is a text that does not change.
func fixed(s string) func() string { return func() string { return s } }

// cardStyle is the glass card of the browse and status cards (.smart-source-card, .smart-status-card).
func cardStyle() core.Style {
	return core.Style{Gap: 6, MinWidth: core.Pt(0), Padding: core.Symmetric(18, 20), CornerRadius: kit.RadiusCard,
		Background: kit.SurfaceCard, BorderWidth: 1, BorderColor: kit.BorderCard}
}

// cardHead is a card's tile and title (.smart-source-head).
func cardHead(tone kit.Tone, glyph string, size, glyphSize core.Points, title string) core.Node {
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, Margin: core.Edges{Bottom: 4}},
		Children: []core.Node{
			kit.IconTile{Tone: tone, Size: size, GlyphSize: glyphSize, Glyph: glyph, Radius: kit.RadiusCardIcon},
			kit.Text(title, kit.FontCardTitle, kit.Semibold, kit.ColorText),
		}}
}

// cardGrid lays cards out in columns of at least 200 points (.smart-source-grid, .smart-status-grid).
func cardGrid(label, testID string, cards ...core.Node) core.Node {
	return core.View{Role: core.RoleGroup, Label: label, TestID: testID,
		Style:    core.Style{Display: core.DisplayGrid, GridTemplateColumns: "repeat(auto-fit, minmax(200px, 1fr))", Gap: 14},
		Children: cards}
}

// mutedText is a line in the muted color (.smart-source-note, .smart-status-note); warning switches it to the warning color.
func mutedText(text func() string, warning func() bool, testID string) core.Text {
	return core.Text{ContentFunc: text, TestID: testID, StyleFunc: func() core.Style {
		c := kit.ColorTextMuted
		if warning != nil && warning() {
			c = kit.ColorWarning
		}
		return core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.45, Color: c}
	}}
}

// bodyText is a line of body text in color, wrapping.
func bodyText(text func() string, color core.Color, testID string) core.Text {
	return core.Text{ContentFunc: text, TestID: testID,
		Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: color}}
}

// row lays children out in a row that wraps, gap points apart.
func row(gap core.Points, children ...core.Node) core.View {
	return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, ColumnGap: gap, RowGap: 6}, Children: children}
}
