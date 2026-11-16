package kit

import (
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// ProgressRing is the overlays' ring: a filling amber arc when Value knows the fraction done, with a faint glint circling the track until it is full, so work that goes on shows as moving while its fraction stands still; the 维护 page's spinning quarter arc when it does not. Children sit in its middle (the percent, the bytes found). Size is its diameter (96 when 0), Stroke its line (8 when 0), both at 96 points and scaled with Size.
type ProgressRing struct {
	Value    func() (fraction float64, known bool)
	Size     core.Points
	Stroke   core.Points
	Label    string
	Children []core.Node
	TestID   string
}

// spin is the unknown ring's turn: 0.9 s, slowed to 3 s while the user asks for reduced motion, as the design's CSS does.
func spin(reduce bool) core.KeyframeAnimation {
	d := 900 * time.Millisecond
	if reduce {
		d = 3 * time.Second
	}
	return core.KeyframeAnimation{Keyframes: core.KeyframesSpin, Duration: d, Easing: core.Linear}
}

// Render implements core.Node.
func (p ProgressRing) Render(cx *core.Scope) core.Node {
	size, stroke := p.Size, p.Stroke
	if size == 0 {
		size = 96
	}
	if stroke == 0 {
		stroke = 8
	}
	value := func() (float64, bool) {
		if p.Value == nil {
			return 0, false
		}
		f, ok := p.Value()
		if !ok || math.IsNaN(f) {
			return 0, false
		}
		return min(max(f, 0), 1), true
	}
	canvas := core.Canvas{
		Draw: func(d *core.Drawing, sz core.Size) {
			s := float32(sz.Width) / 96
			cxy, r := 48*s, 40*s
			fraction, known := value()
			d.SetLineWidth(float32(stroke) * s).SetLineCap(core.CapRound)
			d.BeginPath().Arc(cxy, cxy, r, 0, 2*math.Pi, false).SetStrokeColor(ProgressTrack).Stroke()
			start := float32(-math.Pi / 2)
			end := start + float32(math.Pi/2)
			if known {
				if fraction <= 0 {
					return
				}
				end = start + float32(2*math.Pi*fraction)
			}
			d.BeginPath().Arc(cxy, cxy, r, start, end, false).SetStrokeColor(ProgressFill).Stroke()
		},
		StyleFunc: func() core.Style {
			st := core.Style{Position: core.Absolute, Left: core.Pt(0), Top: core.Pt(0), Width: core.Pt(size), Height: core.Pt(size)}
			if _, known := value(); !known {
				st.Animation = spin(cx.Env().ReduceMotion())
			}
			return st
		},
	}
	// The glint turns with the ring's own spin, slower; it is not drawn once the ring is full or while it spins anyway.
	glint := core.Canvas{
		Draw: func(d *core.Drawing, sz core.Size) {
			s := float32(sz.Width) / 96
			cxy, r := 48*s, 40*s
			d.SetLineWidth(float32(stroke) * s).SetLineCap(core.CapRound)
			d.BeginPath().Arc(cxy, cxy, r, float32(-math.Pi/2), float32(-math.Pi/2+math.Pi/4), false).SetStrokeColor(core.RGBA(0xffffff, 0.28)).Stroke()
		},
		StyleFunc: func() core.Style {
			st := core.Style{Position: core.Absolute, Left: core.Pt(0), Top: core.Pt(0), Width: core.Pt(size), Height: core.Pt(size)}
			f, known := value()
			if !known || f >= 1 || cx.Env().ReduceMotion() {
				st.Display = core.DisplayNone
				return st
			}
			st.Animation = core.KeyframeAnimation{Keyframes: core.KeyframesSpin, Duration: 1600 * time.Millisecond, Easing: core.Linear}
			return st
		},
	}
	children := []core.Node{canvas, glint}
	if len(p.Children) > 0 {
		children = append(children, core.View{Style: core.Style{Position: core.Relative, AlignItems: core.AlignCenter,
			FontFamily: FontFace(Bold), FontSize: 20, FontWeight: FaceWeight(Bold), FontVariant: core.FontTabularNums, Color: ColorText}, Children: p.Children})
	}
	return core.View{Role: core.RoleProgressBar, Label: p.Label, TestID: p.TestID,
		AccessibilityValue: func() core.AccessibilityValue {
			f, known := value()
			if !known {
				return core.AccessibilityValue{Min: 0, Max: 100, NoNow: true}
			}
			return core.AccessibilityValue{Min: 0, Max: 100, Now: float32(math.Round(f * 100))}
		},
		Style:    core.Style{Width: core.Pt(size), Height: core.Pt(size), FlexShrink: 0, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter},
		Children: children}
}

// Spinner is a small spinning arc for work of unknown length, Size points across (20 when 0); Label names it for assistive technology.
type Spinner struct {
	Size  core.Points
	Label string
}

// Render implements core.Node.
func (s Spinner) Render(*core.Scope) core.Node {
	size := s.Size
	if size == 0 {
		size = 20
	}
	return ProgressRing{Size: size, Stroke: 10, Label: s.Label}
}
