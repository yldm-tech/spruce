package smartscan

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// The parts below look shared but only this page uses them so far; the integration step decides whether they move into kit (heroButton as a RunButton option, gradientFigure as gradient text).

// heroButton is the 148-point round 扫描 of the hero (.smart-hero-button): kit.RunButton's radial gradient, rings and glow at the hero's size, with the page's glyph over the text in the title size.
type heroButton struct {
	Text     string
	Glyph    string
	Label    string
	Disabled func() bool
	OnPress  func()
	Ref      *core.Ref
	TestID   string
}

// heroSize is --smart-hero-button-size.
const heroSize core.Points = 148

// Render implements core.Node.
func (b heroButton) Render(*core.Scope) core.Node {
	bg := core.Gradient{Kind: core.GradientRadial, Circle: true, Space: core.ColorSpaceSRGB,
		Center: core.ObjectPosition{X: core.PositionAxis{Offset: core.Pct(35)}, Y: core.PositionAxis{Offset: core.Pct(30)}},
		Stops:  []core.ColorStop{kit.Stop(core.RGB(0xfff6c2), 0), kit.Stop(core.RGB(0xffd23f), 45), kit.Stop(core.RGB(0xe59a0b), 100)}}
	st := core.Style{Width: core.Pt(heroSize), Height: core.Pt(heroSize), FlexShrink: 0, CornerRadius: heroSize / 2,
		AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 6, Gradients: []core.Gradient{bg},
		// --smart-hero-button-shadow: two rings and the amber glow.
		Shadows: []core.BoxShadow{
			{Spread: 12, Color: core.RGBA(0xffffff, 0.07)},
			{Spread: 28, Color: core.RGBA(0xffffff, 0.035)},
			{Offset: core.Point{Y: 20}, Blur: 56, Color: kit.ShadowAccent},
		},
		FontFamily: kit.FontFace(kit.Bold), FontSize: kit.FontTitle, FontWeight: kit.FaceWeight(kit.Bold), Color: kit.RunButtonText,
		Cursor:       core.CursorPointer,
		FocusVisible: &core.Style{OutlineWidth: 3, OutlineOffset: 6, OutlineColor: kit.ColorText},
		Disabled:     &core.Style{Opacity: core.Alpha(0.4), Filters: []core.Filter{core.Saturate(0.4)}, Cursor: core.CursorNotAllowed},
	}
	return core.View{Role: core.RoleButton, Label: b.Label, TestID: b.TestID, Ref: b.Ref, Disabled: b.Disabled, Style: st,
		OnPress: func(core.PressEvent) {
			if b.OnPress != nil && (b.Disabled == nil || !b.Disabled()) {
				b.OnPress()
			}
		},
		Children: []core.Node{
			kit.Glyph{Name: b.Glyph, Size: 40, StrokeWidth: 2.2, Color: core.CurrentColor},
			core.Text{Content: b.Text},
		}}
}

// gradientFigure is the 「可清理」 figure: 56-point bold tabular digits filled with the accent gradient (--gradient-accent, 135deg from #ffe066 to #f5a623, clipped to the text). NSGo cannot clip a gradient to text, so it draws the text in the gradient's end color and the same text in its start color over it, masked from opaque to clear along the same 135 degrees: the blend of the two is the gradient. Assistive technology and tests see the lower text only (TestID).
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

// pageLink is a bare text link to another page (.link-button with no left padding, as the cards align it).
type pageLink struct {
	Text   func() string
	To     func() nav.Route
	TestID string
}

// Render implements core.Node.
func (l pageLink) Render(cx *core.Scope) core.Node {
	return core.View{Role: core.RoleLink, LabelFunc: l.Text, TestID: l.TestID,
		OnPress: func(core.PressEvent) { nav.Use(cx).Go(l.To()) },
		Style: core.Style{AlignSelf: core.AlignStart, FlexShrink: 0, Padding: core.Edges{Top: 2, Right: 4, Bottom: 2}, FontFamily: kit.FontSans,
			FontSize: kit.FontBody, Color: kit.ColorTextSecondary, Cursor: core.CursorPointer,
			Hover:        &core.Style{Color: kit.ColorTextLinkHover},
			FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText}},
		Children: []core.Node{core.Text{ContentFunc: l.Text}}}
}

// to is a fixed link target.
func to(r nav.Route) func() nav.Route { return func() nav.Route { return r } }

// fixed is a text that does not change.
func fixed(s string) func() string { return func() string { return s } }

// cardStyle is the glass card of the source and status cards (.smart-source-card, .smart-status-card).
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

// sectionTitle is a heading over a group of cards (.smart-section-title).
func sectionTitle(s string) core.Node {
	return core.Text{Content: s, Role: core.RoleHeader, Style: core.Style{FontFamily: kit.FontFace(kit.Semibold), FontSize: kit.FontSection,
		FontWeight: kit.FaceWeight(kit.Semibold), LetterSpacingEm: kit.LetterSpacingSection, Color: kit.ColorTextSection}}
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
