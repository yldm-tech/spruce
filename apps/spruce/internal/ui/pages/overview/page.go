// Package overview is 概览, the app's home: the conclusion of the latest check (「可以放心释放 18.7 GB」), the recommendation cards to act on one at a time, what needs the user's judgement, the Trash, the three sources to drill into, what grew and the other checks. Its state and actions come from 概览's controller through store.Overview; the page never scans or cleans on its own.
package overview

import (
	"cmp"
	"context"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/engine/scancache"
	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/shared"
	"spruce/apps/spruce/internal/ui/store"
)

// shownCards is how many cards show before 再显示 {n} 条建议.
const shownCards = 5

// leaveTime is how long a card that was acted on takes to go.
const leaveTime = 200 * time.Millisecond

// stopArmed is how long the header's 停止检查 waits after it shows before it takes a press, about a double click's interval: the second press of a double press on 重新检查 does not stop the check the first one started.
const stopArmed = 500 * time.Millisecond

// view is which body the page shows.
type view uint8

const (
	// viewResult is a result, being checked again or not (states B to F with a result).
	viewResult view = iota
	// viewProgress is the first check, with no result yet (state A).
	viewProgress
	// viewFailed is a failed check with no result (state F).
	viewFailed
	// viewStopped is no result and no check (state G).
	viewStopped
)

// viewOf is the body for st.
func viewOf(st app.OverviewState) view {
	switch {
	case st.Result != nil:
		return viewResult
	case st.Phase == app.OverviewChecking:
		return viewProgress
	case st.Error != "":
		return viewFailed
	}
	return viewStopped
}

// heldCard is a card the page acted on, kept in its place until the action ends and then, once the result no longer has it, while it goes.
type heldCard struct {
	rec       advice.Recommendation
	index     int
	checkedAt time.Time
	released  bool
	leaving   bool
}

// page is the state the page's parts share.
type page struct {
	cx *core.Scope
	// ctx is the page's context, taken as it is made: work for the whole page (a card's clean) ends with the page, not with the part that started it.
	ctx    context.Context
	stores *store.Stores
	ov     *store.Overview
	nav    *nav.Navigator
	deps   Deps
	// kept and choice are what the page keeps in the app's scope: its own, and what is checked, which the drill-down views share.
	kept   *kept
	choice *cleanflow.Kept
	ref    *core.Ref
	// active is the card whose action runs, or emptyTrash, "" for none: one thing at a time.
	active *core.Signal[string]
	// checkErr is why the last 重新检查 or 开始检查 was refused.
	checkErr *core.Signal[string]
	// more shows every card.
	more *core.Signal[bool]
	// focus is the card GoOverviewCard asked for, until it is shown.
	focus *core.Signal[string]
	held  *core.Signal[map[string]heldCard]
	// dialog is the confirmation open, nil for none.
	dialog *core.Signal[*kit.ConfirmDialog]
	// now is the clock the times read, moved every minute.
	now *core.Signal[time.Time]
	// autoSeen is the automatic run told already: 概览 tells one once per app (seenAuto).
	autoSeen *core.Signal[time.Time]
	reduce   func() bool
}

// seenAuto is, by the app's store.Overview, the automatic run 概览 told: a page opened again does not tell it again.
var seenAuto sync.Map

// emptyTrash is page.active while the Trash is emptied, and cleanAllID while 全部清理 runs.
const (
	emptyTrash = "\x00trash"
	cleanAllID = "\x00all"
)

func (p *page) state() app.OverviewState { return p.ov.State() }

func (p *page) result() *app.OverviewResult { return p.state().Result }

func (p *page) checking() bool { return p.state().Phase == app.OverviewChecking }

// progress is the running check's, zero when none runs or it has not reported yet.
func (p *page) progress() cleanup.Progress {
	if pr := p.state().Progress; pr != nil {
		return *pr
	}
	return cleanup.Progress{}
}

// cleaning reports whether a clean runs: one of this page, one the store started (a drill-down view, 撤销) or one the controller runs.
func (p *page) cleaning() bool {
	return p.active.Get() != "" || p.ov.Busy() || p.state().Phase == app.OverviewCleaning
}

// actionable reports whether the result may be acted on now.
func (p *page) actionable() bool {
	r := p.result()
	return r != nil && r.Actionable
}

// cleaningAll reports whether a 全部清理 runs that cleans the card id; reading it subscribes.
func (p *page) cleaningAll(id string) bool {
	o, ok := p.ov.Running()
	return ok && o.All && slices.Contains(p.kept.all.Get(), id)
}

