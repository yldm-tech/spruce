package maintenance_test

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/health"
	enginemaint "github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
	"github.com/yldm-tech/spruce/internal/ui/pages/maintenance"
	"github.com/yldm-tech/spruce/internal/ui/pages/maintenance/maintenancefake"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

type page struct {
	*nsgotest.App
	svc    *maintenancefake.Service
	fakes  *storefake.Services
	stores *store.Stores
}

func start(t *testing.T, svc *maintenancefake.Service, fakes *storefake.Services) *page {
	t.Helper()
	p := &page{svc: svc, fakes: fakes}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		maintenance.DepsKey.Provide(cx, maintenance.Deps{Maintenance: svc})
		return maintenance.Page(cx)
	}, nsgotest.Size(1052, 820))
	p.WaitUntil(wait, func() bool { return len(p.Query("role:switch")) > 0 })
	return p
}

// waitTicking settles until selector matches one view, moving the manual clock so throttled progress (async.Batch) comes through.
func (p *page) waitTicking(selector string) {
	p.WaitUntil(wait, func() bool {
		if len(p.Query(selector)) == 1 {
			return true
		}
		p.Advance(20 * time.Millisecond)
		return false
	})
}

func newFakes(t *testing.T) *storefake.Services { return storefake.New(t.TempDir()) }

func (p *page) checked(name string) bool { return p.Find("role:switch " + name).Props.Checked }

func (p *page) selected() string { return p.Find("text:/^\\d+ 项$/").Text }

func (p *page) doneLines() []string { return strings.Split(p.Find("id:done-lines").Text, "\n") }

func TestIdle(t *testing.T) {
	p := start(t, &maintenancefake.Service{Tasks: maintenancefake.MacCatalog()}, newFakes(t))
	p.WaitUntil(wait, func() bool { return len(p.Query("role:switch")) == 6 })
	for name, on := range map[string]bool{"刷新 DNS 缓存": true, "校验 Spotlight 索引": false, "优化应用数据库": false} {
		if p.checked(name) != on {
			t.Errorf("%s checked = %v", name, !on)
		}
	}
	// A task with a hint shows the hint instead of its description.
	p.Find("text:需要先退出 Safari 和邮件")
	if p.Exists("text:优化应用数据库的说明") {
		t.Error("the description shows beside the hint")
	}
	p.Find("text:维护")
	p.Find("text:给系统做一次轻量保养，不会动你的文档和照片")
	if got := p.selected(); got != "4 项" {
		t.Errorf("selected %q", got)
	}
	// 5 + 5 + 20 + 30 seconds.
	p.Find("text:约 1 分钟")
}

func TestSwitchesCountAndDisableRun(t *testing.T) {
	svc := &maintenancefake.Service{Tasks: maintenancefake.MacCatalog()}
	p := start(t, svc, newFakes(t))
	p.Press("role:switch 校验 Spotlight 索引")
	if got := p.selected(); got != "5 项" {
		t.Fatalf("selected %q", got)
	}
	p.Press("role:switch 修复「打开方式」")
	p.Press("role:switch 刷新 DNS 缓存")
	if got := p.selected(); got != "3 项" {
		t.Fatalf("selected %q", got)
	}
	// 30 + 5 + 20 seconds.
	p.Find("text:不到 1 分钟")
	for _, name := range []string{"校验 Spotlight 索引", "刷新 QuickLook 预览", "重建图标缓存"} {
		p.Press("role:switch " + name)
	}
	if got := p.selected(); got != "0 项" {
		t.Fatalf("selected %q", got)
	}
	p.Find("text:0 分钟")
	if p.Enabled("id:run") {
		t.Error("运行 is enabled with nothing selected")
	}
	if len(svc.Runs()) != 0 {
		t.Error("a run started")
	}
}

