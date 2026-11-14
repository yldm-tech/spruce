package installers_test

import (
	"context"
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
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/pages/installers"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

// roots are InstallersPage.test.tsx's ROOTS.
var roots = []string{"/Users/me/Downloads", "/Users/me/Desktop"}

type entry struct {
	path    string
	size    int64
	modTime time.Time
	note    string
}

func category(id, title string, selected bool, items []entry) cleanup.Category {
	c := cleanup.Category{ID: id, Title: title, Description: title + "的说明", Icon: "installer", Kind: cleanup.KindInstaller,
		Deletion: cleanup.DeleteTrash, Selected: selected}
	for _, e := range items {
		c.Items = append(c.Items, cleanup.Item{ID: id + ":" + e.path, Path: e.path, Size: e.size, ModTime: e.modTime, Note: e.note, Selected: selected})
		c.Size += e.size
	}
	return c
}

// scan is InstallersPage.test.tsx's SCAN: two kinds of old installers, checked, and a recent one that is still attached, unchecked.
func scan() cleanup.ScanResult {
	return cleanflowfake.Result(
		category("disk-images", "磁盘映像", true, []entry{{"/Users/me/Downloads/Firefox 130.0.dmg", 140_000_000, time.Date(2026, 9, 1, 0, 0, 0, 0, time.UTC), ""}}),
		category("installer-packages", "安装器", true, []entry{{"/Users/me/Desktop/node-v22.11.0.pkg", 80_000_000, time.Date(2026, 8, 1, 0, 0, 0, 0, time.UTC), ""}}),
		category("recent-installers", "最近下载或正在使用的安装包", false,
			[]entry{{"/Users/me/Downloads/Slack.dmg", 200_000_000, time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC), "磁盘映像 · 已挂载"}}),
	)
}

// cleanResult is InstallersPage.test.tsx's RESULT.
func cleanResult() cleanup.CleanResult {
	return cleanup.CleanResult{Trashed: 220_000_000, Removed: 2, Categories: []cleanup.CategoryOutcome{
		{CategoryID: "disk-images", Title: "磁盘映像", Removed: 1, Trashed: 140_000_000},
		{CategoryID: "installer-packages", Title: "安装器", Removed: 1, Trashed: 80_000_000},
	}}
}

type page struct {
	*nsgotest.App
	c      *cleanflowfake.Cleaner
	stores *store.Stores
}

func newFakes(t *testing.T, folders []string) *storefake.Services {
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Cleanup.InstallerRoots = folders
	return fakes
}

func start(t *testing.T, c *cleanflowfake.Cleaner, fakes *storefake.Services) *page {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
	p := &page{c: c}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		installers.DepsKey.Provide(cx, installers.Deps{Cleanup: c, Files: &cleanflowfake.Files{}})
		return installers.Page(cx)
	}, nsgotest.Size(1052, 820))
	p.WaitUntil(wait, func() bool { return p.stores.Settings.Loaded() })
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

// texts returns the texts inside v, in order.
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

// Ported from InstallersPage.test.tsx: lists the folders it searches, says where installers go and when it last cleaned.
func TestIdle(t *testing.T) {
	fakes := newFakes(t, roots)
	ended := time.Now().Add(-72 * time.Hour)
	fakes.Runs = []history.Run{
		{ID: "m", Kind: history.KindMaintenance, StartedAt: ended, EndedAt: ended},
		{ID: "r", Kind: history.KindInstallers, StartedAt: ended.Add(-5 * time.Second), EndedAt: ended, Summary: &history.Totals{Trashed: 2_100_000_000, Items: 3}},
	}
	p := start(t, &cleanflowfake.Cleaner{}, fakes)
	p.Find("role:header 安装包")
	p.Find("text:找出装完就没再用的安装包和磁盘映像")
	p.WaitFor("role:group 查找的文件夹", wait)
	if got := texts(p.Find("role:group 查找的文件夹")); !reflect.DeepEqual(got, roots) {
		t.Errorf("folders %q", got)
	}
	p.Find("text:清理的安装包会移到废纸篓，可以随时放回。")
	p.Find("text:/\\.dmg、\\.pkg、\\.iso、\\.xip/")
	p.WaitFor("text:/^上次清理：.*，移到废纸篓 2.1 GB$/", wait)
	if !p.Enabled("id:scan") {
		t.Error("扫描 is disabled")
	}
	if p.Exists("id:no-folders") {
		t.Error("says there are no folders")
	}
}

// A long folder is shortened in the middle, with the whole path in its tooltip.
func TestLongFolder(t *testing.T) {
	long := "/Users/me/" + strings.Repeat("a-very-long-folder-name/", 4) + "Installers"
	p := start(t, &cleanflowfake.Cleaner{}, newFakes(t, []string{long}))
	p.WaitFor("role:group 查找的文件夹", wait)
	got := texts(p.Find("role:group 查找的文件夹"))
	if len(got) != 1 || len([]rune(got[0])) != 64 || !strings.Contains(got[0], "…") || !strings.HasSuffix(got[0], "Installers") {
		t.Errorf("folder %q", got)
	}
}

