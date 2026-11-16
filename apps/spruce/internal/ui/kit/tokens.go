// Package kit is the app's design system on NSGo: the tokens of src/styles/tokens.css as typed values, and the shared components every page builds with (text, tiles, cards, buttons, the action bar, the page header and column, switches, checkboxes, sliders, fields, tabs, progress, dialogs and overlays). Pages style themselves only with these tokens and components.
//
// Every gradient is mixed in sRGB, as CSS mixes legacy colors (core.LinearGradient and the CSS parser default to Oklab); build gradients with Linear, a Tone's Gradient or the components.
package kit

import (
	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/kit/tone"
)

// Colors, after tokens.css.
var (
	ColorBgDeep         = core.RGB(0x1c0f26)
	ColorText           = core.RGB(0xffffff)
	ColorTextLinkHover  = core.RGB(0xfff0c2)
	ColorTextNav        = core.RGBA(0xffffff, 0.88)
	ColorTextNavFooter  = core.RGBA(0xffffff, 0.8)
	ColorTextOverlaySub = core.RGBA(0xffffff, 0.78)
	ColorTextSecondary  = core.RGBA(0xffffff, 0.75)
	ColorTextMuted      = core.RGBA(0xffffff, 0.72)
	ColorTextSection    = core.RGBA(0xffffff, 0.55)

	ColorAccent       = core.RGB(0xf5a623)
	ColorAccentBright = core.RGB(0xffd23f)
	ColorWarning      = core.RGB(0xffd27a)
	ColorDanger       = core.RGB(0xff8a80)
	// ShadowAccent and ShadowAccentSoft are the amber of the run button's and the done badge's glow.
	ShadowAccent     = core.RGBA(0xf5a623, 0.55)
	ShadowAccentSoft = core.RGBA(0xf5a623, 0.45)

	SurfaceSidebar     = core.RGBA(0xffffff, 0.05)
	SurfaceNavActive   = core.RGBA(0xffffff, 0.14)
	SurfaceCard        = core.RGBA(0xffffff, 0.09)
	SurfaceCardMuted   = core.RGBA(0xffffff, 0.05)
	SurfaceBanner      = core.RGBA(0x000000, 0.22)
	SurfaceBannerIcon  = core.RGBA(0xffd278, 0.18)
	SurfaceButtonGhost = core.RGBA(0xffffff, 0.14)
	SurfaceDialog      = core.RGBA(0xffffff, 0.1)
	SurfaceRowHover    = core.RGBA(0xffffff, 0.06)
	SurfaceList        = core.RGBA(0x000000, 0.14)

	BorderSubtle   = core.RGBA(0xffffff, 0.08)
	BorderCard     = core.RGBA(0xffffff, 0.14)
	BorderDialog   = core.RGBA(0xffffff, 0.18)
	BorderBanner   = core.RGBA(0xffd278, 0.35)
	BorderCardHint = core.RGBA(0xffd278, 0.5)

	SwitchOn    = ColorAccent
	SwitchOff   = core.RGBA(0xffffff, 0.22)
	SwitchThumb = core.RGB(0xffffff)

	RunButtonText = core.RGB(0x3a2600)
	// Scrim is the overlays' tint over the page, with BlurOverlay behind it.
	Scrim                 = core.RGBA(0x0a0614, 0.45)
	ShadowDialog          = core.RGBA(0x000000, 0.35)
	ButtonLightText       = core.RGB(0x1a1030)
	ButtonLightBackground = core.RGB(0xffffff)
	ButtonDangerText      = core.RGB(0xffffff)

	ProgressTrack = core.RGBA(0xffffff, 0.15)
	ProgressFill  = ColorAccentBright
	CheckboxOff   = core.RGBA(0xffffff, 0.28)
	CheckboxOn    = ColorAccent
	CheckboxMark  = RunButtonText

	// MeterTrack is the track of the capacity and usage bars.
	MeterTrack = core.RGBA(0xffffff, 0.14)
	// InputBackground, InputBorder and InputPlaceholder are the search field's.
	InputBackground  = core.RGBA(0x000000, 0.2)
	InputBorder      = core.RGBA(0xffffff, 0.16)
	InputPlaceholder = core.RGBA(0xffffff, 0.5)
)

