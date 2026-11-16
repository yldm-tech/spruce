package kit

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// EmptyState fills a page or a list that has nothing to show, e.g. 没有找到可以清理的内容: a 56-point tile (when Glyph is set), the title, the description and an action below. The tile pops in and the rest rises after it.
type EmptyState struct {
	Title, Description string
	Tone               Tone
	Glyph              string
	Action             core.Node
	TestID             string
}

// Render implements core.Node.
func (e EmptyState) Render(cx *core.Scope) core.Node {
	var children []core.Node
	if e.Glyph != "" {
		children = append(children, core.View{Style: core.Style{Animated: Enter(cx, EnterPop, 0)},
			Children: []core.Node{IconTile{Tone: e.Tone, Size: 56, GlyphSize: 26, Glyph: e.Glyph, Radius: 16}}})
	}
	var text []core.Node
	text = append(text, Text(e.Title, FontTitle, Bold, ColorText))
	if e.Description != "" {
		text = append(text, core.Text{Content: e.Description, Style: core.Style{MaxWidth: core.Pt(440), TextAlign: core.TextAlignCenter,
			LineHeightMultiple: 1.5, FontFamily: FontSans, FontSize: FontSubheading, Color: ColorTextOverlaySub}})
	}
	if e.Action != nil {
		text = append(text, e.Action)
	}
	children = append(children, core.View{Style: core.Style{AlignItems: core.AlignCenter, Gap: 12, Animated: Enter(cx, EnterRise, 80*time.Millisecond)},
		Children: text})
	return core.View{TestID: e.TestID, Style: core.Style{FlexGrow: 1, FlexShrink: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
		Gap: 12, Padding: core.Symmetric(24, 0)}, Children: children}
}

// ErrorLine is a line in the warning color that is there only while Text returns something, announced to screen readers as it appears; it fades in.
type ErrorLine struct {
	Text   func() string
	TestID string
}

// Render implements core.Node.
func (e ErrorLine) Render(*core.Scope) core.Node {
	return core.Show{When: func() bool { return e.Text != nil && e.Text() != "" }, Then: func(*core.Scope) core.Node {
		return Appear{Entrance: EnterFade, Style: core.Style{FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
			core.Text{ContentFunc: e.Text, LiveRegion: core.LiveRegionPolite, TestID: e.TestID, Role: core.RoleAlert,
				Style: core.Style{FontFamily: FontSans, FontSize: FontBody, LineHeightMultiple: 1.5, Color: ColorWarning}}}}
	}}
}

// Notice is a line of information under a list or a figure, in the secondary color, or the warning color when Tone is above LevelNormal.
type Notice struct {
	Text   func() string
	Tone   Level
	TestID string
}

// Render implements core.Node.
func (n Notice) Render(*core.Scope) core.Node {
	c := ColorTextSecondary
	if n.Tone > LevelNormal {
		c = ColorWarning
	}
	return core.Show{When: func() bool { return n.Text != nil && n.Text() != "" }, Then: func(*core.Scope) core.Node {
		return core.Text{ContentFunc: n.Text, TestID: n.TestID, Style: core.Style{FontFamily: FontSans, FontSize: FontBody, LineHeightMultiple: 1.5, Color: c}}
	}}
}
