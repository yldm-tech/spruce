package installers_test

import (
	"context"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/cleanflow/cleanflowfake"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/installers"
	"spruce/apps/spruce/internal/ui/store"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// wait is real time; the race detector on a loaded machine slows every frame.
const wait = 20 * time.Second

var modTime = time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)

var roots = []string{"/Users/me/Downloads", "/Users/me/Desktop"}

func item(path string, size int64, selected bool, note string) cleanup.Item {
	return cleanup.Item{ID: cleanup.ItemID(path), Path: path, Size: size, ModTime: modTime, Selected: selected, Note: note}
}

// check is 概览's check of 安装包: a disk image and a package checked, recent and attached ones unchecked.
func check() app.OverviewState {
	cat := func(id, title string, selected bool, items ...cleanup.Item) cleanup.Category {
		c := cleanup.Category{ID: "installers:" + id, Source: "installers", Title: title, Description: title + "的说明", Icon: "installer", Kind: cleanup.KindInstaller,
			Deletion: cleanup.DeleteTrash, Selected: selected, Items: items}
		for _, it := range items {
			c.Size += it.Size
		}
		return c
	}
	res := cleanflowfake.Result(
		cat("disk-images", "磁盘映像", true, item("/Users/me/Downloads/Firefox.dmg", 140_000_000, true, "")),
		cat("installer-packages", "安装器", true, item("/Users/me/Desktop/node.pkg", 80_000_000, true, "")),
		cat("recent-installers", "最近下载或正在使用的安装包", false, item("/Users/me/Downloads/Slack.dmg", 200_000_000, false, "磁盘映像 · 已挂载")),
	)
	res.Sources = []cleanup.SourceState{{ID: "installers", Status: cleanup.SourceDone}}
	return app.OverviewState{Phase: app.OverviewIdle, CanUndo: true,
		Result: &app.OverviewResult{CheckedAt: modTime, Mode: app.CheckForeground, Actionable: true, Scan: res}}
}

// empty is a check that found no installers.
func empty() app.OverviewState {
	st := check()
	st.Result.Scan = cleanflowfake.Result()
	return st
}

type page struct {
	*nsgotest.App
	ov     *storefake.Overview
	stores *store.Stores
	nav    *nav.Navigator
}

func start(t *testing.T, st app.OverviewState, folders []string) *page {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Cleanup.InstallerRoots = folders
	p := &page{ov: storefake.NewOverview(st)}
	fakes.Overview = p.ov
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		p.nav = nav.NewNavigator(cx, nav.Installers)
		nav.Key.Provide(cx, p.nav)
		installers.DepsKey.Provide(cx, installers.Deps{Files: &cleanflowfake.Files{}})
		return core.Dynamic(func(*core.Scope) core.Node {
			if p.nav.Current() != nav.Installers {
				return core.Group{}
			}
			return core.Func(installers.Page)
		})
	}, nsgotest.Size(1052, 1000))
	p.WaitUntil(wait, func() bool {
		return p.stores.Settings.Loaded() && reflect.DeepEqual(p.stores.Overview.State(), p.ov.State())
	})
	return p
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

// The view shows the check's installers with where they go and which stay unchecked; moving them to the Trash takes one click and offers 撤销.
func TestShowsAndMovesToTheTrash(t *testing.T) {
	p := start(t, check(), roots)
	p.Find("id:drill-installers")
	p.Find("role:header 安装包")
	p.Find("role:region 找到的安装包")
	p.Find("text:清理的安装包会移到废纸篓，可以随时放回。最近 7 天内下载的和仍在挂载的默认不选。")
	if !p.Find("id:check-installers:disk-images").Props.Checked || p.Find("id:check-installers:recent-installers").Props.Checked {
		t.Error("defaults")
	}
	p.Press("role:button 展开最近下载或正在使用的安装包")
	p.Find("id:items-installers:recent-installers >> text:/磁盘映像 · 已挂载/")
	if got := p.stat("已选大小"); got != "220 MB" {
		t.Errorf("已选大小 %q", got)
	}
	p.ov.OnClean = func(context.Context, cleanup.CleanRequest, app.CleanOrigin, func(cleanup.Progress)) (app.OverviewOutcome, error) {
		return app.OverviewOutcome{Result: cleanup.CleanResult{Trashed: 220_000_000, Removed: 2}, RunID: "run-2", Undoable: true}, nil
	}
	p.Press("id:clean")
	p.WaitUntil(wait, func() bool { return p.stores.Toasts.Current() != nil })
	if p.Exists("id:confirm") {
		t.Error("asked before a move that can be undone")
	}
	if got := *p.stores.Toasts.Current(); got.Text != "已移到废纸篓 220 MB" || got.UndoRun != "run-2" {
		t.Errorf("toast %+v", got)
	}
	_, _, cleans, _, _ := p.ov.Calls()
	want := storefake.CleanCall{Req: cleanup.CleanRequest{CategoryIDs: []string{"installers:disk-images", "installers:installer-packages"},
		ItemIDs: []string{cleanup.ItemID("/Users/me/Downloads/Firefox.dmg"), cleanup.ItemID("/Users/me/Desktop/node.pkg")}}, Origin: app.CleanOrigin{Source: "installers"}}
	if len(cleans) != 1 || !reflect.DeepEqual(cleans[0], want) {
		t.Errorf("cleans %+v", cleans)
	}
}

// Without a folder to search, the empty view says so and leads to 设置 → 清理.
func TestNoFolders(t *testing.T) {
	p := start(t, empty(), nil)
	p.Find("id:drill-empty >> text:" + installers.NoFolders)
	p.Press("id:drill-settings-link")
	p.WaitUntil(wait, func() bool { return p.nav.Current() == nav.Settings })
}

// With folders but nothing found, the empty view says the folders are clean.
func TestNothingFound(t *testing.T) {
	p := start(t, empty(), roots)
	p.Find("id:drill-empty >> text:没有找到安装包")
	p.Find("text:这些文件夹里没有旧的安装包或磁盘映像。")
	if p.Exists("id:drill-settings-link") {
		t.Error("a link to 设置 with folders set")
	}
}

// The Windows wording: the Recycle Bin, and no attached disk images.
func TestWindowsWording(t *testing.T) {
	if got := installers.TrashLine(true); got != "清理的安装包会移到回收站，可以随时放回。" {
		t.Errorf("trash line %q", got)
	}
	if got := installers.Notice(true); got != "清理的安装包会移到回收站，可以随时放回。最近 7 天内下载的默认不选。" {
		t.Errorf("notice %q", got)
	}
	if got := installers.ScanDescription(true); !strings.Contains(got, ".exe、.msi、.msix、.appx") {
		t.Errorf("description %q", got)
	}
}
