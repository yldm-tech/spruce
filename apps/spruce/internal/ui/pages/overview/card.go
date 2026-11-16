package overview

import (
	"context"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/async"
	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/store"
)

// progressAfter is how long an action to the Trash runs before its card shows the progress; deleting and commands show it at once.
const progressAfter = 600 * time.Millisecond

// Card layout: the icon column and the gap after it, which the consequence and action rows are indented by.
const (
	iconColumn core.Points = 42
	iconGap    core.Points = 14
)

// Long lists scroll inside the card past scrollItemsAfter items, scrollItemsHeight tall.
const (
	scrollItemsAfter              = 60
	scrollItemsHeight core.Points = 360
	itemRowHeight     core.Points = 48
)

// monoFont is the design's --font-mono, for a command in a confirmation.
const monoFont = "Menlo, Consolas, monospace"

// recCard is one recommendation card: what to do, why and what follows, its one button, and its items once expanded.
func recCard(cx *core.Scope, p *page, rec func() advice.Recommendation, index func() int) core.Node {
	id := core.Untracked(cx, rec).ID
	// What is checked, which cards are open and what a clean left are kept in the app's scope, so leaving 概览 loses none of them; what is checked is shared with the drill-down views.
	choice := p.choice.Choice
	pick := core.NewMemoFunc(cx, func() Pick { return CardPick(rec(), choice.Get(), p.scan()) }, nil)
	open := func() bool { return p.kept.isOpen(id) }
	showProgress := core.NewSignal(cx, false)
	ref := new(core.Ref)
	// The card's context, taken as it is made: a handler's own ends with it.
	cardCtx := cx.Context()
	// A confirmation the card opened closes when the card goes.
	ask := p.asker(cx)

	// GoOverviewCard: this card opens and the page scrolls to it.
	cx.Effect(func() {
		if p.focus.Get() != id {
			return
		}
		cx.Untrack(func() {
			p.focus.Set("")
			p.kept.setOpen(id, true)
			ref.Measure(func(_, b core.Bounds, ok bool) {
				content, ok2 := p.ref.ContentBounds()
				if ok && ok2 {
					p.ref.ScrollTo(core.Point{Y: max(0, b.Origin.Y-content.Origin.Y-12)}, !p.reduce())
				}
			})
		})
	})

	// The card's clean is the store's (store.Overview.Running), not the page's: it outlives 概览, so a page opened while it runs shows it as busy, with its progress and 停止. A 全部清理 that cleans the card shows it busy too, its progress and 停止 under the figure.
	own := func() bool { o, ok := p.ov.Running(); return ok && o.Recommendation == id }
	busy := func() bool { return own() || p.cleaningAll(id) }
	// Past progressAfter a running action shows its progress, at once when the card comes back to a clean that has run since; the timer goes with the effect when the action ends.
	showProgress.Set(core.Untracked(cx, own))
	cx.Effect(func() {
		if !own() {
			return
		}
		cx.AfterFunc(progressAfter, func() { showProgress.Set(true) })
	})

	scan := p.scan
	refs := func() []advice.ItemRef { return pick.Get().Refs(rec()) }
	action := core.NewMemo(cx, func() advice.Action {
		if a := advice.ActionOf(scan(), refs()); a != "" {
			return a
		}
		return rec().Action
	})
	bytes := core.NewMemo(cx, func() int64 { return Sum(scan(), refs()) })
	partial := func() bool { n := len(refs()); return n > 0 && n < len(rec().Items) }
	leaving := func() bool { return p.leaving(id) }
	other := core.NewMemo(cx, func() bool { return !busy() && p.cleaning() })
	canUndo := p.canUndo
	title := func() string { return rec().Title }

	reveal := func(path string) {
		if p.deps.Files == nil {
			return
		}
		async.Do(cx, cardCtx, func(ctx context.Context) (struct{}, error) { return struct{}{}, p.deps.Files.Reveal(ctx, path) },
			func(_ struct{}, err error) {
				if err != nil {
					p.stores.Toasts.ShowLater(store.Toast{Text: err.Error(), Duration: toastShort})
				}
			})
	}

	// run cleans req, the card r's items as the user saw them when acting: a newer result that lands while a confirmation is open changes neither what nor how. The clean runs on the app's context, so leaving 概览 does not stop it, and its toast (with 撤销) and the refreshed disks are shown from the app's scope, its progress and 停止 from the store; the page's bookkeeping (the one-at-a-time lock, the held card) belongs to the page, so a card that goes while it runs cannot leave the page locked.
	run := func(a advice.Action, r advice.Recommendation, req cleanup.CleanRequest) {
		if len(req.ItemIDs) == 0 || !p.actionable() || p.cleaning() {
			return
		}
		undo := canUndo()
		stores, k := p.stores, p.kept
		p.active.Set(id)
		p.hold(r, index())
		k.setFailed(id, 0, "")
		showProgress.Set(a == advice.ActionDelete || a == advice.ActionCommand)
		p.ov.Clean(p.cx, req, app.CleanOrigin{Recommendation: r.ID}, nil,
			func(out app.OverviewOutcome, err error) {
				p.active.Set("")
				showProgress.Set(false)
				p.release(id)
			},
			func(out app.OverviewOutcome, err error) {
				// What could not be handled is kept with the card in the app's scope, also when 概览 went meanwhile.
				if err == nil {
					k.setFailed(id, len(out.Result.Failed), out.RunID)
				}
				cleaned(stores, a, out, err, undo)
			})
	}

	act := func() {
		r := rec()
		sel := pick.GetUntracked()
		req := CleanRequest(r, sel)
		a := action.GetUntracked()
		n, size := len(core.Untracked(cx, refs)), bytes.GetUntracked()
		if n == 0 {
			return
		}
		if !NeedsConfirm(a, canUndo()) {
			run(a, r, req)
			return
		}
		var d kit.ConfirmDialog
		switch a {
		case advice.ActionDelete:
			d = confirmDialog(DeleteConfirm(r, n, size))
		case advice.ActionCommand:
			command, description := r.Command, ""
			for _, c := range cardCategories(scan(), r) {
				if c.Command != "" {
					command, description = c.Command, c.Description
					break
				}
			}
			d = confirmDialog(CommandConfirm(command, description, size))
		case advice.ActionMixed:
			d = cleanflow.CleanPlanDialog(cardCategories(scan(), r), sel.Sel)
		default:
			d = confirmDialog(RecycleConfirm(r, n, size))
		}
		d.TestID = "rec-confirm"
		ask(d, func() { run(a, r, req) })
	}

	// A result that may not be cleaned, with no check running, is checked again from where the user looks: the card's own button.
	stale := core.NewMemo(cx, func() bool { return !p.actionable() && !p.checking() })
	actLabel := func() string {
		switch {
		case busy():
			return BusyLabel(action.Get())
		case !p.actionable() && p.checking():
			return "正在核对…"
		case !p.actionable():
			return "重新检查"
		}
		return ActionLabel(action.Get(), partial(), bytes.Get())
	}
	actButton := core.Dynamic(func(*core.Scope) core.Node {
		if stale.Get() {
			b := kit.Button{Variant: kit.ButtonGhost, TestID: "rec-" + id + "-act", TextFunc: actLabel, OnPress: p.checkNow,
				Disabled: func() bool { return p.cleaning() || leaving() }}
			if p.cleaning() {
				b.Label, b.Tooltip = "正在清理，完成后再检查", "正在清理，完成后再检查"
			}
			return core.Func(func(*core.Scope) core.Node { return b })
		}
		b := kit.Button{Variant: kit.ButtonAccent, TestID: "rec-" + id + "-act", TextFunc: actLabel, OnPress: act, Busy: busy,
			Disabled: func() bool { return !p.actionable() || other.Get() || len(refs()) == 0 || leaving() }}
		if other.Get() {
			b.Label, b.Tooltip = "正在清理另一项，完成后再试", "正在清理另一项，完成后再试"
		}
		return core.Func(func(*core.Scope) core.Node { return b })
	})
	auto := func() bool {
		r := rec()
		_, known := Rules[r.Rule]
		return canUndo() && known && p.stores.Settings.Loaded() && !RuleOn(p.stores.Settings.Value().Automation, r.Rule)
	}
	autoLink := core.Show{When: auto, Then: func(cx *core.Scope) core.Node {
		ask := p.asker(cx)
		return kit.Button{Text: "以后自动处理", Variant: kit.ButtonLink, TestID: "rec-" + id + "-auto", Disabled: leaving, OnPress: func() {
			r := rec()
			rule := Rules[r.Rule]
			ask(kit.ConfirmDialog{TestID: "rule-confirm", Title: "开启「" + rule.Label + "」？", Lines: []string{rule.Description},
				ConfirmLabel: "开启", CancelLabel: "取消"}, func() {
				next := p.stores.Settings.Value()
				next.Automation = WithRule(next.Automation, r.Rule)
				p.stores.Settings.Save(next, nil)
				// Turning a rule on is the user's action: the notification prompt (for the weekly summary) comes with it, the first time.
				cx.RequestNotificationPermission(nil)
			})
		}}
	}}
	actions := core.Dynamic(func(*core.Scope) core.Node {
		if own() && showProgress.Get() {
			return core.Func(func(*core.Scope) core.Node {
				return progressRow("rec-"+id, action.Get, bytes.Get, p.ov.CleanProgress, p.ov.StopClean)
			})
		}
		return core.Func(func(*core.Scope) core.Node {
			return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, ColumnGap: 12, RowGap: 6}, Children: []core.Node{
				actButton, autoLink,
				core.Show{When: func() bool { return p.kept.failedOf(id).n > 0 && !busy() }, Then: func(*core.Scope) core.Node {
					return failedLine(p, id)
				}},
			}}
		})
	})

	badge := core.Dynamic(func(*core.Scope) core.Node {
		b := BadgeFor(action.Get(), canUndo())
		level := kit.LevelNormal
		if b.Warning {
			level = kit.LevelWarning
		}
		return core.Func(func(*core.Scope) core.Node {
			return kit.Badge{Text: b.Text, Glyph: b.Glyph, Level: level, TestID: "rec-" + id + "-badge"}
		})
	})
	indent := core.Edges{Left: iconColumn + iconGap}
	// The whole head opens and closes the items; the chevron stays the control the keyboard reaches.
	toggle := func() { p.kept.setOpen(id, !core.Untracked(cx, open)) }
	head := core.View{TestID: "rec-" + id + "-head", NoFocus: true, OnPress: func(core.PressEvent) { toggle() },
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignStart, Gap: iconGap, Cursor: core.CursorPointer}, Children: []core.Node{
			// The icon breathes while the card's clean runs.
			core.View{Style: core.Style{FlexShrink: 0, Animated: kit.Breathing(cx, busy)}, Children: []core.Node{core.Dynamic(func(*core.Scope) core.Node {
				icon := rec().Icon
				return core.Func(func(*core.Scope) core.Node { return kit.CategoryTile(icon) })
			})}},
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 3}, Children: []core.Node{
				core.Text{ContentFunc: title, TestID: "rec-" + id + "-title", Style: core.Style{FontFamily: kit.FontFace(kit.Semibold),
					FontWeight: kit.FaceWeight(kit.Semibold), FontSize: kit.FontCardTitle, LineHeightMultiple: 1.4, Color: kit.ColorText}},
				bodyText(func() string { return ReasonText(rec(), p.now.Get()) }, kit.ColorTextSecondary, "rec-"+id+"-reason"),
				core.Show{When: func() bool { return rec().Reason2 != "" }, Then: func(*core.Scope) core.Node {
					return bodyText(func() string { return rec().Reason2 }, kit.ColorTextMuted, "rec-"+id+"-held")
				}},
			}},
			core.View{Style: core.Style{AlignItems: core.AlignEnd, Gap: 2, FlexShrink: 0}, Children: []core.Node{
				kit.Tabular(core.Text{ContentFunc: func() string { return format.Bytes(rec().Bytes) }, TestID: "rec-" + id + "-size",
					Style: core.Style{FontFamily: kit.FontFace(kit.Bold), FontWeight: kit.FaceWeight(kit.Bold), FontSize: kit.FontCardTitle, Color: kit.ColorText}}),
				kit.Tabular(kit.TextFunc(func() string { return countText(len(rec().Items)) }, kit.FontSmall, kit.Regular, kit.ColorTextMuted)),
			}},
			expandButton(id, title, open, toggle),
		}}
	consequence := core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, ColumnGap: 8, RowGap: 6, Padding: indent},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6, FlexShrink: 1, MinWidth: core.Pt(0), Color: kit.ColorTextMuted},
				Children: []core.Node{
					kit.Glyph{Name: "info", Size: 14, Color: core.CurrentColor},
					bodyText(func() string { return rec().Consequence }, kit.ColorTextMuted, "rec-"+id+"-consequence"),
				}},
			badge,
		}}
	top := core.View{Style: core.Style{Gap: 10, Padding: core.Symmetric(18, 20)}, Children: []core.Node{
		head, consequence,
		core.View{Style: core.Style{Padding: indent}, Children: []core.Node{actions}},
	}}
	items := core.Show{When: open, Then: func(cx *core.Scope) core.Node {
		return kit.Appear{Entrance: kit.EnterFade, Children: []core.Node{cardItems(cx, p, id, rec, pick, reveal, func() bool { return busy() || leaving() })}}
	}}
	// Cards come in one after another as the page shows them.
	card := core.View{Role: core.RoleRegion, LabelFunc: title, TestID: "rec-" + id, Ref: ref,
		Style: core.Style{CornerRadius: kit.RadiusCard, Background: kit.SurfaceCard, BorderWidth: 1, BorderColor: kit.BorderCard, Overflow: core.Hidden,
			FlexShrink: 0, Animated: kit.Enter(cx, kit.EnterRise, kit.StaggerDelay(core.Untracked(cx, index)))},
		Children: []core.Node{top, items}}
	// A card acted on fades and folds away as it goes.
	return core.View{StyleFunc: func() core.Style {
		st := core.Style{FlexShrink: 0, Transition: core.Transition{Duration: leaveTime, Easing: core.Ease, Properties: core.TransitionOpacity | core.TransitionLayout}}
		if leaving() {
			st.Opacity, st.Height, st.Overflow = core.Alpha(0), core.Pt(0), core.Hidden
		}
		return st
	}, Children: []core.Node{card}}
}

