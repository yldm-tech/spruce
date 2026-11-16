package uninstall

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/ui/kit"
)

// The page's own colors, after tokens.css's uninstall section.
var (
	rowSelected       = core.RGBA(0xffffff, 0.16)
	rowSelectedBorder = core.RGBA(0xffd278, 0.55)
	badgeBackground   = core.RGBA(0xffffff, 0.14)
	badgeRunning      = core.RGBA(0xffd278, 0.2)
	iconShadow        = core.RGBA(0x000000, 0.25)
)

// rowHeight is the height of one app row, which the list's estimate relies on.
const rowHeight = 60

// pageKeys is how many rows Page Up and Page Down move.
const pageKeys = 8

// listGroup names the list box for its rows' focus outline.
const listGroup = "applist"

// focusRing is the outline the keyboard's focus draws (2 points, white, offset away).
func focusRing(offset core.Points) *core.Style {
	return &core.Style{OutlineWidth: 2, OutlineOffset: offset, OutlineColor: kit.ColorText}
}

// sortOptions are the list's orders and their labels.
func sortOptions(windows bool) []struct {
	key   SortKey
	label string
	id    string
} {
	recent := "最近使用"
	if windows {
		recent = "安装时间"
	}
	return []struct {
		key   SortKey
		label string
		id    string
	}{{SortName, "名称", "sort-name"}, {SortSize, "大小", "sort-size"}, {SortRecent, recent, "sort-recent"}}
}

// appList is the left column: the search field, the order, the count and the apps as a single-selection list box. While the list box has the keyboard's focus ↑ ↓ Home End Page Up and Page Down move the selection; on Windows, where only text fields take the focus, ↑ and ↓ also move it while the search field has it.
func appList(cx *core.Scope, s *State, listRef *core.Ref, narrow func() bool) core.Node {
	searchFocused := core.NewSignal(cx, false)
	search := core.View{
		OnFocusIn:  func() { searchFocused.Set(true) },
		OnFocusOut: func() { searchFocused.Set(false) },
		Style:      core.Style{Direction: core.Row, FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(140), MinWidth: core.Pt(0)},
		Children: []core.Node{kit.Field{Placeholder: "搜索应用", Label: "搜索应用", TestID: "search", Value: s.Query.Get,
			OnChange: func(q string) { s.Query.Set(q) }}},
	}
	options := sortOptions(s.Windows())
	buttons := make([]core.Node, len(options))
	for i, o := range options {
		buttons[i] = core.View{Role: core.RoleButton, Label: o.label, TestID: o.id,
			Pressed: func() bool { return s.Sort.Get() == o.key },
			OnPress: func(core.PressEvent) {
				if s.Sort.GetUntracked() != o.key {
					// The rows slide into the new order.
					kit.SettleLayout(cx)
					s.Sort.Set(o.key)
				}
			},
			Style: core.Style{MinHeight: core.Pt(28), Padding: core.Symmetric(0, 12), CornerRadius: 14, JustifyContent: core.JustifyCenter,
				FontFamily: kit.FontSans, FontSize: kit.FontSmall, Color: kit.ColorTextSecondary, Cursor: core.CursorPointer, FocusVisible: focusRing(1),
				Pressed: &core.Style{Background: kit.SurfaceButtonGhost, Color: kit.ColorText, FontFamily: kit.FontFace(kit.Semibold),
					FontWeight: kit.FaceWeight(kit.Semibold)}},
			Children: []core.Node{core.Text{Content: o.label}}}
	}
	tools := core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 10,
		Padding: core.Edges{Top: 16, Right: 16, Bottom: 8, Left: 16}},
		Children: []core.Node{
			search,
			core.View{Role: core.RoleGroup, Label: "排序方式", Style: core.Style{Direction: core.Row, Padding: core.All(3), CornerRadius: 17,
				Background: kit.InputBackground}, Children: buttons},
		}}
	count := kit.Tabular(core.Text{LiveRegion: core.LiveRegionPolite, TestID: "app-count",
		ContentFunc: func() string {
			total := strconv.Itoa(len(s.Apps.Get()))
			if strings.TrimSpace(s.Query.Get()) != "" {
				return "找到 " + strconv.Itoa(len(s.Visible())) + " 个，共 " + total + " 个应用"
			}
			return "共 " + total + " 个应用"
		},
		Style: core.Style{Padding: core.Edges{Left: 18, Right: 18, Bottom: 8}, FontFamily: kit.FontSans, FontSize: kit.FontSmall, Color: kit.ColorTextMuted}})

	empty := core.NewMemo(cx, func() bool { return len(s.Visible()) == 0 })
	rows := core.Dynamic(func(cx *core.Scope) core.Node {
		if !empty.Get() {
			return core.Func(func(cx *core.Scope) core.Node { return listBox(cx, s, listRef, narrow, searchFocused.Get) })
		}
		return core.Func(func(*core.Scope) core.Node {
			return core.Text{TestID: "list-empty",
				ContentFunc: func() string {
					if strings.TrimSpace(s.Query.Get()) != "" {
						return "没有符合搜索的应用"
					}
					return "没有可以卸载的应用"
				},
				Style: core.Style{Padding: core.Edges{Top: 24, Left: 18, Right: 18, Bottom: 8}, TextAlign: core.TextAlignCenter, FontFamily: kit.FontSans,
					FontSize: kit.FontBody, Color: kit.ColorTextMuted}}
		})
	})
	return kit.Card{Clip: true, TestID: "app-list-card", Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)},
		Children: []core.Node{tools, count, rows}}
}

