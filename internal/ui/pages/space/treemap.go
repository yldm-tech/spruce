package space

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
)

// Treemap colors, after tokens.css (--space-cell-*).
var (
	// cellTones are the cells' gradients, in turn: the cyan, blue, violet, pink, orange and green tiles at 72% opacity.
	cellTones       = [...][2]uint32{{0x5cf0e6, 0x10a3c4}, {0x6cc4ff, 0x2f6bff}, {0xc08bff, 0x7a3cff}, {0xff8fc8, 0xe63e8c}, {0xffb25c, 0xff6b2c}, {0x4be3a0, 0x13a86b}}
	cellGroup       = core.RGBA(0xffffff, 0.08)
	cellGroupBorder = core.RGBA(0xffffff, 0.3)
	cellEdge        = core.RGBA(0xffffff, 0.16)
	cellTextShadow  = core.RGBA(0x000000, 0.45)
)

// cellRadius is a cell's corners (--radius-space-cell).
const cellRadius core.Points = 8

// cellGradient is the gradient of the cell at index i.
func cellGradient(i int) core.Gradient {
	t := cellTones[i%len(cellTones)]
	return kit.Linear(135, kit.Stops(core.RGBA(t[0], 0.72), core.RGBA(t[1], 0.72))...)
}

// entryKey names an entry among its folder's children: its path, or its kind for a folded group.
func entryKey(e app.SpaceEntry) string {
	if IsGroup(e) {
		return "group-" + string(e.Kind)
	}
	return e.Path
}

// Treemap draws a folder's children (largest first) as a squarified treemap (LayoutTreemap) of absolutely positioned cells: a cell's area is its share of the folder. Folders are buttons that open them; large cells show the name and size, the others show them as a tooltip. The list beside the map holds the same entries for the keyboard and screen readers, so the cells take no keyboard focus.
type Treemap struct {
	Entries func() []app.SpaceEntry
	// Total is the folder's size, for the share in each tooltip.
	Total func() int64
	// Label is the folder's name, for the map's accessible name.
	Label  func() string
	OnOpen func(app.SpaceEntry)
}

// Render implements core.Node.
func (m Treemap) Render(cx *core.Scope) core.Node {
	size := core.NewSignal(cx, core.Size{})
	cells := core.NewMemoFunc(cx, func() []Cell[app.SpaceEntry] {
		sz := size.Get()
		return LayoutTreemap(m.Entries(), func(e app.SpaceEntry) int64 { return e.Size }, float64(sz.Width), float64(sz.Height))
	}, nil)
	return core.View{Role: core.RoleGroup, LabelFunc: func() string { return m.Label() + " 的空间分布图" }, TestID: "treemap",
		// The cells are placed inside the border, so the area they fill is the box less its 1-point border.
		OnLayout: func(b core.Bounds) {
			size.Set(core.Size{Width: max(0, b.Size.Width-2), Height: max(0, b.Size.Height-2)})
		},
		Style: core.Style{Position: core.Relative, Height: core.Pct(100), MinWidth: core.Pt(0), Overflow: core.Hidden, CornerRadius: kit.RadiusCard,
			Background: kit.SurfaceList, BorderWidth: 1, BorderColor: kit.BorderCard},
		Children: []core.Node{core.Each(cells.Get, func(c Cell[app.SpaceEntry]) string { return entryKey(c.Item) },
			func(_ *core.Scope, cell func() Cell[app.SpaceEntry], index func() int) core.Node {
				return treemapCell(cell, index, m)
			})}}
}

// treemapCell is one cell: a button for a folder, a picture for anything else. A cell keeps its entry's key (its path, or a group's kind) and with it its kind; its sizes follow the entry.
func treemapCell(cell func() Cell[app.SpaceEntry], index func() int, m Treemap) core.Node {
	kind := cell().Item.Kind
	entry := func() app.SpaceEntry { return cell().Item }
	name := func() string { return EntryLabel(entry()) }
	size := func() string { return format.Bytes(entry().Size) }
	tip := func() string {
		t := name() + " · " + size() + " · " + FormatShare(ShareOf(entry().Size, m.Total()))
		if kind == app.SpaceDir {
			t += "\n点按打开"
		}
		return t
	}
	group := kind == app.SpaceOther || kind == app.SpaceFiles
	style := func() core.Style {
		c := cell()
		st := core.Style{Position: core.Absolute, Left: core.Pt(core.Points(c.X)), Top: core.Pt(core.Points(c.Y)),
			Width: core.Pt(core.Points(c.Width)), Height: core.Pt(core.Points(c.Height)),
			JustifyContent: core.JustifyEnd, AlignItems: core.AlignStart, Gap: 1, Padding: core.Symmetric(6, 8), Overflow: core.Hidden,
			CornerRadius: cellRadius, Color: kit.ColorText, TextAlign: core.TextAlignLeft,
			TextShadowColor: cellTextShadow, TextShadowOffset: core.Point{Y: 1}, TextShadowRadius: 2}
		if group {
			st.Background, st.BorderWidth, st.BorderColor, st.BorderStyle = cellGroup, 1, cellGroupBorder, core.BorderDashed
		} else {
			st.Gradients = []core.Gradient{cellGradient(index())}
			st.Shadows = []core.BoxShadow{{Spread: 1, Color: cellEdge, Inset: true}}
		}
		if kind == app.SpaceDir {
			st.Cursor = core.CursorPointer
			st.Hover = &core.Style{Filters: []core.Filter{core.Brightness(1.15)}}
		}
		return st
	}
	body := core.Show{When: func() bool { c := cell(); return CellShowsLabel(c.Width, c.Height) }, Then: func(*core.Scope) core.Node {
		return core.Group{
			core.Text{ContentFunc: name, MaxLines: 1, EllipsizeMode: core.EllipsizeTail,
				Style: core.Style{MaxWidth: core.Pct(100), FontFamily: kit.FontFace(kit.Semibold), FontSize: kit.FontSmall, FontWeight: kit.FaceWeight(kit.Semibold)}},
			core.Text{ContentFunc: size, MaxLines: 1, EllipsizeMode: core.EllipsizeTail,
				Style: core.Style{MaxWidth: core.Pct(100), FontFamily: kit.FontSans, FontSize: 11, FontVariant: core.FontTabularNums, Opacity: core.Alpha(0.85)}},
		}
	}}
	v := core.View{StyleFunc: style, Tooltip: core.Dynamic(func(*core.Scope) core.Node { return tooltip(tip()) }), Children: []core.Node{body}}
	if kind == app.SpaceDir {
		v.Role, v.NoFocus = core.RoleButton, true
		v.LabelFunc = func() string { return "打开 " + name() + "，" + size() }
		v.OnPress = func(core.PressEvent) { m.OnOpen(entry()) }
	} else {
		v.AccessibilityElementsHidden = func() bool { return true }
	}
	return v
}