// canUndo is whether moves to the Trash can be undone: macOS, where the automatic rules run too.
func (p *page) canUndo() bool { return p.state().CanUndo }

func (p *page) recs() []advice.Recommendation {
	if r := p.result(); r != nil {
		return r.Recommendations
	}
	return nil
}

func (p *page) scan() cleanup.ScanResult {
	if r := p.result(); r != nil {
		return r.Scan
	}
	return cleanup.ScanResult{}
}

// checkNow starts a foreground check; a refusal shows under the conclusion.
func (p *page) checkNow() {
	p.checkErr.Set("")
	p.ov.CheckNow(func(err error) {
		if err != nil {
			p.checkErr.Set(err.Error())
		}
	})
}

// ask opens d, which runs confirm once confirmed, and returns the confirmation it opened.
func (p *page) ask(d kit.ConfirmDialog, confirm func()) *kit.ConfirmDialog {
	d.OnConfirm = func() {
		p.dialog.Set(nil)
		confirm()
	}
	d.OnCancel = func() { p.dialog.Set(nil) }
	p.dialog.Set(&d)
	return &d
}

// asker is ask for the part of the page made in cx: a confirmation it opened closes when cx is disposed (a card that a newer result moved past the first ones or dropped), so a confirmation never outlives what it confirms. Call it as the part is made, so the cleanup belongs to cx and not to whatever scope runs a handler.
func (p *page) asker(cx *core.Scope) func(kit.ConfirmDialog, func()) {
	var opened *kit.ConfirmDialog
	cx.Cleanup(func() {
		if opened != nil && p.dialog.GetUntracked() == opened {
			p.dialog.Set(nil)
		}
	})
	return func(d kit.ConfirmDialog, confirm func()) { opened = p.ask(d, confirm) }
}

// cleaned is what a clean of 概览 leaves once it ended, shown in the app's scope so leaving the page does not lose it: the disks and the Trash read again, and the toast (CleanToast, with 撤销 when undo was possible as it started).
func cleaned(stores *store.Stores, a advice.Action, out app.OverviewOutcome, err error, undo bool) {
	stores.Locations.Bump()
	stores.Trash.Bump()
	if err != nil {
		stores.Toasts.Show(store.Toast{Text: err.Error(), Duration: toastShort})
		return
	}
	stores.Toasts.Show(CleanToast(a, out, undo))
}

// cleanAll cleans a, 全部清理's selection, whose action is act, in one clean through the store, as a card's clean goes: it outlives the page, the cards in it stay in place and show it while it runs, and what failed stays with the card it is of.
func (p *page) cleanAll(a CleanAll, act advice.Action) {
	req := cleanflow.ItemsRequest(a.Categories, a.Sel)
	if len(req.ItemIDs) == 0 || !p.actionable() || p.cleaning() {
		return
	}
	undo := p.canUndo()
	stores, k, recs := p.stores, p.kept, p.recs()
	p.active.Set(cleanAllID)
	k.all.Set(a.Cards)
	for i, r := range recs {
		if slices.Contains(a.Cards, r.ID) {
			p.hold(r, i)
			k.setFailed(r.ID, 0, "")
		}
	}
	p.ov.Clean(p.cx, req, app.CleanOrigin{All: true}, nil,
		func(app.OverviewOutcome, error) {
			p.active.Set("")
			for _, id := range a.Cards {
				p.release(id)
			}
		},
		func(out app.OverviewOutcome, err error) {
			k.all.Set(nil)
			if err == nil {
				for id, n := range FailedByCard(recs, a.Cards, out.Result.Failed) {
					k.setFailed(id, n, out.RunID)
				}
			}
			cleaned(stores, act, out, err, undo)
		})
}

// drill opens the drill-down view of source, on the 开发者 tab when tab is set, revealing categoryID.
func (p *page) drill(source, categoryID string, tab nav.DevTab) {
	r := nav.Route(source)
	if r == nav.DevJunk && tab != "" {
		p.nav.GoDevTab(tab)
	}
	p.nav.GoDrill(r, categoryID)
}

// hold keeps the card r, at index, in its place while its action runs.
func (p *page) hold(r advice.Recommendation, index int) {
	var at time.Time
	if res := p.result(); res != nil {
		at = res.CheckedAt
	}
	next := cloneHeld(p.held.GetUntracked())
	next[r.ID] = heldCard{rec: r, index: index, checkedAt: at}
	p.held.Set(next)
}

