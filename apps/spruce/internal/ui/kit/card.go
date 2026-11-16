package kit

import "github.com/nsgo-dev/nsgo/core"

// CardVariant is a card's surface.
type CardVariant uint8

// The card surfaces: the glass card, the quieter one, and the dashed one of a 维护 task with a prerequisite.
const (
	CardDefault CardVariant = iota
	CardMuted
	CardHint
)

// Card is the translucent rounded card of the design (.task-card, .category-card, GlassCard): its children in a column Gap apart inside Padding. Clip keeps its children inside its corners, as a list card does.
type Card struct {
	Variant  CardVariant
	Padding  core.Edges
	Gap      core.Points
	Clip     bool
	Children []core.Node
	TestID   string
	// Style merges over the card's own, for its place in a layout (a flex share, a grid area).
	Style core.Style
}

// cardSurface is the background and border of a variant.
func cardSurface(v CardVariant) core.Style {
	switch v {
	case CardMuted:
		return core.Style{Background: SurfaceCardMuted, BorderWidth: 1, BorderColor: BorderCard}
	case CardHint:
		return core.Style{Background: SurfaceCardMuted, BorderWidth: 1, BorderColor: BorderCardHint, BorderStyle: core.BorderDashed}
	}
	return core.Style{Background: SurfaceCard, BorderWidth: 1, BorderColor: BorderCard}
}

// Render implements core.Node.
func (c Card) Render(*core.Scope) core.Node {
	st := cardSurface(c.Variant)
	st.CornerRadius, st.Padding, st.Gap = RadiusCard, c.Padding, c.Gap
	if c.Clip {
		st.Overflow = core.Hidden
	}
	return core.View{Style: st.Merge(c.Style), Children: c.Children, TestID: c.TestID}
}
