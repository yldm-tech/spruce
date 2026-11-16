package kit

import (
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// controlFade is the 0.15 s ease the design's switches and checkboxes change color with.
var controlFade = core.Transition{Duration: 150 * time.Millisecond, Easing: core.Ease, Properties: core.TransitionBackground | core.TransitionBorder}

// thumbSlide moves a switch's thumb across its track; markPop brings a checkbox's mark in from 60% with a little overshoot.
var (
	thumbSlide = core.Transition{Duration: 180 * time.Millisecond, Easing: core.Ease, Properties: core.TransitionLayout}
	markPop    = core.Transition{Duration: 180 * time.Millisecond, Easing: core.Curve{X1: 0.34, Y1: 1.56, X2: 0.64, Y2: 1},
		Properties: core.TransitionOpacity | core.TransitionTransform}
)

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
		ThumbStyle: core.Style{Width: core.Pt(20), Height: core.Pt(20), CornerRadius: 10, Background: SwitchThumb, Transition: thumbSlide},
	}
}

// Checkbox is a checkbox drawn in the app's style, with the dash of a partly checked group (Indeterminate).
type Checkbox struct {
	Checked, Indeterminate, Disabled func() bool
	OnChange                         func(bool)
	// OnShift, when set, takes a Shift-press instead of OnChange: a list checks or unchecks a range up to the checkbox.
	OnShift func()
	Label   string
	TestID  string
}

// Render implements core.Node.
func (c Checkbox) Render(cx *core.Scope) core.Node {
	v := c.checkbox().Render(cx).(core.View)
	if c.OnShift != nil {
		// base.Checkbox hands OnChange only the value a press asks for, not the press, so its view's OnPress is wrapped to see Shift (https://github.com/nsgo-dev/nsgo/issues/398).
		change := v.OnPress
		v.OnPress = func(e core.PressEvent) {
			if e.Modifiers.Shift {
				c.OnShift()
				return
			}
			change(e)
		}
	}
	return v
}

// checkbox is the base.Checkbox c draws.
func (c Checkbox) checkbox() base.Checkbox {
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
				return core.Style{Transition: markPop}
			}
			return core.Style{Opacity: core.Alpha(0), Transform: core.Transform{Scale: 0.6}, Transition: markPop}
		}, Children: []core.Node{mark}},
	}
}