// release ends the hold of the card id once its action ended: it goes as soon as the result no longer has it.
func (p *page) release(id string) {
	next := cloneHeld(p.held.GetUntracked())
	if h, ok := next[id]; ok {
		h.released = true
		next[id] = h
	}
	p.held.Set(next)
	p.settle()
}

// settle follows the result for the held cards: one still there takes its new content and goes once a later check keeps it; one released and gone starts to leave (at once with reduced motion).
func (p *page) settle() {
	held := p.held.GetUntracked()
	if len(held) == 0 {
		return
	}
	cur := core.Untracked(p.cx, p.recs)
	var at time.Time
	if r := core.Untracked(p.cx, p.result); r != nil {
		at = r.CheckedAt
	}
	next := map[string]heldCard{}
	for id, h := range held {
		if i := slices.IndexFunc(cur, func(r advice.Recommendation) bool { return r.ID == id }); i >= 0 {
			if h.released && !at.Equal(h.checkedAt) {
				continue
			}
			h.rec, h.index = cur[i], i
			next[id] = h
			continue
		}
		if h.released && !h.leaving {
			if p.reduce() {
				continue
			}
			h.leaving = true
		}
		next[id] = h
	}
	p.held.Set(next)
}

func cloneHeld(m map[string]heldCard) map[string]heldCard {
	out := make(map[string]heldCard, len(m)+1)
	for k, v := range m {
		out[k] = v
	}
	return out
}

// displayed are the cards the page shows: the result's, with the held cards it no longer has kept in their place.
func (p *page) displayed() []advice.Recommendation {
	cur := p.recs()
	held := p.held.Get()
	if len(held) == 0 {
		return cur
	}
	type entry struct {
		id string
		h  heldCard
	}
	var extra []entry
	for id, h := range held {
		if !slices.ContainsFunc(cur, func(r advice.Recommendation) bool { return r.ID == id }) {
			extra = append(extra, entry{id, h})
		}
	}
	if len(extra) == 0 {
		return cur
	}
	slices.SortFunc(extra, func(a, b entry) int { return cmp.Or(cmp.Compare(a.h.index, b.h.index), cmp.Compare(a.id, b.id)) })
	out := slices.Clone(cur)
	for _, e := range extra {
		out = slices.Insert(out, min(e.h.index, len(out)), e.h.rec)
	}
	return out
}

// leaving reports whether the card id is going.
func (p *page) leaving(id string) bool { return p.held.Get()[id].leaving }

// shouldCheck reports whether opening 概览 on st starts a foreground check: there is no result yet, or the one shown comes from the cache, may be outdated or can no longer be acted on, and nothing runs.
func shouldCheck(st app.OverviewState) bool {
	if st.Phase != app.OverviewIdle {
		return false
	}
	if st.Result == nil {
		return st.Error == ""
	}
	return st.Result.FromCache || st.Result.Stale || !st.Result.Actionable
}

// unanswered reports whether st is the store's state before the controller has loaded: its answer then may hold nothing else (Windows, no cache, no disk read, no rules), so only Loaded tells them apart.
func unanswered(st app.OverviewState) bool { return !st.Loaded }

