// Package cleanflow is what 概览 and its drill-down views (系统垃圾, 开发者, 安装包) share to list and clean the items of a check: the selection and its pure logic (the clean plan, the confirmation), the result list (CategoryList, ItemRow), the confirmation and the cleaning overlay, and Drill, the controller of a drill-down view over store.Overview, with the parts of its page (DrillPage, DrillStatus, DrillNotices, DrillLayers).
package cleanflow

import (
	"context"
	"slices"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/async"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/store"
)

// progressEvery bounds how often a clean's progress reaches the view; only the latest event counts (async.Feed).
const progressEvery = 80 * time.Millisecond

// errorToast is how long a toast with a clean's refusal stays.
const errorToast = 6 * time.Second

// Toaster words the toast after a clean of a drill-down view from the action of what it cleaned (advice.ActionOf), what the clean did and whether moves to the Trash can be undone: overview.CleanToast, which 概览's cards use too.
type Toaster func(a advice.Action, out app.OverviewOutcome, canUndo bool) store.Toast

// NeedsConfirm reports whether a clean of plan asks first: anything deleted for good or cleaned by a command, or a move to the Trash that cannot be undone (Windows). A move to the Trash that can be undone runs at once and offers 撤销 in its toast.
func NeedsConfirm(plan Plan, canUndo bool) bool {
	return len(plan.Permanent) > 0 || len(plan.Commands) > 0 || !canUndo
}

// Drill is a drill-down view's controller: the categories of its source in 概览's latest check (advice.Drill), what is checked among their items (the app's Choice, which 概览's cards share), the clean it runs through store.Overview, its confirmation and its toast. The view never scans; a check is 概览's controller's, which the view starts only when opened on a result it may not act on. Make it in the view's component (NewDrill), whose scope stops its clean (after the current item) when the view goes; its methods run on the engine goroutine.
type Drill struct {
	// Source is the merged scan's source, e.g. "system-junk".
	Source string
	// Selection is what is checked, by merged category ID: the app's Choice (Kept), which 概览's cards share and which follows newer checks. Change changes it for the source.
	Selection *core.Memo[Selection]
	// Progress is the latest event of the view's clean; Sub the line under the cleaning overlay's title.
	Progress *core.Signal[cleanup.Progress]
	Sub      *core.Signal[string]
	// Dialog is the confirmation open, nil for none.
	Dialog *core.Signal[*kit.ConfirmDialog]
	// CheckErr is why the last 重新检查 was refused.
	CheckErr *core.Signal[string]
	// Page names the view's scrolling page, which scrolls to the category it was opened on.
	Page *core.Ref
	// Reveal is the category the view was opened on (nav.Navigator.GoDrill), "" for none.
	Reveal string

	cx       *core.Scope
	ctx      context.Context
	stores   *store.Stores
	ov       *store.Overview
	kept     *Kept
	toast    Toaster
	cats     *core.Memo[[]cleanup.Category]
	cleaning *core.Signal[bool]
	// reading is set while the Trash is measured for the confirmation of a clean that empties it.
	reading *core.Signal[bool]
	// undoShown reports whether the toast offers 撤销, which emptying the Trash would make fail.
	undoShown *core.Memo[bool]
	cancel    context.CancelFunc
	seq       int
}

// NewDrill makes the controller of the drill-down view of source in cx, the view's scope; toast words its toasts. It takes the category to reveal from the navigator, which opens, and starts a foreground check when 概览's controller first answers with no result or one that may not be acted on, as 概览 does when it opens.
func NewDrill(cx *core.Scope, source string, toast Toaster) *Drill {
	stores := store.Use(cx)
	kept := UseKept(cx)
	d := &Drill{Source: source, cx: cx, ctx: cx.Context(), stores: stores, ov: stores.Overview, kept: kept, toast: toast,
		Selection: core.NewMemoFunc(cx, func() Selection { return kept.Choice.Get().Sel }, nil),
		Progress:  core.NewSignalFunc(cx, cleanup.Progress{}, nil),
		Sub:       core.NewSignal(cx, ""),
		Dialog:    core.NewSignal[*kit.ConfirmDialog](cx, nil),
		CheckErr:  core.NewSignal(cx, ""),
		Page:      new(core.Ref),
		cleaning:  core.NewSignal(cx, false),
		reading:   core.NewSignal(cx, false),
	}
	d.undoShown = core.NewMemo(cx, func() bool { t := stores.Toasts.Current(); return t != nil && t.UndoRun != "" })
	if n := nav.Use(cx); n != nil {
		d.Reveal = n.TakeReveal()
	}
	if d.Reveal != "" {
		kept.SetOpen(d.Reveal, true)
	}
	d.cats = core.NewMemoFunc(cx, func() []cleanup.Category {
		if r := d.Result(); r != nil {
			return advice.Drill(r.Scan, source)
		}
		return nil
	}, nil)
	// The menu's 重新检查 (⌘R) checks again from the view, as its header's does.
	nav.UseCommands(cx, nav.Commands{Refresh: d.CheckNow, Busy: func() bool { return d.Checking() || d.Busy() }})
	decided := false
	cx.Effect(func() {
		st := d.State()
		if decided || !d.ov.Connected() || unanswered(st) {
			return
		}
		decided = true
		if shouldCheck(st) {
			cx.Untrack(d.CheckNow)
		}
	})
	return d
}

