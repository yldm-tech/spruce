package smartscan_test

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
	"github.com/yldm-tech/spruce/internal/engine/health"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/pages/smartscan"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

type entry struct {
	name     string
	size     int64
	selected bool
}

func category(source, id, title string, selected bool, items []entry, warning, command string) cleanup.Category {
	c := cleanup.Category{ID: source + ":" + id, Title: title, Description: title + "的说明", Icon: "cache", Kind: cleanup.KindCache,
		Deletion: cleanup.DeletePermanent, Selected: selected, Source: source, Warning: warning, Command: command}
	for _, e := range items {
		c.Items = append(c.Items, cleanup.Item{ID: e.name, Path: "/Users/me/" + e.name, Size: e.size, Selected: e.selected,
			ModTime: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)})
		c.Size += e.size
	}
	return c
}

// categories are SmartScanPage.test.tsx's CATEGORIES.
func categories() []cleanup.Category {
	return []cleanup.Category{
		category("system-junk", "app-caches", "应用缓存", true, []entry{{"caches-a", 300_000_000, false}, {"caches-b", 200_000_000, false}}, "", ""),
		category("system-junk", "app-caches-running", "正在运行的应用的缓存", false, []entry{{"running", 50_000_000, false}}, "这些应用正在运行，建议先退出再清理", ""),
		category("dev-junk", "tool-npm", "npm 缓存", true, []entry{{"npm", 100_000_000, false}}, "", "npm cache clean --force"),
		category("dev-junk", "tool-xcode-device-support", "设备支持文件", false, []entry{{"ios16", 400_000_000, true}, {"ios17", 600_000_000, false}}, "", ""),
	}
}

// scanOf is SmartScanPage.test.tsx's scanOf: the three sources' states, the installers' failed.
func scanOf(cats []cleanup.Category) cleanup.ScanResult {
	r := cleanflowfake.Result(cats...)
	r.Sources = []cleanup.SourceState{
		{ID: "system-junk", Status: cleanup.SourceDone, Bytes: 550_000_000, Items: 3},
		{ID: "dev-junk", Status: cleanup.SourceDone, Bytes: 1_100_000_000, Items: 3},
		{ID: "installers", Status: cleanup.SourceFailed, Error: "需要「完全磁盘访问权限」才能查找"},
	}
	return r
}

func cleanResult() cleanup.CleanResult {
	return cleanup.CleanResult{Freed: 900_000_000, Removed: 4, Categories: []cleanup.CategoryOutcome{
		{CategoryID: "system-junk:app-caches", Title: "应用缓存", Source: "system-junk", Removed: 2, Freed: 500_000_000},
		{CategoryID: "dev-junk:tool-npm", Title: "npm 缓存", Source: "dev-junk", Removed: 1, Freed: 100_000_000},
	}}
}

type page struct {
	*nsgotest.App
	c      *cleanflowfake.Cleaner
	fakes  *storefake.Services
	stores *store.Stores
	nav    *nav.Navigator
	shown  *core.Signal[bool]
	tb     testing.TB
}

// newFakes are the stores of SmartScanPage.test.tsx's beforeEach: no runs, no alerts, Full Disk Access denied and one 1 TB disk with 120 GB free.
func newFakes(t *testing.T) *storefake.Services {
	f := storefake.New(t.TempDir())
	f.Permission = maintenance.PermissionDenied
	f.Locations = app.SpaceLocations{Volumes: []app.SpaceVolume{{Path: "/", Name: "Macintosh HD", Total: 1000e9, Free: 120e9, Startup: true}}}
	return f
}

func start(t *testing.T, c *cleanflowfake.Cleaner, fakes *storefake.Services) *page {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording, SI sizes and the Full Disk Access card")
	}
	p := &page{c: c, fakes: fakes, tb: t}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		p.nav = nav.NewNavigator(cx, nav.SmartScan)
		nav.Key.Provide(cx, p.nav)
		smartscan.DepsKey.Provide(cx, smartscan.Deps{Cleanup: c, Files: &cleanflowfake.Files{}})
		// The page sits behind a switch (shown), so a test can leave it and open it again.
		p.shown = core.NewSignal(cx, true)
		return core.Show{When: p.shown.Get, Then: smartscan.Page}
	}, nsgotest.Size(1052, 820))
	p.WaitUntil(wait, func() bool { return p.stores.Settings.Loaded() })
	return p
}

