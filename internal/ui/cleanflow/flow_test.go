package cleanflow_test

import (
	"context"
	"errors"
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

// macWording skips a test whose expectations are the macOS wording and SI sizes.
func macWording(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
}

var modTime = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

// scanOf is the test's scan: one category of two items, checked.
func scanOf() cleanup.ScanResult {
	return cleanflowfake.Result(cleanup.Category{ID: "caches", Title: "应用缓存", Icon: "cache", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent,
		Selected: true, Size: 300, Items: []cleanup.Item{
			{ID: "a", Path: "/c/a", Size: 200, ModTime: modTime},
			{ID: "b", Path: "/c/b", Size: 100, ModTime: modTime},
		}})
}

var cleaned = cleanup.CleanResult{Freed: 300, Removed: 2,
	Categories: []cleanup.CategoryOutcome{{CategoryID: "caches", Title: "应用缓存", Removed: 2, Freed: 300}}}

// page is a cleaner page on fakes: View over a Flow for the system-junk cleaner, which the test can take away (shown).
type page struct {
	*nsgotest.App
	f      *cleanflow.Flow
	c      *cleanflowfake.Cleaner
	files  *cleanflowfake.Files
	fakes  *storefake.Services
	stores *store.Stores
	shown  *core.Signal[bool]
}

func start(t *testing.T, c *cleanflowfake.Cleaner, fakes *storefake.Services) *page {
	t.Helper()
	p := &page{c: c, files: &cleanflowfake.Files{}, fakes: fakes}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		p.shown = core.NewSignal(cx, true)
		return core.Show{When: p.shown.Get, Then: func(cx *core.Scope) core.Node {
			p.f = cleanflow.NewFlow(cx, c, app.CleanerSystemJunk)
			return cleanflow.View(cx, p.f, cleanflow.ViewConfig{Page: nav.SystemJunk, IdleTitle: "扫描系统垃圾", IdleDescription: "查找缓存和日志",
				ListLabel: "可以清理的项目", SizeLabel: "可释放", EmptyTitle: "没有找到", EmptyDescription: "很干净", Reveal: p.files.Reveal})
		}}
	}, nsgotest.Size(1052, 820))
	p.WaitUntil(wait, func() bool { return p.stores.Settings.Loaded() })
	return p
}

func newFakes(t *testing.T) *storefake.Services { return storefake.New(t.TempDir()) }

// ticking settles until cond holds, moving the manual clock so throttled progress (async.Feed) comes through.
func (p *page) ticking(cond func() bool) {
	p.WaitUntil(wait, func() bool {
		if cond() {
			return true
		}
		p.Advance(20 * time.Millisecond)
		return false
	})
}

// state is what the hook test of cleaner.test.tsx printed: the phase, the checked count, the progress path and the scan error.
func (p *page) state() string {
	path := p.f.Progress.GetUntracked().Path
	if path == "" {
		path = "-"
	}
	return fmt.Sprintf("%d %d %s %s", p.f.Phase.GetUntracked(), p.count(), path, p.f.ScanErr.GetUntracked())
}

// count is how many items are checked.
func (p *page) count() int {
	var cats []cleanup.Category
	if r := p.f.Result.GetUntracked(); r != nil {
		cats = r.Categories
	}
	return cleanflow.SelectedTotals(cats, p.f.Selection.GetUntracked()).Count
}

func phaseState(ph cleanflow.Phase, count int, path, err string) string {
	return fmt.Sprintf("%d %d %s %s", ph, count, path, err)
}

// scripted is a cleaner whose scan sends what the test puts in progress and ends with what it puts in finish.
type scripted struct {
	progress chan cleanup.Progress
	finish   chan error
	result   cleanup.ScanResult
	ctxDone  chan struct{}
}

func newScripted() *scripted {
	return &scripted{progress: make(chan cleanup.Progress), finish: make(chan error), result: scanOf(), ctxDone: make(chan struct{})}
}

func (s *scripted) scan(ctx context.Context, _ app.CleanerID, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	for {
		select {
		case <-ctx.Done():
			close(s.ctxDone)
			return cleanup.ScanResult{}, errors.New("已取消")
		case ev := <-s.progress:
			progress(ev)
		case err := <-s.finish:
			if err != nil {
				return cleanup.ScanResult{}, err
			}
			return s.result, nil
		}
	}
}

