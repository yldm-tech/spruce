package kit

import (
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// controlFade is the 0.15 s ease the design's switches and checkboxes change color with.
var controlFade = core.Transition{Duration: 150 * time.Millisecond, Easing: core.Ease, Properties: core.TransitionBackground | core.TransitionBorder}

// Switch is the 维护 cards' on/off switch for any page: a 44 by 26 track, amber when on, with a white thumb. Label names it for assistive technology.
type Switch struct {
	Checked  func() bool
	OnChange func(bool)
	Disabled func() bool
	Label    string
	TestID   string
}

// Render implements core.Node.
func (s Switch) Render(*core.Scope) core.Node {
	return base.Switch{Checked: s.Checked, Disabled: s.Disabled, OnChange: s.OnChange, Label: s.Label, TestID: s.TestID,
		Style: core.Style{FlexShrink: 0, CornerRadius: 13, Cursor: core.CursorPointer, FocusVisible: focusRing(2, 2),
			Disabled: &core.Style{Cursor: core.CursorDefault}},
		TrackStyle: core.Style{Width: core.Pt(44), Height: core.Pt(26), CornerRadius: 13, Padding: core.All(3), Direction: core.Row,
			JustifyContent: core.JustifyStart, Background: SwitchOff, Transition: controlFade,
			Checked:  &core.Style{JustifyContent: core.JustifyEnd, Background: SwitchOn},
			Disabled: &core.Style{Opacity: core.Alpha(0.45)}},
		ThumbStyle: core.Style{Width: core.Pt(20), Height: core.Pt(20), CornerRadius: 10, Background: SwitchThumb},
	}
}

// Checkbox is a checkbox drawn in the app's style, with the dash of a partly checked group (Indeterminate).
type Checkbox struct {
	Checked, Indeterminate, Disabled func() bool
	OnChange                         func(bool)
	Label                            string
	TestID                           string
}

// Render implements core.Node.
func (c Checkbox) Render(*core.Scope) core.Node {
	on := &core.Style{Background: CheckboxOn, BorderColor: CheckboxOn}
	mark := core.Dynamic(func(cx *core.Scope) core.Node {
		name := "check"
		if is(c.Indeterminate) {
			name = "dash"
		}
		return core.Func(func(*core.Scope) core.Node { return Glyph{Name: name, Size: 12, StrokeWidth: 3.4, Color: CheckboxMark} })
	})
	return base.Checkbox{Checked: c.Checked, Indeterminate: c.Indeterminate, Disabled: c.Disabled, OnChange: c.OnChange,
		Label: c.Label, TestID: c.TestID,
		Style: core.Style{FlexShrink: 0, CornerRadius: RadiusCheckbox, Cursor: core.CursorPointer, FocusVisible: focusRing(2, 2),
			Disabled: &core.Style{Cursor: core.CursorDefault}},
		IndicatorStyle: core.Style{Width: core.Pt(20), Height: core.Pt(20), CornerRadius: RadiusCheckbox, BorderWidth: 1.5,
			BorderColor: CheckboxOff, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Transition: controlFade,
			Checked: on, Indeterminate: on, Disabled: &core.Style{Opacity: core.Alpha(0.45)}},
		Indicator: core.View{StyleFunc: func() core.Style {
			if is(c.Checked) || is(c.Indeterminate) {
				return core.Style{}
			}
			return core.Style{Opacity: core.Alpha(0)}
		}, Children: []core.Node{mark}},
	}
}