func scanning(r cleanup.ScanResult) *cleanflowfake.Cleaner {
	return &cleanflowfake.Cleaner{
		OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
			return r, nil
		},
		OnClean: func(context.Context, app.CleanerID, cleanup.CleanRequest, func(cleanup.Progress)) (cleanup.CleanResult, error) {
			return cleanResult(), nil
		}}
}

// text waits for selector and returns its text.
func (p *page) text(selector string) string {
	p.WaitFor(selector, wait)
	return p.Find(selector).Text
}

// waitText waits until selector shows want.
func (p *page) waitText(selector, want string) {
	p.tb.Helper()
	p.WaitUntil(wait, func() bool {
		vs := p.Query(selector)
		return len(vs) == 1 && vs[0].Text == want
	})
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

// Ported from SmartScanPage.test.tsx: offers the scan and the status cards.
func TestIdle(t *testing.T) {
	p := start(t, &cleanflowfake.Cleaner{}, newFakes(t))
	p.Find("role:header 智能扫描")
	p.Find("text:一次扫描系统垃圾、开发者垃圾和安装包，按推荐勾选可以放心清理的")
	if got := p.Find("id:scan").Props.Role; got != core.RoleButton {
		t.Errorf("扫描 role %v", got)
	}
	p.Find("id:scan >> text:扫描")
	p.Find("id:hero >> text:一次检查系统垃圾、开发者垃圾和安装包")

	p.waitText("id:status-disk-value", "可用 120 GB")
	p.waitText("id:status-disk-note", "Macintosh HD 共 1 TB")
	if v := p.Find("id:disk-meter").Props.Value; v.Now != 88 {
		t.Errorf("used %+v", v)
	}
	p.WaitFor("id:disk-link >> text:空间透视", wait)
	p.waitText("id:status-cpu-value", "没有长时间高占用的进程")
	p.waitText("id:status-fda-value", "未开启")
	p.Find("id:grant >> text:去授权")
	p.waitText("id:status-maintenance-value", "还没有做过维护")
	p.WaitFor("id:maintenance-link", wait)

	p.Press("id:maintenance-link")
	if got := p.nav.Current(); got != nav.Maintenance {
		t.Errorf("去维护 went to %q", got)
	}
}

// Ported from SmartScanPage.test.tsx: shows a sustained CPU alert, a recent maintenance run and the granted permission.
func TestIdleAlertRecentMaintenanceAndGranted(t *testing.T) {
	fakes := newFakes(t)
	fakes.Alerts = []health.Alert{{PID: 1, Name: "Xcode", CPUPercent: 180, Duration: 12 * time.Minute, Threshold: 40}}
	fakes.Permission = maintenance.PermissionGranted
	ended := time.Now().Add(-3 * 24 * time.Hour)
	fakes.Runs = []history.Run{{ID: "m", Kind: history.KindMaintenance, StartedAt: ended, EndedAt: ended}}
	p := start(t, &cleanflowfake.Cleaner{}, fakes)
	p.waitText("id:status-cpu-value", "Xcode 已经持续占用 180% CPU 有 12 分钟")
	p.waitText("id:status-cpu-note", "如果不是你正在用它，可以看看是什么在运行")
	p.waitText("id:status-fda-value", "已开启")
	if p.Exists("id:grant") {
		t.Error("去授权 with the permission granted")
	}
	p.waitText("id:status-maintenance-value", "上次维护：3天前")
	p.waitText("id:status-maintenance-note", "最近做过维护")
	if p.Exists("id:maintenance-link") {
		t.Error("去维护 three days after 维护")
	}

	// A second alert adds a count; the card follows the monitor.
	fakes.Alert([]health.Alert{fakes.Alerts[0], {PID: 2, Name: "node", CPUPercent: 90, Duration: 11 * time.Minute, Threshold: 40}})
	p.waitText("id:status-cpu-note", "另有 1 个进程也在高占用")
	p.Press("id:cpu-link")
	if got := p.nav.Current(); got != nav.Health {
		t.Errorf("健康监测 went to %q", got)
	}
}

// Ported from SmartScanPage.test.tsx: offers 维护 when it last ran more than 30 days ago, and says when CPU alerts are off.
func TestMaintenanceDueAndAlertsOff(t *testing.T) {
	fakes := newFakes(t)
	fakes.Settings.Health.Enabled = false
	old := time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)
	fakes.Runs = []history.Run{{ID: "m", Kind: history.KindMaintenance, StartedAt: old, EndedAt: old}}
	p := start(t, &cleanflowfake.Cleaner{}, fakes)
	p.WaitFor("id:maintenance-link", wait)
	p.waitText("id:status-maintenance-note", "已经超过 30 天没有维护了，可以再做一次")
	p.waitText("id:status-cpu-value", "高 CPU 提醒已关闭")
	p.waitText("id:status-cpu-note", "可以在「设置」里重新打开")
	p.WaitFor("id:cpu-link >> text:去设置", wait)
	p.Press("id:cpu-link")
	if got := p.nav.Current(); got != nav.Settings {
		t.Errorf("去设置 went to %q", got)
	}
}