// Ported from cleaner.test.tsx: scans with progress, selects the defaults, confirms and cleans.
func TestScanSelectConfirmClean(t *testing.T) {
	s := newScripted()
	c := &cleanflowfake.Cleaner{OnScan: s.scan,
		OnClean: func(context.Context, app.CleanerID, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
			return cleaned, nil
		}}
	p := start(t, c, newFakes(t))
	if got := p.state(); got != phaseState(cleanflow.PhaseIdle, 0, "-", "") {
		t.Fatalf("state %q", got)
	}
	p.Press("id:scan")
	p.WaitFor("id:scan-progress", wait)
	p.WaitUntil(wait, func() bool { return len(c.Scans()) > 0 })
	if got := c.Scans(); !reflect.DeepEqual(got, []app.CleanerID{app.CleanerSystemJunk}) {
		t.Fatalf("scans %v", got)
	}
	s.progress <- cleanup.Progress{Phase: cleanup.PhaseScanning, Path: "/c", Bytes: 10}
	p.ticking(func() bool { return p.state() == phaseState(cleanflow.PhaseScanning, 0, "/c", "") })
	s.finish <- nil
	p.WaitFor("id:category-list", wait)
	if got := p.state(); got != phaseState(cleanflow.PhaseResults, 2, "-", "") {
		t.Fatalf("state %q", got)
	}

	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if len(c.Cleans()) != 0 {
		t.Fatal("cleaned before the confirmation")
	}
	p.Press("id:confirm-ok")
	p.WaitFor("id:done", wait)
	if got := c.Cleans(); !reflect.DeepEqual(got, []cleanflowfake.Clean{{ID: app.CleanerSystemJunk, Req: cleanup.CleanRequest{CategoryIDs: []string{"caches"}}}}) {
		t.Errorf("cleans %+v", got)
	}
	if o := p.f.Outcome.GetUntracked(); o == nil || o.Result == nil || !reflect.DeepEqual(*o.Result, cleaned) {
		t.Errorf("outcome %+v", o)
	}
	if p.stores.Trash.Generation() != 1 {
		t.Error("the clean did not tell the Trash")
	}

	p.Press("id:done-close")
	p.WaitFor("id:scan-idle", wait)
	if got := p.state(); got != phaseState(cleanflow.PhaseIdle, 0, "-", "") {
		t.Errorf("state %q", got)
	}
	// 完成 hands the focus back to 扫描.
	if !p.Find("id:scan").Focused {
		t.Error("扫描 does not have the focus")
	}
	if p.stores.Block.Blocked() {
		t.Error("the shell is still blocked")
	}
}

// Ported from cleaner.test.tsx: cleans without asking when the user turned confirmation off, sending the item IDs of a partial selection.
func TestCleansWithoutAskingWhenConfirmationIsOff(t *testing.T) {
	fakes := newFakes(t)
	fakes.Settings.General.ConfirmBeforeClean = false
	c := &cleanflowfake.Cleaner{
		OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
			return scanOf(), nil
		},
		OnClean: func(context.Context, app.CleanerID, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
			return cleaned, nil
		}}
	p := start(t, c, fakes)
	p.Press("id:scan")
	p.WaitFor("id:category-list", wait)
	p.Press("id:expand-caches")
	p.Press("role:checkbox a")
	if got := p.state(); got != phaseState(cleanflow.PhaseResults, 1, "-", "") {
		t.Fatalf("state %q", got)
	}
	p.Press("id:clean")
	p.WaitFor("id:done", wait)
	if p.f.Confirming.GetUntracked() {
		t.Error("asked although confirmation is off")
	}
	if got := c.Cleans(); len(got) != 1 || !reflect.DeepEqual(got[0].Req, cleanup.CleanRequest{CategoryIDs: []string{"caches"}, ItemIDs: []string{"b"}}) {
		t.Errorf("cleans %+v", got)
	}
}

