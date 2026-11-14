package devjunk_test

import (
	"context"
	"errors"
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
	"github.com/yldm-tech/spruce/internal/ui/pages/devjunk"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

var modTime = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

// entry is one item of a test category: its name, size, note and whether it starts checked on its own.
type entry struct {
	name     string
	size     int64
	note     string
	selected bool
}

// category is DevJunkPage.test.tsx's.
func category(id, title string, kind cleanup.CategoryKind, deletion cleanup.DeletionMode, items ...entry) cleanup.Category {
	c := cleanup.Category{ID: id, Title: title, Description: title + "的说明", Icon: "cache", Kind: kind, Deletion: deletion}
	for _, e := range items {
		c.Items = append(c.Items, cleanup.Item{ID: e.name, Path: "/Users/u/x/" + e.name, Size: e.size, ModTime: modTime, Note: e.note, Selected: e.selected})
		c.Size += e.size
	}
	return c
}

// scan is DevJunkPage.test.tsx's SCAN.
func scan() cleanup.ScanResult {
	npm := category("tool-npm", "npm 缓存", cleanup.KindDevCache, cleanup.DeletePermanent, entry{name: "npm", size: 1000})
	npm.Selected, npm.Description, npm.Command = true, "运行 npm cache clean --force，清空 npm 下载的包", "npm cache clean --force"
	archives := category("tool-xcode-archives", "Xcode 归档", cleanup.KindDevCache, cleanup.DeleteTrash, entry{name: "arch", size: 500})
	archives.Warning = "包含已发布版本的符号文件"
	r := cleanflowfake.Result(
		npm,
		category("tool-xcode-device-support", "设备支持文件", cleanup.KindDevCache, cleanup.DeletePermanent,
			entry{name: "ios17", size: 3000, note: "这个平台最新的版本，默认保留"}, entry{name: "ios16", size: 2000, selected: true}),
		archives,
		category("artifact-node-modules", "node_modules", cleanup.KindProjectArtifact, cleanup.DeletePermanent,
			entry{name: "nm-new", size: 5000, note: "new · 最近 30 天内有改动"}, entry{name: "nm-old", size: 4000, note: "old", selected: true}),
	)
	return r
}

func cleanResult() cleanup.CleanResult {
	return cleanup.CleanResult{Freed: 7000, Removed: 3, Categories: []cleanup.CategoryOutcome{
		{CategoryID: "tool-npm", Title: "npm 缓存", Removed: 1, Freed: 1000},
		{CategoryID: "tool-xcode-device-support", Title: "设备支持文件", Removed: 1, Freed: 2000},
		{CategoryID: "artifact-node-modules", Title: "node_modules", Removed: 1, Freed: 4000},
	}}
}

type page struct {
	*nsgotest.App
	c      *cleanflowfake.Cleaner
	files  *cleanflowfake.Files
	stores *store.Stores
	nav    *nav.Navigator
}

func newFakes(t *testing.T, roots ...string) *storefake.Services {
	f := storefake.New(t.TempDir())
	f.Settings.Cleanup.DevProjectRoots = append([]string{}, roots...)
	return f
}

func start(t *testing.T, c *cleanflowfake.Cleaner, fakes *storefake.Services) *page {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
	p := &page{c: c, files: &cleanflowfake.Files{}}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		p.nav = nav.NewNavigator(cx, nav.DevJunk)
		nav.Key.Provide(cx, p.nav)
		devjunk.DepsKey.Provide(cx, devjunk.Deps{Cleanup: c, Files: p.files})
		return devjunk.Page(cx)
	}, nsgotest.Size(1052, 820))
	p.WaitUntil(wait, func() bool { return p.stores.Settings.Loaded() })
	return p
}

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

// scanToResults scans and waits for the two sections with the scan's default selection: some categories are checked in part.
func (p *page) scanToResults(t *testing.T) {
	t.Helper()
	p.Press("id:scan")
	p.WaitFor("id:list-tools", wait)
	if got := p.stat("已选择"); got != "3 项" {
		t.Fatalf("已选择 %q", got)
	}
}

func (p *page) checked(selector string) bool { return p.Find(selector).Props.Checked }

// texts returns the texts inside v, depth first.
func texts(v *nsgotest.View) []string {
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
	return out
}

// Ported from DevJunkPage.test.tsx: lists the project folders it will search before a scan, with a link to 设置.
func TestIdleListsProjectFolders(t *testing.T) {
	p := start(t, &cleanflowfake.Cleaner{}, newFakes(t, "/Users/u/Workspace", "/Users/u/Projects"))
	p.Find("role:header 开发者垃圾")
	p.Find("text:清理开发工具的缓存和项目里的构建产物，大多会在需要时重新下载或生成")
	p.Find("text:查找开发者垃圾")
	p.Find("text:会在这些文件夹里查找项目：")
	if got := texts(p.Find("role:group 项目文件夹")); !reflect.DeepEqual(got, []string{"/Users/u/Workspace", "/Users/u/Projects"}) {
		t.Errorf("folders %q", got)
	}
	p.Press("role:link 更改文件夹")
	p.WaitUntil(wait, func() bool { return p.nav.Current() == nav.Settings })
}

