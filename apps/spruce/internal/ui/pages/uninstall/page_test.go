package uninstall_test

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/nav"
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
	svc      *uninstallfake.Service
	files    *uninstallfake.Files
	stores   *store.Stores
	overview *storefake.Overview
	nav      *nav.Navigator
	// shown is whether the page shows: the tests that leave it and come back set it.
	shown *core.Signal[bool]
	root  *core.Scope
}

func start(t *testing.T, svc *uninstallfake.Service, platform uninstall.Platform, opts ...nsgotest.Option) *page {
	t.Helper()
	return startWith(t, svc, svc, platform, opts...)
}

// startWith starts the page over service, which may wrap svc to answer some calls itself.
func startWith(t *testing.T, service uninstall.Service, svc *uninstallfake.Service, platform uninstall.Platform, opts ...nsgotest.Option) *page {
	t.Helper()
	p := &page{svc: svc, files: &uninstallfake.Files{}}
	fakes := storefake.New(t.TempDir())
	p.overview = storefake.NewOverview(app.OverviewState{})
	fakes.Overview = p.overview
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		p.nav = nav.NewNavigator(cx, nav.Uninstall)
		nav.Key.Provide(cx, p.nav)
		uninstall.Provide(cx, uninstall.Deps{Uninstall: service, Files: p.files, Platform: platform})
		p.shown, p.root = core.NewSignal(cx, true), cx
		return core.Show{When: p.shown.Get, Then: uninstall.Page}
	}, append([]nsgotest.Option{nsgotest.Size(1052, 820), desktop(platform)}, opts...)...)
	return p
}

// show shows the page or takes it away, as the sidebar does.
func (p *page) show(on bool) {
	p.Post(func() { p.shown.Set(on) })
	if on {
		p.waitList()
	} else {
		p.WaitForGone("id:uninstall-page", wait)
	}
}

// toast is the toast's text and its 撤销 run, "" for none.
func (p *page) toast() (text, undo string) {
	t := p.stores.Toasts.Current()
	if t == nil {
		return "", ""
	}
	return t.Text, t.UndoRun
}

func (p *page) appsCalls() int { n, _, _, _ := p.svc.Calls(); return n }