// listBox is the apps as a virtualized list box of 60-point rows.
func listBox(cx *core.Scope, s *State, listRef *core.Ref, narrow, searchFocused func() bool) core.Node {
	focused := core.NewSignal(cx, false)
	scroll := new(core.Ref)
	// first and last are the rows wholly shown, -1 before the list reports them.
	first, last := -1, -1
	reveal := func(i int) {
		switch {
		case first < 0:
			scroll.ScrollToIndex(i, false)
		case i < first:
			scroll.ScrollToIndexAt(i, 0, 0, false)
		case i > last:
			scroll.ScrollToIndexAt(i, 1, 0, false)
		}
	}
	index := func() int {
		id := s.Selected.GetUntracked()
		for i, a := range s.visible.GetUntracked() {
			if a.ID == id {
				return i
			}
		}
		return -1
	}
	move := func(to func(i, n int) int) func() {
		return func() {
			apps := s.visible.GetUntracked()
			if len(apps) == 0 || s.Step.GetUntracked() != StepIdle {
				return
			}
			i := min(max(to(index(), len(apps)), 0), len(apps)-1)
			s.Select(apps[i].ID)
			reveal(i)
		}
	}
	down := move(func(i, _ int) int { return max(i+1, 0) })
	up := move(func(i, _ int) int { return max(i-1, 0) })
	repeat := core.KeyOptions{Repeat: true}
	keys := core.Show{When: focused.Get, Then: func(cx *core.Scope) core.Node {
		cx.OnKeyWith("down", repeat, down)
		cx.OnKeyWith("up", repeat, up)
		cx.OnKey("home", move(func(int, int) int { return 0 }))
		cx.OnKey("end", move(func(_, n int) int { return n - 1 }))
		cx.OnKeyWith("pagedown", repeat, move(func(i, _ int) int { return i + pageKeys }))
		cx.OnKeyWith("pageup", repeat, move(func(i, _ int) int { return i - pageKeys }))
		return core.Group{}
	}}
	searchKeys := core.Show{When: func() bool { return s.Windows() && searchFocused() }, Then: func(cx *core.Scope) core.Node {
		always := core.KeyOptions{When: core.KeyAlways, Repeat: true}
		cx.OnKeyWith("down", always, down)
		cx.OnKeyWith("up", always, up)
		return core.Group{}
	}}
	list := core.List{
		Count: func() int { return len(s.Visible()) },
		Key: func(i int) any {
			if apps := s.visible.GetUntracked(); i < len(apps) {
				return apps[i].ID
			}
			return i
		},
		EstimatedSize:     rowHeight,
		Ref:               scroll,
		Style:             core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Padding: core.All(6)},
		ViewabilityConfig: core.ViewabilityConfig{ItemVisiblePercentThreshold: 100},
		OnViewableItemsChange: func(v core.ViewableItems) {
			first, last = -1, -1
			for _, tok := range v.Viewable {
				if first < 0 || tok.Index < first {
					first = tok.Index
				}
				last = max(last, tok.Index)
			}
		},
		Row: func(cx *core.Scope, row core.ListRow) core.Node {
			return appRow(cx, s, row, narrow, func() { listRef.Focus() })
		},
	}
	return core.View{Role: core.RoleListbox, Label: "应用", TestID: "app-list", Ref: listRef,
		Focusable: func() bool { return true },
		OnFocus:   func() { focused.Set(true) },
		OnBlur:    func() { focused.Set(false) },
		Style:     core.Style{Group: listGroup, FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Borders: core.Edges{Top: 1}, BorderColor: kit.BorderSubtle},
		Children:  []core.Node{list, keys, searchKeys}}
}

