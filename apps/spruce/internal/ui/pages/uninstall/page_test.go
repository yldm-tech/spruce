package uninstall_test

import (
	"context"
	"errors"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/pages/uninstall"
	"spruce/apps/spruce/internal/ui/pages/uninstall/uninstallfake"
	"spruce/apps/spruce/internal/ui/store"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

const icon = "data:image/png;base64,iVBORw0KGgo="

func macApps() []app.UninstallApp {
	return []app.UninstallApp{
		{ID: "foo", Name: "Foo", Version: "2.1", BundleID: "com.example.foo", Path: "/Applications/Foo.app", AppStore: true},
		{ID: "bar", Name: "Bar", Version: "1.0", BundleID: "com.example.bar", Path: "/Applications/Bar.app"},
		{ID: "run", Name: "Runner", BundleID: "com.example.run", Path: "/Applications/Runner.app", Running: true},
	}
}

func fooPlan() app.UninstallPlan {
	return app.UninstallPlan{AppID: "foo",
		Bundle: &app.UninstallLeftover{ID: "foo", Path: "/Applications/Foo.app", Kind: app.UninstallKindApp, Size: 100_000_000, Dir: true, Selected: true},
		Leftovers: []app.UninstallLeftover{
			uninstallfake.Leftover("sup", app.UninstallKindSupport, 20_000_000, true),
			uninstallfake.Leftover("cache", app.UninstallKindCaches, 5_000_000, true),
			uninstallfake.Leftover("agent", app.UninstallKindLaunchAgents, 1000, false),
		}}
}

func macService() *uninstallfake.Service {
	return &uninstallfake.Service{List: macApps(), Plan: fooPlan(), Details: []app.UninstallAppDetails{
		{ID: "foo", Size: uninstallfake.Size(120_000_000), Usage: app.UninstallNeverUsed, Icon: icon},
		{ID: "bar", Size: uninstallfake.Size(900_000_000), Usage: app.UninstallUsed, LastUsed: time.Now()},
	}}
}

type page struct {
	*nsgotest.App
	svc    *uninstallfake.Service
	files  *uninstallfake.Files
	stores *store.Stores
}

func start(t *testing.T, svc *uninstallfake.Service, platform uninstall.Platform, opts ...nsgotest.Option) *page {
	t.Helper()
	p := &page{svc: svc, files: &uninstallfake.Files{}}
	fakes := storefake.New(t.TempDir())
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		uninstall.DepsKey.Provide(cx, uninstall.Deps{Uninstall: svc, Files: p.files, Platform: platform})
		return uninstall.Page(cx)
	}, append([]nsgotest.Option{nsgotest.Size(1052, 820)}, opts...)...)
	return p
}

// settle waits until cond holds, moving the manual clock so the batched details and the throttled progress come through.
func (p *page) settle(cond func() bool) {
	p.WaitUntil(wait, func() bool {
		if cond() {
			return true
		}
		p.Advance(40 * time.Millisecond)
		return false
	})
}

func (p *page) waitTicking(selector string) {
	p.settle(func() bool { return len(p.Query(selector)) == 1 })
}

func (p *page) waitList() { p.WaitFor("id:app-list", wait) }

// rows are the names of the list's rows, in order.
func (p *page) rows() []string {
	var out []string
	for _, v := range p.Query("id:app-list >> role:option") {
		out = append(out, v.Props.Label)
	}
	return out
}

func (p *page) selected() string {
	for _, v := range p.Query("id:app-list >> role:option") {
		if v.Props.Selected {
			return v.Props.Label
		}
	}
	return ""
}

// stat reads the action bar's figure under label: the text after the last label, the action bar being the page's last.
func (p *page) stat(label string) string {
	texts := p.Texts()
	for i := len(texts) - 2; i >= 0; i-- {
		if texts[i] == label {
			return texts[i+1]
		}
	}
	return ""
}

func (p *page) doneLines() []string { return strings.Split(p.Find("id:done-lines").Text, "\n") }

func (p *page) pick(name string) {
	p.Press("id:app-list >> role:option " + name)
}