// desktop is the environment of a desktop of platform, where a press on a row picks it and Tab moves the focus.
func desktop(platform uninstall.Platform) nsgotest.Option {
	os := core.OSMacOS
	if platform == uninstall.PlatformWindows {
		os = core.OSWindows
	}
	return nsgotest.Environment(core.Environment{OS: os, Idiom: core.IdiomDesktop})
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

// rows are the names of the list's rows, top to bottom: the list's cells, each in a slot it moves, keep their place in the tree.
func (p *page) rows() []string {
	views := p.Query("id:app-list >> role:option")
	slices.SortStableFunc(views, func(a, b *nsgotest.View) int { return cmp.Compare(a.Parent.Frame.Origin.Y, b.Parent.Frame.Origin.Y) })
	var out []string
	for _, v := range views {
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
	p := start(t, macService(), uninstall.PlatformMac)
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

func TestArrowKeysWaitForTheListOrTheSearch(t *testing.T) {
	p := start(t, macService(), uninstall.PlatformMac)
	p.waitList()
	p.Key("down")
	if got := p.selected(); got != "" {
		t.Fatalf("selected %q without the list's focus", got)
	}
	// The search field takes ↑ and ↓ on macOS too: a search is followed by the arrows.
	p.Focus("id:search")
	p.Key("down")
	p.Key("down")
	if got := p.selected(); got != "Foo" {
		t.Fatalf("selected %q from the search field", got)
	}
	p.Key("up")
	if got := p.selected(); got != "Bar" {
		t.Fatalf("selected %q", got)
	}
}

func TestSearchFieldReturnPicksTheFirstMatchAndEscapeClears(t *testing.T) {
	p := start(t, macService(), uninstall.PlatformMac)
	p.waitList()
	p.Type("id:search", "run")
	p.WaitUntil(wait, func() bool { return len(p.rows()) == 1 })
	p.Submit("id:search")
	if got := p.selected(); got != "Runner" {
		t.Fatalf("selected %q after Return", got)
	}
	// Return leaves the field, as a single-line field does; ⌘⌫ then goes on to 卸载.
	p.Focus("id:search")
	p.Key("escape")
	p.WaitUntil(wait, func() bool { return len(p.rows()) == 3 })
	if got := p.Find("id:search").Text; got != "" {
		t.Errorf("search %q after Escape", got)
	}
}

func TestFindFocusesTheSearchAndRefreshReadsTheListAgain(t *testing.T) {
	p := start(t, macService(), uninstall.PlatformMac)
	p.waitList()
	c := p.nav.Commands()
	if c.Find == nil || c.Refresh == nil {
		t.Fatal("the page registered no 查找 or 重新检查")
	}
	p.Post(c.Find)
	p.WaitUntil(wait, func() bool { return p.Find("id:search").Focused })
	before := p.appsCalls()
	p.Post(c.Refresh)
	p.WaitUntil(wait, func() bool { return p.appsCalls() == before+1 })
}

func TestTheListTakesTheKeysAfterAPress(t *testing.T) {
	p := start(t, macService(), uninstall.PlatformMac)
	p.waitList()
	p.pick("Bar")
	// Typing a name goes to it.
	p.Key("r")
	if got := p.selected(); got != "Runner" {
		t.Fatalf("selected %q after typing", got)
	}
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	// Return asks to uninstall the picked app, as ⌘⌫ does.
	p.Key("enter")
	p.WaitFor("id:confirm", wait)
	p.Find("id:confirm >> text:卸载「Foo」")
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:confirm", wait)
	p.Key("mod+backspace")
	p.WaitFor("id:confirm", wait)
	// Under the confirmation the arrows do not move the selection.
	p.Key("down")
	if got := p.selected(); got != "Foo" {
		t.Errorf("selected %q under the confirmation", got)
	}
}

func TestASearchThatHidesThePickedAppPicksTheFirstMatch(t *testing.T) {
	p := start(t, macService(), uninstall.PlatformMac)
	p.waitList()
	p.pick("Bar")
	p.Type("id:search", "foo")
	p.WaitUntil(wait, func() bool { return p.selected() == "Foo" })
	p.WaitFor("id:detail >> role:header Foo", wait)
	p.Clear("id:search")
	p.Type("id:search", "zzz")
	p.WaitFor("text:选择一个应用", wait)
	if p.Enabled("id:uninstall") {
		t.Error("卸载 is on for an app the search hides")
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
	apps := p.appsCalls()
	svc.SetList(slices.DeleteFunc(macApps(), func(a app.UninstallApp) bool { return a.ID == "foo" }), nil)
	finish <- app.UninstallOutcome{App: "Foo", BundleRemoved: true, RunID: "run-1", Undoable: true,
		Result: uninstallfake.Result(cleanup.CleanResult{Trashed: 120_000_000, Removed: 2,
			Categories: []cleanup.CategoryOutcome{
				{CategoryID: "app", Title: "app", Removed: 1, Trashed: 100_000_000},
				{CategoryID: "support", Title: "support", Removed: 1, Trashed: 20_000_000},
			}})}
	// A whole uninstall ends in the toast, with 撤销, not in a card to close.
	p.WaitForGone("id:removing", wait)
	if p.Exists("id:done") {
		t.Error("a done card for a whole uninstall")
	}
	if text, undo := p.toast(); text != "已卸载「Foo」，120 MB 已移到废纸篓" || undo != "run-1" {
		t.Errorf("toast %q undo %q", text, undo)
	}
	if p.stores.Trash.Generation() == gen {
		t.Error("the Trash's generation did not move")
	}
	// The list is read again and the selection lands on the row now where Foo was.
	p.WaitUntil(wait, func() bool { return p.appsCalls() == apps+1 })
	p.WaitUntil(wait, func() bool { return slices.Equal(p.rows(), []string{"Bar", "Runner"}) })
	if got := p.selected(); got != "Runner" {
		t.Errorf("selected %q after the uninstall", got)
	}
	if p.stores.Block.Blocked() {
		t.Error("the shell is still blocked")
	}

	// 撤销 brings Foo back: the list is read again.
	svc.SetList(macApps(), nil)
	p.overview.OnUndo = func(context.Context, string) (app.RestoreOutcome, error) { return app.RestoreOutcome{Restored: 2}, nil }
	p.Post(func() { p.stores.Overview.Undo(p.root, "run-1", nil, nil) })
	p.WaitUntil(wait, func() bool { return slices.Contains(p.rows(), "Foo") })
}

// A finished uninstall keeps its target picked until the list comes back, so nothing may ask to uninstall it again meanwhile.
func TestNothingAsksAgainBeforeTheListComesBack(t *testing.T) {
	svc := macService()
	svc.OnRemove = func(context.Context, app.UninstallRequest, func(cleanup.Progress)) (app.UninstallOutcome, error) {
		return app.UninstallOutcome{App: "Foo", BundleRemoved: true, RunID: "run-1", Undoable: true,
			Result: uninstallfake.Result(cleanup.CleanResult{Trashed: 100_000_000, Removed: 1})}, nil
	}
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	release := make(chan struct{})
	svc.OnApps = func(context.Context) ([]app.UninstallApp, error) {
		<-release
		return slices.DeleteFunc(macApps(), func(a app.UninstallApp) bool { return a.ID == "foo" }), nil
	}
	p.Key("mod+backspace")
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { text, _ := p.toast(); return text != "" })
	if p.Enabled("id:uninstall") {
		t.Error("卸载 is on for the app just uninstalled")
	}
	p.Key("mod+backspace")
	p.Key("enter")
	if p.Exists("id:confirm") {
		t.Fatal("asked to uninstall the app just uninstalled")
	}
	close(release)
	p.WaitUntil(wait, func() bool { return slices.Equal(p.rows(), []string{"Bar", "Runner"}) })
	if got := p.selected(); got != "Runner" {
		t.Errorf("selected %q", got)
	}
}

func TestWindowsAsksNothingBeforeTheListComesBack(t *testing.T) {
	svc := windowsService()
	svc.OnRunUninstaller = func(context.Context, string, func(app.UninstallerStatus)) (app.UninstallerOutcome, error) {
		return app.UninstallerOutcome{Removed: true, Waited: true}, nil
	}
	p := start(t, svc, uninstall.PlatformWindows)
	p.waitList()
	p.pick("Foo")
	release := make(chan struct{})
	svc.OnApps = func(context.Context) ([]app.UninstallApp, error) {
		<-release
		return []app.UninstallApp{}, nil
	}
	p.Press("id:uninstall")
	p.Press("id:confirm-ok")
	p.WaitFor("id:done", wait)
	p.Press("id:done-close")
	p.WaitForGone("id:done", wait)
	if p.Enabled("id:uninstall") {
		t.Error("卸载 is on for the program just uninstalled")
	}
	p.Key("mod+backspace")
	if p.Exists("id:confirm") {
		t.Fatal("asked to uninstall the program just uninstalled")
	}
	close(release)
	p.WaitFor("id:apps-empty", wait)
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
	// The button says why it is off.
	p.Hover("id:uninstall")
	p.settle(func() bool { return p.Exists("text:先退出「Runner」") })
	p.HoverOut()
	plan.Running = false
	svc.SetPlan(plan, nil)
	p.Press("id:recheck-running")
	p.WaitUntil(wait, func() bool { return p.Enabled("id:uninstall") })
	if p.Exists("text:请先退出「Runner」再卸载") {
		t.Error("the prompt stays")
	}
	// The list's badge goes with it.
	p.WaitForGone("id:app-list >> text:运行中", wait)
}

func TestComingBackToSpruceChecksARunningAppAgain(t *testing.T) {
	svc := macService()
	plan := fooPlan()
	plan.AppID, plan.Running = "run", true
	svc.Plan = plan
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Runner")
	p.WaitFor("id:running", wait)
	// The user quits Runner in another app and comes back.
	plan.Running = false
	svc.SetPlan(plan, nil)
	p.SetEnvironment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Lifecycle: core.LifecycleInactive})
	p.SetEnvironment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop, Lifecycle: core.LifecycleActive})
	p.WaitForGone("id:running", wait)
	p.WaitForGone("id:app-list >> text:运行中", wait)
	p.WaitUntil(wait, func() bool { return p.Enabled("id:uninstall") })
}