// Page is 概览.
func Page(cx *core.Scope) core.Node {
	stores := store.Use(cx)
	k := keptKey.Use(cx)
	p := &page{cx: cx, ctx: cx.Context(), stores: stores, ov: stores.Overview, nav: nav.Use(cx), deps: DepsKey.Use(cx), kept: k, choice: cleanflow.UseKept(cx),
		ref: new(core.Ref), active: core.NewSignal(cx, ""), checkErr: core.NewSignal(cx, ""), more: k.more, focus: core.NewSignal(cx, ""),
		held: core.NewSignalFunc(cx, map[string]heldCard{}, nil), dialog: core.NewSignal[*kit.ConfirmDialog](cx, nil), now: core.NewSignal(cx, cx.WallTime()),
		reduce: cx.Env().ReduceMotion}
	seen, _ := seenAuto.Load(p.ov)
	at, _ := seen.(time.Time)
	p.autoSeen = core.NewSignal(cx, at)
	if id := p.nav.TakeOverviewCard(); id != "" {
		p.focus.Set(id)
	}
	// The times read again every minute, on the scope's clock (a test's manual clock and wall time).
	var tick func()
	tick = func() {
		p.now.Set(cx.WallTime())
		cx.AfterFunc(time.Minute, tick)
	}
	cx.AfterFunc(time.Minute, tick)

	// Opening 概览 shows free space as it is now.
	p.ov.ReadDisk()
	// The menu's 重新检查 (⌘R) checks again as the header's does.
	nav.UseCommands(cx, nav.Commands{Refresh: p.checkNow, Busy: func() bool { return p.checking() || p.cleaning() }})
	// Opening 概览 checks again when what it shows may be outdated; a check opened this way is the foreground's, not held back by power or load. It decides once, on the controller's state: the store answers it after the page opens at launch.
	decided := false
	cx.Effect(func() {
		st := p.state()
		if decided || !p.ov.Connected() || unanswered(st) {
			return
		}
		decided = true
		if shouldCheck(st) {
			cx.Untrack(p.checkNow)
		}
	})
	// The window coming back after it was hidden (关闭窗口后继续在菜单栏运行) is opening it again for 概览, which stayed mounted: it decides again, on the state then.
	shown := core.Untracked(cx, p.nav.Shown)
	cx.Effect(func() {
		n := p.nav.Shown()
		if n == shown {
			return
		}
		shown = n
		cx.Untrack(func() {
			if decided && shouldCheck(p.state()) {
				p.checkNow()
			}
		})
	})
	// A change of what a check looks at (exclusions, folders) while 概览 is open checks again at once.
	base, have := "", false
	cx.Effect(func() {
		if !stores.Settings.Loaded() {
			return
		}
		k := scancache.Key(stores.Settings.Value())
		if !have {
			base, have = k, true
			return
		}
		if k == base {
			return
		}
		base = k
		cx.Untrack(func() {
			if p.ov.Connected() && !p.cleaning() {
				p.checkNow()
			}
		})
	})
	cx.Effect(func() {
		p.recs()
		cx.Untrack(p.settle)
	})
	// Cards that leave go after leaveTime; the timer belongs to this effect, so it is set again when another starts to leave.
	cx.Effect(func() {
		var gone []string
		for id, h := range p.held.Get() {
			if h.leaving {
				gone = append(gone, id)
			}
		}
		if len(gone) == 0 {
			return
		}
		cx.AfterFunc(leaveTime, func() {
			rest := cloneHeld(p.held.GetUntracked())
			for _, id := range gone {
				delete(rest, id)
			}
			p.held.Set(rest)
		})
	})
	// A card GoOverviewCard asked for past the first ones shows them all.
	cx.Effect(func() {
		id := p.focus.Get()
		if id == "" {
			return
		}
		if i := slices.IndexFunc(p.displayed(), func(r advice.Recommendation) bool { return r.ID == id }); i >= shownCards {
			cx.Untrack(func() { p.more.Set(true) })
		}
	})

	mode := core.NewMemo(cx, func() view { return viewOf(p.state()) })
	header := kit.PageHeader{Route: nav.Overview, Actions: []core.Node{recheckButton(cx, p)}}
	body := core.Dynamic(func(*core.Scope) core.Node {
		switch mode.Get() {
		case viewProgress:
			return core.Func(func(*core.Scope) core.Node {
				return core.Group{
					checkProgress(p.progress, p.ov.StopCheck),
					checkRefused(p), diskRow(cx, p), statusCards{},
				}
			})
		case viewFailed:
			return core.Func(func(*core.Scope) core.Node {
				return core.Group{failedBlock(p), checkRefused(p), diskRow(cx, p), statusCards{}}
			})
		case viewStopped:
			return core.Func(func(*core.Scope) core.Node {
				return core.Group{stoppedBlock(p), diskRow(cx, p), statusCards{}}
			})
		}
		return core.Func(func(cx *core.Scope) core.Node { return resultBody(cx, p) })
	})

	live := kit.LiveStatus{Text: func() string {
		st := p.state()
		switch {
		case st.Phase == app.OverviewChecking:
			return "正在检查…"
		case st.Error != "":
			return "检查没有完成"
		case st.Result != nil && len(st.Result.Recommendations) > 0:
			return "检查完成，可以放心释放 " + format.Bytes(st.Result.Headline)
		case st.Result != nil:
			return "没有需要清理的内容"
		}
		return ""
	}}

	dialog := core.Dynamic(func(*core.Scope) core.Node {
		d := p.dialog.Get()
		if d == nil {
			return core.Group{}
		}
		return core.Func(func(*core.Scope) core.Node {
			c := *d
			c.Open = func() bool { return true }
			return c
		})
	})

	return core.Group{
		kit.Page{TestID: "overview-page", Ref: p.ref, ScrollKey: nav.Overview, Header: header, Children: []core.Node{body}},
		live, dialog,
	}
}

