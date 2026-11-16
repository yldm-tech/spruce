package shell

import (
	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/store"
)

// titleStrip is the height of the sidebar's top strip, where the traffic lights sit on macOS; it drags the window. The design's is max(the window controls' bottom, 30): the controls of the platform's title bar end above 30 points on macOS (about 23) and are on the right on Windows, so it is 30, while desktop.TitleBar reports the whole bar's height (32 on macOS), which would push the sidebar down.
const titleStrip core.Points = 30

// Sidebar is the app's sidebar: one entry per page in its group, the current one lit, the footer's two as bare glyphs. It shows disabled and out of reach of assistive technology while an overlay is open.
type Sidebar struct {
	Nav   *nav.Navigator
	Block *store.Block
}

// Render implements core.Node.
func (s Sidebar) Render(*core.Scope) core.Node {
	strip := core.View{Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn}, Style: core.Style{FlexShrink: 0, Height: core.Pt(titleStrip)}}
	children := []core.Node{strip}
	var group []core.Node
	section := "-"
	flush := func() {
		if group != nil {
			children = append(children, core.View{Style: core.Style{Gap: 3}, Children: group})
		}
		group = nil
	}
	var footer []core.Node
	for _, r := range nav.Pages {
		m := r.Meta()
		if m.Footer {
			footer = append(footer, s.item(r, true))
			continue
		}
		if m.Section != section {
			flush()
			section = m.Section
			if section != "" {
				group = append(group, core.Text{Content: section, Style: core.Style{Padding: core.Edges{Left: 10, Right: 10, Bottom: 6},
					FontFamily: kit.FontFace(kit.Semibold), FontSize: kit.FontSection, FontWeight: kit.FaceWeight(kit.Semibold), LetterSpacingEm: kit.LetterSpacingSection,
					Color: kit.ColorTextSection}})
			}
		}
		group = append(group, s.item(r, false))
	}
	flush()
	children = append(children, core.View{Style: core.Style{AutoMargin: core.SideTop, Gap: 2, Padding: core.Edges{Top: 12},
		Borders: core.Edges{Top: 1}, BorderColor: kit.BorderSubtle}, Children: footer})
	return core.View{Role: core.RoleNavigation, Label: "Spruce", TestID: "sidebar",
		AccessibilityElementsHidden: s.Block.Blocked,
		Style: core.Style{Width: core.Pt(kit.SidebarWidth), FlexShrink: 0, Gap: 22, Padding: core.Edges{Left: 14, Right: 14, Bottom: 20},
			Background: kit.SurfaceSidebar, Borders: core.Edges{Right: 1}, BorderColor: kit.BorderSubtle},
		Children: children}
}

// item is one page's entry: a 26-point tile, or in the footer an 18-point glyph, and the title.
func (s Sidebar) item(r nav.Route, footer bool) core.Node {
	m := r.Meta()
	// A drill-down view lights the page it belongs to.
	current := func() bool { cur := s.Nav.Current(); return cur == r || cur.Meta().Parent == r }
	var icon core.Node = kit.IconTile{Tone: m.Tone, Size: 26, GlyphSize: 15, Glyph: m.Glyph, StrokeWidth: 2.2}
	height, size, color := core.Points(40), kit.FontNav, kit.ColorTextNav
	if footer {
		icon = core.View{Style: core.Style{Margin: core.Symmetric(0, 4)}, Children: []core.Node{kit.Glyph{Name: m.Glyph, Size: 18, StrokeWidth: 1.9, Color: core.CurrentColor}}}
		height, size, color = 38, kit.FontNavFooter, kit.ColorTextNavFooter
	}
	return core.View{Role: core.RoleLink, Label: m.Title, TestID: "nav-" + string(r), Selected: current, Disabled: s.Block.Blocked,
		OnPress: func(core.PressEvent) { s.Nav.Go(r) },
		StyleFunc: func() core.Style {
			st := core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, MinHeight: core.Pt(height), Padding: core.Symmetric(0, 10),
				CornerRadius: kit.RadiusNavItem, FontFamily: kit.FontSans, FontSize: size, Color: color, Cursor: core.CursorPointer,
				FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText}}
			if current() {
				st.FontFamily, st.FontWeight, st.Color, st.Background = kit.FontFace(kit.Semibold), kit.FaceWeight(kit.Semibold), kit.ColorText, kit.SurfaceNavActive
			}
			// The highlight fades from the page left to the page opened, and the entry gives a little under a press.
			return kit.Pressable(st, 0.98)
		},
		Children: []core.Node{icon, core.Text{Content: m.Title}}}
}
