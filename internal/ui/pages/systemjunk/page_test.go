package systemjunk_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/pages/systemjunk"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

var modTime = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

type entry struct {
	path string
	size int64
}

func category(id, title string, deletion cleanup.DeletionMode, selected bool, items []entry, warning string) cleanup.Category {
	c := cleanup.Category{ID: id, Title: title, Description: title + "的说明", Icon: "cache", Kind: cleanup.KindCache, Deletion: deletion,
		Selected: selected, Warning: warning}
	for _, e := range items {
		c.Items = append(c.Items, cleanup.Item{ID: id + ":" + e.path, Path: e.path, Size: e.size, ModTime: modTime})
		c.Size += e.size
	}
	return c
}

// categories are SystemJunkPage.test.tsx's.
func categories() []cleanup.Category {
	trash := category("trash", "废纸篓", cleanup.DeletePermanent, false, []entry{{"/Users/me/.Trash", 1_000_000_000}}, "清空后无法恢复，默认不选")
	trash.Kind, trash.Icon = cleanup.KindTrash, "trash"
	return []cleanup.Category{
		category("app-caches", "应用缓存", cleanup.DeletePermanent, true,
			[]entry{{"/Users/me/Library/Caches/com.example.app", 300_000_000}, {"/Users/me/Library/Caches/com.other.app", 200_000_000}}, ""),
		category("app-caches-running", "正在运行的应用的缓存", cleanup.DeletePermanent, false,
			[]entry{{"/Users/me/Library/Caches/com.running.app", 50_000_000}}, "这些应用正在运行，建议先退出再清理"),
		category("incomplete-downloads", "未完成的下载", cleanup.DeleteTrash, true, []entry{{"/Users/me/Downloads/a.crdownload", 10_000_000}}, ""),
		trash,
	}
}

// cleanResult is the test's clean: one item of the app caches was held open by another program.
func cleanResult() cleanup.CleanResult {
	return cleanup.CleanResult{Freed: 500_000_000, Trashed: 10_000_000, Removed: 2, Partial: true,
		Failed: []cleanup.Failure{{ItemID: "app-caches:/Users/me/Library/Caches/com.other.app", CategoryID: "app-caches",
			Path: "/Users/me/Library/Caches/com.other.app", Reason: "有 1 个文件未能删除，可能正在使用", InUse: true}},
		Categories: []cleanup.CategoryOutcome{
			{CategoryID: "app-caches", Title: "应用缓存", Removed: 1, Freed: 500_000_000, Failed: 1},
			{CategoryID: "incomplete-downloads", Title: "未完成的下载", Removed: 1, Trashed: 10_000_000},
		}}
}

type page struct {
	*nsgotest.App
	c      *cleanflowfake.Cleaner
	files  *cleanflowfake.Files
	stores *store.Stores
}

func start(t *testing.T, c *cleanflowfake.Cleaner, fakes *storefake.Services) *page {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
	p := &page{c: c, files: &cleanflowfake.Files{}}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		systemjunk.DepsKey.Provide(cx, systemjunk.Deps{Cleanup: c, Files: p.files})
		return systemjunk.Page(cx)
	}, nsgotest.Size(1052, 820))
	p.WaitUntil(wait, func() bool { return p.stores.Settings.Loaded() })
	return p
}

func newFakes(t *testing.T) *storefake.Services { return storefake.New(t.TempDir()) }

func scanning(r cleanup.ScanResult) *cleanflowfake.Cleaner {
	return &cleanflowfake.Cleaner{OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
		return r, nil
	}}
}

// ticking settles until selector matches one view, moving the manual clock so throttled progress comes through.
func (p *page) ticking(selector string) {
	p.WaitUntil(wait, func() bool {
		if len(p.Query(selector)) == 1 {
			return true
		}
		p.Advance(20 * time.Millisecond)
		return false
	})
}

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

func (p *page) scanned() {
	p.Press("id:scan")
	p.WaitFor("id:category-list", wait)
}

func (p *page) checked(selector string) bool { return p.Find(selector).Props.Checked }

