package cleanflow_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"sync/atomic"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/cleanflow/cleanflowfake"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/store"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// wait is real time; the race detector on a loaded machine slows every frame.
const wait = 20 * time.Second

// macWording skips a test whose expectations are the macOS wording and SI sizes.
func macWording(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
}

var modTime = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

// junk are a check's 系统垃圾 categories: app caches (two items, checked), logs (unchecked, with a warning) and downloads that go to the Trash (checked), in merged IDs.
func junk() []cleanup.Category {
	return []cleanup.Category{
		{ID: "system-junk:caches", Source: "system-junk", Title: "应用缓存", Description: "应用临时保存的数据，删除后会自动重建", Icon: "cache", Kind: cleanup.KindCache,
			Deletion: cleanup.DeletePermanent, Selected: true, Size: 3_000_000, Items: []cleanup.Item{
				{ID: "a", Path: "/Users/me/Library/Caches/com.example.a", Size: 2_000_000, ModTime: modTime, Dir: true, Selected: true},
				{ID: "b", Path: "/Users/me/Library/Caches/com.example.b", Size: 1_000_000, ModTime: modTime, Note: "3 个月未修改", Selected: true},
			}},
		{ID: "system-junk:logs", Source: "system-junk", Title: "日志", Description: "应用和系统的运行记录", Warning: "排查问题时可能用得到", Icon: "log", Kind: cleanup.KindLog,
			Deletion: cleanup.DeletePermanent, Size: 500_000, Items: []cleanup.Item{
				{ID: "c", Path: "/Users/me/Library/Logs/x.log", Size: 500_000, ModTime: modTime},
			}},
		{ID: "system-junk:downloads", Source: "system-junk", Title: "未完成的下载", Description: "中断后留下的半截文件", Icon: "temp", Kind: cleanup.KindCache,
			Deletion: cleanup.DeleteTrash, Selected: true, Size: 10_000_000, Items: []cleanup.Item{
				{ID: "d", Path: "/Users/me/Downloads/a.crdownload", Size: 10_000_000, ModTime: modTime, Selected: true},
			}},
	}
}

// other is a category of another source, which 系统垃圾 does not show.
func other() cleanup.Category {
	return cleanup.Category{ID: "installers:disk-images", Source: "installers", Title: "磁盘映像", Icon: "installer", Kind: cleanup.KindInstaller,
		Deletion: cleanup.DeleteTrash, Selected: true, Size: 7, Items: []cleanup.Item{{ID: "e", Path: "/Users/me/Downloads/x.dmg", Size: 7, ModTime: modTime, Selected: true}}}
}

// stateOf is an actionable result over cats, on macOS (moves to the Trash can be undone).
func stateOf(cats ...cleanup.Category) app.OverviewState {
	res := cleanflowfake.Result(cats...)
	res.Sources = []cleanup.SourceState{{ID: "system-junk", Status: cleanup.SourceDone}, {ID: "installers", Status: cleanup.SourceDone}}
	return app.OverviewState{Phase: app.OverviewIdle, CanUndo: true,
		Result: &app.OverviewResult{CheckedAt: modTime, Mode: app.CheckForeground, Actionable: true, Scan: res}}
}

// ready is stateOf the 系统垃圾 categories and one of another source.
func ready() app.OverviewState { return stateOf(append(junk(), other())...) }

// toastOf is the tests' Toaster: the action and what the clean did.
func toastOf(a advice.Action, out app.OverviewOutcome, canUndo bool) store.Toast {
	t := store.Toast{Text: fmt.Sprintf("%s %d %v", a, out.Result.Removed, canUndo), Duration: time.Hour}
	if canUndo && out.Undoable {
		t.UndoRun = out.RunID
	}
	return t
}

type page struct {
	*nsgotest.App
	ov     *storefake.Overview
	files  *cleanflowfake.Files
	stores *store.Stores
	nav    *nav.Navigator
	shown  *core.Signal[bool]
	tb     testing.TB
}

// start opens 系统垃圾 as a DrillPage on the controller state st.
func start(t *testing.T, st app.OverviewState, before ...func(*nav.Navigator)) *page {
	t.Helper()
	return startSized(t, st, nsgotest.Size(1052, 1200), before...)
}

