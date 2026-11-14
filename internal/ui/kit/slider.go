package kit

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// Slider is the 设置 page's 220-point slider with its value right of it. It draws what the old app's <input type=range> looked like in the dark appearance on macOS (WebKit ignores the CSS accent-color there): a 10-point dark track with a darker edge, a grey fill up to the thumb and a white 24×16 capsule thumb that stays inside the track at both ends. OnChange follows a drag, OnCommit runs once it ends; ValueText renders the value beside it and for assistive technology.
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

// The slider's measures and colors, sampled from the old app's screenshot (reference/settings-idle.png).
const (
	sliderTrack  core.Points = 10
	sliderThumbW core.Points = 24
	sliderThumbH core.Points = 16
)

var (
	sliderTrackColor = core.RGB(0x343434)
	sliderEdgeColor  = core.RGB(0x1e1e1e)
	sliderFillColor  = core.RGB(0x666666)
)

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
		Style: core.Style{Width: core.Pt(220), Height: core.Pt(20), JustifyContent: core.JustifyCenter, Cursor: core.CursorPointer},
		// The 1-point edge is a ring drawn outside the track (a border would draw over the thumb), so the track is 2 points smaller and inset by 1. The indicator, which values map onto, is inset by half the thumb so the thumb ends flush with the edge; the fill reaches back over the inset to the track's start.
		TrackStyle: core.Style{Height: core.Pt(sliderTrack - 2), Margin: core.Edges{Left: 1, Right: 1}, CornerRadius: sliderTrack/2 - 1, Background: sliderTrackColor,
			Shadows: []core.BoxShadow{{Spread: 1, Color: sliderEdgeColor}}, Padding: core.Edges{Left: sliderThumbW/2 - 1, Right: sliderThumbW/2 - 1}},
		IndicatorStyle: core.Style{FlexGrow: 1},
		RangeStyle:     core.Style{Margin: core.Edges{Left: -(sliderThumbW/2 - 1)}, CornerRadius: sliderTrack/2 - 1, Background: sliderFillColor},
		ThumbStyle: core.Style{Width: core.Pt(sliderThumbW), Height: core.Pt(sliderThumbH), CornerRadius: sliderThumbH / 2, Background: SwitchThumb,
			Shadows: []core.BoxShadow{shadow(1, 3, core.RGBA(0x000000, 0.3))}, FocusVisible: focusRing(2, 4)},
	}
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
		Children: []core.Node{slider, Tabular(core.Text{ContentFunc: text, Style: core.Style{MinWidth: core.Pt(64), TextAlign: core.TextAlignEnd,
			FontFamily: FontFace(Bold), FontSize: FontCardTitle, FontWeight: FaceWeight(Bold), Color: ColorText}})}}
}
