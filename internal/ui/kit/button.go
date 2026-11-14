package kit

import (
	"github.com/nsgo-dev/nsgo/core"
)

// ButtonVariant is a button's look.
type ButtonVariant uint8

// The button looks: the translucent pill (.ghost-button), the white one that ends a dialog (.done-button), the amber one (.permission-action-button), the red one of a destructive confirmation, and a bare text link (.link-button).
const (
	ButtonGhost ButtonVariant = iota
	ButtonLight
	ButtonAccent
	ButtonDanger
	ButtonLink
)

// Button is a text button, with an optional glyph before its text. TextFunc, when set, replaces Text as it changes (a pending action's "等待授权…"). Busy disables it and tells assistive technology it works; Label replaces its text for assistive technology. Large is the 46-point size of a dialog's buttons.
type Button struct {
	Text      string
	TextFunc  func() string
	Variant   ButtonVariant
	Glyph     string
	Disabled  func() bool
	Busy      func() bool
	OnPress   func()
	Ref       *core.Ref
	Label     string
	TestID    string
	Large     bool
	Tooltip   string
	FullWidth bool
}

// buttonStyle is a variant's look.
func buttonStyle(v ButtonVariant, large bool) core.Style {
	st := core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 6,
		FontFamily: FontSans, FontSize: FontBody, Cursor: core.CursorPointer, FlexShrink: 0,
		FocusVisible: focusRing(2, 2)}
	switch v {
	case ButtonGhost:
		st.MinHeight, st.Padding, st.CornerRadius = core.Pt(38), core.Symmetric(0, 18), 19
		st.Background, st.Color = SurfaceButtonGhost, ColorText
		st.Hover = &core.Style{Color: ColorTextLinkHover}
		st.Disabled = &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault, Color: ColorText}
	case ButtonLight, ButtonDanger:
		st.MinHeight, st.Padding, st.CornerRadius = core.Pt(46), core.Symmetric(0, 40), 23
		st.FontSize, st.FontFamily, st.FontWeight = FontCardTitle, FontFace(Bold), FaceWeight(Bold)
		st.Background, st.Color = ButtonLightBackground, ButtonLightText
		if v == ButtonDanger {
			st.Background, st.Color = 0, ButtonDangerText
			st.Gradients = []core.Gradient{GradientDanger()}
		}
		st.Disabled = &core.Style{Opacity: core.Alpha(0.6), Cursor: core.CursorDefault}
	case ButtonAccent:
		st.MinHeight, st.Padding, st.CornerRadius = core.Pt(32), core.Symmetric(0, 14), 16
		st.FontFamily, st.FontWeight, st.Color = FontFace(Semibold), FaceWeight(Semibold), RunButtonText
		st.Gradients = []core.Gradient{GradientAccent()}
		st.Disabled = &core.Style{Opacity: core.Alpha(0.6), Cursor: core.CursorDefault}
	case ButtonLink:
		st.Padding, st.Color = core.Symmetric(2, 4), ColorTextSecondary
		st.Hover = &core.Style{Color: ColorTextLinkHover}
		st.Disabled = &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault, Color: ColorTextSecondary}
	}
	if large && (v == ButtonGhost || v == ButtonLight || v == ButtonDanger) {
		st.MinHeight, st.Padding, st.CornerRadius, st.FontSize = core.Pt(46), core.Symmetric(0, 28), 23, FontCardTitle
	}
	return st
}

func is(fn func() bool) bool { return fn != nil && fn() }

// Render implements core.Node.
func (b Button) Render(*core.Scope) core.Node {
	st := buttonStyle(b.Variant, b.Large)
	if b.FullWidth {
		st.AlignSelf = core.AlignStretch
	}
	text := core.Text{Content: b.Text, ContentFunc: b.TextFunc}
	var children []core.Node
	if b.Glyph != "" {
		children = append(children, Glyph{Name: b.Glyph, Size: 16, Color: core.CurrentColor})
	}
	children = append(children, text)
	disabled := func() bool { return is(b.Disabled) || is(b.Busy) }
	var tip core.Node
	if b.Tooltip != "" {
		tip = tooltip(b.Tooltip)
	}
	return core.View{Role: core.RoleButton, Label: b.Label, TestID: b.TestID, Ref: b.Ref, Style: st, Tooltip: tip,
		Disabled: disabled, Busy: b.Busy,
		OnPress: func(core.PressEvent) {
			if b.OnPress != nil && !disabled() {
				b.OnPress()
			}
		},
		Children: children}
}