// confirmDialog is c as a confirmation; a command is drawn in the monospaced font.
func confirmDialog(c Confirm) kit.ConfirmDialog {
	d := kit.ConfirmDialog{Title: c.Title, ConfirmLabel: c.ConfirmLabel, Danger: c.Danger}
	if c.Command == "" {
		d.Lines = []string{c.Message}
		return d
	}
	d.Message = core.Text{TestID: "rec-confirm-message", Spans: []core.Span{
		{Content: c.Message},
		{Content: c.Command, Style: core.Style{FontFamily: monoFont}},
		{Content: c.After},
	}}
	return d
}

// progressRow is a card's action while it runs past progressAfter, and 全部清理's while it runs: the bar, how far it is (CleanProgressText of the action a and the total bytes it cleans) and 停止; its parts' TestIDs start with prefix.
func progressRow(prefix string, a func() advice.Action, total func() int64, progress func() cleanup.Progress, stop func()) core.Node {
	text := kit.Tabular(kit.TextFunc(func() string { return CleanProgressText(a(), progress(), total()) }, kit.FontBody, kit.Regular, kit.ColorTextSecondary))
	text.TestID = prefix + "-progress-text"
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12}, Children: []core.Node{
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(80)}, Children: []core.Node{
			// The row shows only while the clean runs, so the bar's light always sweeps: one long item or a command stands still until it ends.
			kit.Meter{Height: 4, Label: "清理进度", TestID: prefix + "-progress", Active: func() bool { return true },
				Value: func() float64 { return CleanProgressFraction(progress(), total()) }},
		}},
		text,
		kit.Button{Text: "停止", Variant: kit.ButtonLink, TestID: prefix + "-stop", OnPress: stop},
	}}
}