// startSized is start in a window of size.
func startSized(t *testing.T, st app.OverviewState, size nsgotest.Option, before ...func(*nav.Navigator)) *page {
	t.Helper()
	macWording(t)
	fakes := storefake.New(t.TempDir())
	ov := storefake.NewOverview(st)
	fakes.Overview = ov
	p := &page{ov: ov, files: &cleanflowfake.Files{}, tb: t}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		cleanflow.Provide(cx)
		p.nav = nav.NewNavigator(cx, nav.Overview)
		nav.Key.Provide(cx, p.nav)
		for _, f := range before {
			f(p.nav)
		}
		p.shown = core.NewSignal(cx, true)
		return core.Show{When: p.shown.Get, Then: func(*core.Scope) core.Node {
			return cleanflow.DrillPage{Route: nav.SystemJunk, Toast: toastOf, Notice: func() string { return "检查应用缓存和日志" }, ListLabel: "可以清理的系统垃圾",
				SizeLabel: "可释放", EmptyTitle: "没有找到系统垃圾", EmptyDescription: "很干净", Reveal: p.files.Reveal}
		}}
	}, size)
	p.WaitUntil(wait, func() bool {
		return p.stores.Settings.Loaded() && reflect.DeepEqual(p.stores.Overview.State(), ov.State())
	})
	return p
}

// set publishes st and waits for the view to take it.
func (p *page) set(st app.OverviewState) {
	p.tb.Helper()
	p.ov.Set(st)
	p.WaitUntil(wait, func() bool { return reflect.DeepEqual(p.stores.Overview.State(), p.ov.State()) })
}

func (p *page) checked(selector string) bool { return p.Find(selector).Props.Checked }

func (p *page) toolbar() string { return p.Find("id:category-list >> text:/^\\d+ 类，/").Text }

func (p *page) checks() int { c, _, _, _, _ := p.ov.Calls(); return c }

func (p *page) cleans() []storefake.CleanCall { _, _, c, _, _ := p.ov.Calls(); return c }

// stat returns the figure beside 清理 under label.
func (p *page) stat(label string) string {
	for _, v := range p.Query("text:" + label) {
		kids := v.Parent.Children
		for i, k := range kids {
			if k == v && i+1 < len(kids) {
				return kids[i+1].Text
			}
		}
	}
	return ""
}

// ticking waits for cond, moving the manual clock so throttled progress comes through.
func (p *page) ticking(cond func() bool) {
	p.tb.Helper()
	p.WaitUntil(wait, func() bool {
		if cond() {
			return true
		}
		p.Advance(20 * time.Millisecond)
		return false
	})
}

// The view lists its own source of the check with the check's defaults, under the header with its link back, and starts no check on a result it may act on.
func TestDrillListsItsSource(t *testing.T) {
	p := start(t, ready())
	p.Find("id:drill-system-junk")
	p.Find("role:header 系统垃圾")
	p.Find("id:back")
	p.Find("text:检查应用缓存和日志")
	if p.Exists("id:category-installers:disk-images") {
		t.Error("another source's category shows")
	}
	if !p.checked("id:check-system-junk:caches") || p.checked("id:check-system-junk:logs") || !p.checked("id:check-system-junk:downloads") {
		t.Error("defaults")
	}
	p.Find("id:category-system-junk:logs >> text:排查问题时可能用得到")
	if got := p.toolbar(); got != "3 类，共 13.5 MB，已选 3 项" {
		t.Errorf("toolbar %q", got)
	}
	if got := p.stat("可释放"); got != "13 MB" {
		t.Errorf("可释放 %q", got)
	}
	if p.Exists("id:drill-checking") || p.Exists("id:drill-stale") || p.Exists("id:drill-empty") {
		t.Error("a status row or the empty state shows on an actionable result")
	}
	p.Settle()
	if p.checks() != 0 {
		t.Error("checked again on an actionable result")
	}
}

// Ported from CategoryList.test.tsx: expands to items, checks them one by one and shows the partial state.
func TestCategoryListItems(t *testing.T) {
	p := start(t, ready())
	p.Press("role:button 展开应用缓存")
	if got := len(p.Query("id:items-system-junk:caches >> role:checkbox")); got != 2 {
		t.Fatalf("%d item rows", got)
	}
	p.Find(`id:items-system-junk:caches >> text:"/Users/me/Library/Caches · 3 个月未修改"`)
	p.Press("role:checkbox com.example.a")
	caches := p.Find("id:check-system-junk:caches")
	if !caches.Props.Indeterminate || caches.Props.Checked {
		t.Errorf("category state %+v", caches.Props)
	}
	p.Find("id:category-system-junk:caches >> text:已选 1/2 项")
	p.Press("id:check-system-junk:caches")
	if !p.checked("id:check-system-junk:caches") || !p.checked("role:checkbox com.example.a") {
		t.Error("the category is not checked")
	}
	p.Press("role:button 收起应用缓存")
	p.WaitForGone("id:items-system-junk:caches", wait)
}

