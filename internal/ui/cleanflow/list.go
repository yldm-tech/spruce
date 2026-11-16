package cleanflow

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/async"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
)

// timeNow is the clock the items' ages count from.
var timeNow = time.Now

// scrollItemsAfter is how many items a category lists in place; a longer list scrolls inside its card, scrollItemsHeight tall, and mounts only the rows near view (core.List).
const (
	scrollItemsAfter              = 60
	scrollItemsHeight core.Points = 360
	itemRowHeight     core.Points = 48
)

// CategoryList is the result list of the cleaner pages: a toolbar with the totals, 全选 and 全不选 (acting on these categories only), and one glass card per category with its tri-state checkbox, tile, size and warning, expanding to its items, each with a checkbox, its folder, age and size and a button that shows it in Finder or Explorer. Selection is controlled: OnChange receives the next one (see selection.go). Reveal shows a path in the file manager; its error shows in the toolbar.
type CategoryList struct {
	Categories func() []cleanup.Category
	Selection  func() Selection
	OnChange   func(Selection)
	// Label names the list for assistive technology, e.g. 可以清理的系统垃圾.
	Label  string
	Reveal func(ctx context.Context, path string) error
	// Disabled disables every checkbox and 全选/全不选, e.g. while a clean runs.
	Disabled func() bool
	// Expand is the category that starts expanded (a drill-down view opened on it, nav.Navigator.GoDrill), "" for none; Scroll, when set, receives its card once it is shown, for the page to scroll to it.
	Expand string
	Scroll func(card *core.Ref)
	TestID string
}

// Render implements core.Node.
func (l CategoryList) Render(cx *core.Scope) core.Node {
	ctx := cx.Context()
	revealErr := core.NewSignal(cx, "")
	seq := 0
	reveal := func(path string) {
		if l.Reveal == nil {
			return
		}
		seq++
		mine := seq
		revealErr.Set("")
		async.Do(cx, ctx, func(ctx context.Context) (struct{}, error) { return struct{}{}, l.Reveal(ctx, path) }, func(_ struct{}, err error) {
			if mine != seq || err == nil {
				return
			}
			revealErr.Set(err.Error())
		})
	}
	disabled := func() bool { return l.Disabled != nil && l.Disabled() }
	summary := func() string {
		cats := l.Categories()
		var total int64
		for _, c := range cats {
			total += c.Size
		}
		return strconv.Itoa(len(cats)) + " 类，共 " + format.Bytes(total) + "，已选 " + strconv.Itoa(SelectedTotals(cats, l.Selection()).Count) + " 项"
	}
	toolbar := core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, RowGap: 8, ColumnGap: 12}, Children: []core.Node{
		core.View{Style: core.Style{AutoMargin: core.SideRight}, Children: []core.Node{
			kit.Tabular(kit.TextFunc(summary, kit.FontBody, kit.Regular, kit.ColorTextSecondary))}},
		kit.Button{Text: "全选", Variant: kit.ButtonLink, Disabled: disabled, TestID: "select-all",
			OnPress: func() { l.OnChange(SelectAll(core.Untracked(cx, l.Categories))) }},
		kit.Button{Text: "全不选", Variant: kit.ButtonLink, Disabled: disabled, TestID: "select-none",
			OnPress: func() { l.OnChange(NothingSelected) }},
		core.View{Style: core.Style{FlexBasis: core.Pct(100)}, Children: []core.Node{kit.ErrorLine{Text: revealErr.Get, TestID: "reveal-error"}}},
	}}
	cards := core.Each(l.Categories, func(c cleanup.Category) string { return c.ID }, func(cx *core.Scope, c func() cleanup.Category, index func() int) core.Node {
		return categoryCard(cx, c, l, disabled, reveal, core.Untracked(cx, index))
	})
	// A newer result (after a clean, a check) moves the cards into place: categories that went leave a gap that closes.
	first := true
	cx.Effect(func() {
		l.Categories()
		if first {
			first = false
			return
		}
		cx.Untrack(func() { kit.SettleLayout(cx) })
	})
	return core.View{Role: core.RoleRegion, Label: l.Label, TestID: l.TestID, Style: core.Style{Gap: 12}, Children: []core.Node{toolbar, cards}}
}

