package systemjunk_test

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/pages/systemjunk"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// wait is real time; the race detector on a loaded machine slows every frame.
const wait = 20 * time.Second

var modTime = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

type entry struct {
	path string
	size int64
}

func category(id, title string, deletion cleanup.DeletionMode, selected bool, items []entry, warning string) cleanup.Category {
	c := cleanup.Category{ID: "system-junk:" + id, Source: "system-junk", Title: title, Description: title + "的说明", Icon: "cache", Kind: cleanup.KindCache,
		Deletion: deletion, Selected: selected, Warning: warning}
	for _, e := range items {
		c.Items = append(c.Items, cleanup.Item{ID: id + ":" + e.path, Path: e.path, Size: e.size, ModTime: modTime, Selected: selected})
		c.Size += e.size
	}
	return c
}

// categories are a check's 系统垃圾: app caches, the running apps' caches (unchecked), downloads that go to the Trash and the Trash (unchecked).
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

func stateOf(cats ...cleanup.Category) app.OverviewState {
	res := cleanflowfake.Result(cats...)
	res.Sources = []cleanup.SourceState{{ID: "system-junk", Status: cleanup.SourceDone}}
	return app.OverviewState{Phase: app.OverviewIdle, CanUndo: true,
		Result: &app.OverviewResult{CheckedAt: modTime, Mode: app.CheckForeground, Actionable: true, Scan: res}}
}

type page struct {
	*nsgotest.App
	ov     *storefake.Overview
	files  *cleanflowfake.Files
	stores *store.Stores
}

func start(t *testing.T, st app.OverviewState) *page {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
	fakes := storefake.New(t.TempDir())
	p := &page{ov: storefake.NewOverview(st), files: &cleanflowfake.Files{}}
	fakes.Overview = p.ov
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		nav.Key.Provide(cx, nav.NewNavigator(cx, nav.SystemJunk))
		systemjunk.DepsKey.Provide(cx, systemjunk.Deps{Files: p.files})
		return systemjunk.Page(cx)
	}, nsgotest.Size(1052, 1000))
	p.WaitUntil(wait, func() bool {
		return p.stores.Settings.Loaded() && reflect.DeepEqual(p.stores.Overview.State(), p.ov.State())
	})
	return p
}

func (p *page) checked(selector string) bool { return p.Find(selector).Props.Checked }

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

// The view shows 概览's check of 系统垃圾 with what it looks at, the check's defaults and what is freed; there is nothing to scan.
func TestShowsTheCheck(t *testing.T) {
	p := start(t, stateOf(categories()...))
	p.Find("id:drill-system-junk")
	p.Find("role:header 系统垃圾")
	p.Find("text:清理应用缓存、日志和临时文件，不会动你的文档和照片")
	p.Find("text:检查应用缓存、浏览器缓存、日志、崩溃报告、未完成的下载和废纸篓。只看你自己的用户文件夹，不需要管理员权限。")
	p.Find("role:region 可以清理的系统垃圾")
	if p.Exists("id:scan") {
		t.Error("the view offers a scan")
	}
	if !p.checked("id:check-system-junk:app-caches") || !p.checked("id:check-system-junk:incomplete-downloads") ||
		p.checked("id:check-system-junk:app-caches-running") || p.checked("id:check-system-junk:trash") {
		t.Error("defaults")
	}
	if got := p.stat("已选择"); got != "3 项" {
		t.Errorf("已选择 %q", got)
	}
	if got := p.stat("已选大小"); got != "510 MB" {
		t.Errorf("已选大小 %q", got)
	}
}

func TestScanDescriptionOnWindows(t *testing.T) {
	if got := systemjunk.ScanDescription(true); !strings.HasPrefix(got, "检查临时文件、应用缓存、浏览器缓存、错误报告、未完成的下载和回收站") {
		t.Errorf("windows %q", got)
	}
}

