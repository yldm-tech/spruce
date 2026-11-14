package kit

import "github.com/nsgo-dev/nsgo/core"

// EmptyState fills a page or a list that has nothing to show, e.g. 没有找到可以清理的内容: a 56-point tile (when Glyph is set), the title, the description and an action below.
type EmptyState struct {
	Title, Description string
	Tone               Tone
	Glyph              string
	Action             core.Node
	TestID             string
}

// Render implements core.Node.
func (e EmptyState) Render(*core.Scope) core.Node {
	var children []core.Node
	if e.Glyph != "" {
		children = append(children, IconTile{Tone: e.Tone, Size: 56, GlyphSize: 26, Glyph: e.Glyph, Radius: 16})
	}
	children = append(children, Text(e.Title, FontTitle, Bold, ColorText))
	if e.Description != "" {
		children = append(children, core.Text{Content: e.Description, Style: core.Style{MaxWidth: core.Pt(440), TextAlign: core.TextAlignCenter,
			LineHeightMultiple: 1.5, FontFamily: FontSans, FontSize: FontSubheading, Color: ColorTextOverlaySub}})
	}
	if e.Action != nil {
		children = append(children, e.Action)
	}
	return core.View{TestID: e.TestID, Style: core.Style{FlexGrow: 1, FlexShrink: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
		Gap: 12, Padding: core.Symmetric(24, 0)}, Children: children}
}

// ErrorLine is a line in the warning color that is there only while Text returns something, announced to screen readers as it appears.
type ErrorLine struct {
	Text   func() string
	TestID string
}

// Render implements core.Node.
func (e ErrorLine) Render(*core.Scope) core.Node {
	return core.Show{When: func() bool { return e.Text != nil && e.Text() != "" }, Then: func(*core.Scope) core.Node {
		return core.Text{ContentFunc: e.Text, LiveRegion: core.LiveRegionPolite, TestID: e.TestID, Role: core.RoleAlert,
			Style: core.Style{FontFamily: FontSans, FontSize: FontBody, LineHeightMultiple: 1.5, Color: ColorWarning}}
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