// categoryCard is one category: its head and, once expanded, its items.
func categoryCard(cx *core.Scope, c func() cleanup.Category, l CategoryList, disabled func() bool, reveal func(string), index int) core.Node {
	id := core.Untracked(cx, c).ID
	expand := id == l.Expand && len(core.Untracked(cx, c).Items) > 0
	open := core.NewSignal(cx, expand)
	ref := new(core.Ref)
	if expand && l.Scroll != nil {
		cx.Effect(func() { cx.Untrack(func() { l.Scroll(ref) }) })
	}
	state := func() CheckState { return CategoryState(c(), l.Selection()) }
	empty := func() bool { return len(c().Items) == 0 }
	// A category whose items are all held (Item.Hold) has nothing to check.
	none := func() bool { return Selectable(c()) == 0 }
	title := func() string { return c().Title }
	line := func() string {
		if w := c().Warning; w != "" {
			return w
		}
		return c().Description
	}
	count := func() string {
		cat := c()
		if state() == CheckSome {
			return "已选 " + strconv.Itoa(l.Selection().Count(cat.ID)) + "/" + strconv.Itoa(len(cat.Items)) + " 项"
		}
		return strconv.Itoa(len(cat.Items)) + " 项"
	}
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14, Padding: core.Symmetric(16, 18)}, Children: []core.Node{
		kit.Checkbox{Label: core.Untracked(cx, title), TestID: "check-" + id,
			Checked:       func() bool { return state() == CheckAll },
			Indeterminate: func() bool { return state() == CheckSome },
			Disabled:      func() bool { return disabled() || none() },
			OnChange:      func(on bool) { l.OnChange(SetCategory(core.Untracked(cx, l.Selection), core.Untracked(cx, c), on)) }},
		core.Dynamic(func(*core.Scope) core.Node {
			icon := c().Icon
			return core.Func(func(*core.Scope) core.Node { return kit.CategoryTile(icon) })
		}),
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2}, Children: []core.Node{
			kit.TextFunc(title, kit.FontCardTitle, kit.Semibold, kit.ColorText),
			core.Text{ContentFunc: line, StyleFunc: func() core.Style {
				color := kit.ColorTextMuted
				if c().Warning != "" {
					color = kit.ColorWarning
				}
				return core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: color}
			}},
		}},
		core.View{Style: core.Style{AlignItems: core.AlignEnd, Gap: 2, FlexShrink: 0}, Children: []core.Node{
			kit.Tabular(kit.TextFunc(func() string { return format.Bytes(c().Size) }, kit.FontCardTitle, kit.Bold, kit.ColorText)),
			kit.Tabular(kit.TextFunc(count, kit.FontSmall, kit.Regular, kit.ColorTextMuted)),
		}},
		expandButton(id, title, open, empty),
	}}
	items := core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node {
		return kit.Appear{Entrance: kit.EnterFade, Children: []core.Node{itemList(cx, c, l, disabled, reveal)}}
	}}
	// The cards come in one after another as the view shows them.
	return core.View{Ref: ref, Style: core.Style{FlexShrink: 0, Animated: kit.Enter(cx, kit.EnterRise, kit.StaggerDelay(index))}, Children: []core.Node{
		kit.Card{Clip: true, TestID: "category-" + id, Children: []core.Node{head, items}},
	}}
}

// expandButton opens and closes a category's items (.icon-button.category-expand).
func expandButton(id string, title func() string, open *core.Signal[bool], empty func() bool) core.Node {
	return core.View{Role: core.RoleButton, TestID: "expand-" + id, Disabled: empty, Expandable: func() bool { return true }, Expanded: open.Get,
		LabelFunc: func() string {
			if open.Get() {
				return "收起" + title()
			}
			return "展开" + title()
		},
		Style: core.Style{Width: core.Pt(30), Height: core.Pt(30), FlexShrink: 0, CornerRadius: 15, AlignItems: core.AlignCenter,
			JustifyContent: core.JustifyCenter, Color: kit.ColorTextSecondary, Cursor: core.CursorPointer,
			FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText},
			Hover:        &core.Style{Background: kit.SurfaceButtonGhost, Color: kit.ColorText},
			Disabled:     &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault}},
		OnPress: func(core.PressEvent) {
			if !empty() {
				open.Set(!open.GetUntracked())
			}
		},
		Children: []core.Node{kit.Chevron{Open: open.Get}},
	}
}

