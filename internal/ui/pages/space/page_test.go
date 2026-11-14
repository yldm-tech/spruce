package space_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/ui/pages/space"
	"github.com/yldm-tech/spruce/internal/ui/pages/space/spacefake"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

type page struct {
	*nsgotest.App
	svc    *spacefake.Service
	files  *spacefake.Files
	fakes  *storefake.Services
	stores *store.Stores
	shown  *core.Signal[bool]
}

// start mounts the page on svc and store fakes holding the fixture's disks and folders. The page sits behind a switch (shown), so a test can leave it.
func start(t *testing.T, svc *spacefake.Service, fakes *storefake.Services, opts ...nsgotest.Option) *page {
	t.Helper()
	fakes.Locations = spacefake.Locations()
	p := &page{svc: svc, files: &spacefake.Files{}, fakes: fakes}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		space.DepsKey.Provide(cx, space.Deps{Space: svc, Files: p.files})
		p.shown = core.NewSignal(cx, true)
		return core.Show{When: p.shown.Get, Then: space.Page}
	}, append([]nsgotest.Option{nsgotest.Size(1052, 820)}, opts...)...)
	return p
}

func newFakes(t *testing.T) *storefake.Services { return storefake.New(t.TempDir()) }

// waitTicking settles until selector matches one view, moving the manual clock so throttled progress (async.Feed) comes through.
func (p *page) waitTicking(selector string) {
	p.WaitUntil(wait, func() bool {
		if len(p.Query(selector)) == 1 {
			return true
		}
		p.Advance(20 * time.Millisecond)
		return false
	})
}

// actionStat is the figure beside the round button: start is the scanned location's, else 上次扫描.
func (p *page) actionStat(start bool) (label, value string) {
	bar := p.Find("id:rescan").Parent
	stat := bar.Children[len(bar.Children)-1]
	if start {
		stat = bar.Children[0]
	}
	return stat.Children[0].Text, stat.Children[1].Text
}

// cellLabels are the labels of the treemap's buttons, in order.
func (p *page) cellLabels() []string {
	var out []string
	for _, v := range p.Query("id:treemap >> role:button") {
		out = append(out, v.Props.Label)
	}
	return out
}

const homeList = `role:group "个人文件夹 的内容"`

func TestStartListsDisksAndFolders(t *testing.T) {
	p := start(t, &spacefake.Service{Listings: spacefake.Listings()}, newFakes(t))
	p.WaitFor("id:volume-/", wait)
	p.Find(`text:"空间透视"`)
	p.Find("text:看看磁盘空间都被什么占用了")
	disk := p.Find("id:volume-/")
	if disk.Props.Label != "Macintosh HD，可用 120 GB，共 500 GB，已用 76%" {
		t.Errorf("disk label %q", disk.Props.Label)
	}
	p.Find("id:volume-/ >> text:可用 120 GB，共 500 GB")
	p.Find("id:volume-/Volumes/Archive >> text:Archive")
	for _, id := range []string{"home", "downloads", "applications"} {
		p.Find("id:target-" + id)
	}
	p.Find(`id:target-downloads >> text:"/Users/me/Downloads"`)
	p.Find("id:choose-folder >> text:选择其他文件夹…")
	if p.Exists("id:return-results") || p.Exists("id:change-location") {
		t.Error("a header action shows without a scan")
	}
}

func TestChooseFolderScansIt(t *testing.T) {
	svc := &spacefake.Service{Listings: spacefake.Listings()}
	p := start(t, svc, newFakes(t))
	p.PickDirectories = func(core.DirectoriesRequest) []core.Directory {
		return []core.Directory{{Name: "Music", Path: "/Users/me/Music"}}
	}
	p.WaitFor("id:choose-folder", wait)
	p.Press("id:choose-folder")
	p.WaitFor("text:正在分析「Music」…", wait)
	// The fake records the scan on async.Do's goroutine, which can run after the page already shows the scan.
	p.WaitUntil(wait, func() bool { return len(svc.Scans()) > 0 })
	if got := svc.Scans(); len(got) != 1 || got[0] != "/Users/me/Music" {
		t.Errorf("scans %v", got)
	}
	if len(p.DirectoryPicks) != 1 || p.DirectoryPicks[0].Limit != 1 {
		t.Errorf("picks %+v", p.DirectoryPicks)
	}
}