// failedLine is 「{n} 项未能处理」 under a card whose last clean left items, with 查看, the run in 操作历史 that tells why, once the toast that offered it is gone.
func failedLine(p *page, id string) core.Node {
	f := func() failure { return p.kept.failedOf(id) }
	return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter}, Children: []core.Node{
		bodyText(func() string { return strconv.Itoa(f().n) + " 项未能处理" }, kit.ColorWarning, "rec-"+id+"-failed"),
		core.Show{When: func() bool { return f().run != "" }, Then: func(*core.Scope) core.Node {
			return kit.Link{Text: fixed("查看"), TestID: "rec-" + id + "-failed-view", OnPress: func() { p.nav.GoHistory("", f().run) }}
		}},
	}}
}

// expandButton opens and closes a card's items (.icon-button.category-expand): open reports whether they show, toggle opens or closes them.
func expandButton(id string, title func() string, open func() bool, toggle func()) core.Node {
	return core.View{Role: core.RoleButton, TestID: "rec-" + id + "-expand", Expandable: func() bool { return true }, Expanded: open,
		LabelFunc: func() string {
			if open() {
				return "收起" + title()
			}
			return "展开" + title()
		},
		Style: core.Style{Width: core.Pt(30), Height: core.Pt(30), FlexShrink: 0, CornerRadius: 15, AlignItems: core.AlignCenter,
			JustifyContent: core.JustifyCenter, Color: kit.ColorTextSecondary, Cursor: core.CursorPointer,
			FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText},
			Hover:        &core.Style{Background: kit.SurfaceButtonGhost, Color: kit.ColorText}},
		OnPress:  func(core.PressEvent) { toggle() },
		Children: []core.Node{kit.Chevron{Open: open}},
	}
}