// Ported from cleaner.test.tsx: goes back to idle without an error when the user stops a scan, and shows a real failure.
func TestStopReturnsToIdleAndAFailureShows(t *testing.T) {
	s := newScripted()
	p := start(t, &cleanflowfake.Cleaner{OnScan: s.scan}, newFakes(t))
	p.Press("id:scan")
	p.WaitFor("id:scan-progress", wait)
	p.Press("id:scan-stop")
	p.WaitFor("id:scan-idle", wait)
	<-s.ctxDone
	if got := p.state(); got != phaseState(cleanflow.PhaseIdle, 0, "-", "") {
		t.Fatalf("state %q", got)
	}
	if p.Exists("id:scan-error") {
		t.Error("a stopped scan shows an error")
	}

	s2 := newScripted()
	p.c.OnScan = s2.scan
	p.Press("id:scan")
	p.WaitFor("id:scan-progress", wait)
	s2.finish <- errors.New("扫描没能完成，详情已记录到日志")
	p.WaitFor("id:scan-error", wait)
	if got := p.Find("id:scan-error").Text; got != "扫描没能完成，详情已记录到日志" {
		t.Errorf("error %q", got)
	}
	if got := p.state(); got != phaseState(cleanflow.PhaseIdle, 0, "-", "扫描没能完成，详情已记录到日志") {
		t.Errorf("state %q", got)
	}
}

// Ported from cleaner.test.tsx: leaving the page stops the running scan.
func TestLeavingThePageStopsTheScan(t *testing.T) {
	s := newScripted()
	p := start(t, &cleanflowfake.Cleaner{OnScan: s.scan}, newFakes(t))
	p.Press("id:scan")
	p.WaitFor("id:scan-progress", wait)
	p.shown.Set(false)
	p.Settle()
	select {
	case <-s.ctxDone:
	case <-time.After(wait):
		t.Fatal("the scan goes on after the page went")
	}
}

// listCategories are CategoryList.test.tsx's.
func listCategories() []cleanup.Category {
	return []cleanup.Category{
		{ID: "caches", Title: "应用缓存", Description: "应用临时保存的数据，删除后会自动重建", Icon: "cache", Kind: cleanup.KindCache,
			Deletion: cleanup.DeletePermanent, Selected: true, Size: 3_000_000, Items: []cleanup.Item{
				{ID: "a", Path: "/Users/me/Library/Caches/com.example.a", Size: 2_000_000, ModTime: modTime, Dir: true},
				{ID: "b", Path: "/Users/me/Library/Caches/com.example.b", Size: 1_000_000, ModTime: modTime, Note: "3 个月未修改"},
			}},
		{ID: "logs", Title: "日志", Description: "应用和系统的运行记录", Warning: "排查问题时可能用得到", Icon: "log", Kind: cleanup.KindLog,
			Deletion: cleanup.DeletePermanent, Size: 500_000, Items: []cleanup.Item{
				{ID: "c", Path: "/Users/me/Library/Logs/x.log", Size: 500_000, ModTime: modTime},
			}},
	}
}

// scanned starts a page whose scan finds categories and waits for the list.
func scanned(t *testing.T, categories []cleanup.Category) *page {
	t.Helper()
	p := start(t, &cleanflowfake.Cleaner{OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
		return cleanflowfake.Result(categories...), nil
	}}, newFakes(t))
	p.Press("id:scan")
	p.WaitFor("id:category-list", wait)
	return p
}

func (p *page) checked(selector string) bool { return p.Find(selector).Props.Checked }

func (p *page) toolbar() string { return p.Find("id:category-list >> text:/^\\d+ 类，/").Text }

// Ported from CategoryList.test.tsx: each category with its default check, size and warning.
func TestCategoryListDefaults(t *testing.T) {
	macWording(t)
	p := scanned(t, listCategories())
	if !p.checked("id:check-caches") || p.checked("id:check-logs") {
		t.Error("defaults")
	}
	p.Find("id:category-logs >> text:排查问题时可能用得到")
	if p.Exists("text:应用和系统的运行记录") {
		t.Error("the description shows beside the warning")
	}
	p.Find("id:category-caches >> text:3 MB")
	if got := p.toolbar(); got != "2 类，共 3.5 MB，已选 2 项" {
		t.Errorf("toolbar %q", got)
	}
}

