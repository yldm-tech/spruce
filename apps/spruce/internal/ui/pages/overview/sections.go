package overview

import (
	"context"
	"errors"
	"slices"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/growth"
	"spruce/apps/spruce/internal/ui/async"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/store"
)

// growthWindow is how far back 空间变化 looks on 概览.
const growthWindow = 7 * 24 * time.Hour

// growthRows is how many locations 空间变化 lists on 概览.
const growthRows = 3

// review is 需要你判断: what starts unchecked, and the drill-down views to judge it in.
func review(cx *core.Scope, p *page) core.Node {
	r := core.NewMemoFunc(cx, func() Review { return ReviewOf(p.scan()) }, nil)
	return core.Show{When: func() bool { return r.Get().Count > 0 }, Then: func(*core.Scope) core.Node {
		links := core.Dynamic(func(*core.Scope) core.Node {
			sources := r.Get().Sources
			return core.Func(func(*core.Scope) core.Node {
				nodes := make([]core.Node, len(sources))
				for i, src := range sources {
					tab := nav.DevTab("")
					if src == string(app.CleanerDevJunk) {
						tab = nav.DevProjects
					}
					nodes[i] = kit.Link{Start: i == 0, Text: fixed("在" + nav.Route(src).Meta().Title + "中查看"), TestID: "review-link-" + src,
						OnPress: func() { p.drill(src, "", tab) }}
				}
				return row(12, nodes...)
			})
		})
		return kit.Card{Variant: kit.CardMuted, TestID: "review", Padding: core.Symmetric(16, 20), Gap: 8, Children: []core.Node{
			kit.Text("需要你判断", kit.FontCardTitle, kit.Semibold, kit.ColorText),
			bodyText(func() string { return ReviewText(r.Get()) }, kit.ColorTextMuted, "review-text"),
			links,
		}}
	}}
}

// trashCat is the result's Trash category, nil when the Trash was empty.
func trashCat(scan cleanup.ScanResult) *cleanup.Category {
	for i := range scan.Categories {
		if scan.Categories[i].ID == trashCategory {
			c := scan.Categories[i]
			return &c
		}
	}
	return nil
}

// trashRow is the 废纸篓 row: how much the Trash holds and 清空废纸篓…, which reads the Trash again and asks before emptying it; or why the Trash could not be read.
func trashRow(cx *core.Scope, p *page) core.Node {
	cat := core.NewMemo(cx, func() *cleanup.Category { return trashCat(p.scan()) })
	ctx := cx.Context()
	ask := p.asker(cx)
	reading := core.NewSignal(cx, false)
	bin := format.TrashName()
	undoShown := core.NewMemo(cx, func() bool { t := p.stores.Toasts.Current(); return t != nil && t.UndoRun != "" })
	empty := func(info app.TrashInfo, err error) {
		text := bin + "里的全部项目会永久删除，无法恢复。"
		if err == nil {
			text = bin + "里的 " + strconv.Itoa(info.Items) + " 项（" + format.Bytes(info.Bytes) + "）会永久删除，无法恢复。"
			if info.Restorable > 0 {
				text += "其中 " + strconv.Itoa(info.Restorable) + " 项是 Spruce 移到" + bin + "的，清空后不能再放回原处。"
			}
		}
		ask(kit.ConfirmDialog{TestID: "empty-trash-confirm", Title: "清空" + bin + "？", Lines: []string{text}, ConfirmLabel: "清空", Danger: true}, func() {
			p.active.Set(emptyTrash)
			undo := p.canUndo()
			stores := p.stores
			// As a card's clean: it outlives the page, its toast comes from the app's scope and the page's lock is the page's.
			p.ov.Clean(p.cx, cleanup.CleanRequest{CategoryIDs: []string{trashCategory}}, app.CleanOrigin{Source: string(app.CleanerSystemJunk)}, nil,
				func(app.OverviewOutcome, error) { p.active.Set("") },
				func(out app.OverviewOutcome, err error) { cleaned(stores, advice.ActionDelete, out, err, undo) })
		})
	}
	press := func() {
		reading.Set(true)
		p.ov.TrashInfo(cx, ctx, func(info app.TrashInfo, err error) {
			reading.Set(false)
			empty(info, err)
		})
	}
	button := core.Dynamic(func(*core.Scope) core.Node {
		b := kit.Button{Variant: kit.ButtonGhost, TestID: "empty-trash", OnPress: press, Busy: reading.Get,
			Disabled: func() bool { return undoShown.Get() || p.cleaning() || !p.actionable() },
			TextFunc: func() string {
				if reading.Get() {
					return "正在读取…"
				}
				return "清空" + bin + "…"
			}}
		if undoShown.Get() {
			b.Label, b.Tooltip = "请等撤销提示消失后再清空", "请等撤销提示消失后再清空"
		}
		return core.Func(func(*core.Scope) core.Node { return b })
	})
	return core.Show{When: func() bool { return cat.Get() != nil }, Then: func(*core.Scope) core.Node {
		has := func() bool { c := cat.Get(); return c != nil && len(c.Items) > 0 }
		text := func() string {
			c := cat.Get()
			if c == nil {
				return ""
			}
			if len(c.Items) == 0 {
				return c.Warning
			}
			return bin + "里有 " + format.Bytes(c.Size) + "。移到" + bin + "的文件，清空" + bin + "后才真正腾出空间。"
		}
		return kit.Card{Variant: kit.CardMuted, TestID: "trash-row", Padding: core.Symmetric(14, 20), Style: core.Style{FlexShrink: 0}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, ColumnGap: 14, RowGap: 8}, Children: []core.Node{
				core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(260), Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
					Children: []core.Node{
						kit.CategoryTile("trash"),
						core.Text{ContentFunc: text, TestID: "trash-text", StyleFunc: func() core.Style {
							c := kit.ColorTextSecondary
							if !has() {
								c = kit.ColorWarning
							}
							return core.Style{FlexShrink: 1, FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: c}
						}},
					}},
				core.Show{When: has, Then: func(*core.Scope) core.Node { return button }},
			}},
		}}
	}}
}