// Ported from CategoryList.test.tsx: selects all and none.
func TestCategoryListSelectAllAndNone(t *testing.T) {
	p := start(t, ready())
	p.Press("id:select-all")
	if got := p.toolbar(); got != "3 类，共 13.5 MB，已选 4 项" {
		t.Errorf("toolbar %q", got)
	}
	p.Press("id:select-none")
	for _, v := range p.Query("role:checkbox") {
		if v.Props.Checked || v.Props.Indeterminate {
			t.Errorf("%s is still checked", v.Props.Label)
		}
	}
	if p.Enabled("id:clean") {
		t.Error("清理 is enabled with nothing checked")
	}
}

// Ported from CategoryList.test.tsx: reveals an item in the file manager and reports a failure.
func TestCategoryListReveal(t *testing.T) {
	p := start(t, ready())
	p.files.Err = errors.New("这个项目已经不存在了")
	p.Press("role:button 展开日志")
	p.Press("role:button 在访达中显示 x.log")
	p.WaitFor("id:reveal-error", wait)
	if got := p.files.Revealed(); !reflect.DeepEqual(got, []string{"/Users/me/Library/Logs/x.log"}) {
		t.Errorf("revealed %v", got)
	}
	if got := p.Find("id:reveal-error").Text; got != "这个项目已经不存在了" {
		t.Errorf("error %q", got)
	}
}

// The design's largest case: 3,000 items in one category scroll inside the card and mount only the rows near view.
func TestCategoryListVirtualizesLongLists(t *testing.T) {
	items := make([]cleanup.Item, 3000)
	for i := range items {
		items[i] = cleanup.Item{ID: fmt.Sprintf("i%04d", i), Path: fmt.Sprintf("/Users/me/Library/Caches/item-%04d", i), Size: int64(3000 - i), ModTime: modTime, Selected: true}
	}
	big := cleanup.Category{ID: "system-junk:big", Source: "system-junk", Title: "大量缓存", Icon: "cache", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent,
		Selected: true, Size: 4_501_500, Items: items}
	p := start(t, stateOf(big))
	p.Press("role:button 展开大量缓存")
	list := p.Find("id:items-system-junk:big")
	if list.Frame.Size.Height != 360 {
		t.Errorf("list height %v", list.Frame.Size.Height)
	}
	if n := len(p.QueryMounted("id:items-system-junk:big >> role:checkbox")); n == 0 || n > 100 {
		t.Fatalf("%d rows mounted for 3,000 items", n)
	}
	p.ScrollTo("id:items-system-junk:big", core.Point{Y: 48 * 2000})
	p.WaitUntil(wait, func() bool { return len(p.QueryMounted("role:checkbox item-2000")) == 1 })
	p.Press("role:checkbox item-2000")
	if got := p.stat("已选择"); got != "2999 项" {
		t.Errorf("已选择 %q", got)
	}
}

// A held item (Item.Hold) never starts checked, cannot be checked by hand, by its category or by 全选, and its category counts as all checked without it.
func TestHeldItemsStayUnchecked(t *testing.T) {
	cats := junk()
	cats[0].Items[1].Hold, cats[0].Items[1].Selected = "项目有未提交的改动", false
	cats[0].Selected = false
	p := start(t, stateOf(cats...))
	if !p.checked("id:check-system-junk:caches") {
		t.Error("the category is not all checked without its held item")
	}
	p.Press("role:button 展开应用缓存")
	if !p.Find("role:checkbox com.example.b").Props.Disabled {
		t.Error("the held item can be checked")
	}
	p.Press("id:select-none")
	p.Press("id:select-all")
	if p.checked("role:checkbox com.example.b") {
		t.Error("全选 checked the held item")
	}
	p.Press("id:check-system-junk:caches")
	p.Press("id:check-system-junk:caches")
	if p.checked("role:checkbox com.example.b") || !p.checked("role:checkbox com.example.a") {
		t.Error("the category's checkbox checked the held item")
	}
}