// The maintenance card reads the history again when a run is recorded.
func TestMaintenanceFollowsTheHistory(t *testing.T) {
	fakes := newFakes(t)
	p := start(t, &cleanflowfake.Cleaner{}, fakes)
	p.waitText("id:status-maintenance-value", "还没有做过维护")
	now := time.Now()
	fakes.Record(history.Run{ID: "m", Kind: history.KindMaintenance, StartedAt: now, EndedAt: now})
	p.waitText("id:status-maintenance-value", "上次维护：刚刚")
	p.WaitForGone("id:maintenance-link", wait)
}

// A low disk warns and links to 空间透视 with other words.
func TestLowDisk(t *testing.T) {
	fakes := newFakes(t)
	fakes.Locations.Volumes[0].Free = 15e9
	p := start(t, &cleanflowfake.Cleaner{}, fakes)
	p.waitText("id:status-disk-note", "Macintosh HD 共 1 TB，空间不多了")
	p.WaitFor("id:disk-link >> text:看看是什么占用了空间", wait)
	p.Press("id:disk-link")
	if got := p.nav.Current(); got != nav.Space {
		t.Errorf("went to %q", got)
	}
}

// reopen leaves the page and opens it again.
func (p *page) reopen() {
	p.Post(func() { p.shown.Set(false) })
	p.WaitForGone("id:status", wait)
	p.Post(func() { p.shown.Set(true) })
	p.WaitFor("id:status", wait)
}

// Full Disk Access says 正在检查… with no 去授权 until the first check answers; it and the free space are read again each time the page opens, since both change outside the app.
func TestStatusIsReadAgainAsThePageOpens(t *testing.T) {
	fakes := newFakes(t)
	release := make(chan struct{})
	fakes.CheckRelease = release
	p := start(t, &cleanflowfake.Cleaner{}, fakes)
	p.waitText("id:status-fda-value", "正在检查…")
	if p.Exists("id:grant") {
		t.Error("去授权 offered before the first check answered")
	}
	close(release)
	p.waitText("id:status-fda-value", "未开启")
	p.WaitFor("id:grant", wait)
	p.waitText("id:status-disk-value", "可用 120 GB")

	fakes.SetPermission(maintenance.PermissionGranted)
	l := fakes.Deps().Locations.Locations(context.Background())
	l.Volumes = []app.SpaceVolume{{Path: "/", Name: "Macintosh HD", Total: 1000e9, Free: 200e9, Startup: true}}
	fakes.SetLocations(l)
	p.reopen()
	p.waitText("id:status-fda-value", "已开启")
	p.WaitForGone("id:grant", wait)
	p.waitText("id:status-disk-value", "可用 200 GB")
}

// 去授权 waits in System Settings and says when it could not open it.
func TestGrantFullDiskAccess(t *testing.T) {
	fakes := newFakes(t)
	fakes.GuideErr = errors.New("no System Settings")
	p := start(t, &cleanflowfake.Cleaner{}, fakes)
	p.waitText("id:status-fda-value", "未开启")
	p.Press("id:grant")
	p.waitText("id:grant-error", "无法打开系统设置")

	fakes.GuideErr = nil
	p.Press("id:grant")
	p.waitText("id:status-fda-value", "已开启")
	p.WaitForGone("id:grant", wait)
	p.WaitForGone("id:grant-error", wait)
}