// cardItems is an expanded card: how much is checked with 全选 and 全不选, what a newer check added unchecked, the items by category, and the link to the drill-down view.
func cardItems(cx *core.Scope, p *page, id string, rec func() advice.Recommendation, pick *core.Memo[Pick], reveal func(string), disabled func() bool) core.Node {
	cats := core.NewMemoFunc(cx, func() []cleanup.Category { return cardCategories(p.scan(), rec()) }, nil)
	summary := func() string {
		refs := pick.Get().Refs(rec())
		return "已选 " + strconv.Itoa(len(refs)) + "/" + strconv.Itoa(len(rec().Items)) + " 项 · " + format.Bytes(Sum(p.scan(), refs))
	}
	choice := p.choice.Choice
	set := func(sel cleanflow.Selection) {
		choice.Set(ChangeCard(choice.GetUntracked(), core.Untracked(cx, rec), core.Untracked(cx, p.scan), sel))
	}
	toolbar := core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, RowGap: 6, ColumnGap: 12, Padding: core.Symmetric(10, 20)},
		Children: []core.Node{
			core.View{Style: core.Style{AutoMargin: core.SideRight}, Children: []core.Node{
				kit.Tabular(kit.TextFunc(summary, kit.FontBody, kit.Regular, kit.ColorTextSecondary))}},
			kit.Button{Text: "全选", Variant: kit.ButtonLink, Disabled: disabled, TestID: "rec-" + id + "-all",
				OnPress: func() { set(DefaultPick(core.Untracked(cx, rec)).Sel) }},
			kit.Button{Text: "全不选", Variant: kit.ButtonLink, Disabled: disabled, TestID: "rec-" + id + "-none",
				OnPress: func() { set(cleanflow.NothingSelected) }},
			core.Show{When: func() bool { return pick.Get().Added > 0 }, Then: func(*core.Scope) core.Node {
				t := bodyText(func() string { return "检查后新增 " + strconv.Itoa(pick.Get().Added) + " 项，未选中" }, kit.ColorTextMuted, "rec-"+id+"-added")
				t.Style.FlexBasis = core.Pct(100)
				return t
			}},
		}}
	list := core.Dynamic(func(*core.Scope) core.Node {
		groups := cats.Get()
		return core.Func(func(cx *core.Scope) core.Node {
			var children []core.Node
			for _, c := range groups {
				if len(groups) > 1 {
					children = append(children, core.Text{Content: c.Title, Style: core.Style{Padding: core.Edges{Top: 8, Bottom: 4, Left: 20, Right: 20},
						FontFamily: kit.FontFace(kit.Semibold), FontWeight: kit.FaceWeight(kit.Semibold), FontSize: kit.FontSection,
						LetterSpacingEm: kit.LetterSpacingSection, Color: kit.ColorTextSection}})
				}
				children = append(children, categoryItems(c, pick, set, disabled, reveal))
			}
			return core.View{Role: core.RoleGroup, Label: core.Untracked(cx, rec).Title + "的项目", TestID: "rec-" + id + "-items", Children: children}
		})
	})
	more := core.View{Style: core.Style{Padding: core.Symmetric(10, 20), AlignItems: core.AlignStart}, Children: []core.Node{
		kit.Link{Start: true, TestID: "rec-" + id + "-more",
			Text: func() string { return "在" + nav.Route(rec().Source).Meta().Title + "中查看全部" },
			OnPress: func() {
				r := rec()
				cat := ""
				if len(r.Categories) > 0 {
					cat = r.Categories[0]
				}
				p.drill(r.Source, cat, devTab(r))
			}},
	}}
	return core.View{Style: core.Style{Background: kit.SurfaceList, Borders: core.Edges{Top: 1}, BorderColor: kit.BorderSubtle},
		Children: []core.Node{toolbar, list, more}}
}

