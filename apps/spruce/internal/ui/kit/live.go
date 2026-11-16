package kit

import "github.com/nsgo-dev/nsgo/core"

// LiveStatus is a status line screen readers announce as it changes and nobody sees (the design's visually hidden role=status). Mount it from the page's first render, so the first change is heard.
type LiveStatus struct {
	Text func() string
}

// Render implements core.Node.
func (l LiveStatus) Render(*core.Scope) core.Node {
	return core.View{PointerEvents: core.PointerEventsNone,
		Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Top: core.Pt(0), Width: core.Pt(1), Height: core.Pt(1), Overflow: core.Hidden},
		Children: []core.Node{core.Text{ContentFunc: l.Text, LiveRegion: core.LiveRegionPolite, Role: core.RoleText,
			Style: core.Style{FontSize: 1, Color: core.Transparent, WhiteSpace: core.WhiteSpaceNoWrap}}}}
}
