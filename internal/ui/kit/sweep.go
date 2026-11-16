package kit

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// SweepBar is 智能扫描's bar for a source whose total is not known yet: a band of amber light 40% as wide as the bar sweeping across it every 1.4 s, still while the user asks for reduced motion. Height is its thickness (6 when 0).
type SweepBar struct {
	Height core.Points
	TestID string
}

// Render implements core.Node.
func (s SweepBar) Render(cx *core.Scope) core.Node {
	h := s.Height
	if h == 0 {
		h = 6
	}
	width := core.NewSignal(cx, core.Points(0))
	band := sweepBand(cx, width.Get, nil, 0xffd23f, 0.7)
	return core.View{TestID: s.TestID, OnLayout: func(b core.Bounds) { width.Set(b.Size.Width) },
		Style:    core.Style{Height: core.Pt(h), CornerRadius: h / 2, Background: MeterTrack, Overflow: core.Hidden},
		Children: []core.Node{band}}
}

// sweepBand is a band of light 40% as wide as its bar (width, which the bar measures) sweeping across it every 1.4 s while on says so (always when nil), absent otherwise and while the user asks for reduced motion. hex and alpha are its color at its middle. Place it as an absolute child of a bar that clips.
func sweepBand(cx *core.Scope, width func() core.Points, on func() bool, hex uint32, alpha float32) core.Node {
	pos := core.NewAnimatedValue(cx, 0)
	active := func() bool { return (on == nil || on()) && !cx.Env().ReduceMotion() }
	var run *core.AnimationRun
	stop := func() {
		if run != nil {
			run.Stop()
			run = nil
		}
	}
	cx.Effect(func() {
		a := active()
		cx.Untrack(func() {
			stop()
			pos.Set(0)
			if a {
				run = cx.StartAnimation(core.AtFrameRate(core.Loop(core.Timing(pos, 1, core.TimingConfig{Duration: 1400 * time.Millisecond,
					Easing: core.BezierEasing(core.EaseInOut)}), 0), 60), nil)
			}
		})
	})
	cx.Cleanup(stop)
	light, clear := core.RGBA(hex, alpha), core.RGBA(hex, 0)
	// The band is there only while it sweeps.
	return core.Show{When: func() bool { return active() && width() > 0 }, Then: func(*core.Scope) core.Node {
		return core.View{PointerEvents: core.PointerEventsNone, TestID: "sweep-light", StyleFunc: func() core.Style {
			band := float64(width()) * 0.4
			return core.Style{Position: core.Absolute, Top: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0), Width: core.Pct(40),
				Gradients: []core.Gradient{Linear(90, Stop(clear, 0), Stop(light, 50), Stop(clear, 100))},
				Animated:  &core.AnimatedStyle{TranslateX: core.Interpolate(pos, core.Interpolation{Input: []float64{0, 1}, Output: []float64{-band, 2.5 * band}})}}
		}}
	}}
}