// recheckButton is the header's 重新检查: 正在检查… while the first check runs, 停止检查 while one runs over a result (which stays), disabled while a clean runs. It stays one button throughout, so the keyboard focus stays on it, and as 停止检查 it takes presses only stopArmed after it shows.
func recheckButton(cx *core.Scope, p *page) core.Node {
	cleaning := core.NewMemo(cx, p.cleaning)
	over := core.NewMemo(cx, func() bool { return p.checking() && p.result() != nil })
	armed := core.NewSignal(cx, false)
	cx.Effect(func() {
		armed.Set(false)
		if over.Get() {
			cx.AfterFunc(stopArmed, func() { armed.Set(true) })
		}
	})
	press := func() {
		switch {
		case !over.GetUntracked():
			p.checkNow()
		case armed.GetUntracked():
			p.ov.StopCheck()
		}
	}
	return core.Dynamic(func(*core.Scope) core.Node {
		b := kit.Button{Variant: kit.ButtonGhost, TestID: "recheck", OnPress: press, Disabled: cleaning.Get,
			Busy: func() bool { return p.checking() && !over.Get() },
			TextFunc: func() string {
				switch {
				case over.Get():
					return "停止检查"
				case p.checking():
					return "正在检查…"
				}
				return "重新检查"
			}}
		if cleaning.Get() {
			b.Label, b.Tooltip = "正在清理，完成后再检查", "正在清理，完成后再检查"
		}
		return core.Func(func(*core.Scope) core.Node { return b })
	})
}

// checkRefused is why 重新检查 was refused, while it holds.
func checkRefused(p *page) core.Node {
	return core.View{Style: core.Style{AlignItems: core.AlignCenter}, Children: []core.Node{kit.ErrorLine{Text: p.checkErr.Get, TestID: "recheck-error"}}}
}

// failedBlock is the conclusion of a failed check with no result to keep (state F).
func failedBlock(p *page) core.Node {
	return core.View{Role: core.RoleRegion, Label: "检查结果", TestID: "conclusion",
		Style:    core.Style{AlignItems: core.AlignCenter, Gap: 12, Padding: core.Edges{Top: 24, Bottom: 8}, TextAlign: core.TextAlignCenter},
		Children: []core.Node{checkFailed(p)}}
}

// checkFailed is 「这次检查没有完成：…」 and 重试.
func checkFailed(p *page) core.Node {
	return core.View{Style: core.Style{AlignItems: core.AlignCenter, Gap: 10}, Children: []core.Node{
		kit.ErrorLine{TestID: "check-error", Text: func() string {
			if e := p.state().Error; e != "" {
				return "这次检查没有完成：" + e
			}
			return ""
		}},
		kit.Button{Text: "重试", Variant: kit.ButtonGhost, TestID: "check-retry", OnPress: p.checkNow, Disabled: p.cleaning},
	}}
}

// stoppedBlock is no result and no check (state G): the one button that starts a check.
func stoppedBlock(p *page) core.Node {
	return core.View{Style: core.Style{FlexShrink: 0, AlignItems: core.AlignCenter, Gap: 4}, Children: []core.Node{
		kit.EmptyState{Glyph: "page/overview", Tone: kit.ToneViolet, Title: "检查已停止", Description: "随时可以重新开始，检查只读取，不会删除任何东西。",
			Action: kit.Button{Text: "开始检查", Variant: kit.ButtonLight, Large: true, TestID: "check-start", OnPress: p.checkNow, Disabled: p.cleaning}},
		kit.ErrorLine{Text: p.checkErr.Get, TestID: "check-error"},
	}}
}