func TestComingBackToThePageChecksARunningAppAgain(t *testing.T) {
	svc := macService()
	plan := fooPlan()
	plan.AppID, plan.Running = "run", true
	svc.Plan = plan
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Runner")
	p.WaitFor("id:running", wait)
	// The user goes to another page, quits Runner meanwhile and comes back.
	p.show(false)
	plan.Running = false
	svc.SetPlan(plan, nil)
	p.show(true)
	p.WaitForGone("id:running", wait)
	p.WaitUntil(wait, func() bool { return p.Enabled("id:uninstall") })
}

func TestCheckingAgainKeepsTheLeftoversTheUserChanged(t *testing.T) {
	svc := macService()
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	p.Press("role:checkbox 缓存：cache")
	// The app wrote a new file meanwhile.
	plan := fooPlan()
	plan.Leftovers = append(plan.Leftovers, uninstallfake.Leftover("logs", app.UninstallKindLogs, 3000, true))
	svc.SetPlan(plan, nil)
	p.Press("id:refresh")
	p.WaitFor("id:leftover-logs", wait)
	for id, on := range map[string]bool{"sup": true, "cache": false, "agent": false, "logs": false} {
		if got := p.Find("id:leftover-" + id).Props.Checked; got != on {
			t.Errorf("%s checked %v", id, got)
		}
	}
}