// Moves to the Trash that can be undone run at once, name every item, belong to the source, show the overlay with progress and end in the toast with 撤销.
func TestTrashRunsAtOnceWithUndo(t *testing.T) {
	p := start(t, ready())
	progress := make(chan cleanup.Progress)
	p.ov.OnClean = func(ctx context.Context, _ cleanup.CleanRequest, _ app.CleanOrigin, send func(cleanup.Progress)) (app.OverviewOutcome, error) {
		for {
			select {
			case ev, ok := <-progress:
				if !ok {
					return app.OverviewOutcome{Result: cleanup.CleanResult{Trashed: 10_000_000, Removed: 1}, RunID: "run-1", Undoable: true}, nil
				}
				send(ev)
			case <-ctx.Done():
				return app.OverviewOutcome{}, ctx.Err()
			}
		}
	}
	p.Press("id:check-system-junk:caches")
	p.Press("id:clean")
	p.WaitFor("id:clean-overlay", wait)
	if p.Exists("id:confirm") {
		t.Error("asked before a move that can be undone")
	}
	if !p.stores.Block.Blocked() {
		t.Error("the overlay does not block the shell")
	}
	want := []storefake.CleanCall{{Req: cleanup.CleanRequest{CategoryIDs: []string{"system-junk:downloads"}, ItemIDs: []string{"d"}},
		Origin: app.CleanOrigin{Source: "system-junk"}}}
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	if got := p.cleans(); !reflect.DeepEqual(got, want) {
		t.Errorf("cleans %+v", got)
	}
	p.Find("id:clean-overlay >> text:共 1 项")
	progress <- cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: "/Users/me/Downloads/a.crdownload", Done: 1, Total: 2, Bytes: 5_000_000}
	p.ticking(func() bool { return p.Exists("id:clean-overlay >> text:已处理 1/2 · 已释放 5 MB") })
	close(progress)
	p.WaitForGone("id:clean-overlay", wait)
	p.WaitUntil(wait, func() bool { return p.stores.Toasts.Current() != nil })
	if got := *p.stores.Toasts.Current(); got.Text != "trash 1 true" || got.UndoRun != "run-1" {
		t.Errorf("toast %+v", got)
	}
	if p.stores.Trash.Generation() != 1 {
		t.Error("the clean did not tell the Trash")
	}
}

// Deleting for good asks first with the plan, focusing 取消; Escape and 取消 close it and hand the focus back to 清理; 清理 runs what the plan showed.
func TestPermanentAsksFirst(t *testing.T) {
	p := start(t, ready())
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if got := p.Find("id:plan-message").Text; got != "将清理 3 项，共 13 MB。其中 2 项会永久删除，无法恢复；其余移到废纸篓。" {
		t.Errorf("message %q", got)
	}
	if !p.Find("id:confirm-cancel").Focused {
		t.Error("取消 does not have the focus")
	}
	p.Key("Escape")
	p.WaitForGone("id:confirm", wait)
	if !p.Find("id:clean").Focused {
		t.Error("the focus did not go back to 清理")
	}
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:confirm", wait)
	if len(p.cleans()) != 0 {
		t.Fatal("cleaned after 取消")
	}
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	// A newer check that lands while the question is open changes nothing of what was confirmed.
	next := ready()
	next.Result.CheckedAt = modTime.Add(time.Minute)
	next.Result.Scan.Categories[0].Items = next.Result.Scan.Categories[0].Items[:1]
	p.set(next)
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	want := cleanup.CleanRequest{CategoryIDs: []string{"system-junk:caches", "system-junk:downloads"}, ItemIDs: []string{"a", "b", "d"}}
	if got := p.cleans()[0].Req; !reflect.DeepEqual(got, want) {
		t.Errorf("request %+v", got)
	}
}

// Where a move to the Trash cannot be undone (Windows), it asks first, without the danger tone.
func TestTrashAsksWithoutUndo(t *testing.T) {
	st := ready()
	st.CanUndo = false
	p := start(t, st)
	p.Press("id:check-system-junk:caches")
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if !p.Find("id:confirm-ok").Focused {
		t.Error("清理 does not have the focus")
	}
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	p.WaitUntil(wait, func() bool { return p.stores.Toasts.Current() != nil })
	if got := p.stores.Toasts.Current(); got.Text != "trash 0 false" || got.UndoRun != "" {
		t.Errorf("toast %+v", got)
	}
}