// resultBody is the page with a result (states B to F): the conclusion, the disk, the notices, the cards, what needs judgement, the Trash, the sources, what grew and the other checks.
func resultBody(cx *core.Scope, p *page) core.Node {
	displayed := core.NewMemoFunc(cx, p.displayed, nil)
	hasCards := core.NewMemo(cx, func() bool { return len(displayed.Get()) > 0 })
	conclusion := core.Dynamic(func(*core.Scope) core.Node {
		if hasCards.Get() {
			return core.Func(func(*core.Scope) core.Node {
				return kit.Appear{Entrance: kit.EnterRise, Style: core.Style{AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{figure(p)}}
			})
		}
		return core.Func(func(*core.Scope) core.Node {
			return kit.EmptyState{Glyph: "check", Tone: kit.ToneGreen, TestID: "overview-empty", Title: "没有需要清理的内容",
				Description: "系统垃圾、开发者垃圾和安装包都很干净。Spruce 会定期再看看。"}
		})
	})
	failed := func() bool { return p.state().Error != "" && !p.checking() }
	head := core.View{Role: core.RoleRegion, Label: "检查结果", TestID: "conclusion",
		Style: core.Style{FlexShrink: 0, AlignItems: core.AlignCenter, Gap: 6, Padding: core.Edges{Top: 12, Bottom: 4}, TextAlign: core.TextAlignCenter},
		Children: []core.Node{
			conclusion, checkedAt(cx, p),
			core.Show{When: failed, Then: func(*core.Scope) core.Node { return checkFailed(p) }},
			kit.ErrorLine{Text: p.checkErr.Get, TestID: "recheck-error"},
		}}
	return core.Group{
		head,
		diskRow(cx, p),
		notices(cx, p),
		core.Show{When: hasCards.Get, Then: func(cx *core.Scope) core.Node { return cards(cx, p, displayed.Get) }},
		review(cx, p),
		trashRow(cx, p),
		browse(cx, p),
		growthCard(cx, p),
		statusCards{},
	}
}

// figure is the conclusion of a result with cards: 「可以放心释放」, the figure and how many cards.
func figure(p *page) core.Node {
	low := func() bool { d := p.state().Disk; return d != nil && d.Low }
	auto := func() *app.AutoRun {
		a := p.state().LastAuto
		if !p.canUndo() || a == nil || a.Trashed <= 0 || p.now.Get().Sub(a.At) > autoRecent || a.At.Equal(p.autoSeen.Get()) {
			return nil
		}
		// Told now, so not again by a page opened later.
		seenAuto.Store(p.ov, a.At)
		return a
	}
	return core.Group{
		core.Text{ContentFunc: func() string { return ConclusionLabel(low()) }, TestID: "conclusion-label",
			Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontCardTitle, Color: kit.ColorTextSecondary}},
		gradientFigure(func() string {
			if r := p.result(); r != nil {
				return format.Bytes(r.Headline)
			}
			return ""
		}, "conclusion-value"),
		core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, MaxWidth: core.Pt(600)},
			Children: []core.Node{
				bodyText(func() string { return ConclusionSub(p.recs()) }, kit.ColorTextMuted, "conclusion-sub"),
				core.Show{When: func() bool { return auto() != nil }, Then: func(*core.Scope) core.Node {
					return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter}, Children: []core.Node{
						bodyText(func() string {
							if a := auto(); a != nil {
								return AutoJustRanText(a.Trashed)
							}
							return ""
						}, kit.ColorTextMuted, ""),
						kit.Link{Text: fixed("查看"), TestID: "auto-just-ran", OnPress: func() {
							if a := auto(); a != nil {
								p.autoSeen.Set(a.At)
							}
							p.nav.GoHistory(history.KindAuto, "")
						}},
					}}
				}},
			}},
		core.Func(func(cx *core.Scope) core.Node { return cleanAllRow(cx, p) }),
	}
}

// cleanAllRow is 全部清理 under the figure, while it does more than one card's button on a result that may be cleaned: one question with the plan, then one clean of every card's checked items but a command's (AllOf); while it runs, its progress and 停止 take its place.
func cleanAllRow(cx *core.Scope, p *page) core.Node {
	all := core.NewMemoFunc(cx, func() CleanAll { return AllOf(p.recs(), p.choice.Choice.Get(), p.scan()) }, nil)
	bytes := core.NewMemo(cx, func() int64 { a := all.Get(); return cleanflow.SelectedTotals(a.Categories, a.Sel).Bytes })
	action := core.NewMemo(cx, func() advice.Action {
		a := all.Get()
		return advice.ActionOf(p.scan(), cleanflow.Refs(a.Categories, a.Sel))
	})
	running := core.NewMemo(cx, func() bool { o, ok := p.ov.Running(); return ok && o.All })
	offered := core.NewMemo(cx, func() bool { return len(all.Get().Cards) > 1 && p.actionable() && !p.checking() })
	cleaning := core.NewMemo(cx, p.cleaning)
	ask := p.asker(cx)
	press := func() {
		a := all.GetUntracked()
		if len(a.Cards) == 0 {
			return
		}
		d := cleanflow.CleanPlanDialog(a.Categories, a.Sel)
		d.TestID = "all-confirm"
		act := action.GetUntracked()
		ask(d, func() { p.cleanAll(a, act) })
	}
	box := core.Style{Width: core.Pct(100), MaxWidth: core.Pt(420), AlignItems: core.AlignCenter, Margin: core.Edges{Top: 10}}
	return core.Dynamic(func(*core.Scope) core.Node {
		switch {
		case running.Get():
			return core.Func(func(*core.Scope) core.Node {
				return core.View{Style: box, Children: []core.Node{core.View{Style: core.Style{AlignSelf: core.AlignStretch}, Children: []core.Node{
					progressRow("clean-all", action.Get, bytes.Get, p.ov.CleanProgress, p.ov.StopClean)}}}}
			})
		case offered.Get():
			b := kit.Button{Variant: kit.ButtonLight, Large: true, TestID: "clean-all", OnPress: press, Disabled: cleaning.Get,
				TextFunc: func() string { return "全部清理 " + format.Bytes(bytes.Get()) + "…" }}
			if cleaning.Get() {
				b.Label, b.Tooltip = "正在清理另一项，完成后再试", "正在清理另一项，完成后再试"
			}
			return core.Func(func(*core.Scope) core.Node { return core.View{Style: box, Children: []core.Node{b}} })
		}
		return core.Func(func(*core.Scope) core.Node { return core.Group{} })
	})
}

