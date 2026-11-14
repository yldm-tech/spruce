package kit

import (
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// meterFill is the gradient of a meter at a level: amber, then orange, then red (--meter-fill, --meter-fill-warning, --meter-fill-critical).
func meterFill(l Level) core.Gradient {
	switch l {
	case LevelWarning:
		return Linear(90, Stops(core.RGB(0xffd27a), core.RGB(0xffb25c))...)
	case LevelCritical:
		return GradientDanger()
	}
	return GradientAccent()
}

// Meter is a capacity or usage bar: Value from 0 to 1 fills it in the color of its Level, its width moving smoothly. Height is its thickness (8 when 0).
type Meter struct {
	Value  func() float64
	Level  func() Level
	Height core.Points
	Label  string
	TestID string
}

// Render implements core.Node.
func (m Meter) Render(*core.Scope) core.Node {
	h := m.Height
	if h == 0 {
		h = 8
	}
	value := func() float64 {
		if m.Value == nil {
			return 0
		}
		v := m.Value()
		if math.IsNaN(v) {
			return 0
		}
		return min(max(v, 0), 1)
	}
	fill := core.View{StyleFunc: func() core.Style {
		l := LevelNormal
		if m.Level != nil {
			l = m.Level()
		}
		return core.Style{Width: core.Pct(float32(value() * 100)), Height: core.Pct(100), CornerRadius: h / 2,
			Gradients:  []core.Gradient{meterFill(l)},
			Transition: core.Transition{Duration: 250 * time.Millisecond, Easing: core.Ease, Properties: core.TransitionLayout | core.TransitionBackground}}
	}}
	return core.View{Role: core.RoleProgressBar, Label: m.Label, TestID: m.TestID,
		AccessibilityValue: func() core.AccessibilityValue {
			return core.AccessibilityValue{Min: 0, Max: 100, Now: float32(math.Round(value() * 100))}
		},
		Style:    core.Style{Height: core.Pt(h), CornerRadius: h / 2, Background: MeterTrack, Overflow: core.Hidden, Direction: core.Row},
		Children: []core.Node{fill}}
}