// Ported from DevJunkPage.test.tsx: says only tool caches are scanned when no project folder is set.
func TestIdleWithoutProjectFolders(t *testing.T) {
	p := start(t, &cleanflowfake.Cleaner{}, newFakes(t))
	p.Find("text:还没有设置项目文件夹，这次只扫描开发工具的缓存。")
	if p.Exists("role:group 项目文件夹") {
		t.Error("an empty folder list shows")
	}
	p.Press("role:link 去设置")
	p.WaitUntil(wait, func() bool { return p.nav.Current() == nav.Settings })
}

// Ported from DevJunkPage.test.tsx: shows scan progress and stops the scan.
func TestScanProgressAndStop(t *testing.T) {
	progress := make(chan cleanup.Progress)
	stopped := make(chan struct{})
	c := &cleanflowfake.Cleaner{OnScan: func(ctx context.Context, _ app.CleanerID, send func(cleanup.Progress)) (cleanup.ScanResult, error) {
		for {
			select {
			case ev := <-progress:
				send(ev)
			case <-ctx.Done():
				close(stopped)
				return cleanup.ScanResult{}, ctx.Err()
			}
		}
	}}
	p := start(t, c, newFakes(t, "/Users/u/Workspace"))
	p.Press("id:scan")
	p.WaitFor("id:scan-progress", wait)
	progress <- cleanup.Progress{Phase: cleanup.PhaseScanning, Path: "/Users/u/Workspace/app", Done: 3, Total: 10, Bytes: 2000}
	p.ticking(`text:"/Users/u/Workspace/app"`)
	p.Find("text:已找到 2 KB")
	p.Press("id:scan-stop")
	select {
	case <-stopped:
	case <-time.After(wait):
		t.Fatal("the scan was not stopped")
	}
	p.WaitFor("id:scan", wait)
	if p.Exists("role:alert") {
		t.Error("a stopped scan shows an alert")
	}
}

// Ported from DevJunkPage.test.tsx: shows the two sections with the scan's default selection, confirms commands, deletions and the Trash apart, and cleans.
func TestSectionsConfirmAndClean(t *testing.T) {
	c := scanning(scan())
	c.OnClean = func(context.Context, app.CleanerID, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
		return cleanResult(), nil
	}
	p := start(t, c, newFakes(t, "/Users/u/Workspace"))
	p.scanToResults(t)
	if got := c.Scans(); !reflect.DeepEqual(got, []app.CleanerID{app.CleanerDevJunk}) {
		t.Errorf("scans %v", got)
	}

	p.Find("role:region 工具缓存")
	p.Find("role:region 项目构建产物")
	if got := []string{p.Find("id:heading-tools").Text, p.Find("id:heading-projects").Text}; !reflect.DeepEqual(got, []string{"工具缓存", "项目构建产物"}) {
		t.Errorf("headings %q", got)
	}
	if hs := p.Query("role:header"); len(hs) != 3 {
		t.Errorf("%d headings, want the page's and the two sections'", len(hs))
	}
	p.Find("id:list-tools >> text:已选 1/2 项")
	p.Find("id:list-tools >> text:包含已发布版本的符号文件")
	p.Find("id:list-projects >> text:已选 1/2 项")
	if got := p.stat("已选大小"); got != "7 KB" {
		t.Errorf("已选大小 %q", got)
	}

	// Expanding node_modules shows each project with its note; the recent one is not checked.
	p.Press("id:list-projects >> role:button 展开node_modules")
	p.Find("id:list-projects >> text:/最近 30 天内有改动/")
	if p.checked("id:item-nm-new") || !p.checked("id:item-nm-old") {
		t.Error("node_modules defaults")
	}

	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if got := p.Find("id:confirm-summary").Text; got != "将清理 3 项，共 7 KB。" {
		t.Errorf("summary %q", got)
	}
	var lines []string
	for _, v := range p.Query("id:confirm-lines >> text:/./") {
		if v.Text != "•" {
			lines = append(lines, v.Text)
		}
	}
	if want := []string{"运行 1 个清理命令：npm cache clean --force", "永久删除 2 项，无法恢复"}; !reflect.DeepEqual(lines, want) {
		t.Errorf("lines %q", lines)
	}
	// A command or a permanent deletion is the danger kind: 取消 has the focus first.
	if !p.Find("id:confirm-cancel").Focused {
		t.Error("取消 does not have the focus")
	}
	if len(c.Cleans()) != 0 {
		t.Error("cleaned before the answer")
	}
	p.Press("id:confirm-ok")

	p.WaitFor("id:done", wait)
	p.Find("id:done >> text:清理完成")
	p.WaitUntil(wait, func() bool { return len(c.Cleans()) == 1 })
	want := cleanup.CleanRequest{CategoryIDs: []string{"tool-npm", "tool-xcode-device-support", "artifact-node-modules"}, ItemIDs: []string{"npm", "ios16", "nm-old"}}
	if got := c.Cleans()[0]; got.ID != app.CleanerDevJunk || !reflect.DeepEqual(got.Req, want) {
		t.Errorf("clean %+v", got)
	}
	p.Press("id:done-close")
	p.WaitFor("id:scan", wait)
	if p.stores.Block.Blocked() {
		t.Error("the shell is still blocked")
	}
}

