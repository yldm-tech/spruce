// Package tone holds the tile gradients of the design (--gradient-<tone> and --glow-<tone> in src/styles/tokens.css). It is apart from kit so that nav, which kit reads, can name a page's tone; kit re-exports it as kit.Tone.
package tone

import "github.com/nsgo-dev/nsgo/core"

// Tone is one of the tile gradients.
type Tone uint8

// The tones, in tokens.css's order.
const (
	Violet Tone = iota
	Green
	Orange
	Pink
	Blue
	Cyan
	Amber
	Red
	Slate
	Graphite
)

// stops are each tone's two colors, top left to bottom right, and its glow.
var stops = [...]struct{ from, to, glow core.Color }{
	Violet:   {core.RGB(0xc08bff), core.RGB(0x7a3cff), core.RGBA(0x7a3cff, 0.45)},
	Green:    {core.RGB(0x4be3a0), core.RGB(0x13a86b), core.RGBA(0x13a86b, 0.45)},
	Orange:   {core.RGB(0xffb25c), core.RGB(0xff6b2c), core.RGBA(0xff6b2c, 0.45)},
	Pink:     {core.RGB(0xff8fc8), core.RGB(0xe63e8c), core.RGBA(0xe63e8c, 0.45)},
	Blue:     {core.RGB(0x6cc4ff), core.RGB(0x2f6bff), core.RGBA(0x2f6bff, 0.45)},
	Cyan:     {core.RGB(0x5cf0e6), core.RGB(0x10a3c4), core.RGBA(0x10a3c4, 0.45)},
	Amber:    {core.RGB(0xffe066), core.RGB(0xf5a623), core.RGBA(0xf5a623, 0.45)},
	Red:      {core.RGB(0xff8a80), core.RGB(0xf0435a), core.RGBA(0xf0435a, 0.45)},
	Slate:    {core.RGB(0xa9b8d6), core.RGB(0x5a6b93), core.RGBA(0x5a6b93, 0.45)},
	Graphite: {core.RGB(0xc4bcd8), core.RGB(0x6f668c), core.RGBA(0x6f668c, 0.45)},
}

// Colors returns the tone's two gradient colors.
func (t Tone) Colors() (from, to core.Color) {
	s := stops[t]
	return s.from, s.to
}

// Gradient returns the tone's 135° gradient, mixed in sRGB as CSS does.
func (t Tone) Gradient() core.Gradient {
	s := stops[t]
	return core.Gradient{Kind: core.GradientLinear, Angle: 135, Space: core.ColorSpaceSRGB,
		Stops: []core.ColorStop{{Color: s.from}, {Color: s.to}}}
}

// Glow returns the color of the glow under a page header's tile of the tone.
func (t Tone) Glow() core.Color { return stops[t].glow }

// Named returns the tone CSS calls name ("violet", ...), Graphite for any other.
func Named(name string) Tone {
	for t, n := range names {
		if n == name {
			return Tone(t)
		}
	}
	return Graphite
}

var names = [...]string{"violet", "green", "orange", "pink", "blue", "cyan", "amber", "red", "slate", "graphite"}

// String returns the tone's CSS name.
func (t Tone) String() string { return names[t] }