// browse is 按类别查看: one card per source with what it found and what the cards take of it, each opening the source's drill-down view. It shows, all three cards together, while any source found something or failed; three cards of 「0 B」 would only repeat the empty conclusion.
func browse(cx *core.Scope, p *page) core.Node {
	list := core.NewMemoFunc(cx, func() []Browse {
		if r := p.result(); r != nil {
			return BrowseOf(*r)
		}
		return nil
	}, nil)
	shown := core.NewMemo(cx, func() bool {
		return slices.ContainsFunc(list.Get(), func(b Browse) bool { return b.Items > 0 || b.Error != "" })
	})
	cards := make([]core.Node, len(Sources))
	for i, id := range Sources {
		meta := nav.Route(id).Meta()
		b := func() Browse {
			if l := list.Get(); i < len(l) {
				return l[i]
			}
			return Browse{Source: string(id)}
		}
		tab := nav.DevTab("")
		if id == app.CleanerDevJunk {
			tab = nav.DevProjects
		}
		st := cardStyle()
		st.Cursor = core.CursorPointer
		st.Hover = &core.Style{Background: kit.SurfaceRowHover}
		st.FocusVisible = &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText}
		cards[i] = core.View{Role: core.RoleLink, Label: meta.Title, TestID: "browse-" + string(id), Style: st,
			OnPress: func(core.PressEvent) { p.drill(string(id), "", tab) },
			Children: []core.Node{
				cardHead(meta.Tone, meta.Glyph, 36, 18, meta.Title),
				core.Text{ContentFunc: func() string { return format.Bytes(b().Bytes) }, TestID: "browse-size-" + string(id),
					Style: core.Style{FontFamily: kit.FontFace(kit.Bold), FontWeight: kit.FaceWeight(kit.Bold), FontSize: kit.FontStat,
						LetterSpacingEm: kit.LetterSpacingStat, FontVariant: core.FontTabularNums, Color: kit.ColorText}},
				mutedText(func() string { return BrowseNote(b()) }, func() bool { return b().Error != "" }, "browse-note-"+string(id)),
			}}
	}
	section := core.View{Role: core.RoleRegion, Label: "按类别查看", Style: core.Style{Gap: 12, FlexShrink: 0}, Children: []core.Node{
		kit.SectionTitle("按类别查看"),
		cardGrid("按类别查看", "browse", cards...),
	}}
	return core.Show{When: shown.Get, Then: func(*core.Scope) core.Node { return section }}
}

// growthState is what 空间变化 read.
type growthState struct {
	list   []growth.Growth
	err    error
	loaded bool
}