func TestAPressOnALeftoversRowChecksIt(t *testing.T) {
	p := start(t, macService(), uninstall.PlatformMac)
	p.waitList()
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	p.Press("id:leftover-row-agent")
	if !p.Find("id:leftover-agent").Props.Checked {
		t.Error("a press on the row left it unchecked")
	}
	// The checkbox still takes its own press, once.
	p.Press("id:leftover-agent")
	if p.Find("id:leftover-agent").Props.Checked {
		t.Error("the checkbox's press did not uncheck it")
	}
	// Tab goes through the leftovers' checkboxes, never the rows around them, which the checkboxes speak for.
	p.Focus("id:search")
	checkboxes := 0
	for range 40 {
		p.Key("tab")
		if p.Find("id:search").Focused {
			break
		}
		for _, id := range []string{"sup", "cache", "agent"} {
			if p.Find("id:leftover-row-" + id).Focused {
				t.Fatalf("Tab stopped on %s's row", id)
			}
			if p.Find("id:leftover-" + id).Focused {
				checkboxes++
			}
		}
	}
	if checkboxes != 3 {
		t.Errorf("Tab stopped on %d of the 3 checkboxes", checkboxes)
	}
	// The row's menu shows it in Finder.
	p.RightClick("id:leftover-row-cache")
	p.ChoosePopup("在访达中显示")
	p.WaitUntil(wait, func() bool { return slices.Equal(p.files.Paths(), []string{"/Users/me/Library/caches/cache"}) })
}

func TestOnlyACutNameShowsWholeUnderThePointer(t *testing.T) {
	const long = "Xylophone Studio Professional Edition for Musicians and Composers 2026"
	svc := macService()
	svc.List = append(svc.List, app.UninstallApp{ID: "long", Name: long, Path: "/Applications/Long.app"})
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.Hover("id:app-list >> text:Foo")
	p.Advance(time.Second)
	if n := len(p.Query("text:Foo")); n != 1 {
		t.Errorf("a name that fits shows %d times under the pointer", n)
	}
	p.Hover("id:app-list >> text:" + long)
	p.settle(func() bool { return len(p.Query("text:"+long)) == 2 })
}

func TestAnAppsMenuUninstallsRevealsAndCopies(t *testing.T) {
	p := start(t, macService(), uninstall.PlatformMac)
	p.waitList()
	p.RightClick("id:app-list >> text:Bar")
	p.ChoosePopup("拷贝路径")
	if p.Clipboard != "/Applications/Bar.app" {
		t.Errorf("clipboard %q", p.Clipboard)
	}
	p.RightClick("id:app-list >> text:Bar")
	p.ChoosePopup("在访达中显示")
	p.WaitUntil(wait, func() bool { return slices.Equal(p.files.Paths(), []string{"/Applications/Bar.app"}) })
	// 卸载… picks the app and asks once its leftovers are found.
	p.RightClick("id:app-list >> text:Foo")
	p.ChoosePopup("卸载…")
	p.WaitFor("id:confirm >> text:卸载「Foo」", wait)
}