// Ported from SystemJunkPage.test.tsx: says what it scans and when it last cleaned.
func TestIdle(t *testing.T) {
	fakes := newFakes(t)
	run := history.Run{ID: "r", Kind: history.KindSystemJunk, StartedAt: time.Now().Add(-72 * time.Hour), EndedAt: time.Now().Add(-72 * time.Hour),
		Summary: &history.Totals{Freed: 1_200_000_000, Items: 4}}
	other := run
	other.ID, other.Kind = "m", history.KindMaintenance
	fakes.Runs = []history.Run{other, run}
	p := start(t, &cleanflowfake.Cleaner{}, fakes)
	p.Find("role:header 系统垃圾")
	p.Find("text:清理应用缓存、日志和临时文件，不会动你的文档和照片")
	p.Find("text:检查应用缓存、浏览器缓存、日志、崩溃报告、未完成的下载和废纸篓")
	p.WaitFor("text:/^上次清理：.*，释放 1.2 GB$/", wait)
}

func TestScanDescriptionOnWindows(t *testing.T) {
	if got := systemjunk.ScanDescription(true); !strings.HasPrefix(got, "检查临时文件、应用缓存、浏览器缓存、错误报告、未完成的下载和回收站") {
		t.Errorf("windows %q", got)
	}
}

// Ported from SystemJunkPage.test.tsx: shows why the last scan failed.
func TestScanFailure(t *testing.T) {
	p := start(t, &cleanflowfake.Cleaner{OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
		return cleanup.ScanResult{}, errors.New("扫描没能完成，详情已记录到日志")
	}}, newFakes(t))
	p.Press("id:scan")
	p.WaitFor("role:alert", wait)
	if got := p.Find("role:alert").Text; got != "扫描没能完成，详情已记录到日志" {
		t.Errorf("alert %q", got)
	}
}

// Ported from SystemJunkPage.test.tsx: shows progress, lists the categories with their defaults, confirms by deletion mode and cleans.
func TestScanAndClean(t *testing.T) {
	progress := make(chan cleanup.Progress)
	finish := make(chan struct{})
	cleanProgress := make(chan cleanup.Progress)
	cleanFinish := make(chan struct{})
	c := &cleanflowfake.Cleaner{
		OnScan: func(ctx context.Context, _ app.CleanerID, send func(cleanup.Progress)) (cleanup.ScanResult, error) {
			for {
				select {
				case ev := <-progress:
					send(ev)
				case <-finish:
					return cleanflowfake.Result(categories()...), nil
				}
			}
		},
		OnClean: func(ctx context.Context, _ app.CleanerID, _ cleanup.CleanRequest, send func(cleanup.Progress)) (cleanup.CleanResult, error) {
			for {
				select {
				case ev := <-cleanProgress:
					send(ev)
				case <-cleanFinish:
					return cleanResult(), nil
				}
			}
		}}
	p := start(t, c, newFakes(t))
	p.Press("id:scan")
	p.WaitFor("id:scan-progress", wait)
	p.WaitUntil(wait, func() bool { return reflect.DeepEqual(c.Scans(), []app.CleanerID{app.CleanerSystemJunk}) })
	progress <- cleanup.Progress{Phase: cleanup.PhaseScanning, Path: "/Users/me/Library/Caches", Done: 1, Total: 4, Bytes: 2_000_000}
	p.ticking("text:已找到 2 MB")
	p.Find("id:scan-progress >> text:25%")
	close(finish)
	p.WaitFor("id:category-list", wait)

	// The defaults: the app caches and the downloads, not the running app's caches or the Trash.
	if !p.checked("id:check-app-caches") || !p.checked("id:check-incomplete-downloads") || p.checked("id:check-app-caches-running") || p.checked("id:check-trash") {
		t.Error("defaults")
	}
	p.Find("id:category-trash >> text:清空后无法恢复，默认不选")
	if got := p.stat("已选择"); got != "3 项" {
		t.Errorf("已选择 %q", got)
	}
	if got := p.stat("可释放"); got != "510 MB" {
		t.Errorf("可释放 %q", got)
	}
	p.FindMounted("text:扫描完成，找到 1.6 GB")

	// Checking the Trash adds a permanent line to the confirmation.
	p.Press("id:check-trash")
	if got := p.stat("已选择"); got != "4 项" {
		t.Errorf("已选择 %q", got)
	}
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	for group, want := range map[string][]string{
		"plan-permanent": {"应用缓存|2 项 · 500 MB", "废纸篓|1 项 · 1 GB"},
		"plan-trash":     {"未完成的下载|1 项 · 10 MB"},
	} {
		var got []string
		for i := 0; p.Exists("id:" + group + "-" + strconv.Itoa(i)); i++ {
			row := p.Find("id:" + group + "-" + strconv.Itoa(i))
			got = append(got, texts(row))
		}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("%s %q", group, got)
		}
	}
	p.Find("role:region 永久删除")
	p.Find("role:region 移到废纸篓")
	if !strings.Contains(p.Find("id:plan-message").Text, "其中 3 项会永久删除，无法恢复；其余移到废纸篓。") {
		t.Errorf("message %q", p.Find("id:plan-message").Text)
	}
	// Deleting for good focuses 取消 first.
	if !p.Find("id:confirm-cancel").Focused {
		t.Error("取消 does not have the focus")
	}

	p.Press("id:confirm-ok")
	p.WaitFor("id:clean-overlay", wait)
	p.WaitUntil(wait, func() bool { return len(c.Cleans()) == 1 })
	if got := c.Cleans()[0]; got.ID != app.CleanerSystemJunk ||
		!reflect.DeepEqual(got.Req, cleanup.CleanRequest{CategoryIDs: []string{"app-caches", "incomplete-downloads", "trash"}}) {
		t.Errorf("clean %+v", got)
	}
	p.Find("id:clean-overlay >> text:共 4 项")
	cleanProgress <- cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: "/Users/me/.Trash", Done: 1, Total: 4, Bytes: 1_000_000_000}
	p.ticking("id:clean-overlay >> text:已处理 1/4 · 已释放 1 GB")

	close(cleanFinish)
	p.WaitFor("id:done", wait)
	p.Find("text:清理完成")
	p.Find("id:done >> text:510 MB")
	// The in-use item is one gentle line, not a failure.
	want := "应用缓存：已清理 1 项，释放 500 MB\n未完成的下载：1 项已移到废纸篓（10 MB）\n移到废纸篓的项目在清空废纸篓后才会释放空间"
	if got := p.Find("id:done-lines").Text; got != want {
		t.Errorf("lines %q", got)
	}
	if got := p.Find("id:done-in-use").Text; got != "1 项里有文件正被其他程序使用，已跳过这些文件，下次可以再试" {
		t.Errorf("in use %q", got)
	}
	p.Press("id:done-close")
	p.WaitForGone("id:done", wait)
	if !p.Find("id:scan").Focused {
		t.Error("扫描 does not have the focus")
	}
	if p.stores.Block.Blocked() {
		t.Error("the shell is still blocked")
	}
}

