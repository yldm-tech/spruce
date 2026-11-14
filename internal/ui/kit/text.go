package kit

import "github.com/nsgo-dev/nsgo/core"

// textStyle is the style of a text in the app's font.
func textStyle(size core.Points, weight core.FontWeight, color core.Color) core.Style {
	return core.Style{FontFamily: FontSans, FontSize: size, FontWeight: weight, Color: color}
}

// Text returns s in the app's font at size, weight and color.
func Text(s string, size core.Points, weight core.FontWeight, color core.Color) core.Text {
	return core.Text{Content: s, Style: textStyle(size, weight, color)}
}

// TextFunc returns a text showing what fn returns, as it changes.
func TextFunc(fn func() string, size core.Points, weight core.FontWeight, color core.Color) core.Text {
	return core.Text{ContentFunc: fn, Style: textStyle(size, weight, color)}
}

// Tabular returns t with tabular figures, for numbers that change in place.
func Tabular(t core.Text) core.Text {
	t.Style.FontVariant |= core.FontTabularNums
	return t
}

// Weights, for Text.
const (
	Regular  = core.FontWeightNormal
	Medium   = core.FontWeightMedium
	Semibold = core.FontWeightSemibold
	Bold     = core.FontWeightBold
)