func TestTheTotalWaitsForTheLeftovers(t *testing.T) {
	svc := macService()
	release := make(chan struct{})
	svc.OnLeftovers = func(ctx context.Context, _ string) (app.UninstallPlan, error) {
		select {
		case <-release:
		case <-ctx.Done():
		}
		return fooPlan(), nil
	}
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Runner")
	// Runner's size is not known: the total waits.
	if got := p.stat("将移到废纸篓"); got != "…" {
		t.Errorf("total %q while the leftovers are looked up", got)
	}
	p.waitTicking("id:app-list >> text:120 MB")
	p.pick("Foo")
	if got := p.stat("将移到废纸篓"); got != "120 MB" {
		t.Errorf("total %q while the leftovers are looked up", got)
	}
	close(release)
	p.WaitUntil(wait, func() bool { return p.stat("将移到废纸篓") == "125 MB" })
}

func TestTheOrderHoldsWhileSizesStreamIn(t *testing.T) {
	svc := macService()
	sent := make(chan struct{})
	release := make(chan struct{})
	measure := &steppedMeasure{Service: svc, sent: sent, release: release, first: []app.UninstallAppDetails{
		{ID: "run", Size: uninstallfake.Size(5_000_000_000)},
		{ID: "bar", Size: uninstallfake.Size(900_000_000)},
	}}
	p := startWith(t, measure, svc, uninstall.PlatformMac)
	p.waitList()
	<-sent
	// Runner's size is shown, but no row moves while the measure goes on.
	p.waitTicking("id:app-list >> text:5 GB")
	if got := p.rows(); !slices.Equal(got, []string{"Bar", "Foo", "Runner"}) {
		t.Fatalf("rows moved while measuring: %v", got)
	}
	close(release)
	p.settle(func() bool { return slices.Equal(p.rows(), []string{"Runner", "Bar", "Foo"}) })
}

// steppedMeasure sends first, then waits for release before ending.
type steppedMeasure struct {
	*uninstallfake.Service
	first         []app.UninstallAppDetails
	sent, release chan struct{}
}

func (m *steppedMeasure) Measure(ctx context.Context, details func(app.UninstallAppDetails)) error {
	for _, d := range m.first {
		details(d)
	}
	close(m.sent)
	select {
	case <-m.release:
	case <-ctx.Done():
	}
	return nil
}

func TestComingBackKeepsTheListAndWhatWasPicked(t *testing.T) {
	svc := macService()
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.Press("id:sort-name")
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	p.Press("role:checkbox 缓存：cache")
	p.Type("id:search", "o")
	apps, _, _, _ := svc.Calls()

	p.show(false)
	p.show(true)
	// Nothing is read again: the list, the order, the search, the app and its checks are as they were.
	if p.Exists("id:apps-loading") || p.appsCalls() != apps {
		t.Errorf("the list was read again (%d → %d)", apps, p.appsCalls())
	}
	if got := p.selected(); got != "Foo" {
		t.Errorf("selected %q", got)
	}
	if !p.Find("id:sort-name").Props.Pressed {
		t.Error("the order went back to 大小")
	}
	if got := p.Find("id:search").Text; got != "o" {
		t.Errorf("search %q", got)
	}
	p.WaitFor("id:leftovers", wait)
	if p.Find("id:leftover-cache").Props.Checked {
		t.Error("cache is checked again")
	}

	// A list older than a few minutes is read again in the background, the old one shown meanwhile.
	p.show(false)
	p.Advance(6 * time.Minute)
	p.show(true)
	if p.Exists("id:apps-loading") {
		t.Error("the list was replaced by the loading state")
	}
	p.WaitUntil(wait, func() bool { return p.appsCalls() == apps+1 })
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
	if p.Exists("id:done-undo") {
		t.Error("撤销 with nothing to put back")
	}
	// Foo is still installed: it stays picked, its leftovers looked up again.
	_, plans, _, _ := svc.Calls()
	p.Press("id:done-close")
	p.WaitForGone("id:done", wait)
	p.WaitUntil(wait, func() bool { _, n, _, _ := svc.Calls(); return n == plans+1 })
	if got := p.selected(); got != "Foo" {
		t.Errorf("selected %q after a failed uninstall", got)
	}
}