// shouldCheck reports whether opening a drill-down view on st starts a foreground check: nothing runs, and there is no result (and no failure to show) or the one there comes from the cache, may be outdated or may not be acted on.
func shouldCheck(st app.OverviewState) bool {
	if st.Phase != app.OverviewIdle {
		return false
	}
	if st.Result == nil {
		return st.Error == ""
	}
	return st.Result.FromCache || st.Result.Stale || !st.Result.Actionable
}

// unanswered reports whether st is the store's state before the controller has loaded (OverviewState.Loaded).
func unanswered(st app.OverviewState) bool { return !st.Loaded }

// State is 概览's controller's state; reading it subscribes.
func (d *Drill) State() app.OverviewState { return d.ov.State() }

// Result is the latest check's, nil before one; reading it subscribes.
func (d *Drill) Result() *app.OverviewResult { return d.State().Result }

// Categories are the source's categories of the latest check, in scan order; reading it subscribes.
func (d *Drill) Categories() []cleanup.Category { return d.cats.Get() }

// Scan is the latest check's merged scan, the zero ScanResult before one.
func (d *Drill) Scan() cleanup.ScanResult {
	if r := d.Result(); r != nil {
		return r.Scan
	}
	return cleanup.ScanResult{}
}

// SourceState is how the source did in the latest check, and whether the check reported it.
func (d *Drill) SourceState() (cleanup.SourceState, bool) {
	for _, s := range d.Scan().Sources {
		if s.ID == d.Source {
			return s, true
		}
	}
	return cleanup.SourceState{}, false
}

// Checking reports whether a check runs.
func (d *Drill) Checking() bool { return d.State().Phase == app.OverviewChecking }

// Actionable reports whether the result may be cleaned now (app.OverviewResult.Actionable).
func (d *Drill) Actionable() bool {
	r := d.Result()
	return r != nil && r.Actionable
}

// CanUndo reports whether moves to the Trash can be undone (macOS).
func (d *Drill) CanUndo() bool { return d.State().CanUndo }

// Cleaning reports whether the view's own clean runs.
func (d *Drill) Cleaning() bool { return d.cleaning.Get() }

// Busy reports whether any clean runs: the view's, one the store started elsewhere (概览, 撤销) or one 概览's controller runs. One thing at a time.
func (d *Drill) Busy() bool {
	return d.cleaning.Get() || d.ov.Busy() || d.State().Phase == app.OverviewCleaning
}

// BusyElsewhere reports whether a clean or an 撤销 runs that is not the view's own: 清理 waits for it (BusyText).
func (d *Drill) BusyElsewhere() bool { return d.Busy() && !d.Cleaning() }

// BusyText is why 清理 waits while BusyElsewhere.
const BusyText = "正在清理其他项目…"

// CanClean reports whether 清理 may act on a selection of count items: a result to act on, no check and no clean running, the Trash not being measured, and no Trash checked while the toast offers 撤销 (TrashWait).
func (d *Drill) CanClean(count int) bool {
	return count > 0 && d.Actionable() && !d.Checking() && !d.Busy() && !d.reading.Get() && !d.TrashWait()
}

// Failed reports whether the source failed in the latest check.
func (d *Drill) Failed() bool {
	s, ok := d.SourceState()
	return ok && s.Status == cleanup.SourceFailed
}

// TrashWait reports whether the selection empties the Trash while the toast offers 撤销: emptying it would destroy what 撤销 puts back, so 清理 waits for the toast to go, as 概览's 清空废纸篓… does.
func (d *Drill) TrashWait() bool {
	return d.undoShown.Get() && TrashChecked(d.cats.Get(), d.Selection.Get())
}