// 停止 ends the clean after the current item; a clean refused shows its reason in the toast.
func TestStopAndRefusal(t *testing.T) {
	p := start(t, ready())
	var calls atomic.Int32
	p.ov.OnClean = func(ctx context.Context, _ cleanup.CleanRequest, _ app.CleanOrigin, _ func(cleanup.Progress)) (app.OverviewOutcome, error) {
		if calls.Add(1) == 1 {
			<-ctx.Done()
			return app.OverviewOutcome{Result: cleanup.CleanResult{Interrupted: true}}, nil
		}
		return app.OverviewOutcome{}, errors.New("Spruce 正在命令行中清理，请稍后再试")
	}
	p.Press("id:check-system-junk:caches")
	p.Press("id:clean")
	p.WaitFor("id:clean-overlay", wait)
	p.Press("id:overlay-cancel")
	p.WaitForGone("id:clean-overlay", wait)
	p.Press("id:clean")
	p.WaitUntil(wait, func() bool {
		t := p.stores.Toasts.Current()
		return t != nil && t.Text == "Spruce 正在命令行中清理，请稍后再试"
	})
}

// While a check runs the view says so and 清理 waits; a result it may not act on says why, offers 重新检查 and was checked again as the view opened.
func TestCheckingAndNotActionable(t *testing.T) {
	st := ready()
	st.Phase = app.OverviewChecking
	p := start(t, st)
	p.Find("id:drill-checking >> text:正在检查…")
	if p.Enabled("id:clean") {
		t.Error("清理 is enabled while checking")
	}

	cached := ready()
	cached.Result.FromCache, cached.Result.Actionable = true, false
	q := start(t, cached)
	q.WaitUntil(wait, func() bool { return q.checks() == 1 })
	if got := q.Find("id:drill-stale-text").Text; got != "重新检查后才能清理" {
		t.Errorf("stale %q", got)
	}
	if q.Enabled("id:clean") {
		t.Error("清理 is enabled on a result it may not act on")
	}
	q.ov.CheckErr = errors.New("正在清理，完成后再检查")
	q.Press("id:drill-recheck")
	q.WaitUntil(wait, func() bool { return q.checks() == 2 })
	q.WaitFor("text:正在清理，完成后再检查", wait)
}

// Opened with no result yet, the view checks; a failed check says why.
func TestNoResult(t *testing.T) {
	p := start(t, app.OverviewState{Phase: app.OverviewIdle, CanUndo: true})
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })
	if got := p.Find("id:drill-stale-text").Text; got != "还没有检查结果" {
		t.Errorf("stale %q", got)
	}
	if p.Exists("id:category-list") || p.Exists("id:clean") {
		t.Error("a list without a result")
	}
	p.set(app.OverviewState{Phase: app.OverviewIdle, CanUndo: true, Error: "没有权限"})
	p.waitText("id:drill-stale-text", "这次检查没有完成：没有权限")
}

func (p *page) waitText(selector, want string) {
	p.tb.Helper()
	p.WaitUntil(wait, func() bool { vs := p.Query(selector); return len(vs) == 1 && vs[0].Text == want })
}

// One thing at a time: a clean elsewhere disables 清理.
func TestBusyElsewhere(t *testing.T) {
	st := ready()
	st.Phase = app.OverviewCleaning
	p := start(t, st)
	if p.Enabled("id:clean") {
		t.Error("清理 is enabled while another clean runs")
	}
	p.set(ready())
	p.WaitUntil(wait, func() bool { return p.Enabled("id:clean") })
}

// Nothing of the source: the empty state; a failed source and unreadable places are told.
func TestEmptyFailedAndDenied(t *testing.T) {
	p := start(t, stateOf(other()))
	p.Find("id:drill-empty >> text:没有找到系统垃圾")

	// A failed source found nothing because it was not read: no clean empty state under its notice, and 重新检查 checks again.
	st := stateOf(other())
	st.Result.Scan.Sources = []cleanup.SourceState{{ID: "system-junk", Status: cleanup.SourceFailed, Error: "读取超时"}}
	p.set(st)
	p.waitText("id:source-failed-system-junk", "系统垃圾没能检查完成：读取超时")
	p.WaitFor("id:drill-failed >> text:暂时没有结果", wait)
	if p.Exists("id:drill-empty") || p.Exists("text:没有找到系统垃圾") {
		t.Error("the clean empty state shows under the failure")
	}
	p.Press("id:drill-retry")
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })
	// While the result may not be acted on, the status row offers 重新检查 and the empty state does not repeat it.
	stale := stateOf(other())
	stale.Result.Scan.Sources = st.Result.Scan.Sources
	stale.Result.Actionable = false
	p.set(stale)
	p.WaitForGone("id:drill-retry", wait)
	p.Find("id:drill-recheck")
	p.Find("id:drill-failed")

	denied := ready()
	denied.Result.Scan.Sources[0].Denied = 2
	p.set(denied)
	p.waitText("id:denied-notice", "有 2 个位置没有读取权限，结果可能不完整")
	if p.Exists("id:drill-empty") || p.Exists("id:source-failed-system-junk") {
		t.Error("the empty state or the failure stays")
	}
}