func TestCancelledPickerStaysPut(t *testing.T) {
	svc := &spacefake.Service{Listings: spacefake.Listings()}
	p := start(t, svc, newFakes(t))
	p.PickDirectories = func(core.DirectoriesRequest) []core.Directory { return nil }
	p.WaitFor("id:choose-folder", wait)
	p.Press("id:choose-folder")
	p.WaitUntil(wait, func() bool { return len(p.DirectoryPicks) == 1 })
	p.Find("id:volume-/")
	p.Find("id:choose-folder >> text:选择其他文件夹…")
	if len(svc.Scans()) != 0 {
		t.Errorf("scans %v", svc.Scans())
	}
}

func TestScanShowsProgressThenResults(t *testing.T) {
	progress := make(chan app.SpaceProgress)
	finish := make(chan app.SpaceSummary)
	svc := &spacefake.Service{Listings: spacefake.Listings(),
		OnScan: func(ctx context.Context, path string, send func(app.SpaceProgress)) (app.SpaceSummary, error) {
			for {
				select {
				case pr := <-progress:
					send(pr)
				case sum := <-finish:
					return sum, nil
				}
			}
		}}
	p := start(t, svc, newFakes(t))
	p.WaitFor("id:target-home", wait)
	p.Press("id:target-home")
	p.WaitFor("text:正在分析「个人文件夹」…", wait)
	// The fake records the scan on async.Do's goroutine, which can run after the page already shows the scan.
	p.WaitUntil(wait, func() bool { return len(svc.Scans()) > 0 })
	if got := svc.Scans(); len(got) != 1 || got[0] != spacefake.Home {
		t.Fatalf("scans %v", got)
	}
	p.Find("text:正在准备…")

	progress <- app.SpaceProgress{Files: 1204, Dirs: 80, Bytes: 2.5e9, Path: spacefake.Home + "/Library/Caches"}
	p.waitTicking("text:已扫描 1,204 个文件 · 2.5 GB")
	if got := p.Find("id:space-scan-path").Text; got != spacefake.Home+"/Library/Caches" {
		t.Errorf("path %q", got)
	}

	finish <- spacefake.Summary(time.Now())
	p.waitTicking(homeList)
	p.Find(homeList + ` >> role:button "Projects"`)
	p.Find(homeList + " >> text:其他 4 项")
	p.Find(homeList + " >> text:30 个小文件")
	p.Find(homeList + " >> text:60% · 4,000 个文件")
	// The bottom bar: the scanned size, 重新扫描 and when.
	if l, v := p.actionStat(true); l != "个人文件夹" || v != "10 GB" {
		t.Errorf("start stat %q %q", l, v)
	}
	if l, v := p.actionStat(false); l != "上次扫描" || v != "刚刚" {
		t.Errorf("end stat %q %q", l, v)
	}
}

func TestStoppedScanGoesBackWithoutError(t *testing.T) {
	svc := &spacefake.Service{Listings: spacefake.Listings()}
	p := start(t, svc, newFakes(t))
	p.WaitFor("id:target-downloads", wait)
	p.Press("id:target-downloads")
	p.WaitFor("id:space-stop", wait)
	p.Press("id:space-stop")
	p.WaitFor("id:volume-/", wait)
	if p.Exists("role:alert") {
		t.Errorf("an alert shows: %v", p.Texts())
	}
}