// Font sizes, after tokens.css.
const (
	FontSection    core.Points = 11
	FontNavFooter  core.Points = 13
	FontBody       core.Points = 13
	FontNav        core.Points = 14
	FontCardTitle  core.Points = 15
	FontRunButton  core.Points = 17
	FontTitle      core.Points = 22
	FontStat       core.Points = 26
	FontDone       core.Points = 46
	FontHero       core.Points = 56
	FontSmall      core.Points = 12
	FontSubheading core.Points = 14
)

// Letter spacing, in em.
const (
	LetterSpacingSection float32 = 0.08
	LetterSpacingStat    float32 = -0.02
)

// Radii, after tokens.css.
const (
	RadiusNavIcon    core.Points = 8
	RadiusNavItem    core.Points = 10
	RadiusCardIcon   core.Points = 12
	RadiusHeaderIcon core.Points = 14
	RadiusCard       core.Points = 20
	RadiusCheckbox   core.Points = 6
	RadiusDialog     core.Points = 28
	RadiusPill       core.Points = 999
)

// Layout, after tokens.css.
const (
	SidebarWidth core.Points = 228
	MainMinWidth core.Points = 560
	// BlurOverlay is the blur behind the overlays' scrim.
	BlurOverlay core.Points = 16
)

// FontSans is the app's font list at the regular weight: Inter (bundled, Latin only), then the system's Chinese UI font. Styles that set a weight take FontFace and FaceWeight instead.
var FontSans = FontFace(core.FontWeightNormal)

// Tone is one of the tile gradients (package tone).
type Tone = tone.Tone

// The tones.
const (
	ToneViolet   = tone.Violet
	ToneGreen    = tone.Green
	ToneOrange   = tone.Orange
	TonePink     = tone.Pink
	ToneBlue     = tone.Blue
	ToneCyan     = tone.Cyan
	ToneAmber    = tone.Amber
	ToneRed      = tone.Red
	ToneSlate    = tone.Slate
	ToneGraphite = tone.Graphite
)

// Linear returns a linear gradient along angle (CSS degrees: 90 runs left to right) through stops, mixed in sRGB. It is the only way pages build gradients; stops without At spread evenly.
func Linear(angle float32, stops ...core.ColorStop) core.Gradient {
	return core.Gradient{Kind: core.GradientLinear, Angle: angle, Space: core.ColorSpaceSRGB, Stops: stops}
}

// Stop is a gradient stop of color c at pct percent along it.
func Stop(c core.Color, pct float32) core.ColorStop {
	return core.ColorStop{Color: c, At: core.Pct(pct)}
}

// Stops returns evenly spread stops of colors.
func Stops(colors ...core.Color) []core.ColorStop {
	out := make([]core.ColorStop, len(colors))
	for i, c := range colors {
		out[i].Color = c
	}
	return out
}

// GradientAccent is the amber of the 维护 page (--gradient-accent).
func GradientAccent() core.Gradient { return ToneAmber.Gradient() }

// GradientDanger is the red of destructive buttons (--button-danger-background).
func GradientDanger() core.Gradient { return ToneRed.Gradient() }

// Level is how serious a figure or a notice is.
type Level uint8

// The levels.
const (
	LevelNormal Level = iota
	LevelWarning
	LevelCritical
)

// shadow is a box shadow of color c offset y down and blurred blur (CSS's blur radius).
func shadow(y, blur core.Points, c core.Color) core.BoxShadow {
	return core.BoxShadow{Offset: core.Point{Y: y}, Blur: blur, Color: c}
}

// focusRing is the outline the keyboard's focus draws around a control (2 points, white, 2 away).
func focusRing(width, offset core.Points) *core.Style {
	return &core.Style{OutlineWidth: width, OutlineOffset: offset, OutlineColor: ColorText}
}