func TestRunShowsProgressThenTheSummary(t *testing.T) {
	events := make(chan app.ProgressEvent)
	finish := make(chan app.RunSummary)
	svc := &maintenancefake.Service{Tasks: maintenancefake.MacCatalog(),
		OnRun: func(ctx context.Context, ids []string, progress func(app.ProgressEvent)) (app.RunSummary, error) {
			for {
				select {
				case ev := <-events:
					progress(ev)
				case s := <-finish:
					return s, nil
				}
			}
		}}
	p := start(t, svc, newFakes(t))
	p.Press("role:switch 优化应用数据库")
	p.Press("id:run")
	p.WaitFor("id:run-overlay", wait)
	if got := svc.Runs(); len(got) != 1 || strings.Join(got[0], ",") != "dns,quicklook,icons,launchservices,databases" {
		t.Fatalf("runs %v", got)
	}
	if !p.stores.Block.Blocked() {
		t.Error("the shell is not blocked while the run goes on")
	}
	p.Find("text:共 5 项任务，不会动你的文档和照片")
	p.Find("id:run-overlay >> text:正在准备…")
	p.Find("text:部分任务需要管理员权限，请在系统弹出的授权窗口中确认")
	p.Find("text:正在维护系统…，共 5 项任务")

	events <- app.ProgressEvent{Kind: app.ProgressStarted, TaskID: "dns", Done: 0, Total: 5}
	p.WaitUntil(wait, func() bool { return len(p.Query("text:刷新 DNS 缓存 · 已完成 0/5")) == 2 })
	for _, ev := range []app.ProgressEvent{
		{Kind: app.ProgressFinished, TaskID: "dns", Done: 1, Total: 5},
		{Kind: app.ProgressStarted, TaskID: "icons", Done: 1, Total: 5},
		{Kind: app.ProgressFinished, TaskID: "icons", Done: 2, Total: 5},
		{Kind: app.ProgressStarted, TaskID: "quicklook", Done: 2, Total: 5},
	} {
		events <- ev
	}
	p.waitTicking("id:run-overlay >> text:刷新 QuickLook 预览 · 已完成 2/5")
	// Every admin task finished, so the prompt note is gone.
	p.WaitForGone("text:/管理员权限/", wait)

	finish <- maintenancefake.Summary(
		maintenancefake.Result("dns", taskresult.Applied, "已刷新 DNS 缓存"),
		maintenancefake.Result("quicklook", taskresult.Applied, "已刷新 QuickLook"),
		maintenancefake.Result("icons", taskresult.Applied, "已重建图标缓存"),
		maintenancefake.Result("launchservices", taskresult.Unchanged, "没有需要清理的登记"),
		maintenancefake.Result("databases", taskresult.Skipped, "Safari 仍在运行，已跳过"),
	)
	p.WaitFor("id:done", wait)
	p.Find("text:维护完成")
	p.Find("text:4 项 >> nth=0")
	want := []string{
		"优化应用数据库：Safari 仍在运行，已跳过",
		"刷新 DNS 缓存：已刷新 DNS 缓存",
		"刷新 QuickLook 预览：已刷新 QuickLook",
		"重建图标缓存：已重建图标缓存",
		"修复「打开方式」：没有需要清理的登记",
	}
	if got := p.doneLines(); strings.Join(got, "|") != strings.Join(want, "|") {
		t.Errorf("lines %q", got)
	}
	// Full Disk Access is granted, so the skip was not about the permission.
	if p.Exists("role:button 去授权") {
		t.Error("去授权 offered although the permission is granted")
	}
	if p.Exists("id:run-overlay") || !p.stores.Block.Blocked() {
		t.Error("the run overlay is still up, or the shell is not blocked under the done card")
	}
	p.Press("id:done-close")
	p.WaitForGone("id:done", wait)
	if got := p.selected(); got != "5 项" {
		t.Errorf("selection after the run %q", got)
	}
	if p.stores.Block.Blocked() {
		t.Error("the shell is still blocked")
	}
}

// runWith starts a run that answers summary at once and waits for its done card titled title.
func runWith(t *testing.T, title string, summary app.RunSummary, err error) *page {
	t.Helper()
	svc := &maintenancefake.Service{Tasks: maintenancefake.MacCatalog(),
		OnRun: func(context.Context, []string, func(app.ProgressEvent)) (app.RunSummary, error) { return summary, err }}
	p := start(t, svc, newFakes(t))
	p.Press("id:run")
	p.WaitFor("id:done", wait)
	p.Find("text:" + title)
	return p
}