// TrashChecked reports whether sel checks the Trash (cleanup.KindTrash) among categories, which a clean empties for good as it is when confirmed.
func TrashChecked(categories []cleanup.Category, sel Selection) bool {
	for _, c := range categories {
		if c.Kind == cleanup.KindTrash && sel.Count(c.ID) > 0 {
			return true
		}
	}
	return false
}

// withoutTrash returns categories without the Trash (cleanup.KindTrash): the confirmation's summary and plan rows leave it to the Trash line, which has its live figures, so the scan's single Trash item and its older size are not shown beside them.
func withoutTrash(categories []cleanup.Category) []cleanup.Category {
	return slices.DeleteFunc(slices.Clone(categories), func(c cleanup.Category) bool { return c.Kind == cleanup.KindTrash })
}

// CleanSub is the line under the cleaning overlay for the checked items of categories: as many as the confirmation counts, which leaves the Trash to its own line, and whether the Trash is emptied too.
func CleanSub(categories []cleanup.Category, sel Selection) string {
	bin := format.TrashName()
	n := SelectedTotals(withoutTrash(categories), sel).Count
	switch {
	case !TrashChecked(categories, sel):
		return "共 " + strconv.Itoa(n) + " 项"
	case n == 0:
		return "清空" + bin
	}
	return "共 " + strconv.Itoa(n) + " 项，并清空" + bin
}

// EmptyTrashConfirm is the question when the Trash is all that is checked, as 概览's 清空废纸篓… asks it: line is TrashLine.
func EmptyTrashConfirm(line string) kit.ConfirmDialog {
	bin := format.TrashName()
	return kit.ConfirmDialog{TestID: "confirm", Title: "清空" + bin + "？", Message: core.Text{Content: line, TestID: "confirm-trash"}, ConfirmLabel: "清空", Danger: true}
}

// TrashLine is the confirmation's line about emptying the Trash, from the live Trash (Overview.TrashInfo) as 概览's 清空废纸篓 dialog words it: what is deleted for good and what Spruce moved there that could no longer be put back.
func TrashLine(info app.TrashInfo, err error) string {
	bin := format.TrashName()
	if err != nil {
		return bin + "里的全部项目会永久删除，无法恢复。"
	}
	text := bin + "里的 " + strconv.Itoa(info.Items) + " 项（" + format.Bytes(info.Bytes) + "）会永久删除，无法恢复。"
	if info.Restorable > 0 {
		text += "其中 " + strconv.Itoa(info.Restorable) + " 项是 Spruce 移到" + bin + "的，清空后不能再放回原处。"
	}
	return text
}

// withTrashLine adds line, the Trash line (TrashLine), under the question's message. Emptying the Trash deletes for good whatever the rest does, so the question is the danger kind, focusing 取消 first.
func withTrashLine(q kit.ConfirmDialog, line string) kit.ConfirmDialog {
	q.Danger = true
	if q.Message == nil {
		q.Lines = append(slices.Clone(q.Lines), line)
		return q
	}
	q.Message = core.View{Style: core.Style{AlignItems: core.AlignStretch}, Children: []core.Node{
		q.Message,
		core.Text{Content: line, TestID: "confirm-trash", Style: core.Style{Margin: core.Edges{Top: 14}}},
	}}
	return q
}

// Change replaces what is checked among the source's categories with sel's checks of them, leaving other sources' as they are; from now on newer results keep what is still there in the source.
func (d *Drill) Change(sel Selection) {
	cats := d.cats.GetUntracked()
	ch := d.kept.Choice.GetUntracked()
	d.kept.Choice.Set(ch.Change(WithinGroup(ch.Sel, cats, sel), cats, d.Source))
}

// Open reports whether the row key of the view's lists (a category ID or a ProjectKey) is open, as the user left it; reading it subscribes.
func (d *Drill) Open(key string) bool { return d.kept.Open(key) }

// SetOpen opens or closes the row key.
func (d *Drill) SetOpen(key string, on bool) { d.kept.SetOpen(key, on) }

// CheckNow starts a foreground check; a refusal shows in the status row.
func (d *Drill) CheckNow() {
	d.CheckErr.Set("")
	d.ov.CheckNow(func(err error) {
		if err != nil {
			d.CheckErr.Set(err.Error())
		}
	})
}

// ScrollTo scrolls the page to card, the category the view was opened on, once it is laid out.
func (d *Drill) ScrollTo(card *core.Ref) {
	card.Measure(func(_, b core.Bounds, ok bool) {
		content, ok2 := d.Page.ContentBounds()
		if ok && ok2 {
			d.Page.ScrollTo(core.Point{Y: max(0, b.Origin.Y-content.Origin.Y-12)}, !d.cx.Env().ReduceMotion())
		}
	})
}