func TestListShowsTheStreamedDetailsSearchesAndSorts(t *testing.T) {
	p := start(t, macService(), uninstall.PlatformMac)
	p.waitList()
	p.Find("text:应用卸载")
	p.Find("text:连同应用留下的缓存和设置一起移除")
	if got := p.rows(); !slices.Equal(got, []string{"Bar", "Foo", "Runner"}) {
		t.Fatalf("rows %v", got)
	}
	p.waitTicking("id:app-list >> text:120 MB")
	p.Find("id:app-list >> text:从未使用")
	p.Find("id:app-list >> text:App Store")
	p.Find("id:app-list >> text:运行中")
	// Runner's size never came: once measuring ends it is unknown.
	p.settle(func() bool { return p.Exists("id:app-list >> text:大小未知") })
	p.Find("text:共 3 个应用")

	// The list is ordered by size until the user picks another order.
	if !p.Find("id:sort-size").Props.Pressed || p.Find("id:sort-name").Props.Pressed {
		t.Error("大小 is not the default order")
	}
	if got := p.rows(); !slices.Equal(got, []string{"Bar", "Foo", "Runner"}) {
		t.Errorf("by size %v", got)
	}
	p.Press("id:sort-name")
	if !p.Find("id:sort-name").Props.Pressed || p.Find("id:sort-size").Props.Pressed {
		t.Error("名称 did not take over")
	}
	if got := p.rows(); !slices.Equal(got, []string{"Bar", "Foo", "Runner"}) {
		t.Errorf("by name %v", got)
	}
	p.Type("id:search", "example.f")
	p.WaitUntil(wait, func() bool { return len(p.rows()) == 1 })
	p.Find("text:找到 1 个，共 3 个应用")
	p.Clear("id:search")
	p.Type("id:search", "zzz")
	p.WaitFor("text:没有符合搜索的应用", wait)
}

func TestArrowKeysMoveTheSelection(t *testing.T) {
	// Tab moves the focus on macOS.
	p := start(t, macService(), uninstall.PlatformMac, nsgotest.Environment(core.Environment{OS: core.OSMacOS}))
	p.waitList()
	// Tab goes from the search field over the three orders to the list.
	p.Focus("id:search")
	for range 4 {
		p.Key("tab")
	}
	p.Key("down")
	if got := p.selected(); got != "Bar" {
		t.Fatalf("selected %q", got)
	}
	p.Key("down")
	if got := p.selected(); got != "Foo" {
		t.Fatalf("selected %q", got)
	}
	p.Key("end")
	if got := p.selected(); got != "Runner" {
		t.Fatalf("selected %q", got)
	}
	p.Key("home")
	if got := p.selected(); got != "Bar" {
		t.Fatalf("selected %q", got)
	}
	// A press on a row gives the list the keys too.
	p.pick("Foo")
	p.Key("down")
	if got := p.selected(); got != "Runner" {
		t.Fatalf("selected %q after a press", got)
	}
}

func TestArrowKeysWaitForTheListsFocus(t *testing.T) {
	p := start(t, macService(), uninstall.PlatformMac)
	p.waitList()
	p.Key("down")
	if got := p.selected(); got != "" {
		t.Fatalf("selected %q without the list's focus", got)
	}
	// On macOS the search field leaves ↑ and ↓ to the caret.
	p.Focus("id:search")
	p.Key("down")
	if got := p.selected(); got != "" {
		t.Fatalf("selected %q from the search field on macOS", got)
	}
}

func TestSearchFieldTakesTheArrowKeysOnWindows(t *testing.T) {
	svc := macService()
	p := start(t, svc, uninstall.PlatformWindows)
	p.waitList()
	p.Focus("id:search")
	p.Key("down")
	p.Key("down")
	if got := p.selected(); got != "Foo" {
		t.Fatalf("selected %q", got)
	}
	p.Key("up")
	if got := p.selected(); got != "Bar" {
		t.Fatalf("selected %q", got)
	}
}

func TestListErrorWithRetry(t *testing.T) {
	svc := macService()
	svc.ListErr = errors.New("无法读取已安装的应用，详情已记录到日志")
	p := start(t, svc, uninstall.PlatformMac)
	p.WaitFor("text:无法读取已安装的应用，详情已记录到日志", wait)
	svc.SetList(macApps(), nil)
	p.Press("id:retry")
	p.waitList()
}