// tooltip is the small dark label a control shows on hover.
func tooltip(s string) core.Node {
	return core.View{Style: core.Style{Padding: core.Symmetric(4, 8), CornerRadius: 6, Background: core.RGBA(0x1a1030, 0.92)},
		Children: []core.Node{Text(s, FontSmall, Regular, ColorText)}}
}

// IconButton is a 30-point round button showing only a glyph (.icon-button), such as 在访达中显示; Label names it for assistive technology and its tooltip.
type IconButton struct {
	Glyph    string
	Label    string
	Disabled func() bool
	OnPress  func()
	TestID   string
}

// Render implements core.Node.
func (b IconButton) Render(*core.Scope) core.Node {
	return core.View{Role: core.RoleButton, Label: b.Label, TestID: b.TestID, Tooltip: tooltip(b.Label), Disabled: b.Disabled,
		Style: core.Style{Width: core.Pt(30), Height: core.Pt(30), FlexShrink: 0, CornerRadius: 15, AlignItems: core.AlignCenter,
			JustifyContent: core.JustifyCenter, Color: ColorTextSecondary, Cursor: core.CursorPointer, FocusVisible: focusRing(2, 2),
			Hover:    &core.Style{Background: SurfaceButtonGhost, Color: ColorText},
			Disabled: &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault}},
		OnPress: func(core.PressEvent) {
			if b.OnPress != nil && !is(b.Disabled) {
				b.OnPress()
			}
		},
		Children: []core.Node{Glyph{Name: b.Glyph, Size: 16, Color: core.CurrentColor}}}
}

// RunButton is the design's round button with its glowing rings, the one main action of a page: 92 points (运行, 扫描, 清理) or 148 (智能扫描's hero, with a glyph above the text).
type RunButton struct {
	Text     string
	Glyph    string
	Size     core.Points
	Disabled func() bool
	OnPress  func()
	Ref      *core.Ref
	TestID   string
	Label    string
}

// Render implements core.Node.
func (b RunButton) Render(*core.Scope) core.Node {
	size := b.Size
	if size == 0 {
		size = 92
	}
	// The rings and the glow scale with the button: 8/18/16/40 at 92 points, 12/28/20/56 at the hero's 148.
	inner, outer, dy, blur := core.Points(8), core.Points(18), core.Points(16), core.Points(40)
	if size >= 148 {
		inner, outer, dy, blur = 12, 28, 20, 56
	}
	bg := core.Gradient{Kind: core.GradientRadial, Circle: true, Space: core.ColorSpaceSRGB,
		Center: core.ObjectPosition{X: core.PositionAxis{Offset: core.Pct(35)}, Y: core.PositionAxis{Offset: core.Pct(30)}},
		Stops:  []core.ColorStop{Stop(core.RGB(0xfff6c2), 0), Stop(core.RGB(0xffd23f), 45), Stop(core.RGB(0xe59a0b), 100)}}
	st := core.Style{Width: core.Pt(size), Height: core.Pt(size), FlexShrink: 0, CornerRadius: size / 2,
		AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 6, Gradients: []core.Gradient{bg},
		Shadows: []core.BoxShadow{
			{Spread: inner, Color: core.RGBA(0xffffff, 0.07)},
			{Spread: outer, Color: core.RGBA(0xffffff, 0.035)},
			shadow(dy, blur, ShadowAccent),
		},
		FontFamily: FontFace(Bold), FontSize: FontRunButton, FontWeight: FaceWeight(Bold), Color: RunButtonText, Cursor: core.CursorPointer,
		Transition:   core.Transition{Duration: 150_000_000, Easing: core.Ease, Properties: core.TransitionOpacity},
		FocusVisible: focusRing(3, 4),
		Disabled:     &core.Style{Opacity: core.Alpha(0.4), Filters: []core.Filter{core.Saturate(0.4)}, Cursor: core.CursorNotAllowed},
	}
	var children []core.Node
	if b.Glyph != "" {
		children = append(children, Glyph{Name: b.Glyph, Size: 40, StrokeWidth: 2.2, Color: core.CurrentColor})
	}
	children = append(children, core.Text{Content: b.Text})
	return core.View{Role: core.RoleButton, Label: b.Label, TestID: b.TestID, Ref: b.Ref, Disabled: b.Disabled, Style: st,
		OnPress: func(core.PressEvent) {
			if b.OnPress != nil && !is(b.Disabled) {
				b.OnPress()
			}
		},
		Children: children}
}