// texts joins the texts inside v with |.
func texts(v *nsgotest.View) string {
	var out []string
	var walk func(*nsgotest.View)
	walk = func(v *nsgotest.View) {
		if v.Text != "" {
			out = append(out, v.Text)
		}
		for _, k := range v.Children {
			walk(k)
		}
	}
	walk(v)
	return strings.Join(out, "|")
}

// Ported from SystemJunkPage.test.tsx: cleans part of a category without asking when confirmation is off.
func TestPartialCleanWithoutConfirmation(t *testing.T) {
	fakes := newFakes(t)
	fakes.Settings.General.ConfirmBeforeClean = false
	c := scanning(cleanflowfake.Result(categories()...))
	c.OnClean = func(context.Context, app.CleanerID, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
		r := cleanResult()
		r.Failed = nil
		return r, nil
	}
	p := start(t, c, fakes)
	p.scanned()
	p.Press("role:button 展开应用缓存")
	p.Press("role:checkbox com.other.app")
	p.Press("id:check-incomplete-downloads")
	if got := p.stat("可释放"); got != "300 MB" {
		t.Errorf("可释放 %q", got)
	}
	p.Press("id:clean")
	p.WaitFor("id:done", wait)
	if p.Exists("id:confirm") {
		t.Error("asked although confirmation is off")
	}
	want := cleanup.CleanRequest{CategoryIDs: []string{"app-caches"}, ItemIDs: []string{"app-caches:/Users/me/Library/Caches/com.example.app"}}
	if got := c.Cleans(); len(got) != 1 || !reflect.DeepEqual(got[0].Req, want) {
		t.Errorf("cleans %+v", got)
	}
}