// Ported from CategoryList.test.tsx: expands to items, checks them one by one and shows the partial state.
func TestCategoryListItems(t *testing.T) {
	macWording(t)
	p := scanned(t, listCategories())
	p.Press("role:button 展开应用缓存")
	if got := len(p.Query("id:items-caches >> role:checkbox")); got != 2 {
		t.Fatalf("%d item rows", got)
	}
	p.Find("id:items-caches >> text:/3 个月未修改/")
	p.Find(`id:items-caches >> text:"/Users/me/Library/Caches · 3 个月未修改"`)
	p.Press("role:checkbox com.example.a")
	caches := p.Find("id:check-caches")
	if !caches.Props.Indeterminate || caches.Props.Checked {
		t.Errorf("category state %+v", caches.Props)
	}
	p.Find("id:category-caches >> text:已选 1/2 项")
	// Checking the partial category checks all of it.
	p.Press("id:check-caches")
	if !p.checked("id:check-caches") || !p.checked("role:checkbox com.example.a") {
		t.Error("the category is not checked")
	}
	expand := p.Find("role:button 收起应用缓存")
	if !expand.Props.Expandable || !expand.Props.Expanded {
		t.Errorf("expand %+v", expand.Props)
	}
	p.Press("role:button 收起应用缓存")
	p.WaitForGone("id:items-caches", wait)
}

// Ported from CategoryList.test.tsx: selects all and none.
func TestCategoryListSelectAllAndNone(t *testing.T) {
	macWording(t)
	p := scanned(t, listCategories())
	p.Press("id:select-all")
	if got := p.toolbar(); got != "2 类，共 3.5 MB，已选 3 项" {
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
	macWording(t)
	p := scanned(t, listCategories())
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
		items[i] = cleanup.Item{ID: fmt.Sprintf("i%04d", i), Path: fmt.Sprintf("/Users/me/Library/Caches/item-%04d", i), Size: int64(3000 - i), ModTime: modTime}
	}
	big := cleanup.Category{ID: "big", Title: "大量缓存", Icon: "cache", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent,
		Selected: true, Size: 4_501_500, Items: items}
	p := scanned(t, []cleanup.Category{big})
	p.Press("role:button 展开大量缓存")
	list := p.Find("id:items-big")
	if list.Frame.Size.Height != 360 {
		t.Errorf("list height %v", list.Frame.Size.Height)
	}
	if n := len(p.QueryMounted("id:items-big >> role:checkbox")); n == 0 || n > 100 {
		t.Fatalf("%d rows mounted for 3,000 items", n)
	}
	p.Find("role:checkbox item-0000")
	p.ScrollTo("id:items-big", core.Point{Y: 48 * 2000})
	p.WaitUntil(wait, func() bool { return len(p.QueryMounted("role:checkbox item-2000")) == 1 })
	p.Press("role:checkbox item-2000")
	if got := p.count(); got != 2999 {
		t.Errorf("checked %d", got)
	}
	if n := len(p.QueryMounted("id:items-big >> role:checkbox")); n > 100 {
		t.Errorf("%d rows mounted after scrolling", n)
	}
}

// Ported from ScanState.test.tsx: ScanProgress fills the ring once the total is known and stops on request.
func TestScanProgress(t *testing.T) {
	macWording(t)
	s := newScripted()
	p := start(t, &cleanflowfake.Cleaner{OnScan: s.scan}, newFakes(t))
	p.Press("id:scan")
	p.WaitFor("id:scan-progress", wait)
	if ring := p.Find("id:scan-ring"); !ring.Props.Value.NoNow {
		t.Errorf("ring %+v", ring.Props.Value)
	}
	p.Find("id:scan-progress >> text:正在准备…")
	s.progress <- cleanup.Progress{Phase: cleanup.PhaseScanning, Path: "/Users/me/Library/Caches", Done: 3, Total: 4, Bytes: 2_500_000}
	p.ticking(func() bool { return p.Exists("id:scan-progress >> text:75%") })
	if ring := p.Find("id:scan-ring"); ring.Props.Value.NoNow || ring.Props.Value.Now != 75 {
		t.Errorf("ring %+v", ring.Props.Value)
	}
	p.Find("text:已找到 2.5 MB")
	p.Find(`id:scan-progress >> text:"/Users/me/Library/Caches"`)
	p.Press("id:scan-stop")
	p.WaitFor("id:scan-idle", wait)
}

