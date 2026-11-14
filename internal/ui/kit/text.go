package kit

import "github.com/nsgo-dev/nsgo/core"

// textStyle is the style of a text in the app's font.
func textStyle(size core.Points, weight core.FontWeight, color core.Color) core.Style {
	return core.Style{FontFamily: FontFace(weight), FontSize: size, FontWeight: FaceWeight(weight), Color: color}
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

// SectionTitle is the small spaced heading over a group of cards or rows (.space-section-title, .smart-section-title, a detail card's section).
func SectionTitle(s string) core.Text {
	return core.Text{Content: s, Role: core.RoleHeader, Style: core.Style{FontFamily: FontFace(Semibold), FontSize: FontSection,
		FontWeight: FaceWeight(Semibold), LetterSpacingEm: LetterSpacingSection, Color: ColorTextSection}}
}

// Weights, for Text.
const (
	Regular  = core.FontWeightNormal
	Medium   = core.FontWeightMedium
	Semibold = core.FontWeightSemibold
	Bold     = core.FontWeightBold
)

// FontFace returns the font list that draws weight w in the app's fonts, with FaceWeight(w) as the style's weight. On macOS every weight is named by its faces' PostScript names (Inter-SemiBold, then PingFangSC-Semibold) at the regular weight: NSGo (ab01541) matches a weight inside a family through CoreText descriptors, which do not see the fonts it registers from the app's resources and do not carry the weight into the cascade list, so naming the family draws every weight of Inter and of the Chinese fallback as Regular. Elsewhere it is the family list, which DirectWrite matches by weight.
func FontFace(w core.FontWeight) string { return fontFace(w) }

// FaceWeight is the weight a style sets beside FontFace(w).
func FaceWeight(w core.FontWeight) core.FontWeight {
	if namesFaces {
		return core.FontWeightNormal
	}
	return w
}