func TestNothingToUninstall(t *testing.T) {
	p := start(t, &uninstallfake.Service{}, uninstall.PlatformMac)
	p.WaitFor("text:没有找到可以卸载的应用", wait)
	p.Find("text:应用程序文件夹里没有系统自带应用以外的应用。")
}

func TestUninstallsTheAppWithTheCheckedLeftovers(t *testing.T) {
	svc := macService()
	progress := make(chan func(cleanup.Progress), 1)
	finish := make(chan app.UninstallOutcome)
	svc.OnRemove = func(ctx context.Context, _ app.UninstallRequest, send func(cleanup.Progress)) (app.UninstallOutcome, error) {
		progress <- send
		return <-finish, nil
	}
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	if p.Enabled("id:uninstall") {
		t.Error("卸载 enabled with no app picked")
	}
	p.Find("text:选择一个应用")
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	for id, on := range map[string]bool{"sup": true, "cache": true, "agent": false} {
		if got := p.Find("id:leftover-" + id).Props.Checked; got != on {
			t.Errorf("%s checked %v", id, got)
		}
	}
	if got := p.stat("已选择"); got != "3 项" {
		t.Errorf("selected %q", got)
	}
	if got := p.stat("将移到废纸篓"); got != "125 MB" {
		t.Errorf("size %q", got)
	}
	p.Find("text:已选 2/3 项，共 25 MB")
	p.Find("text:应用本身 100 MB，会和勾选的文件一起移到废纸篓。")
	p.Press("role:checkbox 缓存：cache")
	if got := p.stat("已选择"); got != "2 项" {
		t.Errorf("selected %q", got)
	}
	if got := p.stat("将移到废纸篓"); got != "120 MB" {
		t.Errorf("size %q", got)
	}

	p.Press("id:uninstall")
	p.WaitFor("id:confirm", wait)
	p.Find("id:confirm >> text:卸载「Foo」")
	p.Find("id:confirm >> text:「Foo」和勾选的 1 个相关文件（共 120 MB）会移到废纸篓，可以随时放回。")
	p.Press("id:confirm-ok")
	p.WaitFor("id:removing", wait)
	// The overlay shows before the service, called on its own goroutine, has the request.
	p.WaitUntil(wait, func() bool { return len(svc.Removes()) > 0 })
	if got := svc.Removes(); len(got) != 1 || got[0].AppID != "foo" || !slices.Equal(got[0].LeftoverIDs, []string{"sup"}) {
		t.Fatalf("removes %+v", got)
	}
	if !p.stores.Block.Blocked() {
		t.Error("the shell is not blocked while removing")
	}
	p.Find("id:removing >> text:正在卸载「Foo」…")
	send := <-progress
	send(cleanup.Progress{Phase: cleanup.PhaseCleaning, Path: "/Applications/Foo.app", Done: 1, Total: 2, Bytes: 100_000_000})
	p.waitTicking("id:removing >> text:已处理 1/2 · 已释放 100 MB")

	gen := p.stores.Trash.Generation()
	apps, _, _, _ := svc.Calls()
	finish <- app.UninstallOutcome{App: "Foo", BundleRemoved: true, Result: uninstallfake.Result(cleanup.CleanResult{Trashed: 120_000_000, Removed: 2,
		Categories: []cleanup.CategoryOutcome{
			{CategoryID: "app", Title: "app", Removed: 1, Trashed: 100_000_000},
			{CategoryID: "support", Title: "support", Removed: 1, Trashed: 20_000_000},
		}})}
	p.waitTicking("id:done")
	p.Find("id:done >> text:已卸载「Foo」")
	want := []string{"「Foo」已移到废纸篓", "相关文件：1 项已移到废纸篓", "移到废纸篓的项目在清空废纸篓后才会释放空间"}
	if got := p.doneLines(); !slices.Equal(got, want) {
		t.Errorf("lines %q", got)
	}
	if p.stores.Trash.Generation() == gen {
		t.Error("the Trash's generation did not move")
	}
	p.Press("id:done-close")
	p.WaitForGone("id:done", wait)
	p.WaitUntil(wait, func() bool { n, _, _, _ := svc.Calls(); return n == apps+1 })
	p.WaitFor("text:选择一个应用", wait)
	if p.stores.Block.Blocked() {
		t.Error("the shell is still blocked")
	}
}

