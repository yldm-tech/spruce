package uninstall

import (
	"math"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

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

// rowHeight is the height of one app row, which the list's estimate and its reveal rely on.
const rowHeight = 60

// listPad is the padding around the list's rows.
const listPad = 6

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
	unused := "最久未用"
	if windows {
		unused = "安装时间"
	}
	return []struct {
		key   SortKey
		label string
		id    string
	}{{SortName, "名称", "sort-name"}, {SortSize, "大小", "sort-size"}, {SortUnused, unused, "sort-unused"}}
}

// appList is the left column: the search field, the order, the count and the apps as a single-selection list box. In the search field ↑ and ↓ move the selection, Return picks the first match and Escape clears the search.
func appList(cx *core.Scope, s *State, searchRef *core.Ref, narrow func() bool) core.Node {
	searchFocused := core.NewSignal(cx, false)
	search := core.View{
		OnFocusIn:  func() { searchFocused.Set(true) },
		OnFocusOut: func() { searchFocused.Set(false) },
		Style:      core.Style{Direction: core.Row, FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(140), MinWidth: core.Pt(0)},
		Children: []core.Node{
			kit.Field{Placeholder: "搜索应用", Label: "搜索应用", TestID: "search", Value: s.Query.Get, Ref: searchRef,
				OnChange: func(q string) { s.Query.Set(q) },
				OnSubmit: func(string) { s.PickFirst() }},
			core.Show{When: searchFocused.Get, Then: func(cx *core.Scope) core.Node {
				always := core.KeyOptions{When: core.KeyAlways, Repeat: true}
				cx.OnKeyWith("down", always, func() { s.Move(1) })
				cx.OnKeyWith("up", always, func() { s.Move(-1) })
				return core.Show{When: func() bool { return s.Query.Get() != "" }, Then: func(cx *core.Scope) core.Node {
					cx.OnKeyWith("escape", core.KeyOptions{When: core.KeyAlways}, func() { s.Query.Set("") })
					return nil
				}}
			}},
		},
	}
	options := sortOptions(s.Windows())
	buttons := make([]core.Node, len(options))
	for i, o := range options {
		buttons[i] = core.View{Role: core.RoleButton, Label: o.label, TestID: o.id,
			Pressed: func() bool { return s.Sort.Get() == o.key },
			OnPress: func(core.PressEvent) { s.SetSort(o.key) },
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
			return core.Func(func(cx *core.Scope) core.Node { return listBox(cx, s, narrow) })
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

// listBox is the apps as a virtualized list box of 60-point rows (base.List over Picked): a press or the keyboard picks an app, ↑ ↓ Home End and the page keys go on from the row pressed, typing a name goes to it, and Return or a double click asks to uninstall it. It lands where it was scrolled to when the page last went, and brings the picked row into view when the State asks (Reveals).
func listBox(cx *core.Scope, s *State, narrow func() bool) core.Node {
	scroll := new(core.Ref)
	reveal := func(i int) {
		if i < 0 {
			return
		}
		pos, ok := scroll.ListPosition()
		frame, framed := scroll.Bounds()
		if !ok || !framed {
			scroll.ScrollToIndexAt(i, 0.5, 0, false)
			return
		}
		top := core.Points(pos.Index)*rowHeight + pos.Offset
		bottom := top + frame.Size.Height - 2*listPad
		switch {
		case core.Points(i)*rowHeight < top:
			scroll.ScrollToIndexAt(i, 0, 0, false)
		case core.Points(i+1)*rowHeight > bottom:
			scroll.ScrollToIndexAt(i, 1, 0, false)
		}
	}
	asked := core.Untracked(cx, s.Reveals)
	cx.Effect(func() {
		if n := s.Reveals(); n != asked {
			asked = n
			cx.Untrack(func() { reveal(s.Index()) })
		}
	})
	landed := false
	land := func(core.Bounds) {
		if landed {
			return
		}
		landed = true
		if p, ok := s.ListPos(); ok {
			scroll.ScrollToIndexAt(p.Index, 0, -p.Offset, false)
			return
		}
		reveal(s.Index())
	}
	cx.Cleanup(func() {
		if p, ok := scroll.ListPosition(); ok {
			s.SaveListPos(p)
		}
	})
	id := func(i int) string {
		if apps := s.visible.GetUntracked(); i >= 0 && i < len(apps) {
			return apps[i].ID
		}
		return ""
	}
	list := base.List{
		Count: func() int { return len(s.Visible()) },
		Key: func(i int) any {
			if id := id(i); id != "" {
				return id
			}
			return i
		},
		Selection: s.Picked,
		// A tap on a touch screen only picks the app; asking to uninstall it takes a second.
		TapSelects: true,
		OnActivate: func(i int) {
			if id := id(i); id != "" {
				s.UninstallApp(id)
			}
		},
		RowLabel: func(i int) string {
			if apps := s.Visible(); i >= 0 && i < len(apps) {
				return apps[i].Name
			}
			return ""
		},
		EstimatedSize: rowHeight,
		Label:         "应用",
		Ref:           scroll,
		TestID:        "app-list",
		Style:         core.Style{Group: listGroup, FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Borders: core.Edges{Top: 1}, BorderColor: kit.BorderSubtle},
		ListStyle:     core.Style{Padding: core.All(listPad)},
		// A row gives a little under a press.
		ItemStyle: kit.Pressable(core.Style{CornerRadius: 14}, 0.99),
		Row: func(cx *core.Scope, row core.ListRow) core.Node {
			return appRow(cx, s, row, narrow)
		},
	}
	return core.View{OnLayout: land, Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)}, Children: []core.Node{list}}
}

// appRow is the look of one app of the list: its icon, name, version and last use, its badges and its size; the list's row around it takes the presses. A secondary click opens its menu: 卸载…, 在访达中显示, 拷贝路径.
func appRow(cx *core.Scope, s *State, row core.ListRow, narrow func() bool) core.Node {
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
	menu := kit.RowMenu(cx, func() []desktop.MenuItem {
		a := item()
		if a.ID == "" {
			return nil
		}
		items := []desktop.MenuItem{{Label: "卸载…", Disabled: s.Step.GetUntracked() != StepIdle || !s.Windows() && a.Running,
			OnSelect: func() { s.UninstallApp(a.ID) }}}
		if a.Path != "" {
			items = append(items, kit.RevealItem(fileManagerName(s.Windows()), func() { s.Reveal(a.Path) }), kit.CopyPathItem(cx, a.Path))
		}
		return items
	})
	return core.View{OnContextMenu: menu,
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
				// A name too long for the row shows whole under the pointer.
				core.View{Tooltip: core.Dynamic(func(*core.Scope) core.Node {
					name := item().Name
					return core.Func(func(*core.Scope) core.Node { return kit.Tooltip(name) })
				}), Style: core.Style{MinWidth: core.Pt(0)}, Children: []core.Node{
					core.Text{ContentFunc: func() string { return item().Name }, MaxLines: 1,
						Style: core.Style{FontFamily: kit.FontFace(kit.Semibold), FontSize: kit.FontBody, FontWeight: kit.FaceWeight(kit.Semibold), Color: kit.ColorText}},
				}},
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