// The cleaning overlay says that a running command finishes before a stop takes effect.
func TestCleaningOverlayWithACommand(t *testing.T) {
	release := make(chan struct{})
	c := scanning(scan())
	c.OnClean = func(ctx context.Context, _ app.CleanerID, _ cleanup.CleanRequest, _ func(cleanup.Progress)) (cleanup.CleanResult, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return cleanResult(), nil
	}
	fakes := newFakes(t, "/Users/u/Workspace")
	fakes.Settings.General.ConfirmBeforeClean = false
	p := start(t, c, fakes)
	p.scanToResults(t)
	p.Press("id:clean")
	p.WaitFor("id:clean-overlay", wait)
	p.Find("id:clean-overlay >> text:共 3 项；停止后，正在运行的命令会先完成")
	close(release)
	p.WaitFor("id:done", wait)
	p.Press("id:done-close")
	p.WaitFor("id:scan", wait)

	// Without the command checked, the line is only the count.
	release = make(chan struct{})
	p.Press("id:scan")
	p.WaitFor("id:list-tools", wait)
	p.Press("id:check-tool-npm")
	p.Press("id:clean")
	p.WaitFor("id:clean-overlay", wait)
	p.Find("id:clean-overlay >> text:共 2 项")
	close(release)
	p.WaitFor("id:done", wait)
}

// Ported from DevJunkPage.test.tsx: selects within one section only.
func TestSelectWithinOneSection(t *testing.T) {
	p := start(t, scanning(scan()), newFakes(t, "/Users/u/Workspace"))
	p.scanToResults(t)
	p.Press("id:list-projects >> id:select-none")
	if got := p.stat("已选择"); got != "2 项" {
		t.Errorf("after 全不选 in the projects %q", got)
	}
	p.Press("id:list-tools >> id:select-all")
	if got := p.stat("已选择"); got != "4 项" {
		t.Errorf("after 全选 in the tools %q", got)
	}
	p.Find("id:category-artifact-node-modules >> text:2 项")
	if p.checked("id:check-artifact-node-modules") {
		t.Error("全选 in the tools checked the projects")
	}
}

// Ported from DevJunkPage.test.tsx: says when nothing was found.
func TestNothingFound(t *testing.T) {
	p := start(t, scanning(cleanflowfake.Result()), newFakes(t, "/Users/u/Workspace"))
	p.Press("id:scan")
	p.WaitFor("text:没有找到开发者垃圾", wait)
	p.Find("text:开发工具的缓存和项目里的构建产物都已经很干净了。")
	if p.Exists("id:clean") {
		t.Error("清理 shows without results")
	}
}

// Ported from DevJunkPage.test.tsx: shows a scan that failed and lets the user try again.
func TestScanFailure(t *testing.T) {
	c := &cleanflowfake.Cleaner{OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
		return cleanup.ScanResult{}, errors.New("扫描没能完成，详情已记录到日志")
	}}
	p := start(t, c, newFakes(t, "/Users/u/Workspace"))
	p.Press("id:scan")
	p.WaitFor("role:alert", wait)
	if got := p.Find("role:alert").Text; got != "扫描没能完成，详情已记录到日志" {
		t.Errorf("alert %q", got)
	}
	if !p.Enabled("id:scan") {
		t.Error("扫描 is disabled")
	}
}

// Ported from DevJunkPage.test.tsx: notes when the projects held no build output.
func TestNoBuildOutput(t *testing.T) {
	r := scan()
	r = cleanflowfake.Result(r.Categories[0])
	r.Denied = 2
	p := start(t, scanning(r), newFakes(t, "/Users/u/Workspace"))
	p.Press("id:scan")
	p.WaitFor("text:项目文件夹里没有找到构建产物。", wait)
	p.Find("text:有 2 个位置没有权限读取，结果可能不完整。")
	if p.Exists("id:list-projects") {
		t.Error("an empty projects section shows")
	}
}

// Without a project folder set, the note says there was nowhere to look.
func TestNoBuildOutputWithoutFolders(t *testing.T) {
	r := scan()
	p := start(t, scanning(cleanflowfake.Result(r.Categories[0])), newFakes(t))
	p.Press("id:scan")
	p.WaitFor("text:没有设置项目文件夹，所以没有查找项目的构建产物。", wait)
}

// Reveals an item of a section in Finder.
func TestReveal(t *testing.T) {
	p := start(t, scanning(scan()), newFakes(t, "/Users/u/Workspace"))
	p.scanToResults(t)
	p.Press("id:expand-artifact-node-modules")
	p.Press("id:reveal-nm-old")
	p.WaitUntil(wait, func() bool { return len(p.files.Revealed()) == 1 })
	if got := p.files.Revealed()[0]; !strings.HasSuffix(got, "/x/nm-old") {
		t.Errorf("revealed %q", got)
	}
}