// Ported from InstallersPage.test.tsx: cannot scan without folders to search.
func TestNoFolders(t *testing.T) {
	p := start(t, &cleanflowfake.Cleaner{}, newFakes(t, []string{}))
	p.WaitFor("text:还没有要查找的文件夹，可以在「设置」里添加。", wait)
	if p.Enabled("id:scan") {
		t.Error("扫描 is enabled")
	}
	if p.Exists("role:group 查找的文件夹") {
		t.Error("lists folders")
	}
}

// Adding a folder in 设置 enables 扫描 and lists the folder without leaving the page.
func TestFolderAdded(t *testing.T) {
	fakes := newFakes(t, []string{})
	p := start(t, &cleanflowfake.Cleaner{}, fakes)
	p.WaitFor("id:no-folders", wait)
	next := p.stores.Settings.Value()
	next.Cleanup.InstallerRoots = roots[:1]
	p.stores.Settings.Save(next, func(settings.AppSettings, error) {})
	p.WaitFor("role:group 查找的文件夹", wait)
	if !p.Enabled("id:scan") {
		t.Error("扫描 is still disabled")
	}
}

// Ported from InstallersPage.test.tsx: uses the Windows wording in a Windows window.
func TestWindowsWording(t *testing.T) {
	if got := installers.TrashLine(true); got != "清理的安装包会移到回收站，可以随时放回。" {
		t.Errorf("trash line %q", got)
	}
	if got := installers.ScanDescription(true); !strings.Contains(got, ".exe、.msi、.msix、.appx") {
		t.Errorf("description %q", got)
	}
	if got := installers.Notice(true); got != "清理的安装包会移到回收站，可以随时放回。最近 7 天内下载的默认不选。" {
		t.Errorf("notice %q", got)
	}
	if got := installers.ScanDescription(false); !strings.Contains(got, ".dmg、.pkg、.iso、.xip") {
		t.Errorf("macOS description %q", got)
	}
}

// Ported from InstallersPage.test.tsx: leaves recent and attached installers unchecked and moves the rest to the Trash.
func TestScanAndClean(t *testing.T) {
	c := &cleanflowfake.Cleaner{
		OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
			return scan(), nil
		},
		OnClean: func(context.Context, app.CleanerID, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
			return cleanResult(), nil
		}}
	p := start(t, c, newFakes(t, roots))
	p.Press("id:scan")
	p.WaitFor("id:category-list", wait)
	if got := c.Scans(); !reflect.DeepEqual(got, []app.CleanerID{app.CleanerInstallers}) {
		t.Errorf("scans %v", got)
	}
	p.Find("role:region 找到的安装包")
	p.Find("text:清理的安装包会移到废纸篓，可以随时放回。最近 7 天内下载的和仍在挂载的默认不选。")
	if !p.Find("id:check-disk-images").Props.Checked {
		t.Error("磁盘映像 is unchecked")
	}
	if p.Find("id:check-recent-installers").Props.Checked {
		t.Error("最近下载或正在使用的安装包 is checked")
	}
	p.Press("role:button 展开最近下载或正在使用的安装包")
	p.Find("id:items-recent-installers >> text:/磁盘映像 · 已挂载/")
	if got := p.stat("已选择"); got != "2 项" {
		t.Errorf("已选择 %q", got)
	}
	if got := p.stat("已选大小"); got != "220 MB" {
		t.Errorf("已选大小 %q", got)
	}

	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if got := p.Find("id:plan-message").Text; got != "将清理 2 项，共 220 MB。它们会移到废纸篓，可以随时放回。" {
		t.Errorf("message %q", got)
	}
	if p.Exists("role:region 永久删除") {
		t.Error("lists a permanent group")
	}
	var rows []string
	for i := 0; p.Exists("id:plan-trash-" + strconv.Itoa(i)); i++ {
		rows = append(rows, strings.Join(texts(p.Find("id:plan-trash-"+strconv.Itoa(i))), ""))
	}
	if want := []string{"磁盘映像1 项 · 140 MB", "安装器1 项 · 80 MB"}; !reflect.DeepEqual(rows, want) {
		t.Errorf("trash rows %q", rows)
	}
	// Nothing is deleted for good, so 清理 has focus.
	if !p.Find("id:confirm-ok").Focused {
		t.Error("清理 does not have the focus")
	}
	p.Press("id:confirm-ok")
	p.WaitFor("id:done", wait)
	if got := c.Cleans(); len(got) != 1 || got[0].ID != app.CleanerInstallers ||
		!reflect.DeepEqual(got[0].Req, cleanup.CleanRequest{CategoryIDs: []string{"disk-images", "installer-packages"}}) {
		t.Errorf("cleans %+v", got)
	}
	p.Find("text:清理完成")
	p.Find("id:done >> text:220 MB")
	p.Press("id:done-close")
	p.WaitForGone("id:done", wait)
	p.WaitFor("id:scan", wait)
}

// Ported from InstallersPage.test.tsx: says so when there are no installers.
func TestNothingFound(t *testing.T) {
	p := start(t, &cleanflowfake.Cleaner{OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
		return cleanflowfake.Result(), nil
	}}, newFakes(t, roots))
	p.Press("id:scan")
	p.WaitFor("text:没有找到安装包", wait)
	p.Find("text:这些文件夹里没有旧的安装包或磁盘映像。")
}