// growthCard is 空间变化: the locations that grew most over the last 7 days, read again with every new result, each with 去清理 when a card covers it, else 在空间透视中查看.
func growthCard(cx *core.Scope, p *page) core.Node {
	s := core.NewSignalFunc(cx, growthState{}, nil)
	ctx := cx.Context()
	seq := 0
	checked := core.NewMemo(cx, func() time.Time {
		if r := p.result(); r != nil {
			return r.CheckedAt
		}
		return time.Time{}
	})
	cx.Effect(func() {
		checked.Get()
		seq++
		mine := seq
		cx.Untrack(func() {
			p.ov.Growth(cx, ctx, growthWindow, func(list []growth.Growth, err error) {
				if mine == seq {
					s.Set(growthState{list: list, err: err, loaded: true})
				}
			})
		})
	})
	reveal := func(path string) {
		if p.deps.Files == nil {
			return
		}
		async.Do(cx, ctx, func(ctx context.Context) (struct{}, error) { return struct{}{}, p.deps.Files.Reveal(ctx, path) },
			func(_ struct{}, err error) {
				if err != nil {
					p.stores.Toasts.ShowLater(store.Toast{Text: err.Error(), Duration: toastShort})
				}
			})
	}
	body := core.Dynamic(func(*core.Scope) core.Node {
		g := s.Get()
		switch {
		// Fewer than two snapshots, or no controller to keep them; any other error is records that cannot be read, as 空间透视's 最近变化 says.
		case errors.Is(g.err, growth.ErrNoHistory), g.err != nil && !p.ov.Connected():
			return core.Func(func(*core.Scope) core.Node {
				return bodyText(fixed("Spruce 会定期记下主要位置的大小，过一两天这里就会显示哪些位置在变大。"), kit.ColorTextMuted, "growth-empty")
			})
		case g.err != nil:
			return core.Func(func(*core.Scope) core.Node {
				return kit.ErrorLine{Text: fixed("无法读取空间变化的记录"), TestID: "growth-error"}
			})
		case len(g.list) == 0:
			return core.Func(func(*core.Scope) core.Node {
				return bodyText(fixed("最近 7 天没有明显变大的位置。"), kit.ColorTextMuted, "growth-empty")
			})
		}
		list := g.list[:min(growthRows, len(g.list))]
		return core.Func(func(cx *core.Scope) core.Node {
			rows := make([]core.Node, len(list))
			for i, item := range list {
				rows[i] = growthRow(p, i, item, reveal)
			}
			return core.View{Style: core.Style{Gap: 4}, Children: rows}
		})
	})
	return core.Show{When: func() bool { return s.Get().loaded }, Then: func(*core.Scope) core.Node {
		return kit.Card{TestID: "growth", Padding: core.Symmetric(16, 20), Gap: 10, Style: core.Style{FlexShrink: 0}, Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8}, Children: []core.Node{
				kit.Text("空间变化", kit.FontCardTitle, kit.Semibold, kit.ColorText),
				kit.Text("最近 7 天", kit.FontBody, kit.Regular, kit.ColorTextMuted),
			}},
			body,
			kit.Link{Start: true, Text: fixed("查看全部"), To: to(nav.Space), TestID: "growth-more"},
		}}
	}}
}

// growthRow is one location that grew: its tile and name, since when and by how much, the figures and what to do about it.
func growthRow(p *page, i int, g growth.Growth, reveal func(string)) core.Node {
	icon := g.Icon
	if icon == "" {
		icon = "folder"
	}
	name := GrowthName(g, p.deps.Home)
	action := core.Dynamic(func(*core.Scope) core.Node {
		card := advice.Covering(p.recs(), p.scan(), g.Path)
		return core.Func(func(*core.Scope) core.Node {
			if card != "" {
				return kit.Link{Text: fixed("去清理"), TestID: "growth-row-" + strconv.Itoa(i) + "-clean", OnPress: func() {
					p.more.Set(true)
					p.focus.Set(card)
				}}
			}
			return kit.Link{Text: fixed("在空间透视中查看"), TestID: "growth-row-" + strconv.Itoa(i) + "-space", OnPress: func() { p.nav.GoSpace(g.Path) }}
		})
	})
	return core.View{TestID: "growth-row-" + strconv.Itoa(i), Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Padding: core.Symmetric(6, 0)},
		Children: []core.Node{
			kit.CategoryTile(icon),
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2}, Children: []core.Node{
				core.Text{Content: name, MaxLines: 1, EllipsizeMode: core.EllipsizeMiddle, Style: core.Style{FontFamily: kit.FontFace(kit.Semibold),
					FontWeight: kit.FaceWeight(kit.Semibold), FontSize: kit.FontBody, Color: kit.ColorText}},
				bodyText(func() string { return GrowthSentence(g, p.now.Get()) }, kit.ColorTextMuted, "growth-row-"+strconv.Itoa(i)+"-text"),
			}},
			core.View{Style: core.Style{AlignItems: core.AlignEnd, Gap: 2, FlexShrink: 0}, Children: []core.Node{
				kit.Tabular(kit.Text("+"+format.Bytes(g.After-g.Before), kit.FontCardTitle, kit.Bold, kit.ColorText)),
				kit.Tabular(kit.Text(format.Bytes(g.Before)+" → "+format.Bytes(g.After), kit.FontSmall, kit.Regular, kit.ColorTextMuted)),
			}},
			action,
			kit.IconButton{Glyph: "reveal", Label: "在" + format.FileManagerName() + "中显示 " + name, TestID: "growth-row-" + strconv.Itoa(i) + "-reveal",
				OnPress: func() { reveal(g.Path) }},
		}}
}