// GoDrill opens the view with its category expanded.
func TestOpenedOnACategory(t *testing.T) {
	p := start(t, ready(), func(n *nav.Navigator) { n.GoDrill(nav.SystemJunk, "system-junk:logs") })
	p.WaitFor("id:items-system-junk:logs", wait)
	if p.Exists("id:items-system-junk:caches") {
		t.Error("another category opened")
	}
}

// many are n 系统垃圾 categories of one checked item each, enough to start below the fold of a short window.
func many(n int) []cleanup.Category {
	cats := make([]cleanup.Category, n)
	for i := range cats {
		id := fmt.Sprintf("system-junk:c%02d", i)
		cats[i] = cleanup.Category{ID: id, Source: "system-junk", Title: fmt.Sprintf("缓存 %02d", i), Icon: "cache", Kind: cleanup.KindCache,
			Deletion: cleanup.DeleteTrash, Selected: true, Size: int64(1000 + i), Items: []cleanup.Item{
				{ID: fmt.Sprintf("i%02d", i), Path: fmt.Sprintf("/Users/me/Library/Caches/c%02d", i), Size: int64(1000 + i), ModTime: modTime, Selected: true}}}
	}
	return cats
}

// GoDrill on a category below the fold scrolls the page to it; opened without one, the page stays at its top.
func TestOpenedOnACategoryScrollsToIt(t *testing.T) {
	const last = "system-junk:c19"
	p := startSized(t, stateOf(many(20)...), nsgotest.Size(1052, 600), func(n *nav.Navigator) { n.GoDrill(nav.SystemJunk, last) })
	p.WaitFor("id:items-"+last, wait)
	p.WaitUntil(wait, func() bool { return p.Find("id:drill-system-junk").Offset.Y > 0 })
	page, cat := p.Find("id:drill-system-junk"), p.Find("id:category-"+last)
	if top := inPage(page, cat) - float32(page.Offset.Y); top < 0 || top > float32(page.Frame.Size.Height) {
		t.Errorf("the category is at %v of a page %v tall, scrolled %v", top, page.Frame.Size.Height, page.Offset.Y)
	}

	q := startSized(t, stateOf(many(20)...), nsgotest.Size(1052, 600))
	q.Settle()
	if y := q.Find("id:drill-system-junk").Offset.Y; y != 0 {
		t.Errorf("scrolled %v without a category to reveal", y)
	}
}

// inPage is where v starts in the content of page, an ancestor of it.
func inPage(page, v *nsgotest.View) float32 {
	var y float32
	for ; v != nil && v != page; v = v.Parent {
		y += float32(v.Frame.Origin.Y)
		if v.Parent != page && v.Parent != nil {
			y -= float32(v.Parent.Offset.Y)
		}
	}
	return y
}

// undoToast shows a toast that offers 撤销, on the engine goroutine.
func (p *page) undoToast() {
	p.Post(func() {
		p.stores.Toasts.Show(store.Toast{Text: "已移到废纸篓 10 MB", UndoRun: "run-1", Duration: time.Hour})
	})
}

// trash is the Trash as the check measured it: one item, deleted for good, unchecked.
func trash() cleanup.Category {
	return cleanup.Category{ID: "system-junk:trash", Source: "system-junk", Title: "废纸篓", Description: "废纸篓里的全部项目", Icon: "trash", Kind: cleanup.KindTrash,
		Deletion: cleanup.DeletePermanent, Warning: "清空后无法恢复，默认不选", Size: 1_000_000, Items: []cleanup.Item{
			{ID: "t", Path: "/Users/me/.Trash", Size: 1_000_000, ModTime: modTime, Dir: true, Note: "共 3 项"}}}
}

