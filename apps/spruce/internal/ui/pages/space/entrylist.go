package space

import (
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
)

// Row colors, after tokens.css.
var (
	rowIconBackground = core.RGBA(0xffffff, 0.08)
	rowIconDir        = core.RGB(0x5cf0e6)
	// rowSelected is a chosen row: the page's cyan, faint.
	rowSelected = core.RGBA(0x5cf0e6, 0.12)
)

// rowHeight is a row's height, for the list's estimate.
const rowHeight = 52

// Detail is what a row says under an entry's name.
type Detail uint8

// The details: each entry's share of the folder with a bar (a folder's children), or the folder holding it and its age (large files).
const (
	DetailShare Detail = iota
	DetailLocation
)

// EntryActions are what a row does; the page decides how (opening a folder, confirming a move). OnTrash receives the row's 移到废纸篓 button, to take the focus back to, nil from the row's menu. OnOpenParent opens the folder holding a large file on the 空间分布 tab. Menu gives a row's context menu, made as it opens.
type EntryActions struct {
	OnOpen       func(app.SpaceEntry)
	OnReveal     func(app.SpaceEntry)
	OnTrash      func(e app.SpaceEntry, opener *core.Ref)
	OnOpenParent func(app.SpaceEntry)
	Menu         func(cx *core.Scope, e app.SpaceEntry) []desktop.MenuItem
}

// EntryList lists entries largest first, each with what it holds and its actions: 打开 for folders, 在访达中显示 (资源管理器) and 移到废纸篓 (回收站). An entry the page may not move keeps its button, which then says why. It is one keyboard list (base.List) that builds only the rows near the view: the arrows, Home, End and typeahead move through Selection, Return opens a folder, and the rows' buttons take no Tab stop of their own. With Checks (大文件) Selection holds any number of rows and every row that can be moved shows a checkbox for it.
type EntryList struct {
	Label   func() string
	Entries func() []app.SpaceEntry
	Detail  Detail
	// Total is the folder's size, for the shares.
	Total     func() int64
	Actions   EntryActions
	Selection *base.Selection
	Checks    bool
	// Busy draws the rows' buttons as unavailable while a move to the Trash runs.
	Busy func() bool
	// Ref is the list's, for its position and ScrollToIndex.
	Ref    *core.Ref
	TestID string
}

// Render implements core.Node.
func (l EntryList) Render(*core.Scope) core.Node {
	at := func(i int) app.SpaceEntry {
		if es := l.Entries(); i >= 0 && i < len(es) {
			return es[i]
		}
		return app.SpaceEntry{}
	}
	return core.View{Role: core.RoleGroup, LabelFunc: l.Label, TestID: l.TestID, Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)},
		Children: []core.Node{base.List{
			Count:     func() int { return len(l.Entries()) },
			Key:       func(i int) any { return entryKey(at(i)) },
			Selection: l.Selection,
			RowLabel:  func(i int) string { return EntryLabel(at(i)) },
			OnActivate: func(i int) {
				if e := at(i); e.Kind == app.SpaceDir {
					l.Actions.OnOpen(e)
				}
			},
			Row: func(cx *core.Scope, row core.ListRow) core.Node {
				return entryRow(cx, func() app.SpaceEntry { return at(row.Index()) }, l)
			},
			EstimatedSize: rowHeight, Ref: l.Ref,
			Style:     core.Style{FlexGrow: 1, FlexShrink: 1},
			ListStyle: core.Style{Padding: core.Symmetric(4, 0)},
			ItemStyle: core.Style{Cursor: core.CursorDefault, Hover: &core.Style{Background: kit.SurfaceRowHover}, Selected: &core.Style{Background: rowSelected}},
		}}}
}

// entryRow is the row of entry: its glyph (or checkbox), its name (a link for a folder), the detail line, the share bar, the size and the actions. A list cell shows one row after another as it scrolls, so the row follows entry, its kind included.
func entryRow(cx *core.Scope, entry func() app.SpaceEntry, l EntryList) core.Node {
	kind := core.NewMemo(cx, func() app.SpaceKind { return entry().Kind })
	return core.Dynamic(func(*core.Scope) core.Node {
		k := kind.Get()
		return core.Func(func(cx *core.Scope) core.Node { return entryRowOf(cx, k, entry, l) })
	})
}

