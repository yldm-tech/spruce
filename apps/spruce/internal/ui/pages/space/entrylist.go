package space

import (
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
)

// Row colors, after tokens.css.
var (
	rowIconBackground = core.RGBA(0xffffff, 0.08)
	rowIconDir        = core.RGB(0x5cf0e6)
)

// Detail is what a row says under an entry's name.
type Detail uint8

// The details: each entry's share of the folder with a bar (a folder's children), or the folder holding it and its age (large files).
const (
	DetailShare Detail = iota
	DetailLocation
)

// EntryActions are what a row does; the page decides how (opening a folder, confirming a move). OnTrash receives the row's 移到废纸篓 button, to take the focus back to.
type EntryActions struct {
	OnOpen   func(app.SpaceEntry)
	OnReveal func(app.SpaceEntry)
	OnTrash  func(e app.SpaceEntry, opener *core.Ref)
}

// EntryList lists entries largest first, each with what it holds and its actions: 打开 for folders, 在访达中显示 (资源管理器) and 移到废纸篓 (回收站). An entry the page may not move keeps its button, which then says why.
type EntryList struct {
	Label   func() string
	Entries func() []app.SpaceEntry
	Detail  Detail
	// Total is the folder's size, for the shares.
	Total   func() int64
	Actions EntryActions
	TestID  string
}

// Render implements core.Node.
func (l EntryList) Render(*core.Scope) core.Node {
	return core.View{Role: core.RoleGroup, LabelFunc: l.Label, TestID: l.TestID, Style: core.Style{Padding: core.Symmetric(4, 0)},
		Children: []core.Node{core.Each(l.Entries, entryKey, func(_ *core.Scope, e func() app.SpaceEntry, _ func() int) core.Node {
			return entryRow(e, l)
		})}}
}

// entryRow is one entry: its glyph, its name (a link for a folder), the detail line, the share bar, the size and the actions.
func entryRow(entry func() app.SpaceEntry, l EntryList) core.Node {
	kind := entry().Kind
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
			var parts []string
			if p := format.TruncateMiddle(format.ParentPath(e.Path), 64); p != "" {
				parts = append(parts, p)
			}
			if t := format.RelativeTime(e.ModTime, time.Now()); t != "" {
				parts = append(parts, t)
			}
			return strings.Join(parts, " · ")
		case e.Kind == app.SpaceDir && e.Denied:
			return "无权读取"
		case e.Kind == app.SpaceDir:
			return FormatShare(share()) + " · " + FileCount(e.Files)
		}
		return FormatShare(share())
	}

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
		title = core.View{Role: core.RoleButton, LabelFunc: name, OnPress: func(core.PressEvent) { l.Actions.OnOpen(entry()) },
			Style: core.Style{AlignSelf: core.AlignStart, MaxWidth: core.Pct(100), Cursor: core.CursorPointer, Color: kit.ColorText,
				Hover: &core.Style{Color: kit.ColorTextLinkHover}, FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText}},
			Children: []core.Node{core.Text{ContentFunc: name, MaxLines: 1, EllipsizeMode: core.EllipsizeTail,
				Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody}}}}
	}
	text := []core.Node{
		title,
		core.Text{ContentFunc: sub, MaxLines: 1, EllipsizeMode: core.EllipsizeTail,
			Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, FontVariant: core.FontTabularNums, Color: kit.ColorTextMuted}},
	}
	if l.Detail == DetailShare {
		text = append(text, core.View{AccessibilityElementsHidden: func() bool { return true },
			Style: core.Style{Height: core.Pt(4), CornerRadius: kit.RadiusPill, Background: kit.ProgressTrack, Overflow: core.Hidden, Direction: core.Row},
			Children: []core.Node{core.View{StyleFunc: func() core.Style {
				return core.Style{Width: core.Pct(float32(share() * 100)), Height: core.Pct(100), CornerRadius: kit.RadiusPill,
					Gradients: []core.Gradient{kit.ToneCyan.Gradient()}}
			}}}})
	}

	actions := core.View{Style: core.Style{Width: core.Pt(62), FlexShrink: 0, Direction: core.Row, JustifyContent: core.JustifyEnd, Gap: 2}}
	if kind != app.SpaceOther && kind != app.SpaceFiles {
		trashRef := new(core.Ref)
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
				Dimmed:  func() bool { return entry().Locked != "" },
				OnPress: func() { l.Actions.OnTrash(entry(), trashRef) }},
		}
	}

	return core.View{Tooltip: core.Dynamic(func(*core.Scope) core.Node {
		if p := entry().Path; p != "" {
			return kit.Tooltip(p)
		}
		return nil
	}),
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, MinHeight: core.Pt(52), Padding: core.Edges{Top: 6, Right: 10, Bottom: 6, Left: 14},
			Hover: &core.Style{Background: kit.SurfaceRowHover}},
		Children: []core.Node{
			icon,
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 3}, Children: text},
			core.Text{ContentFunc: func() string { return format.Bytes(entry().Size) },
				Style: core.Style{FlexShrink: 0, MinWidth: core.Pt(64), TextAlign: core.TextAlignRight, FontFamily: kit.FontSans, FontSize: kit.FontBody,
					FontVariant: core.FontTabularNums, Color: kit.ColorText}},
			actions,
		}}
}

// rowButton is a row's 30-point round icon button (.icon-button). Dimmed draws it as unavailable while it still answers, so pressing it can say why (aria-disabled).
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
	return core.View{Role: core.RoleButton, LabelFunc: b.Label, Ref: b.Ref,
		Tooltip: core.Dynamic(func(*core.Scope) core.Node { return kit.Tooltip(b.Tip()) }),
		StyleFunc: func() core.Style {
			st := core.Style{Width: core.Pt(30), Height: core.Pt(30), FlexShrink: 0, CornerRadius: 15, AlignItems: core.AlignCenter,
				JustifyContent: core.JustifyCenter, Color: kit.ColorTextSecondary, Cursor: core.CursorPointer,
				FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText},
				Hover:        &core.Style{Background: kit.SurfaceButtonGhost, Color: kit.ColorText}}
			if b.Dimmed != nil && b.Dimmed() {
				st.Opacity = core.Alpha(0.45)
			}
			return st
		},
		OnPress:  func(core.PressEvent) { b.OnPress() },
		Children: []core.Node{kit.Glyph{Name: b.Glyph, Size: 16, Color: core.CurrentColor}}}
}
