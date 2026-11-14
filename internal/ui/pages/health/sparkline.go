package health

import (
	"github.com/nsgo-dev/nsgo/core"
)

// chartSeries is one line of a sparkline: its color, and the color of the area under it (none when 0).
type chartSeries struct {
	stroke, fill core.Color
}

// The charts' colors (--chart-* in tokens.css).
var (
	cpuSeries     = []chartSeries{{core.RGB(0xffd23f), core.RGBA(0xffd23f, 0.16)}}
	memorySeries  = []chartSeries{{core.RGB(0xc08bff), core.RGBA(0xc08bff, 0.16)}}
	diskSeries    = []chartSeries{{stroke: diskReadColor}, {stroke: diskWriteColor}}
	networkSeries = []chartSeries{{stroke: netDownColor}, {stroke: netUpColor}}

	diskReadColor  = core.RGB(0x5cf0e6)
	diskWriteColor = core.RGB(0xff8fc8)
	netDownColor   = core.RGB(0x6cc4ff)
	netUpColor     = core.RGB(0x4be3a0)
)

const (
	// windowSeconds is the span of every sparkline: the two minutes the sampler keeps.
	windowSeconds = 120
	// minRateTop is the lowest top of a throughput chart's y axis (1 KB/s), so an idle disk or network draws a flat line at the bottom instead of magnifying noise.
	minRateTop = 1000
	// sparklineHeight is a sparkline's height.
	sparklineHeight core.Points = 56
	// sparklineLine is the width of its lines.
	sparklineLine = 1.5
)

// Sparkline is a small line chart without axes, as the design drew it with uPlot: a straight line from sample to sample, broken where a value is missing or sampling stopped for a while (Chart.Segments), with the area under it filled for series that have a fill. The x axis spans windowSeconds and ends at the newest sample; the y axis starts at 0 and ends at Max, or fits the data but never below MinTop (Chart.Top). It is decorative: the card around it states the numbers.
type Sparkline struct {
	Chart  func() Chart
	Series []chartSeries
	Max    float64
	MinTop float64
	TestID string
}

// Render implements core.Node.
func (s Sparkline) Render(*core.Scope) core.Node {
	minTop := s.MinTop
	if minTop == 0 {
		minTop = 1
	}
	return core.Canvas{TestID: s.TestID,
		Style: core.Style{Height: core.Pt(sparklineHeight), MinWidth: core.Pt(0), FlexShrink: 0},
		Draw: func(d *core.Drawing, size core.Size) {
			c := s.Chart()
			if len(c.X) == 0 || size.Width <= 0 || size.Height <= 0 {
				return
			}
			w, h := float64(size.Width), float64(size.Height)
			left := c.X[len(c.X)-1] - windowSeconds
			top := c.Top(s.Max, minTop)
			px := func(p Point) float32 { return float32((p.X - left) / windowSeconds * w) }
			py := func(p Point) float32 { return float32(h - p.Y/top*h) }
			d.SetLineWidth(sparklineLine).SetLineJoin(core.JoinRound)
			for i, series := range s.Series {
				for _, seg := range c.Segments(i) {
					// A lone point between gaps draws nothing, as uPlot does without point markers.
					if len(seg) < 2 {
						continue
					}
					if series.fill != 0 {
						d.BeginPath().MoveTo(px(seg[0]), float32(h))
						for _, p := range seg {
							d.LineTo(px(p), py(p))
						}
						d.LineTo(px(seg[len(seg)-1]), float32(h)).ClosePath().SetFillColor(series.fill).Fill()
					}
					d.BeginPath().MoveTo(px(seg[0]), py(seg[0]))
					for _, p := range seg[1:] {
						d.LineTo(px(p), py(p))
					}
					d.SetStrokeColor(series.stroke).Stroke()
				}
			}
		}}
}