// Emptying the Trash waits while the toast offers 撤销, which it would destroy, and asks with the Trash measured now: the check's size is older than the moves since.
func TestEmptyingTheTrash(t *testing.T) {
	p := start(t, stateOf(append(junk(), trash())...))
	p.undoToast()
	p.Press("id:select-none")
	p.Press("id:check-system-junk:trash")
	p.WaitUntil(wait, func() bool { return !p.Enabled("id:clean") })
	if got := p.Find("id:drill-trash-wait").Text; got != "请等撤销提示消失后再清空废纸篓" {
		t.Errorf("wait %q", got)
	}
	// Anything but the Trash may still be cleaned while 撤销 shows.
	p.Press("id:check-system-junk:trash")
	p.Press("id:check-system-junk:downloads")
	p.WaitUntil(wait, func() bool { return p.Enabled("id:clean") })
	if p.Exists("id:drill-trash-wait") {
		t.Error("the wait shows without the Trash checked")
	}
	p.Press("id:check-system-junk:downloads")
	p.Press("id:check-system-junk:trash")
	p.WaitUntil(wait, func() bool { return !p.Enabled("id:clean") })

	p.Post(p.stores.Toasts.Dismiss)
	p.WaitUntil(wait, func() bool { return p.Enabled("id:clean") })
	p.WaitForGone("id:drill-trash-wait", wait)
	p.ov.TrashInfoValue = app.TrashInfo{Items: 42, Bytes: 5_000_000, Restorable: 3}
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if got := p.Find("id:confirm-trash").Text; got != "废纸篓里的 42 项（5 MB）会永久删除，无法恢复。其中 3 项是 Spruce 移到废纸篓的，清空后不能再放回原处。" {
		t.Errorf("trash line %q", got)
	}
	// The Trash alone asks as 概览's 清空废纸篓… does: one line with the live figures, no plan of one item beside it.
	if got := p.Find("id:confirm >> role:header").Text; got != "清空废纸篓？" {
		t.Errorf("title %q", got)
	}
	if p.Exists("id:plan-message") || p.Exists("id:plan-permanent-0") || !p.Exists("id:confirm-ok >> text:清空") {
		t.Error("the Trash alone is asked about with the plan")
	}
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:confirm", wait)

	// Unmeasured, the question says the whole Trash goes.
	p.ov.TrashErr = errors.New("没有权限")
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if got := p.Find("id:confirm-trash").Text; got != "废纸篓里的全部项目会永久删除，无法恢复。" {
		t.Errorf("trash line %q", got)
	}
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	want := cleanup.CleanRequest{CategoryIDs: []string{"system-junk:trash"}, ItemIDs: []string{"t"}}
	if got := p.cleans()[0].Req; !reflect.DeepEqual(got, want) {
		t.Errorf("request %+v", got)
	}
}

// The Trash checked with items that only move to the Trash still asks with the danger tone, since the Trash goes for good; the overlay counts what the question counts and says the Trash is emptied too.
func TestTrashWithMovesAsksAsDanger(t *testing.T) {
	p := start(t, stateOf(append(junk(), trash())...))
	release := make(chan struct{})
	p.ov.OnClean = func(ctx context.Context, _ cleanup.CleanRequest, _ app.CleanOrigin, _ func(cleanup.Progress)) (app.OverviewOutcome, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return app.OverviewOutcome{}, nil
	}
	p.ov.TrashInfoValue = app.TrashInfo{Items: 42, Bytes: 5_000_000}
	p.Press("id:select-none")
	p.Press("id:check-system-junk:downloads")
	p.Press("id:check-system-junk:trash")
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if got := p.Find("id:plan-message").Text; got != "将清理 1 项，共 10 MB。它们会移到废纸篓，可以随时放回。" {
		t.Errorf("message %q", got)
	}
	if got := p.Find("id:confirm-trash").Text; got != "废纸篓里的 42 项（5 MB）会永久删除，无法恢复。" {
		t.Errorf("trash line %q", got)
	}
	if !p.Find("id:confirm-cancel").Focused {
		t.Error("取消 does not have the focus")
	}
	p.Press("id:confirm-ok")
	p.WaitFor("id:clean-overlay >> text:共 1 项，并清空废纸篓", wait)
	close(release)
	p.WaitForGone("id:clean-overlay", wait)
}

// While the shell's toast shows at the bottom of the page area (24 points up, at least 44 tall), 清理 stands above it, and comes back down once it goes.
func TestActionBarRisesOverTheToast(t *testing.T) {
	p := start(t, ready())
	bottom := func() float32 {
		v := p.Find("id:clean")
		y := float32(v.Frame.Size.Height)
		for ; v != nil; v = v.Parent {
			y += float32(v.Frame.Origin.Y)
			if v.Props.TestID == "drill-system-junk" {
				y -= float32(v.Offset.Y)
			}
		}
		return y
	}
	without := bottom()
	p.undoToast()
	p.Settle()
	if got := bottom(); got > 1200-24-44-12 || got >= without {
		t.Errorf("清理 ends at %v with the toast, %v without it", got, without)
	}
	p.Post(p.stores.Toasts.Dismiss)
	p.Settle()
	if got := bottom(); got != without {
		t.Errorf("清理 ends at %v after the toast, %v before it", got, without)
	}
}