func TestStopEndsTheRemoval(t *testing.T) {
	svc := macService()
	svc.OnRemove = func(ctx context.Context, _ app.UninstallRequest, _ func(cleanup.Progress)) (app.UninstallOutcome, error) {
		<-ctx.Done()
		return app.UninstallOutcome{App: "Foo", BundleRemoved: true, Result: uninstallfake.Result(cleanup.CleanResult{Interrupted: true})}, nil
	}
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	p.Press("id:uninstall")
	p.Press("id:confirm-ok")
	p.WaitFor("id:removing", wait)
	p.Press("id:overlay-cancel")
	p.WaitFor("id:done", wait)
	p.Find("id:done >> text:卸载已中断")
}

func TestCancelKeepsEverything(t *testing.T) {
	svc := macService()
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	p.Press("id:uninstall")
	p.WaitFor("id:confirm", wait)
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:confirm", wait)
	if len(svc.Removes()) != 0 {
		t.Error("a removal started")
	}
	if got := p.selected(); got != "Foo" {
		t.Errorf("selected %q", got)
	}
}

func TestRefusesARunningAppUntilItQuits(t *testing.T) {
	svc := macService()
	plan := fooPlan()
	plan.AppID, plan.Running = "run", true
	svc.Plan = plan
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Runner")
	p.WaitFor("text:请先退出「Runner」再卸载", wait)
	p.WaitFor("id:leftovers", wait)
	if p.Enabled("id:uninstall") {
		t.Error("卸载 enabled for a running app")
	}
	plan.Running = false
	svc.SetPlan(plan, nil)
	p.Press("id:recheck-running")
	p.WaitUntil(wait, func() bool { return p.Enabled("id:uninstall") })
	if p.Exists("text:请先退出「Runner」再卸载") {
		t.Error("the prompt stays")
	}
}

func TestExplainsAnAppThatNeedsAnAdministrator(t *testing.T) {
	const failure = "需要管理员权限才能把「Foo」移到废纸篓。请在访达中把它拖到废纸篓，系统会请你输入管理员密码。"
	svc := macService()
	svc.OnRemove = func(context.Context, app.UninstallRequest, func(cleanup.Progress)) (app.UninstallOutcome, error) {
		return app.UninstallOutcome{App: "Foo", BundleFailure: failure, Result: uninstallfake.Result(cleanup.CleanResult{})}, nil
	}
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	p.Press("id:uninstall")
	p.Press("id:confirm-ok")
	p.WaitFor("id:done", wait)
	p.Find("id:done >> text:没能卸载「Foo」")
	if got := p.doneLines(); !slices.Equal(got, []string{failure, "它的相关文件也没有改动。"}) {
		t.Errorf("lines %q", got)
	}
}

func TestShowsWhyTheLeftoversCouldNotBeFound(t *testing.T) {
	svc := macService()
	svc.PlanErr = errors.New("没能查找相关文件，详情已记录到日志")
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Bar")
	p.WaitFor("text:没能查找相关文件，详情已记录到日志", wait)
	if p.Enabled("id:uninstall") {
		t.Error("卸载 enabled without the leftovers")
	}
	svc.SetPlan(fooPlan(), nil)
	p.Press("id:plan-retry")
	p.WaitFor("id:leftovers", wait)
}

func TestRevealsTheAppAndSaysWhenItCannot(t *testing.T) {
	p := start(t, macService(), uninstall.PlatformMac)
	p.waitList()
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	p.Press("id:reveal-app")
	p.WaitUntil(wait, func() bool { return slices.Equal(p.files.Paths(), []string{"/Applications/Foo.app"}) })
	p.files.SetErr(errors.New("这个项目已经不存在了"))
	p.Press("id:reveal-cache")
	p.WaitFor("id:reveal-error", wait)
	p.Find("text:这个项目已经不存在了")
}

