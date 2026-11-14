package kit

import "github.com/nsgo-dev/nsgo/core"

// Badge is a 22-point pill on the ghost button's surface with a 12-point glyph before a short text, such as 概览's 「可撤销」 under a card's consequence. Level above LevelNormal draws the text and the glyph in the warning color (「运行命令」).
type Badge struct {
	Text   string
	Glyph  string
	Level  Level
	TestID string
}

// Render implements core.Node.
func (b Badge) Render(*core.Scope) core.Node {
	color := ColorTextSecondary
	if b.Level > LevelNormal {
		color = ColorWarning
	}
	var children []core.Node
	if b.Glyph != "" {
		children = append(children, Glyph{Name: b.Glyph, Size: 12, Color: core.CurrentColor})
	}
	children = append(children, core.Text{Content: b.Text, MaxLines: 1})
	return core.View{TestID: b.TestID,
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, AlignSelf: core.AlignStart, Gap: 4, Height: core.Pt(22), FlexShrink: 0,
			Padding: core.Symmetric(0, 9), CornerRadius: 11, Background: SurfaceButtonGhost, FontFamily: FontSans, FontSize: FontSmall, Color: color,
			WhiteSpace: core.WhiteSpaceNoWrap},
		Children: children}
}