// Emptying the Trash with the rest asks with the plan of the rest and a line for the Trash, measured now; the clean belongs to 系统垃圾 and its toast says what it freed.
func TestCleanWithThePlan(t *testing.T) {
	p := start(t, stateOf(categories()...))
	p.ov.TrashInfoValue = app.TrashInfo{Items: 12, Bytes: 2_000_000_000}
	p.Press("id:check-system-junk:trash")
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	p.Find("role:region 永久删除")
	p.Find("role:region 移到废纸篓")
	if got := p.Find("id:plan-message").Text; got != "将清理 3 项，共 510 MB。其中 2 项会永久删除，无法恢复；其余移到废纸篓。" {
		t.Errorf("message %q", got)
	}
	if p.Exists("id:confirm >> text:废纸篓 1 项 · 2 GB") {
		t.Error("the plan lists the Trash beside its line")
	}
	if got := p.Find("id:confirm-trash").Text; got != "废纸篓里的 12 项（2 GB）会永久删除，无法恢复。" {
		t.Errorf("trash line %q", got)
	}
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { _, _, c, _, _ := p.ov.Calls(); return len(c) == 1 })
	_, _, cleans, _, _ := p.ov.Calls()
	if got := cleans[0]; got.Origin != (app.CleanOrigin{Source: "system-junk"}) ||
		!reflect.DeepEqual(got.Req.CategoryIDs, []string{"system-junk:app-caches", "system-junk:incomplete-downloads", "system-junk:trash"}) || len(got.Req.ItemIDs) != 4 {
		t.Errorf("clean %+v", got)
	}
	p.WaitUntil(wait, func() bool { return p.stores.Toasts.Current() != nil })
	if got := p.stores.Toasts.Current().Text; got != "已释放 0 B" {
		t.Errorf("toast %q", got)
	}
}

// Only the downloads checked: one click moves them to the Trash and the toast offers 撤销.
func TestMoveToTheTrashWithUndo(t *testing.T) {
	p := start(t, stateOf(categories()...))
	p.ov.OnClean = func(_ context.Context, _ cleanup.CleanRequest, _ app.CleanOrigin, _ func(cleanup.Progress)) (app.OverviewOutcome, error) {
		return app.OverviewOutcome{Result: cleanup.CleanResult{Trashed: 10_000_000, Removed: 1}, RunID: "run-7", Undoable: true}, nil
	}
	p.Press("id:check-system-junk:app-caches")
	p.Press("id:clean")
	p.WaitUntil(wait, func() bool { return p.stores.Toasts.Current() != nil })
	if p.Exists("id:confirm") {
		t.Error("asked before a move that can be undone")
	}
	if got := *p.stores.Toasts.Current(); got.Text != "已移到废纸篓 10 MB" || got.UndoRun != "run-7" || got.Duration != 10*time.Second {
		t.Errorf("toast %+v", got)
	}
}

// Reveals an item in Finder.
func TestReveal(t *testing.T) {
	p := start(t, stateOf(categories()...))
	p.Press("role:button 展开未完成的下载")
	p.Press("role:button 在访达中显示 a.crdownload")
	p.WaitUntil(wait, func() bool { return len(p.files.Revealed()) == 1 })
	if got := p.files.Revealed(); got[0] != "/Users/me/Downloads/a.crdownload" {
		t.Errorf("revealed %v", got)
	}
}

// The Trash it could not read cannot be checked or expanded, and the places it could not read are told.
func TestUnreadableTrash(t *testing.T) {
	noAccess := category("trash", "废纸篓", cleanup.DeletePermanent, false, nil, "需要为 Spruce 开启「完全磁盘访问权限」才能读取废纸篓，开启后重新检查")
	st := stateOf(categories()[0], noAccess)
	st.Result.Scan.Sources[0].Denied = 2
	p := start(t, st)
	p.Find("text:/完全磁盘访问权限/")
	if p.Enabled("id:check-system-junk:trash") || p.Enabled("id:expand-system-junk:trash") {
		t.Error("the empty Trash can be checked or expanded")
	}
	p.Find("text:有 2 个位置没有读取权限，结果可能不完整")
}

// Nothing of 系统垃圾 in the check: the empty state, without 清理.
func TestNothingFound(t *testing.T) {
	p := start(t, stateOf())
	p.Find("id:drill-empty >> text:没有找到系统垃圾")
	p.Find("text:缓存、日志和临时文件都很干净，过段时间再来看看。")
	if p.Exists("id:clean") {
		t.Error("清理 shows without results")
	}
}