// entryRowOf is entryRow for an entry of kind.
func entryRowOf(cx *core.Scope, kind app.SpaceKind, entry func() app.SpaceEntry, l EntryList) core.Node {
	name := func() string { return EntryLabel(entry()) }
	share := func() float64 {
		if l.Total == nil {
			return 0
		}
		return ShareOf(entry().Size, l.Total())
	}
	sub := func() string {
		e := entry()
		switch {
		case l.Detail == DetailLocation:
			return locationLine(e)
		case e.Kind == app.SpaceDir && e.Denied:
			return "无权读取"
		case e.Kind == app.SpaceDir:
			return FormatShare(share()) + " · " + FileCount(e.Files)
		}
		return FormatShare(share())
	}
	group := kind == app.SpaceOther || kind == app.SpaceFiles

	glyph, iconColor := "group", kit.ColorTextSecondary
	switch kind {
	case app.SpaceDir:
		glyph, iconColor = "folder", rowIconDir
	case app.SpaceFile:
		glyph = "file"
	}
	icon := core.View{AccessibilityElementsHidden: func() bool { return true },
		Style: core.Style{Width: core.Pt(28), Height: core.Pt(28), FlexShrink: 0, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter,
			CornerRadius: kit.RadiusNavIcon, Background: rowIconBackground, Color: iconColor},
		Children: []core.Node{kit.Glyph{Name: glyph, Size: 16, Color: core.CurrentColor}}}

	textStyle := core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorText}
	var title core.Node = core.Text{ContentFunc: name, MaxLines: 1, EllipsizeMode: core.EllipsizeTail, Style: textStyle}
	if kind == app.SpaceDir {
		title = core.View{Role: core.RoleButton, LabelFunc: name, NoFocus: true, OnPress: func(core.PressEvent) { l.Actions.OnOpen(entry()) },
			Style: core.Style{AlignSelf: core.AlignStart, MaxWidth: core.Pct(100), Cursor: core.CursorPointer, Color: kit.ColorText,
				Hover: &core.Style{Color: kit.ColorTextLinkHover}},
			Children: []core.Node{core.Text{ContentFunc: name, MaxLines: 1, EllipsizeMode: core.EllipsizeTail,
				Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody}}}}
	}
	detail := core.Node(core.Text{ContentFunc: sub, MaxLines: 1, EllipsizeMode: core.EllipsizeTail,
		Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, FontVariant: core.FontTabularNums, Color: kit.ColorTextMuted}})
	if l.Detail == DetailLocation && l.Actions.OnOpenParent != nil {
		// A large file's folder opens on the 空间分布 tab.
		detail = core.View{Role: core.RoleLink, LabelFunc: func() string { return "在空间分布中打开 " + format.ParentPath(entry().Path) }, NoFocus: true,
			OnPress: func(core.PressEvent) { l.Actions.OnOpenParent(entry()) },
			Style: core.Style{AlignSelf: core.AlignStart, MaxWidth: core.Pct(100), Cursor: core.CursorPointer, Color: kit.ColorTextMuted,
				Hover: &core.Style{Color: kit.ColorTextLinkHover}},
			Children: []core.Node{core.Text{ContentFunc: sub, MaxLines: 1, EllipsizeMode: core.EllipsizeTail,
				Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, FontVariant: core.FontTabularNums}}}}
	}
	text := []core.Node{title, detail}
	if l.Detail == DetailShare {
		text = append(text, core.View{AccessibilityElementsHidden: func() bool { return true },
			Style: core.Style{Height: core.Pt(4), CornerRadius: kit.RadiusPill, Background: kit.ProgressTrack, Overflow: core.Hidden, Direction: core.Row},
			Children: []core.Node{core.View{StyleFunc: func() core.Style {
				return core.Style{Width: core.Pct(float32(share() * 100)), Height: core.Pct(100), CornerRadius: kit.RadiusPill,
					Gradients: []core.Gradient{kit.ToneCyan.Gradient()}}
			}}}})
	}

	// The two buttons stand apart, so aiming at one does not hit the other.
	actions := core.View{Style: core.Style{Width: core.Pt(70), FlexShrink: 0, Direction: core.Row, JustifyContent: core.JustifyEnd, Gap: 10}}
	if !group {
		trashRef := new(core.Ref)
		busy := func() bool { return l.Busy != nil && l.Busy() }
		actions.Children = []core.Node{
			rowButton{Glyph: "reveal", Ref: new(core.Ref),
				Label:   func() string { return "在" + format.FileManagerName() + "中显示 " + name() },
				Tip:     func() string { return "在" + format.FileManagerName() + "中显示" },
				OnPress: func() { l.Actions.OnReveal(entry()) }},
			rowButton{Glyph: "trash", Ref: trashRef,
				Label: func() string { return "将 " + name() + " 移到" + format.TrashName() },
				Tip: func() string {
					if r := entry().Locked; r != "" {
						return r
					}
					return "移到" + format.TrashName()
				},
				Dimmed:  func() bool { return entry().Locked != "" || busy() },
				OnPress: func() { l.Actions.OnTrash(entry(), trashRef) }},
		}
	}

	lead := core.Node(icon)
	if l.Checks && !group {
		lead = core.Show{When: func() bool { return entry().Locked == "" }, Then: func(*core.Scope) core.Node { return rowCheck(entry, name, l.Selection) },
			Else: func(*core.Scope) core.Node { return icon }}
	}

	var menu func(core.ContextMenuEvent)
	if l.Actions.Menu != nil && !group {
		menu = kit.RowMenu(cx, func() []desktop.MenuItem { return l.Actions.Menu(cx, entry()) })
	}
	return core.View{OnContextMenu: menu, Tooltip: core.Dynamic(func(*core.Scope) core.Node {
		if p := entry().Path; p != "" {
			return kit.Tooltip(p)
		}
		return nil
	}),
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, MinHeight: core.Pt(rowHeight), Padding: core.Edges{Top: 6, Right: 10, Bottom: 6, Left: 14}},
		Children: []core.Node{
			lead,
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 3}, Children: text},
			core.Text{ContentFunc: func() string { return format.Bytes(entry().Size) },
				Style: core.Style{FlexShrink: 0, MinWidth: core.Pt(64), TextAlign: core.TextAlignRight, FontFamily: kit.FontSans, FontSize: kit.FontBody,
					FontVariant: core.FontTabularNums, Color: kit.ColorText}},
			actions,
		}}
}

