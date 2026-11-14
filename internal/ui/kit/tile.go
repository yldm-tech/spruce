package kit

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/kit/icons"
)

// Glyph draws one of the app's line icons (package icons) Size points square in Color (white when 0, the inherited text color when core.CurrentColor), StrokeWidth units wide on its 24-unit grid (2 when 0). It is decorative: assistive technology does not see it.
type Glyph struct {
	Name        string
	Size        core.Points
	Color       core.Color
	StrokeWidth float32
}

// Render implements core.Node.
func (g Glyph) Render(*core.Scope) core.Node {
	c := g.Color
	switch c {
	case 0:
		c = ColorText
	case core.CurrentColor:
		c = 0
	}
	return core.Icon{SVG: icons.SVG(g.Name, g.StrokeWidth),
		Style: core.Style{Width: core.Pt(g.Size), Height: core.Pt(g.Size), FlexShrink: 0, Color: c}}
}

// IconTile is a gradient tile holding a white glyph, as on the sidebar, the page headers and the cards. Radius is its corners' (by its size when 0: 8 at 26, 12 at 42, 14 at 48, 16 above); Glow adds the tone's glow under it, as the page header does.
type IconTile struct {
	Tone        Tone
	Size        core.Points
	GlyphSize   core.Points
	Glyph       string
	StrokeWidth float32
	Glow        bool
	Radius      core.Points
}

func (t IconTile) radius() core.Points {
	switch {
	case t.Radius > 0:
		return t.Radius
	case t.Size <= 26:
		return RadiusNavIcon
	case t.Size <= 42:
		return RadiusCardIcon
	case t.Size <= 48:
		return RadiusHeaderIcon
	}
	return 16
}

// Render implements core.Node.
func (t IconTile) Render(*core.Scope) core.Node {
	st := core.Style{Width: core.Pt(t.Size), Height: core.Pt(t.Size), FlexShrink: 0, CornerRadius: t.radius(),
		AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gradients: []core.Gradient{t.Tone.Gradient()}}
	if t.Glow {
		st.Shadows = []core.BoxShadow{shadow(10, 24, t.Tone.Glow())}
	}
	return core.View{Style: st, AccessibilityElementsHidden: func() bool { return true },
		Children: []core.Node{Glyph{Name: t.Glyph, Size: t.GlyphSize, StrokeWidth: t.StrokeWidth}}}
}

// categoryTones are the tones of the cleaner lists' category glyphs (Category.Icon keys).
var categoryTones = map[string]Tone{
	"cache": ToneGreen, "log": ToneBlue, "crash": ToneRed, "temp": ToneCyan, "trash": ToneSlate, "browser": ToneBlue,
	"mail": ToneCyan, "update": ToneViolet, "xcode": ToneBlue, "simulator": ToneSlate, "node": ToneGreen,
	"package": ToneOrange, "docker": ToneCyan, "build": ToneOrange, "installer": TonePink, "disk": ToneCyan,
	"app": ToneBlue, "leftover": ToneViolet, "file": ToneGraphite, "video": TonePink, "archive": ToneAmber,
	"folder": ToneGraphite,
}

// CategoryTile is the 42-point tile of a cleaner category, from its Icon key; an unknown key shows the folder.
func CategoryTile(key string) IconTile {
	name := "category/" + key
	t, ok := categoryTones[key]
	if !ok || !icons.Has(name) {
		name, t = icons.Fallback, ToneGraphite
	}
	return IconTile{Tone: t, Size: 42, GlyphSize: 20, Glyph: name}
}

// taskTones are the tones of the 维护 cards' glyphs (maintenance.Icon).
var taskTones = map[string]Tone{
	"network": ToneBlue, "search": ToneViolet, "preview": ToneCyan, "icons": TonePink, "open-with": ToneGreen,
	"database": ToneAmber, "cleanup": ToneGreen, "repair": ToneOrange, "disk": ToneCyan, "update": ToneBlue,
	"font": ToneViolet, "store": TonePink, "network-fix": ToneRed,
}

// TaskTile is the 42-point tile at the top left of a 维护 card, from the task's icon.
func TaskTile(icon string) IconTile {
	return IconTile{Tone: taskTones[icon], Size: 42, GlyphSize: 20, Glyph: "task/" + icon, Radius: RadiusCardIcon}
}