func TestFailedScanSaysWhy(t *testing.T) {
	svc := &spacefake.Service{Listings: spacefake.Listings(),
		OnScan: func(context.Context, string, func(app.SpaceProgress)) (app.SpaceSummary, error) {
			return app.SpaceSummary{}, errors.New("没有读取这个文件夹的权限")
		}}
	p := start(t, svc, newFakes(t))
	p.WaitFor("id:target-applications", wait)
	p.Press("id:target-applications")
	if got := p.WaitFor("role:alert", wait).Text; got != "没有读取这个文件夹的权限" {
		t.Errorf("alert %q", got)
	}
	p.Find("id:volume-/")
}

func TestLeavingStopsTheScan(t *testing.T) {
	ended := make(chan struct{})
	svc := &spacefake.Service{Listings: spacefake.Listings(),
		OnScan: func(ctx context.Context, _ string, _ func(app.SpaceProgress)) (app.SpaceSummary, error) {
			<-ctx.Done()
			close(ended)
			return app.SpaceSummary{}, ctx.Err()
		}}
	p := start(t, svc, newFakes(t))
	p.WaitFor("id:target-home", wait)
	p.Press("id:target-home")
	p.WaitFor("id:space-stop", wait)
	p.Post(func() { p.shown.Set(false) })
	p.WaitForGone("id:space-page", wait)
	select {
	case <-ended:
	case <-time.After(wait):
		t.Fatal("the scan goes on after the page went")
	}
}

func TestResultsOpenFoldersFromTheListTheMapAndThePath(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	if len(svc.Scans()) != 0 {
		t.Error("the kept scan was scanned again")
	}
	if got := svc.ChildrenCalls(); len(got) != 1 || got[0] != 0 {
		t.Errorf("children %v", got)
	}
	p.Find(`role:group "个人文件夹 的空间分布图"`)
	// The treemap draws one cell per child with a size; folders are buttons.
	p.WaitUntil(wait, func() bool { return len(p.cellLabels()) == 2 })
	if got := p.cellLabels(); !slices.Equal(got, []string{"打开 Projects，6 GB", "打开 Movies，500 MB"}) {
		t.Errorf("cells %v", got)
	}

	// The labelled cells draw their name over a soft shadow.
	if sh := p.Find(`id:treemap >> text:"Projects"`).TextShadow; sh.Color == 0 || sh.Offset.Y != 1 || sh.Radius != 2 {
		t.Errorf("cell text shadow %+v", sh)
	}

	p.Press(homeList + ` >> role:button "Projects"`)
	p.WaitFor(`role:group "Projects 的内容"`, wait)
	if got := svc.ChildrenCalls(); got[len(got)-1] != 1 {
		t.Errorf("children %v", got)
	}
	if got := p.Find("id:space-path-current").Text; got != "Projects" {
		t.Errorf("current %q", got)
	}

	p.Press(`id:space-path >> role:button "个人文件夹"`)
	p.WaitFor(homeList, wait)

	p.WaitUntil(wait, func() bool { return len(p.cellLabels()) == 2 })
	p.Press(`id:treemap >> role:button "打开 Projects，6 GB"`)
	p.WaitFor(`role:group "Projects 的内容"`, wait)
}

func TestTrashAfterConfirming(t *testing.T) {
	svc := spacefake.New(time.Now())
	root := spacefake.Root
	root.Size = 7e9
	svc.TrashResult = app.SpaceTrashResult{Trashed: 3e9, Items: 1, Failed: []app.SpaceFailure{}, Root: root}
	fakes := newFakes(t)
	p := start(t, svc, fakes)
	p.WaitFor(homeList, wait)
	trashGen := p.stores.Trash.Generation()
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	p.WaitFor("id:trash-confirm", wait)
	p.Find("id:trash-confirm >> text:将把「big.iso」（3 GB）移到废纸篓。需要时可以从废纸篓放回原处。")
	if !p.stores.Block.Blocked() {
		t.Error("the shell is not blocked while the confirmation shows")
	}
	p.Press("id:confirm-ok")
	p.WaitForGone("id:trash-confirm", wait)
	if got := svc.Trashes(); len(got) != 1 || len(got[0]) != 1 || got[0][0] != spacefake.ISO.Path {
		t.Errorf("trashes %v", got)
	}
	if got := p.WaitFor("id:space-notice", wait).Text; got != "已把「big.iso」移到废纸篓，清倒废纸篓后可释放 3 GB" {
		t.Errorf("notice %q", got)
	}
	if _, v := p.actionStat(true); v != "7 GB" {
		t.Errorf("stat %q", v)
	}
	// The folder is asked for again, so its sizes follow; the Trash's size is measured again.
	p.WaitUntil(wait, func() bool {
		n := 0
		for _, id := range svc.ChildrenCalls() {
			if id == 0 {
				n++
			}
		}
		return n == 2
	})
	if p.stores.Trash.Generation() == trashGen {
		t.Error("the Trash's generation did not change")
	}
}