// locationLine is a large file's detail: the folder holding it and its age.
func locationLine(e app.SpaceEntry) string {
	var parts []string
	if p := format.TruncateMiddle(format.ParentPath(e.Path), 64); p != "" {
		parts = append(parts, p)
	}
	if t := format.RelativeTime(e.ModTime, time.Now()); t != "" {
		parts = append(parts, t)
	}
	return strings.Join(parts, " · ")
}

// rowCheck is a 28-point checkbox in a row's glyph place, chosen with the row in sel: pressing it adds the row to the rows chosen or takes it out, where pressing the row chooses it alone. The list holds the keyboard, so it takes no Tab stop.
func rowCheck(entry func() app.SpaceEntry, name func() string, sel *base.Selection) core.Node {
	checked := func() bool { return sel.Has(entryKey(entry())) }
	return core.View{Role: core.RoleCheckbox, LabelFunc: func() string { return "选择 " + name() }, NoFocus: true, Checked: checked,
		OnPress: func(core.PressEvent) { sel.Toggle(entryKey(entry())) },
		Style:   core.Style{Width: core.Pt(28), Height: core.Pt(28), FlexShrink: 0, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Cursor: core.CursorPointer},
		Children: []core.Node{core.View{
			StyleFunc: func() core.Style {
				st := core.Style{Width: core.Pt(20), Height: core.Pt(20), CornerRadius: kit.RadiusCheckbox, BorderWidth: 1.5, BorderColor: kit.CheckboxOff,
					AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter}
				if checked() {
					st.Background, st.BorderColor = kit.CheckboxOn, kit.CheckboxOn
				}
				return st
			},
			Children: []core.Node{core.Show{When: checked, Then: func(*core.Scope) core.Node {
				return kit.Glyph{Name: "check", Size: 12, StrokeWidth: 3.4, Color: kit.CheckboxMark}
			}}}}}}
}

// rowButton is a row's 30-point round icon button (.icon-button). Dimmed draws it as unavailable while it still answers, so pressing it can say why (aria-disabled). The list holds the keyboard, so it takes no Tab stop.
type rowButton struct {
	Glyph   string
	Label   func() string
	Tip     func() string
	Dimmed  func() bool
	OnPress func()
	Ref     *core.Ref
}

// Render implements core.Node.
func (b rowButton) Render(*core.Scope) core.Node {
	return core.View{Role: core.RoleButton, LabelFunc: b.Label, Ref: b.Ref, NoFocus: true,
		Tooltip: core.Dynamic(func(*core.Scope) core.Node { return kit.Tooltip(b.Tip()) }),
		StyleFunc: func() core.Style {
			st := core.Style{Width: core.Pt(30), Height: core.Pt(30), FlexShrink: 0, CornerRadius: 15, AlignItems: core.AlignCenter,
				JustifyContent: core.JustifyCenter, Color: kit.ColorTextSecondary, Cursor: core.CursorPointer,
				Hover: &core.Style{Background: kit.SurfaceButtonGhost, Color: kit.ColorText}}
			if b.Dimmed != nil && b.Dimmed() {
				st.Opacity = core.Alpha(0.45)
			}
			return st
		},
		OnPress:  func(core.PressEvent) { b.OnPress() },
		Children: []core.Node{kit.Glyph{Name: b.Glyph, Size: 16, Color: core.CurrentColor}}}
}