func windowsService() *uninstallfake.Service {
	return &uninstallfake.Service{List: []app.UninstallApp{{ID: "foo", Name: "Foo", Version: "2.1", Publisher: "Foo Software",
		InstallDate: time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC)}}}
}

func TestWindowsRunsTheUninstallerThenOffersWhatItLeft(t *testing.T) {
	svc := windowsService()
	status := make(chan func(app.UninstallerStatus), 1)
	finish := make(chan app.UninstallerOutcome)
	svc.OnRunUninstaller = func(_ context.Context, _ string, send func(app.UninstallerStatus)) (app.UninstallerOutcome, error) {
		status <- send
		return <-finish, nil
	}
	svc.Plan = app.UninstallPlan{AppID: "foo", Leftovers: []app.UninstallLeftover{
		uninstallfake.Leftover("roaming", app.UninstallKindAppData, 3000, true),
		uninstallfake.Leftover("local", app.UninstallKindLocalAppData, 4000, true),
	}}
	svc.OnRemove = func(context.Context, app.UninstallRequest, func(cleanup.Progress)) (app.UninstallOutcome, error) {
		return app.UninstallOutcome{App: "Foo", Result: uninstallfake.Result(cleanup.CleanResult{Trashed: 3000, Removed: 1,
			Categories: []cleanup.CategoryOutcome{{CategoryID: "appData", Title: "appData", Removed: 1, Trashed: 3000}}})}, nil
	}
	p := start(t, svc, uninstall.PlatformWindows)
	p.waitList()
	p.Find("id:sort-recent >> text:安装时间")
	p.Find("text:/Microsoft Store 应用/")
	p.pick("Foo")
	p.WaitFor("text:卸载方式 >> nth=0", wait)
	// The leftovers are looked up only after the uninstaller.
	if _, leftovers, _, _ := svc.Calls(); leftovers != 0 {
		t.Errorf("leftovers looked up %d times", leftovers)
	}
	if got := p.stat("卸载方式"); got != "自带卸载程序" {
		t.Errorf("method %q", got)
	}
	p.Press("id:uninstall")
	p.WaitFor("id:confirm", wait)
	p.Find("id:confirm >> role:button 打开卸载程序")
	p.Press("id:confirm-ok")
	p.WaitFor("id:waiting >> text:正在打开卸载程序…", wait)
	p.Find("text:如果系统请求管理员授权，请在弹出的窗口中确认")
	(<-status)(app.UninstallerStatus{Running: true})
	p.WaitFor("id:waiting >> text:正在等待卸载程序…", wait)
	finish <- app.UninstallerOutcome{Removed: true, Waited: true}

	p.WaitFor("id:leftovers-dialog", wait)
	p.Find("id:leftovers-dialog >> text:「Foo」留下的文件")
	p.Press("role:checkbox 本地数据（Local）：local")
	p.Find("id:remove-found >> text:移到回收站（3 KB）")
	p.Press("id:remove-found")
	p.WaitFor("id:done", wait)
	if got := svc.Removes(); len(got) != 1 || !slices.Equal(got[0].LeftoverIDs, []string{"roaming"}) {
		t.Fatalf("removes %+v", got)
	}
	p.Find("id:done >> text:已卸载「Foo」")
	if got := p.doneLines(); len(got) < 2 || got[0] != "卸载程序已移除「Foo」" || got[1] != "相关文件：1 项已移到回收站" {
		t.Errorf("lines %q", got)
	}
}

func TestWindowsKeepsWhatTheProgramLeft(t *testing.T) {
	svc := windowsService()
	svc.OnRunUninstaller = func(context.Context, string, func(app.UninstallerStatus)) (app.UninstallerOutcome, error) {
		return app.UninstallerOutcome{Removed: true, Waited: true}, nil
	}
	svc.Plan = app.UninstallPlan{AppID: "foo", Leftovers: []app.UninstallLeftover{uninstallfake.Leftover("roaming", app.UninstallKindAppData, 3000, true)}}
	p := start(t, svc, uninstall.PlatformWindows)
	p.waitList()
	p.pick("Foo")
	p.Press("id:uninstall")
	p.Press("id:confirm-ok")
	p.WaitFor("id:leftovers-dialog", wait)
	p.Press("id:keep")
	p.WaitFor("id:done", wait)
	if got := p.doneLines(); !slices.Equal(got, []string{"卸载程序已移除「Foo」", "留下的 1 个相关文件没有改动"}) {
		t.Errorf("lines %q", got)
	}
	if len(svc.Removes()) != 0 {
		t.Error("the leftovers were removed")
	}
}

