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
	pos := core.NewAnimatedValue(cx, 0)
	var run *core.AnimationRun
	cx.Effect(func() {
		reduce := cx.Env().ReduceMotion()
		if run != nil {
			run.Stop()
			run = nil
		}
		if reduce {
			pos.Set(0)
			return
		}
		pos.Set(0)
		run = cx.StartAnimation(core.AtFrameRate(core.Loop(core.Timing(pos, 1, core.TimingConfig{Duration: 1400 * time.Millisecond,
			Easing: core.BezierEasing(core.EaseInOut)}), 0), 60), nil)
	})
	cx.Cleanup(func() {
		if run != nil {
			run.Stop()
		}
	})
	band := core.View{PointerEvents: core.PointerEventsNone, StyleFunc: func() core.Style {
		w := width.Get()
		st := core.Style{Position: core.Absolute, Top: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0), Width: core.Pct(40),
			Gradients: []core.Gradient{Linear(90, Stop(core.RGBA(0xffd23f, 0), 0), Stop(core.RGBA(0xffd23f, 0.7), 50), Stop(core.RGBA(0xffd23f, 0), 100))}}
		if cx.Env().ReduceMotion() || w == 0 {
			st.Display = core.DisplayNone
			return st
		}
		band := float64(w) * 0.4
		st.Animated = &core.AnimatedStyle{TranslateX: core.Interpolate(pos, core.Interpolation{Input: []float64{0, 1}, Output: []float64{-band, 2.5 * band}})}
		return st
	}}
	return core.View{TestID: s.TestID, OnLayout: func(b core.Bounds) { width.Set(b.Size.Width) },
		Style:    core.Style{Height: core.Pt(h), CornerRadius: h / 2, Background: MeterTrack, Overflow: core.Hidden},
		Children: []core.Node{band}}
}