func TestDoneCardOffersUndoForWhatMoved(t *testing.T) {
	svc := macService()
	svc.OnRemove = func(context.Context, app.UninstallRequest, func(cleanup.Progress)) (app.UninstallOutcome, error) {
		return app.UninstallOutcome{App: "Foo", BundleRemoved: true, RunID: "run-2", Undoable: true, Result: uninstallfake.Result(cleanup.CleanResult{
			Trashed: 100_000_000, Removed: 1, Failed: []cleanup.Failure{{Path: "/Users/me/Library/Caches/cache", Reason: "被占用"}}})}, nil
	}
	p := start(t, svc, uninstall.PlatformMac)
	restored := make(chan struct{})
	p.overview.OnUndo = func(context.Context, string) (app.RestoreOutcome, error) {
		<-restored
		return app.RestoreOutcome{Restored: 1}, nil
	}
	p.waitList()
	p.pick("Foo")
	p.WaitFor("id:leftovers", wait)
	p.Press("id:uninstall")
	p.Press("id:confirm-ok")
	p.WaitFor("id:done", wait)
	p.Press("id:done-undo")
	p.WaitForGone("id:done", wait)
	// The card is gone before the files are back: the toast says it goes on.
	if text, _ := p.toast(); text != "正在放回…" {
		t.Errorf("toast %q while putting back", text)
	}
	close(restored)
	p.WaitUntil(wait, func() bool { text, _ := p.toast(); return text == "已放回原处 1 项" })
	if _, _, _, undos, _ := p.overview.Calls(); !slices.Equal(undos, []string{"run-2"}) {
		t.Errorf("undos %v", undos)
	}
}

func TestShowsWhyTheLeftoversCouldNotBeFound(t *testing.T) {
	svc := macService()
	svc.PlanErr = errors.New("没能查找相关文件，详情已记录到日志")
	// No size is measured either.
	svc.Details = nil
	p := start(t, svc, uninstall.PlatformMac)
	p.waitList()
	p.pick("Bar")
	p.WaitFor("text:没能查找相关文件，详情已记录到日志", wait)
	if p.Enabled("id:uninstall") {
		t.Error("卸载 enabled without the leftovers")
	}
	// The total is not known, and no longer looked for.
	if got := p.stat("将移到废纸篓"); got != "—" {
		t.Errorf("size %q after the lookup failed", got)
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
	p.Find("id:sort-unused >> text:安装时间")
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
	p.WaitForGone("id:leftovers-dialog", wait)
	p.WaitUntil(wait, func() bool { text, _ := p.toast(); return text == "已卸载「Foo」，3 KB 已移到回收站" })
	if got := svc.Removes(); len(got) != 1 || !slices.Equal(got[0].LeftoverIDs, []string{"roaming"}) {
		t.Fatalf("removes %+v", got)
	}
	if p.Exists("id:done") {
		t.Error("a done card for a whole uninstall")
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

func TestComingBackLandsWhereTheListWasScrolled(t *testing.T) {
	var apps []app.UninstallApp
	for i := range 40 {
		apps = append(apps, app.UninstallApp{ID: fmt.Sprint(i), Name: fmt.Sprintf("App %02d", i), Path: "/Applications/x.app"})
	}
	p := start(t, &uninstallfake.Service{List: apps}, uninstall.PlatformMac)
	p.waitList()
	p.Scroll("id:app-list >> nth=0", 900)
	p.show(false)
	p.show(true)
	p.Settle()
	if got := p.Find("id:app-list").Children[0].Offset.Y; got != 900 {
		t.Errorf("the list came back at %v", got)
	}
}