func TestCompletedMessagesAreListed(t *testing.T) {
	p := runWith(t, "维护完成", maintenancefake.Summary(
		maintenancefake.Result("dns", taskresult.Applied, "已刷新 DNS 缓存"),
		maintenancefake.Result("quicklook", taskresult.Applied, "已刷新 QuickLook"),
		maintenancefake.Result("icons", taskresult.Applied, "已重建图标缓存并刷新程序坞；如果访达里仍有旧图标，注销并重新登录即可"),
		maintenancefake.Result("launchservices", taskresult.Unchanged, "没有需要清理的登记"),
	), nil)
	if !strings.Contains(p.Find("id:done-lines").Text, "重建图标缓存：已重建图标缓存并刷新程序坞；如果访达里仍有旧图标，注销并重新登录即可") {
		t.Errorf("lines %q", p.doneLines())
	}
	if p.Exists("text:/无需重启/") {
		t.Error("the card claims 无需重启 on its own")
	}
}

func TestPartlyAppliedComesFirst(t *testing.T) {
	partial := maintenancefake.Result("icons", taskresult.Applied, "已清除图标缓存，但未能重启图标服务和程序坞；注销并重新登录后生效")
	partial.Partial = true
	p := runWith(t, "维护完成", maintenancefake.Summary(maintenancefake.Result("dns", taskresult.Applied, "已刷新 DNS 缓存"), partial), nil)
	p.Find("text:2 项 >> nth=0")
	want := "重建图标缓存：已清除图标缓存，但未能重启图标服务和程序坞；注销并重新登录后生效|刷新 DNS 缓存：已刷新 DNS 缓存"
	if got := strings.Join(p.doneLines(), "|"); got != want {
		t.Errorf("lines %q", got)
	}
}

func TestDeclinedAdminPromptIsOneLine(t *testing.T) {
	const cancelled = "已取消管理员授权，未执行"
	p := runWith(t, "维护完成", maintenancefake.Summary(
		maintenancefake.Result("dns", taskresult.Skipped, cancelled),
		maintenancefake.Result("quicklook", taskresult.Applied, "已刷新 QuickLook"),
		maintenancefake.Result("icons", taskresult.Skipped, cancelled),
		maintenancefake.Result("launchservices", taskresult.Failed, "lsregister 运行失败"),
	), nil)
	p.Find("text:1 项 >> nth=0")
	want := "刷新 DNS 缓存、重建图标缓存：已取消管理员授权，未执行\n修复「打开方式」：lsregister 运行失败\n刷新 QuickLook 预览：已刷新 QuickLook"
	if got := p.Find("id:done-lines").Text; got != want {
		t.Errorf("lines %q", got)
	}
}

func TestRunThatCouldNotStart(t *testing.T) {
	p := runWith(t, "维护没有运行", app.RunSummary{}, errors.New("已有维护正在进行，请等它结束后再试"))
	p.Find("text:已有维护正在进行，请等它结束后再试")
}

func TestFullDiskAccessOnTheDatabaseCard(t *testing.T) {
	fakes := newFakes(t)
	fakes.Permission = enginemaint.PermissionDenied
	fakes.GuideRelease = make(chan struct{})
	p := start(t, &maintenancefake.Service{Tasks: maintenancefake.MacCatalog()}, fakes)
	// Not asked while the card is off.
	if p.Exists("role:button 去授权") {
		t.Fatal("去授权 offered while the database card is off")
	}
	p.Press("role:switch 优化应用数据库")
	p.WaitFor("role:button 去授权", wait)
	p.Press("role:button 去授权")
	p.WaitFor("role:button 等待授权…", wait)
	if p.Enabled("role:button 等待授权…") {
		t.Error("the button stays enabled while the guide waits")
	}
	fakes.GuideRelease <- struct{}{}
	p.WaitForGone("text:/完全磁盘访问权限/", wait)
	if _, guides := fakes.Counts(); guides != 1 {
		t.Errorf("guides %d", guides)
	}
}

// onlyDatabases turns 优化应用数据库 on and every other switch off.
func onlyDatabases(p *page) {
	for _, name := range []string{"刷新 DNS 缓存", "刷新 QuickLook 预览", "重建图标缓存", "修复「打开方式」", "优化应用数据库"} {
		p.Press("role:switch " + name)
	}
}

