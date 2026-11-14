package kit

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// Slider is the 设置 page's 220-point slider with its value right of it: an amber range on a light track and a white thumb. OnChange follows a drag, OnCommit runs once it ends; ValueText renders the value beside it and for assistive technology.
type Slider struct {
	Value          func() float64
	Min, Max, Step float64
	OnChange       func(float64)
	OnCommit       func()
	ValueText      func(float64) string
	Label          string
	Disabled       func() bool
	TestID         string
}

// Render implements core.Node.
func (s Slider) Render(*core.Scope) core.Node {
	value := func() float64 {
		if s.Value == nil {
			return s.Min
		}
		return s.Value()
	}
	step := s.Step
	if step == 0 {
		step = 1
	}
	text := func() string {
		if s.ValueText != nil {
			return s.ValueText(value())
		}
		return ""
	}
	slider := base.Slider{
		Value: func() base.SliderValue { return base.SingleValue(value()) },
		Min:   func() float64 { return s.Min }, Max: func() float64 { return s.Max }, Step: func() float64 { return step },
		Disabled: s.Disabled, Label: s.Label, TestID: s.TestID, ValueText: s.ValueText,
		OnChange: func(v base.SliderValue) {
			if s.OnChange != nil {
				s.OnChange(v.End)
			}
		},
		OnChangeEnd: func(base.SliderValue) {
			if s.OnCommit != nil {
				s.OnCommit()
			}
		},
		Style:          core.Style{Width: core.Pt(220), Height: core.Pt(20), JustifyContent: core.JustifyCenter, Cursor: core.CursorPointer},
		TrackStyle:     core.Style{Height: core.Pt(4), CornerRadius: 2, Background: MeterTrack},
		IndicatorStyle: core.Style{FlexGrow: 1},
		RangeStyle:     core.Style{Height: core.Pt(4), CornerRadius: 2, Background: SwitchOn},
		ThumbStyle: core.Style{Width: core.Pt(18), Height: core.Pt(18), CornerRadius: 9, Background: SwitchThumb,
			Shadows: []core.BoxShadow{shadow(1, 3, core.RGBA(0x000000, 0.3))}, FocusVisible: focusRing(2, 4)},
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
		Children: []core.Node{slider, Tabular(core.Text{ContentFunc: text, Style: core.Style{MinWidth: core.Pt(64), TextAlign: core.TextAlignEnd,
			FontFamily: FontSans, FontSize: FontCardTitle, FontWeight: Bold, Color: ColorText}})}}
}