// devTab is the 开发者 tab a card's link opens: 工具缓存 for a tool's card, else 项目.
func devTab(r advice.Recommendation) nav.DevTab {
	if strings.HasPrefix(r.ID, "tool-") {
		return nav.DevTools
	}
	return nav.DevProjects
}

// categoryItems lists one category's items of a card: in place up to scrollItemsAfter, else in a list that scrolls inside the card.
func categoryItems(c cleanup.Category, pick *core.Memo[Pick], set func(cleanflow.Selection),
	disabled func() bool, reveal func(string)) core.Node {
	// anchor is the item the user checked or unchecked last, and how: a Shift-press gives the rows from it to the one pressed the same.
	anchor, anchorOn := "", false
	row := func(it cleanup.Item) core.Node {
		return cleanflow.ItemRow{Item: it, Disabled: disabled, Reveal: reveal,
			Checked: func() bool { return pick.Get().Sel.Has(c.ID, it.ID) },
			OnChange: func(on bool) {
				anchor, anchorOn = it.ID, on
				set(cleanflow.SetItem(pick.GetUntracked().Sel, c, it.ID, on))
			},
			OnRange: func() {
				if anchor == "" {
					anchor, anchorOn = it.ID, !pick.GetUntracked().Sel.Has(c.ID, it.ID)
				}
				set(cleanflow.SetItems(pick.GetUntracked().Sel, c, cleanflow.Between(c.Items, anchor, it.ID), anchorOn))
			}}
	}
	if len(c.Items) <= scrollItemsAfter {
		children := make([]core.Node, len(c.Items))
		for i, it := range c.Items {
			children[i] = row(it)
		}
		return core.View{Style: core.Style{Padding: core.Symmetric(4, 0)}, Children: children}
	}
	return core.List{Style: core.Style{Height: core.Pt(scrollItemsHeight)}, EstimatedSize: itemRowHeight,
		Count: func() int { return len(c.Items) },
		Key:   func(i int) any { return c.Items[i].ID },
		Row: func(cx *core.Scope, r core.ListRow) core.Node {
			return core.Dynamic(func(*core.Scope) core.Node {
				it := c.Items[r.Index()]
				return core.Func(func(*core.Scope) core.Node { return row(it) })
			})
		}}
}