// checkedAt is the time line under the conclusion: when it was checked, that a check updates it, why a scheduled check waits, and that automatic rules are on.
func checkedAt(cx *core.Scope, p *page) core.Node {
	stale := func() bool {
		r := p.result()
		return r != nil && (r.Stale || p.state().Error != "")
	}
	warn := func() bool { return stale() && !p.checking() }
	style := func() core.Style {
		c := kit.ColorTextMuted
		if warn() {
			c = kit.ColorWarning
		}
		return core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: c}
	}
	return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, ColumnGap: 6},
		Children: []core.Node{
			core.Show{When: p.checking, Then: func(*core.Scope) core.Node { return kit.Spinner{Size: 14, Label: "正在检查"} }},
			core.Text{TestID: "checked-at", StyleFunc: style, ContentFunc: func() string {
				r := p.result()
				if r == nil {
					return ""
				}
				return CheckedAtText(r.CheckedAt, stale(), p.checking(), p.progress(), p.now.Get())
			}},
			core.Show{When: func() bool { return p.state().Waiting != "" && !p.checking() }, Then: func(*core.Scope) core.Node {
				return core.Text{TestID: "check-waiting", StyleFunc: style, ContentFunc: func() string { return "· " + p.state().Waiting }}
			}},
			core.Show{When: func() bool { return p.canUndo() && AnyRuleOn(p.stores.Settings.Value().Automation) }, Then: func(*core.Scope) core.Node {
				return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter}, Children: []core.Node{
					core.Text{Content: "·", StyleFunc: style},
					kit.Link{Text: fixed("自动清理已开启"), TestID: "auto-summary", OnPress: func() { p.nav.GoSettings(nav.SettingsAuto) }},
				}}
			}},
		}}
}

// diskRow is the system volume's free space under a meter; when it runs low, the way to 空间透视 and, while the alert is off, 开启提醒.
func diskRow(cx *core.Scope, p *page) core.Node {
	disk := func() app.DiskState {
		if d := p.state().Disk; d != nil {
			return *d
		}
		return app.DiskState{}
	}
	has := func() bool { return p.state().Disk != nil }
	low := func() bool { return has() && disk().Low }
	offer := func() bool {
		return low() && p.stores.Settings.Loaded() && !p.stores.Settings.Value().General.LowDiskAlert
	}
	return core.Show{When: has, Then: func(*core.Scope) core.Node {
		return core.View{Role: core.RoleRegion, Label: "磁盘空间", TestID: "disk", Style: core.Style{Gap: 8, FlexShrink: 0}, Children: []core.Node{
			core.Text{TestID: "disk-text", ContentFunc: func() string { return DiskText(disk()) }, StyleFunc: func() core.Style {
				c := kit.ColorTextSecondary
				if low() {
					c = kit.ColorWarning
				}
				return core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: c, FontVariant: core.FontTabularNums}
			}},
			kit.Meter{Value: func() float64 { return UsedFraction(disk()) }, Height: 6, Label: "已用空间", TestID: "disk-meter",
				Level: func() kit.Level {
					if low() {
						return kit.LevelCritical
					}
					return kit.LevelNormal
				}},
			core.Show{When: low, Then: func(*core.Scope) core.Node {
				v := row(8, bodyText(fixed("空间不多了"), kit.ColorWarning, ""), kit.Link{Text: fixed("看看是什么占用了空间"), To: to(nav.Space), TestID: "space-link"})
				v.TestID = "disk-low"
				return v
			}},
			core.Show{When: offer, Then: func(cx *core.Scope) core.Node {
				v := row(12, bodyText(fixed("空间不足时让 Spruce 提醒你"), kit.ColorTextMuted, ""),
					kit.Button{Text: "开启提醒", Variant: kit.ButtonAccent, TestID: "enable-alert", OnPress: func() {
						next := p.stores.Settings.Value()
						next.General.LowDiskAlert = true
						p.stores.Settings.Save(next, nil)
						// The user asked for it: the system's prompt, the first time, comes with that.
						cx.RequestNotificationPermission(nil)
					}})
				v.TestID = "low-disk-offer"
				return v
			}},
		}}
	}}
}

