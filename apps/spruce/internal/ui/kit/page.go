package kit

import "github.com/nsgo-dev/nsgo/core"

// Page is a page's scrolling column (.page): its children 18 points apart inside 32-point margins, taking the main area's height so an ActionBar sits at its bottom. Ref names its scroll view, for ScrollTo.
type Page struct {
	Children []core.Node
	Ref      *core.Ref
	TestID   string
}

// Render implements core.Node.
func (p Page) Render(*core.Scope) core.Node {
	return core.Scroll{Ref: p.Ref, TestID: p.TestID,
		Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Gap: 18, Padding: core.Edges{Left: 32, Right: 32, Bottom: 24}},
		Children: p.Children}
}