// Ported from SmartScanPage.test.tsx: shows each source's progress and stops.
func TestScanningProgressAndStop(t *testing.T) {
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
	p := start(t, c, newFakes(t))
	p.Press("id:scan")
	p.WaitFor("id:scanning", wait)
	p.WaitUntil(wait, func() bool { return reflect.DeepEqual(c.Scans(), []app.CleanerID{app.CleanerSmartScan}) })
	if n := len(p.Query("text:等待开始…")); n != 3 {
		t.Errorf("%d rows wait", n)
	}
	for _, id := range []string{"system-junk", "dev-junk", "installers"} {
		p.Find("id:sweep-" + id)
	}
	if !p.Find("id:scan-ring").Props.Value.NoNow {
		t.Error("the ring knows the total before the first event")
	}

	progress <- cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 1500, Total: 3000, Bytes: 2_000_000, Sources: []cleanup.SourceState{
		{ID: "system-junk", Status: cleanup.SourceDone, Done: 5, Total: 5, Bytes: 2_000_000, Items: 4},
		{ID: "dev-junk", Status: cleanup.SourceScanning, Done: 1, Total: 2},
		{ID: "installers", Status: cleanup.SourceFailed, Error: "需要权限"},
	}}
	p.ticking("text:完成，找到 4 项，共 2 MB")
	if got := p.Find("id:row-status-dev-junk").Text; got != "正在扫描… 已找到 0 B" {
		t.Errorf("dev-junk %q", got)
	}
	if got := p.Find("id:row-status-installers"); got.Text != "需要权限" {
		t.Errorf("installers %q", got.Text)
	}
	if v := p.Find("id:bar-dev-junk").Props.Value; v.Now != 50 {
		t.Errorf("dev-junk bar %+v", v)
	}
	if v := p.Find("id:scan-ring").Props.Value; v.NoNow || v.Now != 50 {
		t.Errorf("ring %+v", v)
	}
	p.Find("id:scan-ring >> text:50%")
	if p.Exists("id:sweep-dev-junk") {
		t.Error("a source that knows its total still sweeps")
	}
	p.Find("id:scanning >> text:/^正在扫描…$/")

	p.Press("id:scan-stop")
	select {
	case <-stopped:
	case <-time.After(wait):
		t.Fatal("停止 did not stop the scan")
	}
	p.WaitFor("id:hero", wait)
	if p.Exists("id:scan-error") {
		t.Error("a stopped scan shows an error")
	}
}

// Ported from SmartScanPage.test.tsx: totals what starts checked, sums up each source, and cleans through the same confirmation.
func TestResultsAndClean(t *testing.T) {
	fakes := newFakes(t)
	c := scanning(scanOf(categories()))
	p := start(t, c, fakes)
	p.Press("id:scan")
	p.WaitFor("id:total", wait)
	// 应用缓存 (500 MB), npm (100 MB) and the old device support version (400 MB) start checked.
	if got := p.Find("id:total-value").Text; got != "1 GB" {
		t.Errorf("可清理 %q", got)
	}
	if got := p.Find("id:total-sub").Text; !strings.HasPrefix(got, "已按推荐勾选 4 项。一共找到 6 项（1.7 GB）") {
		t.Errorf("sub %q", got)
	}
	p.FindMounted("text:扫描完成，可清理 1 GB")
	p.Find("id:rescan")

	if got := p.Find("id:source-size-system-junk").Text; got != "550 MB" {
		t.Errorf("系统垃圾 %q", got)
	}
	if got := p.Find("id:source-note-system-junk").Text; got != "3 项，推荐清理 2 项（500 MB）" {
		t.Errorf("系统垃圾 note %q", got)
	}
	if got := p.Find("id:source-error-installers").Text; got != "需要「完全磁盘访问权限」才能查找" {
		t.Errorf("安装包 %q", got)
	}
	p.Find("id:source-link-system-junk >> text:查看详情")

	p.Find("role:region 开发者垃圾的项目")
	p.Find("id:list-dev-junk >> text:已选 1/2 项")
	if got := p.stat("已选择"); got != "4 项" {
		t.Errorf("已选择 %q", got)
	}
	if got := p.stat("已选大小"); got != "1 GB" {
		t.Errorf("已选大小 %q", got)
	}
	// The status cards stay under the results.
	p.Find("id:status-disk")

	// A source's 全不选 leaves the other source's checks.
	p.Press("id:list-system-junk >> id:select-none")
	p.WaitUntil(wait, func() bool { return p.stat("已选择") == "2 项" })

	// Folding a source hides its list.
	p.Press("id:toggle-system-junk")
	if p.Find("id:toggle-system-junk").Props.Expanded {
		t.Error("系统垃圾 still expanded")
	}
	p.WaitForGone("id:list-system-junk", wait)

	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	p.Find("id:plan-commands >> text:npm cache clean --force")
	p.Find("id:plan-permanent >> text:设备支持文件")
	p.Press("id:confirm-ok")

	p.WaitFor("id:done", wait)
	p.WaitUntil(wait, func() bool { return len(c.Cleans()) == 1 })
	want := cleanup.CleanRequest{CategoryIDs: []string{"dev-junk:tool-npm", "dev-junk:tool-xcode-device-support"}, ItemIDs: []string{"npm", "ios16"}}
	if got := c.Cleans()[0]; got.ID != app.CleanerSmartScan || !reflect.DeepEqual(got.Req, want) {
		t.Errorf("clean %+v", got)
	}

	// 完成 goes back to the hero, with the focus on 扫描, and reads the disks again.
	fakes.Locations.Volumes[0].Free = 121e9
	p.Press("id:done-close")
	p.WaitFor("id:hero", wait)
	p.WaitUntil(wait, func() bool { return p.Find("id:scan").Focused })
	p.waitText("id:status-disk-value", "可用 121 GB")
}