// RequestClean cleans the checked items of cats (the part of the source the asking list shows), asking first with confirm's question when NeedsConfirm says so; sub is the line under the cleaning overlay. It names every checked item, so the clean acts on what the user saw when asking. It does nothing while CanClean is false. A clean that empties the Trash measures it first (Overview.TrashInfo) and asks with the live figures, because the Trash emptied is the one there when the user confirms: the Trash alone asks 清空废纸篓？ as 概览 does; with other items the summary and the plan cover those, and the Trash line under them the Trash, in a question of the danger kind however the rest goes.
func (d *Drill) RequestClean(cats []cleanup.Category, confirm func(cats []cleanup.Category, sel Selection) kit.ConfirmDialog, sub string, returnFocus *core.Ref) {
	sel := d.Selection.GetUntracked()
	req := ItemsRequest(cats, sel)
	if !core.Untracked(d.cx, func() bool { return d.CanClean(len(req.ItemIDs)) }) {
		return
	}
	action := advice.ActionOf(core.Untracked(d.cx, d.Scan), Refs(cats, sel))
	if !NeedsConfirm(CleanPlan(cats, sel), core.Untracked(d.cx, d.CanUndo)) {
		d.run(req, action, sub)
		return
	}
	if TrashChecked(cats, sel) {
		d.reading.Set(true)
		d.ov.TrashInfo(d.cx, d.ctx, func(info app.TrashInfo, err error) {
			d.reading.Set(false)
			line, rest := TrashLine(info, err), withoutTrash(cats)
			if !slices.ContainsFunc(rest, func(c cleanup.Category) bool { return sel.Count(c.ID) > 0 }) {
				d.ask(EmptyTrashConfirm(line), req, action, sub, returnFocus)
				return
			}
			d.ask(withTrashLine(confirm(rest, sel), line), req, action, sub, returnFocus)
		})
		return
	}
	d.ask(confirm(cats, sel), req, action, sub, returnFocus)
}

// ask opens q, which cleans req when confirmed.
func (d *Drill) ask(q kit.ConfirmDialog, req cleanup.CleanRequest, action advice.Action, sub string, returnFocus *core.Ref) {
	// The question goes with Dialog, before it could hand the focus back itself.
	done := func() {
		d.Dialog.Set(nil)
		if returnFocus != nil {
			returnFocus.Focus()
		}
	}
	q.OnConfirm = func() {
		done()
		d.run(req, action, sub)
	}
	q.OnCancel = done
	d.Dialog.Set(&q)
}

// run cleans req through store.Overview for the source, then shows the toast and refreshes the disks and the Trash.
func (d *Drill) run(req cleanup.CleanRequest, action advice.Action, sub string) {
	if core.Untracked(d.cx, func() bool { return !d.CanClean(len(req.ItemIDs)) }) {
		return
	}
	undo := core.Untracked(d.cx, d.CanUndo)
	d.seq++
	mine := d.seq
	stores, toast := d.stores, d.toast
	d.cx.Batch(func() {
		d.Progress.Set(cleanup.Progress{})
		d.Sub.Set(sub)
		d.cleaning.Set(true)
	})
	feed := async.NewFeed(d.cx, progressEvery, func(p cleanup.Progress) {
		if mine == d.seq && d.cleaning.GetUntracked() {
			d.Progress.Set(p)
		}
	})
	// The view going away stops its clean after the current item (the cleaning overlay holds the sidebar meanwhile, so only quitting does); the toast comes from the app's scope, so what was done is still shown.
	stop := d.ov.Clean(d.cx, req, app.CleanOrigin{Source: d.Source}, feed.Send, func(app.OverviewOutcome, error) {
		if mine != d.seq {
			return
		}
		d.cancel = nil
		d.cx.Batch(func() {
			d.cleaning.Set(false)
			d.Progress.Set(cleanup.Progress{})
		})
	}, func(out app.OverviewOutcome, err error) {
		stores.Locations.Bump()
		stores.Trash.Bump()
		if err != nil {
			stores.Toasts.Show(store.Toast{Text: err.Error(), Duration: errorToast})
			return
		}
		if toast != nil {
			stores.Toasts.Show(toast(action, out, undo))
		}
	})
	d.cancel = stop
	context.AfterFunc(d.ctx, stop)
}

// Stop ends the view's clean after the current item.
func (d *Drill) Stop() {
	if d.cancel != nil {
		d.cancel()
	}
}