// Ported from ScanState.test.tsx: the cleaning overlay blocks the shell and counts what it freed; 停止 interrupts the clean.
func TestCleaningOverlay(t *testing.T) {
	macWording(t)
	fakes := newFakes(t)
	fakes.Settings.General.ConfirmBeforeClean = false
	progress := make(chan cleanup.Progress)
	c := &cleanflowfake.Cleaner{
		OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
			return scanOf(), nil
		},
		OnClean: func(ctx context.Context, _ app.CleanerID, _ cleanup.CleanRequest, send func(cleanup.Progress)) (cleanup.CleanResult, error) {
			for {
				select {
				case ev := <-progress:
					send(ev)
				case <-ctx.Done():
					return cleanup.CleanResult{Freed: 100, Removed: 1, Interrupted: true, Partial: true,
						Categories: []cleanup.CategoryOutcome{{CategoryID: "caches", Title: "应用缓存", Removed: 1, Freed: 100}}}, nil
				}
			}
		}}
	p := start(t, c, fakes)
	p.Press("id:scan")
	p.WaitFor("id:category-list", wait)
	p.Press("id:clean")
	p.WaitFor("id:clean-overlay", wait)
	if !p.stores.Block.Blocked() {
		t.Error("the shell is not blocked")
	}
	p.Find("id:clean-overlay >> text:共 2 项")
	p.Find("id:clean-overlay >> text:正在准备…")
	progress <- cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: "/c/a", Done: 1, Total: 4, Bytes: 1_000_000}
	p.ticking(func() bool { return p.Exists("id:clean-overlay >> text:已处理 1/4 · 已释放 1 MB") })
	p.Find(`id:clean-overlay >> text:"/c/a"`)
	// The scrim takes no Escape: the overlay stays while the clean runs.
	p.Key("Escape")
	p.Find("id:clean-overlay")
	p.Press("id:overlay-cancel")
	p.WaitFor("id:done", wait)
	p.Find("text:清理已中断")
	if !strings.Contains(p.Find("id:done-lines").Text, "清理在中途停止，其余项目没有改动") {
		t.Errorf("lines %q", p.Find("id:done-lines").Text)
	}
}

// Ported from dialogs.test.tsx and ScanState.test.tsx: the danger confirmation focuses 取消, Escape closes it and the focus goes back to 清理; 取消 does the same.
func TestDangerConfirmation(t *testing.T) {
	macWording(t)
	p := scanned(t, listCategories())
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if !p.stores.Block.Blocked() {
		t.Error("the shell is not blocked under the confirmation")
	}
	if got := p.Find("id:plan-message").Text; got != "将清理 2 项，共 3 MB。删除后无法恢复。" {
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
	if p.stores.Block.Blocked() || p.f.Confirming.GetUntracked() {
		t.Error("still blocked or confirming")
	}
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:confirm", wait)
	if len(p.c.Cleans()) != 0 {
		t.Error("cleaned after 取消")
	}
}

// Ported from ScanState.test.tsx and dialogs.test.tsx: a clean that moves to the Trash only asks without the danger tone and focuses 清理; the done card summarises it and focuses 完成.
func TestTrashConfirmationAndDoneCard(t *testing.T) {
	macWording(t)
	cats := listCategories()
	for i := range cats {
		cats[i].Deletion = cleanup.DeleteTrash
	}
	p := scanned(t, cats)
	p.c.OnClean = func(context.Context, app.CleanerID, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
		return cleanup.CleanResult{Trashed: 3_000_000, Removed: 2,
			Categories: []cleanup.CategoryOutcome{{CategoryID: "caches", Title: "应用缓存", Removed: 2, Trashed: 3_000_000}}}, nil
	}
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if got := p.Find("id:plan-message").Text; got != "将清理 2 项，共 3 MB。它们会移到废纸篓，可以随时放回。" {
		t.Errorf("message %q", got)
	}
	if !p.Find("id:confirm-ok").Focused {
		t.Error("清理 does not have the focus")
	}
	p.Find("id:plan-trash >> text:应用缓存")
	if p.Exists("id:plan-permanent") {
		t.Error("a permanent group without permanent items")
	}
	p.Press("id:confirm-ok")
	p.WaitFor("id:done", wait)
	p.Find("text:清理完成")
	if !p.Find("id:done-close").Focused {
		t.Error("完成 does not have the focus")
	}
	want := "应用缓存：2 项已移到废纸篓（3 MB）\n移到废纸篓的项目在清空废纸篓后才会释放空间"
	if got := p.Find("id:done-lines").Text; got != want {
		t.Errorf("lines %q", got)
	}
}
