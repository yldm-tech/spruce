package kit

import "github.com/nsgo-dev/nsgo/core"

// Stat is a figure beside the action button, e.g. 已选择 / 4 项; the zero Stat shows nothing.
type Stat struct {
	Label, Value string
}

// ActionButton is the action bar's round button. Tooltip, when set, shows on hover, also while the button is off (why it is).
type ActionButton struct {
	Label    string
	OnPress  func()
	Disabled func() bool
	Ref      *core.Ref
	TestID   string
	Tooltip  core.Node
}

// ActionBar is the bottom of a page: the round action button between two figures, as on the 维护 page. It sits at the bottom of the page's column (its top margin takes the free space).
type ActionBar struct {
	Start, End func() Stat
	Button     ActionButton
}

// Render implements core.Node.
func (a ActionBar) Render(*core.Scope) core.Node {
	return core.View{
		Style: core.Style{AutoMargin: core.SideTop, Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter,
			JustifyContent: core.JustifyCenter, Gap: 28, Padding: core.Edges{Top: 18}},
		Children: []core.Node{
			statView(a.Start, true),
			RunButton{Text: a.Button.Label, OnPress: a.Button.OnPress, Disabled: a.Button.Disabled, Ref: a.Button.Ref, TestID: a.Button.TestID,
				Tooltip: a.Button.Tooltip},
			statView(a.End, false),
		},
	}
}

// statView is one figure, right-aligned left of the button (end) and left-aligned right of it.
func statView(stat func() Stat, end bool) core.Node {
	align := core.AlignStart
	if end {
		align = core.AlignEnd
	}
	get := func() Stat {
		if stat == nil {
			return Stat{}
		}
		return stat()
	}
	return core.View{
		Style: core.Style{Gap: 2, MinWidth: core.Pt(140), AlignItems: align},
		Children: []core.Node{
			TextFunc(func() string { return get().Label }, FontSmall, Regular, ColorTextMuted),
			core.Text{ContentFunc: func() string { return get().Value },
				Style: core.Style{FontFamily: FontFace(Bold), FontSize: FontStat, FontWeight: FaceWeight(Bold), LetterSpacingEm: LetterSpacingStat, Color: ColorText}},
		},
	}
}