// appRow is one app of the list: its icon, name, version and last use, its badges and its size. A press picks it and gives the list box the keyboard's focus, so the arrow keys go on from it.
func appRow(cx *core.Scope, s *State, row core.ListRow, narrow func() bool, focusList func()) core.Node {
	item := func() app.UninstallApp {
		apps := s.Visible()
		if i := row.Index(); i >= 0 && i < len(apps) {
			return apps[i]
		}
		return app.UninstallApp{}
	}
	details := func() AppDetails { return s.DetailsOf(item().ID) }
	selected := func() bool { a := item(); return a.ID != "" && a.ID == s.Selected.Get() }
	sub := func() string {
		a := item()
		var parts []string
		if a.Version != "" {
			parts = append(parts, a.Version)
		}
		if u := UsageText(a, details(), s.Windows(), time.Now()); u != "" {
			parts = append(parts, u)
		}
		return strings.Join(parts, " · ")
	}
	return core.View{Role: core.RoleOption, NoFocus: true, Selected: selected, LabelFunc: func() string { return item().Name },
		OnPress: func(core.PressEvent) {
			if id := item().ID; id != "" {
				s.Select(id)
				focusList()
			}
		},
		StyleFunc: func() core.Style {
			st := core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Height: core.Pt(rowHeight), Padding: core.Symmetric(0, 12),
				BorderWidth: 1, BorderColor: core.Transparent, CornerRadius: 14, Cursor: core.CursorPointer,
				Hover: &core.Style{Background: kit.SurfaceRowHover}}
			if selected() {
				st.Background, st.BorderColor, st.Hover = rowSelected, rowSelectedBorder, nil
				st.GroupStyle = &core.GroupStyle{Name: listGroup, FocusVisible: focusRing(-2)}
			}
			// The selection fades from row to row.
			return kit.Pressable(st, 0.99)
		},
		Children: []core.Node{
			appIcon(cx, func() string { return details().Icon }, 36),
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2}, Children: []core.Node{
				core.Text{ContentFunc: func() string { return item().Name }, MaxLines: 1,
					Style: core.Style{FontFamily: kit.FontFace(kit.Semibold), FontSize: kit.FontBody, FontWeight: kit.FaceWeight(kit.Semibold), Color: kit.ColorText}},
				core.Show{When: func() bool { return sub() != "" }, Then: func(*core.Scope) core.Node {
					return core.Text{ContentFunc: sub, MaxLines: 1, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, Color: kit.ColorTextMuted}}
				}},
			}},
			core.Show{When: func() bool { return item().AppStore && !narrow() }, Then: func(*core.Scope) core.Node { return badge("App Store", false) }},
			core.Show{When: func() bool { return item().Running }, Then: func(*core.Scope) core.Node { return badge("运行中", true) }},
			kit.Tabular(core.Text{ContentFunc: func() string { return SizeText(details(), s.Measuring.Get(), s.Windows()) },
				Style: core.Style{FlexShrink: 0, MinWidth: core.Pt(64), TextAlign: core.TextAlignEnd, FontFamily: kit.FontSans, FontSize: kit.FontBody,
					Color: kit.ColorText}}),
		}}
}

// badge is a small pill beside an app's name: App Store, or 运行中 in the warning color.
func badge(text string, running bool) core.Node {
	bg, color := badgeBackground, kit.ColorTextSecondary
	if running {
		bg, color = badgeRunning, kit.ColorWarning
	}
	return core.View{Style: core.Style{FlexShrink: 0, Padding: core.Symmetric(2, 8), CornerRadius: kit.RadiusPill, Background: bg},
		Children: []core.Node{core.Text{Content: text, Style: core.Style{FontFamily: kit.FontSans, FontSize: 11, Color: color, WhiteSpace: core.WhiteSpaceNoWrap}}}}
}

// appIcon is an app's icon size points square: its own (a data: URL from Measure) or the generic blue tile with its shadow. Decorative: the name is next to it.
func appIcon(cx *core.Scope, icon func() string, size core.Points) core.Node {
	has := core.NewMemo(cx, func() bool { return icon() != "" })
	return core.Dynamic(func(cx *core.Scope) core.Node {
		if has.Get() {
			return core.Func(func(*core.Scope) core.Node {
				return core.Image{SourceFunc: func() core.ImageSource { return core.Src(icon()) }, ResizeMode: core.Contain, ElementsHidden: true,
					Style: core.Style{Width: core.Pt(size), Height: core.Pt(size), FlexShrink: 0}}
			})
		}
		return core.Func(func(*core.Scope) core.Node {
			radius := core.Points(math.Round(float64(size) * 0.22))
			return core.View{Style: core.Style{FlexShrink: 0, CornerRadius: radius, Shadows: []core.BoxShadow{{Offset: core.Point{Y: 4}, Blur: 10, Color: iconShadow}}},
				Children: []core.Node{kit.IconTile{Tone: kit.ToneBlue, Size: size, GlyphSize: core.Points(math.Round(float64(size) * 0.55)), Glyph: "app", Radius: radius}}}
		})
	})
}