// Ported from SmartScanPage.test.tsx: links a source's card to its page.
func TestSourceLinks(t *testing.T) {
	p := start(t, scanning(scanOf(categories())), newFakes(t))
	p.Press("id:scan")
	p.WaitFor("id:sources", wait)
	p.Press("id:source-link-dev-junk")
	if got := p.nav.Current(); got != nav.DevJunk {
		t.Errorf("查看详情 went to %q", got)
	}
}

// Ported from SmartScanPage.test.tsx: says when nothing was found and keeps the status cards.
func TestNothingFound(t *testing.T) {
	p := start(t, scanning(scanOf(nil)), newFakes(t))
	p.Press("id:scan")
	p.WaitFor("id:empty", wait)
	p.Find("id:empty >> text:没有找到需要清理的内容")
	if p.Exists("id:clean") || p.Exists("id:total") || p.Exists("id:all-items") {
		t.Error("the results of an empty scan offer to clean")
	}
	p.Find("id:status-disk")
	if got := p.Find("id:source-note-system-junk").Text; got != "没有找到可以清理的内容" {
		t.Errorf("note %q", got)
	}
}

// The results warn when some locations could not be read.
func TestDenied(t *testing.T) {
	r := scanOf(categories())
	r.Denied = 2
	p := start(t, scanning(r), newFakes(t))
	p.Press("id:scan")
	p.WaitFor("text:有 2 个位置没有读取权限，结果可能不完整", wait)
}

// Ported from SmartScanPage.test.tsx: shows a scan that failed and lets the user try again.
func TestScanFailure(t *testing.T) {
	p := start(t, &cleanflowfake.Cleaner{OnScan: func(context.Context, app.CleanerID, func(cleanup.Progress)) (cleanup.ScanResult, error) {
		return cleanup.ScanResult{}, errors.New("扫描没能完成，详情已记录到日志")
	}}, newFakes(t))
	p.Press("id:scan")
	if got := p.text("id:scan-error"); got != "扫描没能完成，详情已记录到日志" {
		t.Errorf("alert %q", got)
	}
	p.Find("id:scan")
}

// The hero shows the last smart clean.
func TestLastClean(t *testing.T) {
	fakes := newFakes(t)
	ended := time.Now().Add(-72 * time.Hour)
	fakes.Runs = []history.Run{{ID: "s", Kind: history.KindSmartScan, StartedAt: ended, EndedAt: ended, Summary: &history.Totals{Freed: 1_200_000_000, Items: 4}}}
	p := start(t, &cleanflowfake.Cleaner{}, fakes)
	p.WaitFor("id:last-clean", wait)
	if got := p.Find("id:last-clean").Text; !strings.HasPrefix(got, "上次清理：") || !strings.HasSuffix(got, "，释放 1.2 GB") {
		t.Errorf("last clean %q", got)
	}
}

// Leaving the page stops the scan.
func TestLeavingStopsTheScan(t *testing.T) {
	stopped := make(chan struct{})
	c := &cleanflowfake.Cleaner{OnScan: func(ctx context.Context, _ app.CleanerID, _ func(cleanup.Progress)) (cleanup.ScanResult, error) {
		<-ctx.Done()
		close(stopped)
		return cleanup.ScanResult{}, ctx.Err()
	}}
	fakes := newFakes(t)
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording")
	}
	show := (*core.Signal[bool])(nil)
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		storefake.Provide(cx, fakes.Deps())
		nav.Key.Provide(cx, nav.NewNavigator(cx, nav.SmartScan))
		smartscan.DepsKey.Provide(cx, smartscan.Deps{Cleanup: c})
		show = core.NewSignal(cx, true)
		return core.Show{When: show.Get, Then: smartscan.Page}
	}, nsgotest.Size(1052, 820))
	a.Press("id:scan")
	a.WaitFor("id:scanning", wait)
	a.WaitUntil(wait, func() bool { return len(c.Scans()) == 1 })
	show.Set(false)
	a.Settle()
	select {
	case <-stopped:
	case <-time.After(wait):
		t.Fatal("leaving the page did not stop the scan")
	}
}