// On a long list scrolled to its end, as after pressing 清理, the bar that rises over the toast stays in view above it; a page scrolled away from the bar is left where it is.
func TestActionBarStaysInViewOverTheToast(t *testing.T) {
	const height = 600
	p := startSized(t, stateOf(many(20)...), nsgotest.Size(1052, height))
	p.WaitFor("id:clean", wait)
	p.Scroll("id:drill-system-junk", 100000)
	p.Settle()
	page := func() *nsgotest.View { return p.Find("id:drill-system-junk") }
	// bottom is where 清理 ends in the window.
	bottom := func() float32 {
		pg := page()
		return float32(pg.Frame.Origin.Y) + inPage(pg, p.Find("id:clean")) + float32(p.Find("id:clean").Frame.Size.Height) - float32(pg.Offset.Y)
	}
	if got := bottom(); got > height {
		t.Fatalf("清理 ends at %v at the end of the page, below the window", got)
	}
	p.undoToast()
	p.WaitUntil(wait, func() bool { return bottom() <= height-24-44-12 })

	p.Post(p.stores.Toasts.Dismiss)
	p.Settle()
	p.Scroll("id:drill-system-junk", -100000)
	p.Settle()
	p.undoToast()
	p.Settle()
	if y := page().Offset.Y; y != 0 {
		t.Errorf("the page scrolled to %v for a toast while its bar was out of view", y)
	}
}

// A 撤销 toast that comes while the question is open keeps the confirmed clean from emptying the Trash.
func TestEmptyingTheTrashRefusedUnderUndo(t *testing.T) {
	p := start(t, stateOf(append(junk(), trash())...))
	p.Press("id:select-none")
	p.Press("id:check-system-junk:trash")
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	p.undoToast()
	p.WaitFor("id:drill-trash-wait", wait)
	p.Press("id:confirm-ok")
	p.WaitForGone("id:confirm", wait)
	p.Settle()
	if len(p.cleans()) != 0 {
		t.Error("emptied the Trash while 撤销 shows")
	}
}

// Untouched, the selection takes a newer check's defaults; once the user changed it, it keeps what is still there and leaves the rest unchecked.
func TestSelectionFollowsNewerChecks(t *testing.T) {
	p := start(t, ready())
	next := ready()
	next.Result.Scan.Categories[1].Selected = true
	next.Result.Scan.Categories[1].Items[0].Selected = true
	p.set(next)
	p.WaitUntil(wait, func() bool { return p.checked("id:check-system-junk:logs") })

	p.Press("id:check-system-junk:downloads")
	again := ready()
	again.Result.Scan.Categories[0].Items = append(again.Result.Scan.Categories[0].Items, cleanup.Item{ID: "z", Path: "/Users/me/Library/Caches/z", Size: 1, Selected: true})
	p.set(again)
	// a, b and c stay checked, the new z does not, and the downloads the user unchecked stay unchecked.
	p.WaitUntil(wait, func() bool { return p.Find("id:check-system-junk:caches").Props.Indeterminate })
	if got := p.stat("已选择"); got != "3 项" || p.checked("id:check-system-junk:downloads") || !p.checked("id:check-system-junk:logs") {
		t.Errorf("the user's selection was not kept: %s", got)
	}
}

// Leaving the view stops its clean after the current item.
func TestLeavingStopsTheClean(t *testing.T) {
	p := start(t, ready())
	stopped := make(chan struct{})
	p.ov.OnClean = func(ctx context.Context, _ cleanup.CleanRequest, _ app.CleanOrigin, _ func(cleanup.Progress)) (app.OverviewOutcome, error) {
		<-ctx.Done()
		close(stopped)
		return app.OverviewOutcome{}, nil
	}
	p.Press("id:check-system-junk:caches")
	p.Press("id:clean")
	p.WaitFor("id:clean-overlay", wait)
	p.shown.Set(false)
	p.Settle()
	select {
	case <-stopped:
	case <-time.After(wait):
		t.Fatal("the clean goes on after the view went")
	}
}