func TestCancelledConfirmationMovesNothing(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	p.WaitFor("id:trash-confirm", wait)
	p.Key("Escape")
	p.WaitForGone("id:trash-confirm", wait)
	if len(svc.Trashes()) != 0 {
		t.Errorf("trashes %v", svc.Trashes())
	}
}

func TestLockedEntrySaysWhy(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	trash := p.Find(homeList + ` >> role:button "将 Movies 移到废纸篓"`)
	if trash.Style.Opacity == 0 || trash.Style.Opacity >= 1 {
		t.Errorf("the locked button is not dimmed: opacity %v", trash.Style.Opacity)
	}
	p.Press(homeList + ` >> role:button "将 Movies 移到废纸篓"`)
	if got := p.WaitFor("id:space-notice-error", wait).Text; got != "「Movies」不能移走：个人文件夹和磁盘最上一层的项目请在访达中处理" {
		t.Errorf("alert %q", got)
	}
	if p.Exists("id:trash-confirm") || len(svc.Trashes()) != 0 {
		t.Error("asked to move a locked entry")
	}
	// Folded groups have no actions at all.
	if p.Exists(homeList + " >> role:button /其他 4 项/") {
		t.Error("a folded group has a button")
	}
}

func TestEntryLeftInPlaceSaysWhy(t *testing.T) {
	svc := spacefake.New(time.Now())
	svc.TrashResult = app.SpaceTrashResult{Failed: []app.SpaceFailure{{Path: spacefake.ISO.Path, Reason: "没有权限移动这个项目"}}, Root: spacefake.Root}
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:button "将 big.iso 移到废纸篓"`)
	p.WaitFor("id:trash-confirm", wait)
	p.Press("id:confirm-ok")
	if got := p.WaitFor("id:space-notice-error", wait).Text; got != "「big.iso」没有移走：没有权限移动这个项目" {
		t.Errorf("alert %q", got)
	}
}

func TestRevealInFinder(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:button "在访达中显示 big.iso"`)
	p.WaitUntil(wait, func() bool { return len(p.files.Reveals()) == 1 })
	if got := p.files.Reveals(); got[0] != spacefake.ISO.Path {
		t.Errorf("reveals %v", got)
	}
	p.files.Err = errors.New("无法在访达中显示")
	p.Press(homeList + ` >> role:button "在访达中显示 big.iso"`)
	if got := p.WaitFor("id:space-reveal-error", wait).Text; got != "无法在访达中显示" {
		t.Errorf("reveal error %q", got)
	}
}

func TestLargeFilesTab(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	if svc.LargeCalls() != 0 {
		t.Error("the large files were asked for before their tab showed")
	}
	p.Press("id:tab-large")
	if !p.Find("id:tab-large").Props.Selected {
		t.Error("大文件 is not selected")
	}
	p.WaitFor(`role:group "大文件"`, wait)
	if svc.LargeCalls() != 1 {
		t.Errorf("large calls %d", svc.LargeCalls())
	}
	p.Find(`role:group "大文件" >> text:big.iso`)
	p.Find(`role:group "大文件" >> text:"` + spacefake.Home + `/Projects"`)
	p.Find("text:「个人文件夹」中 500 MB 以上的文件，最多列出 200 个。可以在「设置」中调整大小。")
	if p.Exists("id:treemap") {
		t.Error("the map shows on the 大文件 tab")
	}
}