// Ported from SystemJunkPage.test.tsx: reveals an item in Finder.
func TestReveal(t *testing.T) {
	p := start(t, scanning(cleanflowfake.Result(categories()...)), newFakes(t))
	p.scanned()
	p.Press("role:button 展开未完成的下载")
	p.Press("role:button 在访达中显示 a.crdownload")
	p.WaitUntil(wait, func() bool { return len(p.files.Revealed()) == 1 })
	if got := p.files.Revealed(); got[0] != "/Users/me/Downloads/a.crdownload" {
		t.Errorf("revealed %v", got)
	}
}

// Ported from SystemJunkPage.test.tsx: disables 清理 when nothing is checked, and rescans from the header.
func TestNothingCheckedAndRescan(t *testing.T) {
	c := scanning(cleanflowfake.Result(categories()...))
	p := start(t, c, newFakes(t))
	p.scanned()
	p.Press("id:select-none")
	if p.Enabled("id:clean") {
		t.Error("清理 is enabled")
	}
	p.Press("id:rescan")
	p.WaitUntil(wait, func() bool { return len(c.Scans()) == 2 })
	p.WaitFor("id:category-list", wait)
}

// Ported from SystemJunkPage.test.tsx: explains the Trash it cannot read and a partial scan.
func TestUnreadableTrashAndPartialScan(t *testing.T) {
	noAccess := category("trash", "废纸篓", cleanup.DeletePermanent, false, nil, "需要为 Spruce 开启「完全磁盘访问权限」才能读取废纸篓，开启后重新扫描")
	r := cleanflowfake.Result(categories()[0], noAccess)
	r.Denied = 2
	p := start(t, scanning(r), newFakes(t))
	p.scanned()
	p.Find("text:/完全磁盘访问权限/")
	if p.Enabled("id:check-trash") {
		t.Error("the empty Trash can be checked")
	}
	if p.Enabled("id:expand-trash") {
		t.Error("the empty Trash can be expanded")
	}
	p.Find("text:有 2 个位置没有读取权限，结果可能不完整")
}

// Ported from SystemJunkPage.test.tsx: says so when there is nothing to clean.
func TestNothingFound(t *testing.T) {
	c := scanning(cleanflowfake.Result())
	p := start(t, c, newFakes(t))
	p.Press("id:scan")
	p.WaitFor("text:没有找到系统垃圾", wait)
	p.Find("text:缓存、日志和临时文件都很干净，过段时间再来看看。")
	if p.Exists("id:clean") || p.Exists("id:rescan") {
		t.Error("清理 or the header's 重新扫描 shows without results")
	}
	p.Press("id:empty-rescan")
	p.WaitUntil(wait, func() bool { return len(c.Scans()) == 2 })
}

// Ported from SystemJunkPage.test.tsx: explains a clean that could not start.
func TestCleanThatCouldNotStart(t *testing.T) {
	c := scanning(cleanflowfake.Result(categories()...))
	c.OnClean = func(context.Context, app.CleanerID, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
		return cleanup.CleanResult{}, errors.New("扫描结果已过期，请重新扫描")
	}
	p := start(t, c, newFakes(t))
	p.scanned()
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	p.Press("id:confirm-ok")
	p.WaitFor("id:done", wait)
	p.Find("text:清理没有进行")
	if got := p.Find("id:done-lines").Text; got != "扫描结果已过期，请重新扫描" {
		t.Errorf("lines %q", got)
	}
}

// Ported from SystemJunkPage.test.tsx: stops a scan and returns to idle without an error.
func TestStopScan(t *testing.T) {
	stopped := make(chan struct{})
	c := &cleanflowfake.Cleaner{OnScan: func(ctx context.Context, _ app.CleanerID, _ func(cleanup.Progress)) (cleanup.ScanResult, error) {
		<-ctx.Done()
		close(stopped)
		return cleanup.ScanResult{}, errors.New("已取消")
	}}
	p := start(t, c, newFakes(t))
	p.Press("id:scan")
	p.WaitFor("id:scan-stop", wait)
	p.Press("id:scan-stop")
	p.WaitFor("id:scan", wait)
	<-stopped
	if p.Exists("role:alert") {
		t.Error("a stopped scan shows an alert")
	}
}