// notices are what the result leaves out: sources that failed, locations Spruce may not read (macOS) and the folders a background check skipped.
func notices(cx *core.Scope, p *page) core.Node {
	failedSources := core.NewMemoFunc(cx, func() []cleanup.SourceState {
		var out []cleanup.SourceState
		for _, s := range p.scan().Sources {
			if s.Status == cleanup.SourceFailed {
				out = append(out, s)
			}
		}
		return out
	}, slices.Equal)
	failed := core.Dynamic(func(*core.Scope) core.Node {
		list := failedSources.Get()
		nodes := make([]core.Node, len(list))
		for i, s := range list {
			text := nav.Route(s.ID).Meta().Title + "没能检查完成：" + cmp.Or(s.Error, "没能检查完成")
			nodes[i] = kit.Notice{Tone: kit.LevelWarning, TestID: "source-failed-" + s.ID, Text: fixed(text)}
		}
		return core.Func(func(*core.Scope) core.Node { return core.Group(nodes) })
	})
	perm := p.stores.Permission
	denied := func() bool {
		return runtime.GOOS != "windows" && p.scan().Denied > 0 && shared.NeedsFullDiskAccess(perm.FullDiskAccess())
	}
	skipped := core.NewMemoFunc(cx, func() []string {
		if r := p.result(); r != nil {
			return SkippedNames(p.deps.Home, r.Skipped)
		}
		return nil
	}, slices.Equal)
	return core.View{Style: core.Style{Gap: 8, FlexShrink: 0}, Children: []core.Node{
		failed,
		core.Show{When: denied, Then: func(*core.Scope) core.Node {
			n := core.Untracked(cx, func() int64 { return p.scan().Denied })
			return shared.PermissionAction{Text: "有 " + strconv.FormatInt(n, 10) + " 个位置没有读取权限，结果可能不完整", Start: true, TestID: "denied-notice",
				OnGuided: func(st maintenance.PermissionState) {
					if st == maintenance.PermissionGranted {
						p.checkNow()
					}
				}}
		}},
		core.Show{When: func() bool { return len(skipped.Get()) > 0 }, Then: func(*core.Scope) core.Node {
			return row(12,
				kit.Notice{TestID: "skipped-notice", Text: func() string { return SkippedText(skipped.Get()) }},
				kit.Button{Text: "检查这些位置", Variant: kit.ButtonGhost, TestID: "check-skipped", OnPress: p.checkNow, Disabled: func() bool { return p.checking() || p.cleaning() }})
		}},
	}}
}

// cards are the recommendation cards, the first shownCards and then 再显示 {n} 条建议.
func cards(cx *core.Scope, p *page, displayed func() []advice.Recommendation) core.Node {
	shown := core.NewMemoFunc(cx, func() []advice.Recommendation {
		list := displayed()
		if !p.more.Get() && len(list) > shownCards {
			return list[:shownCards]
		}
		return list
	}, nil)
	hidden := func() int {
		if p.more.Get() {
			return 0
		}
		return max(0, len(displayed())-shownCards)
	}
	return core.View{Role: core.RoleRegion, Label: "建议", TestID: "recs", Style: core.Style{Gap: 12, FlexShrink: 0}, Children: []core.Node{
		kit.SectionTitle("建议"),
		core.Each(shown.Get, func(r advice.Recommendation) string { return r.ID }, func(cx *core.Scope, r func() advice.Recommendation, i func() int) core.Node {
			return recCard(cx, p, r, i)
		}),
		core.Show{When: func() bool { return hidden() > 0 }, Then: func(*core.Scope) core.Node {
			return core.View{Style: core.Style{AlignItems: core.AlignStart}, Children: []core.Node{
				kit.Button{Variant: kit.ButtonLink, TestID: "recs-more", OnPress: func() { p.more.Set(true) },
					TextFunc: func() string { return "再显示 " + strconv.Itoa(hidden()) + " 条建议" }},
			}}
		}},
	}}
}