// TestTabsFollowTheArrowKeys checks ← and → on the tabs, which only macOS can focus: the tab list is one Tab stop.
func TestTabsFollowTheArrowKeys(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	p.WaitFor(homeList, wait)
	for range 20 {
		if p.Find("id:tab-map").Focused {
			break
		}
		p.Key("Tab")
	}
	if !p.Find("id:tab-map").Focused {
		t.Fatal("Tab never reached the tabs")
	}
	p.Key("right")
	p.WaitFor(`role:group "大文件"`, wait)
	if !p.Find("id:tab-large").Props.Selected || !p.Find("id:tab-large").Focused {
		t.Error("→ did not select and focus 大文件")
	}
	p.Key("left")
	p.WaitFor("id:treemap", wait)
	if !p.Find("id:tab-map").Props.Selected {
		t.Error("← did not select 空间分布")
	}
}

func TestDeniedBannerGuidesToFullDiskAccess(t *testing.T) {
	svc := spacefake.New(time.Now())
	svc.Kept.Denied, svc.Kept.DeniedPaths = 3, []string{spacefake.Home + "/Library/Mail"}
	fakes := newFakes(t)
	fakes.Permission = maintenance.PermissionDenied
	fakes.Guided = maintenance.PermissionGranted
	p := start(t, svc, fakes)
	p.WaitFor("text:3 个文件夹无权读取", wait)
	p.Find("text:例如 " + spacefake.Home + "/Library/Mail")
	p.WaitFor("text:开启「完全磁盘访问权限」后重新扫描，就能算进这些文件夹", wait)
	p.Press("id:grant-full-disk-access")
	p.WaitFor("text:已获得完全磁盘访问权限，重新扫描即可算进这些文件夹", wait)
	if _, guides := fakes.Counts(); guides != 1 {
		t.Errorf("guides %d", guides)
	}
	if n := len(p.Query(`role:button "重新扫描"`)); n != 2 {
		t.Errorf("%d 重新扫描 buttons", n)
	}
}

func TestChangeLocationAndReturn(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Press("id:change-location")
	p.WaitFor("id:volume-/", wait)
	p.Find(`id:return-results >> text:返回「个人文件夹」`)
	p.Press("id:return-results")
	p.WaitFor(homeList, wait)
}

func TestRescanTheSameLocation(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Press("id:rescan")
	p.WaitFor("text:正在分析「个人文件夹」…", wait)
	// The fake records the scan on async.Do's goroutine, which can run after the page already shows the scan.
	p.WaitUntil(wait, func() bool { return len(svc.Scans()) > 0 })
	if got := svc.Scans(); len(got) != 1 || got[0] != spacefake.Home {
		t.Errorf("scans %v", got)
	}
}

func TestStaleFolderOffersRescan(t *testing.T) {
	svc := spacefake.New(time.Now())
	delete(svc.Listings, 0)
	p := start(t, svc, newFakes(t))
	if got := p.WaitFor("id:space-listing-error", wait).Text; !strings.Contains(got, "已经不在扫描结果里") {
		t.Errorf("error %q", got)
	}
	p.Press(`role:alert >> role:button "重新扫描"`)
	p.WaitFor("text:正在分析「个人文件夹」…", wait)
}

// TestConfirmationFocus checks where the keyboard's focus goes (macOS): onto 移到废纸篓 as the confirmation opens, since a move can be undone, and back to the row's button as Escape closes it.
func TestConfirmationFocus(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	p.WaitFor(homeList, wait)
	opener := homeList + ` >> role:button "将 big.iso 移到废纸篓"`
	p.Press(opener)
	p.WaitFor("id:trash-confirm", wait)
	p.WaitUntil(wait, func() bool { return p.Find("id:confirm-ok").Focused })
	p.Key("Escape")
	p.WaitForGone("id:trash-confirm", wait)
	p.WaitUntil(wait, func() bool { return p.Find(opener).Focused })
}