func TestDoneCardOffersTheGuideWhenTheTaskWasSkippedForThePermission(t *testing.T) {
	fakes := newFakes(t)
	fakes.Permission = enginemaint.PermissionDenied
	skipped := maintenancefake.Result("databases", taskresult.Skipped, "需要为 Spruce 开启「完全磁盘访问权限」，开启后再运行一次")
	skipped.Reason = taskresult.ReasonFullDiskAccess
	svc := &maintenancefake.Service{Tasks: maintenancefake.MacCatalog(),
		OnRun: func(context.Context, []string, func(app.ProgressEvent)) (app.RunSummary, error) {
			return maintenancefake.Summary(skipped), nil
		}}
	p := start(t, svc, fakes)
	onlyDatabases(p)
	p.Press("id:run")
	p.WaitFor("id:done", wait)
	p.Find("text:维护未完成")
	p.WaitFor("id:done >> role:button 去授权", wait)
	p.Press("id:done >> role:button 去授权")
	// Granted on this card, after the run: now running again helps.
	p.WaitFor("text:已获得完全磁盘访问权限，再运行一次即可优化数据库", wait)
}

func TestDoneCardDoesNotBlameThePermissionForAnotherSkip(t *testing.T) {
	fakes := newFakes(t)
	fakes.Permission = enginemaint.PermissionDenied
	svc := &maintenancefake.Service{Tasks: maintenancefake.MacCatalog(),
		OnRun: func(context.Context, []string, func(app.ProgressEvent)) (app.RunSummary, error) {
			return maintenancefake.Summary(maintenancefake.Result("databases", taskresult.Skipped, "Safari 仍在运行，已跳过；退出后再运行一次")), nil
		}}
	p := start(t, svc, fakes)
	onlyDatabases(p)
	p.WaitFor("role:button 去授权", wait)
	p.Press("role:button 去授权")
	p.WaitForGone("role:button /授权/", wait)
	p.Press("id:run")
	p.WaitFor("id:done", wait)
	p.Find("text:维护未完成")
	if got := strings.Join(p.doneLines(), "|"); got != "优化应用数据库：Safari 仍在运行，已跳过；退出后再运行一次" {
		t.Errorf("lines %q", got)
	}
	if p.Exists("text:/完全磁盘访问权限/") || p.Exists("role:button 去授权") {
		t.Error("the card blames the permission")
	}
}

var xcode = health.Alert{PID: 4242, Name: "Xcode", CPUPercent: 48.3, Since: time.Date(2026, 11, 14, 0, 52, 0, 0, time.UTC), Duration: 12*time.Minute + 20*time.Second, Threshold: 40}

func TestAlertBannerFollowsTheMonitor(t *testing.T) {
	fakes := newFakes(t)
	p := start(t, &maintenancefake.Service{Tasks: maintenancefake.MacCatalog()}, fakes)
	if p.Exists("id:alert-banner") {
		t.Fatal("banner without an alert")
	}
	fakes.Alert([]health.Alert{xcode})
	p.WaitFor("text:Xcode 已经持续占用 48% CPU 有 12 分钟", wait)
	p.Find("text:/Spruce 不会结束进程，只提醒你。/")
	p.Press("id:alert-open-monitor")
	p.WaitUntil(wait, func() bool { m, _ := fakes.Counts(); return m == 1 })
	fakes.Alert(nil)
	p.WaitForGone("id:alert-banner", wait)
}

func TestAlertBannerShowsAnAlertActiveAtOpen(t *testing.T) {
	fakes := newFakes(t)
	other := xcode
	other.PID, other.Name, other.CPUPercent = 7, "node", 41
	fakes.Alerts = []health.Alert{xcode, other}
	p := start(t, &maintenancefake.Service{Tasks: maintenancefake.MacCatalog()}, fakes)
	p.WaitFor("text:Xcode 已经持续占用 48% CPU 有 12 分钟", wait)
	p.Find("text:/另有 1 个进程也在高占用。/")
}

func TestWindowsCatalogHasNoFullDiskAccessUI(t *testing.T) {
	fakes := newFakes(t)
	fakes.Permission = enginemaint.PermissionNotRequired
	p := start(t, &maintenancefake.Service{Tasks: maintenancefake.WindowsCatalog()}, fakes)
	p.WaitUntil(wait, func() bool { return len(p.Query("role:switch")) == 5 })
	if p.Exists("role:button 去授权") || p.Exists("text:/完全磁盘访问权限/") {
		t.Error("Full Disk Access UI on Windows")
	}
}