func TestWindowsChecksAgainWhenTheUninstallerWentOn(t *testing.T) {
	svc := windowsService()
	svc.OnRunUninstaller = func(context.Context, string, func(app.UninstallerStatus)) (app.UninstallerOutcome, error) {
		return app.UninstallerOutcome{Waited: true}, nil
	}
	checks := 0
	svc.OnCheckRemoved = func(context.Context, string) (app.UninstallerOutcome, error) {
		checks++
		return app.UninstallerOutcome{Removed: checks > 1, Waited: true}, nil
	}
	svc.Plan = app.UninstallPlan{AppID: "foo", Leftovers: []app.UninstallLeftover{}}
	p := start(t, svc, uninstall.PlatformWindows)
	p.waitList()
	p.pick("Foo")
	p.Press("id:uninstall")
	p.Press("id:confirm-ok")
	p.WaitFor("id:not-removed", wait)
	if got := p.doneLines(); got[0] != "卸载程序已退出，但「Foo」仍在已安装的程序中。" {
		t.Errorf("lines %q", got)
	}
	p.Press("id:recheck")
	p.WaitUntil(wait, func() bool {
		return p.Exists("id:done-lines") && slices.Contains(p.doneLines(), "「Foo」仍在已安装的程序中")
	})
	p.Press("id:recheck")
	p.WaitFor("id:done", wait)
	if got := p.doneLines(); !slices.Equal(got, []string{"卸载程序已移除「Foo」", "没有找到它留下的文件"}) {
		t.Errorf("lines %q", got)
	}
}

func TestWindowsShowsADeclinedAdministratorPrompt(t *testing.T) {
	svc := windowsService()
	svc.OnRunUninstaller = func(context.Context, string, func(app.UninstallerStatus)) (app.UninstallerOutcome, error) {
		return app.UninstallerOutcome{}, errors.New("没有获得管理员授权，卸载程序没有运行")
	}
	p := start(t, svc, uninstall.PlatformWindows)
	p.waitList()
	p.pick("Foo")
	p.Press("id:uninstall")
	p.Press("id:confirm-ok")
	p.WaitFor("id:done", wait)
	p.Find("id:done >> text:卸载没有进行")
	if got := p.doneLines(); !slices.Equal(got, []string{"没有获得管理员授权，卸载程序没有运行"}) {
		t.Errorf("lines %q", got)
	}
}

func TestLeavingThePageStopsTheMeasure(t *testing.T) {
	stopped := make(chan struct{})
	svc := macService()
	svc.OnApps = func(context.Context) ([]app.UninstallApp, error) { return macApps(), nil }
	measuring := &blockingMeasure{Service: svc, stopped: stopped}
	show := make(chan bool, 1)
	fakes := storefake.New(t.TempDir())
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		storefake.Provide(cx, fakes.Deps())
		uninstall.DepsKey.Provide(cx, uninstall.Deps{Uninstall: measuring, Files: &uninstallfake.Files{}, Platform: uninstall.PlatformMac})
		on := core.NewSignal(cx, true)
		go func() {
			v := <-show
			cx.Post(func() { on.Set(v) })
		}()
		return core.Show{When: on.Get, Then: uninstall.Page}
	}, nsgotest.Size(1052, 820))
	a.WaitFor("id:app-list", wait)
	show <- false
	a.WaitUntil(wait, func() bool {
		select {
		case <-stopped:
			return true
		default:
			return false
		}
	})
}

// blockingMeasure measures until its context ends.
type blockingMeasure struct {
	*uninstallfake.Service
	stopped chan struct{}
}

func (b *blockingMeasure) Measure(ctx context.Context, _ func(app.UninstallAppDetails)) error {
	<-ctx.Done()
	close(b.stopped)
	return ctx.Err()
}