// itemList is a category's items under its head: in place up to scrollItemsAfter, otherwise in a list that scrolls inside the card and mounts only the rows near view.
func itemList(cx *core.Scope, c func() cleanup.Category, l CategoryList, disabled func() bool, reveal func(string)) core.Node {
	cat := core.Untracked(cx, c)
	st := core.Style{Padding: core.Symmetric(4, 0), Background: kit.SurfaceList, Borders: core.Edges{Top: 1}, BorderColor: kit.BorderSubtle}
	row := func(it cleanup.Item) core.Node {
		return ItemRow{Item: it, Disabled: disabled, Reveal: reveal,
			Checked: func() bool { return l.Selection().Has(cat.ID, it.ID) },
			OnChange: func(on bool) {
				for _, c := range l.Categories() {
					if c.ID == cat.ID {
						l.OnChange(SetItem(l.Selection(), c, it.ID, on))
						return
					}
				}
			}}
	}
	if len(cat.Items) <= scrollItemsAfter {
		children := make([]core.Node, len(cat.Items))
		for i, it := range cat.Items {
			children[i] = row(it)
		}
		return core.View{Role: core.RoleGroup, Label: cat.Title, TestID: "items-" + cat.ID, Style: st, Children: children}
	}
	st.Height = core.Pt(scrollItemsHeight)
	return core.List{TestID: "items-" + cat.ID, Style: st, EstimatedSize: itemRowHeight,
		Count: func() int { return len(cat.Items) },
		Key:   func(i int) any { return cat.Items[i].ID },
		Row: func(cx *core.Scope, r core.ListRow) core.Node {
			return core.Dynamic(func(*core.Scope) core.Node {
				it := cat.Items[r.Index()]
				return core.Func(func(*core.Scope) core.Node { return row(it) })
			})
		}}
}

// ItemRow is one item of a list: its checkbox, name, folder (and note), age, size and 在访达中显示. Checked and OnChange are its checkbox; Disabled disables it (nil for never), and a held item (Item.Hold) is always disabled, with the reason as the checkbox's tooltip; Reveal shows the item in Finder or Explorer (nil shows nothing). 概览's cards and the drill-down views list items with it.
type ItemRow struct {
	Item     cleanup.Item
	Checked  func() bool
	OnChange func(on bool)
	Disabled func() bool
	Reveal   func(path string)
}

// Render implements core.Node.
func (r ItemRow) Render(*core.Scope) core.Node {
	it := r.Item
	name := format.BaseName(it.Path)
	where := []string{}
	if p := format.TruncateMiddle(format.ParentPath(it.Path), 72); p != "" {
		where = append(where, p)
	}
	if it.Note != "" {
		where = append(where, it.Note)
	}
	ellipsis := func(t core.Text) core.Text {
		t.MaxLines, t.EllipsizeMode = 1, core.EllipsizeTail
		return t
	}
	var check core.Node = kit.Checkbox{Label: name, TestID: "item-" + it.ID, Disabled: r.Disabled, Checked: r.Checked, OnChange: r.OnChange}
	if it.Hold != "" {
		check = core.View{Tooltip: kit.Tooltip(it.Hold), Style: core.Style{FlexShrink: 0}, Children: []core.Node{
			kit.Checkbox{Label: name, TestID: "item-" + it.ID, Disabled: func() bool { return true }, Checked: r.Checked, OnChange: r.OnChange},
		}}
	}
	children := []core.Node{
		check,
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
			ellipsis(kit.Text(name, kit.FontBody, kit.Regular, kit.ColorText)),
			ellipsis(kit.Text(strings.Join(where, " · "), kit.FontSmall, kit.Regular, kit.ColorTextMuted)),
		}},
	}
	if age := format.RelativeTime(it.ModTime, timeNow()); age != "" {
		children = append(children, kit.Tabular(kit.Text(age, kit.FontSmall, kit.Regular, kit.ColorTextSecondary)))
	}
	size := kit.Tabular(kit.Text(format.Bytes(it.Size), kit.FontBody, kit.Regular, kit.ColorText))
	size.Style.MinWidth, size.Style.TextAlign = core.Pt(64), core.TextAlignRight
	children = append(children, size,
		kit.IconButton{Glyph: "reveal", Label: "在" + format.FileManagerName() + "中显示 " + name, TestID: "reveal-" + it.ID,
			OnPress: func() {
				if r.Reveal != nil {
					r.Reveal(it.Path)
				}
			}})
	return core.View{TestID: "row-" + it.ID, Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Height: core.Pt(itemRowHeight),
		Padding: core.Symmetric(0, 18), Hover: &core.Style{Background: kit.SurfaceRowHover}}, Children: children}
}
