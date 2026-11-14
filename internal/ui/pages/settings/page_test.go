package settings_test

import (
	"cmp"
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	enginesettings "github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/pages/settings"
	"github.com/yldm-tech/spruce/internal/ui/pages/settings/settingsfake"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

const (
	wait     = 5 * time.Second
	debounce = 400 * time.Millisecond
	dataDir  = "/Users/me/Library/Application Support/Spruce"
)

type page struct {
	*nsgotest.App
	t      *testing.T
	svc    *settingsfake.Settings
	files  *settingsfake.Files
	fakes  *storefake.Services
	stores *store.Stores
	shown  *core.Signal[bool]
}

type options struct {
	svc   *settingsfake.Settings
	fakes *storefake.Services
	files *settingsfake.Files
	// goos is the platform the page lays out for; "" is macOS.
	goos string
}

func start(t *testing.T, o options) *page {
	t.Helper()
	if o.svc == nil {
		o.svc = settingsfake.New(dataDir)
	}
	if o.fakes == nil {
		o.fakes = storefake.New(t.TempDir())
	}
	if o.files == nil {
		o.files = &settingsfake.Files{}
	}
	p := &page{t: t, svc: o.svc, files: o.files, fakes: o.fakes}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, o.svc.Use(o.fakes.Deps()))
		settings.DepsKey.Provide(cx, settings.Deps{Files: o.files, GOOS: cmp.Or(o.goos, "darwin")})
		p.shown = core.NewSignal(cx, true)
		return core.Show{When: p.shown.Get, Then: func(*core.Scope) core.Node { return core.Func(settings.Page) }}
	}, nsgotest.Size(1052, 2600))
	p.WaitFor("id:settings-tab-general", wait)
	return p
}

// tab shows the page's tab (general, cleanup, health, history or about).
func (p *page) tab(name string) {
	p.t.Helper()
	p.Press("id:tab-" + name)
	p.WaitFor("id:settings-tab-"+name, wait)
}

// updates waits until Update has been called n times and returns the last settings it received.
func (p *page) updates(n int) enginesettings.AppSettings {
	p.t.Helper()
	p.WaitUntil(wait, func() bool { return len(p.svc.Updates()) >= n })
	if got := len(p.svc.Updates()); got != n {
		p.t.Fatalf("Update was called %d times, want %d", got, n)
	}
	last, _ := p.svc.Last()
	return last
}

func (p *page) checked(name string) bool { return p.Find("role:switch " + name).Props.Checked }

// slider is the thumb of the slider named name, which carries its value.
func (p *page) slider(name string) string { return "role:adjustable " + name }

func (p *page) valueText(name string) string { return p.Find(p.slider(name)).Props.Value.Text }

func (p *page) field(id string) string { return p.Find("id:" + id + "-field").Text }

func (p *page) text(id string) string {
	if !p.Exists("id:" + id) {
		return ""
	}
	return p.Find("id:" + id).Text
}

// pick answers the next folder picks with paths, one per pick; "" cancels.
func (p *page) pick(paths ...string) {
	p.PickDirectories = func(r core.DirectoriesRequest) []core.Directory {
		if len(paths) == 0 || paths[0] == "" {
			paths = paths[min(1, len(paths)):]
			return nil
		}
		d := core.Directory{Name: paths[0], Path: paths[0]}
		paths = paths[1:]
		return []core.Directory{d}
	}
}

func TestSwitchesSaveTheirOwnFieldAtOnce(t *testing.T) {
	p := start(t, options{})
	p.Press("role:switch 登录时启动 Spruce")
	want := settingsfake.Defaults()
	want.General.LaunchAtLogin = true
	if got := p.updates(1); !equal(got, want) {
		t.Fatalf("update %+v", got)
	}

	p.Press("role:switch 在后台定期检查")
	if got := p.updates(2); !got.General.LaunchAtLogin || got.Background.Enabled || got.General != want.General {
		t.Fatalf("background %+v, general %+v", got.Background, got.General)
	}

	p.Press("role:switch 关闭窗口后继续在菜单栏运行")
	if got := p.updates(3).General; got.KeepInTray || !got.LaunchAtLogin || got.LowDiskAlert {
		t.Fatalf("general %+v", got)
	}

	p.tab("health")
	p.Press("role:switch 启用高 CPU 提醒")
	if got := p.updates(4).Health; got != (enginesettings.HealthSettings{CPUThreshold: 40, SustainMinutes: 10, Enabled: false}) {
		t.Fatalf("health %+v", got)
	}
	if p.checked("启用高 CPU 提醒") {
		t.Error("启用高 CPU 提醒 is still on")
	}
}

func TestFollowsChangesFromElsewhereAndShowsWhatGoStored(t *testing.T) {
	svc := settingsfake.New(dataDir)
	p := start(t, options{svc: svc})
	gets := svc.Gets()
	svc.Change(func(s *enginesettings.AppSettings) {
		s.Health.CPUThreshold = 75
		s.General.LaunchAtLogin = true
	})
	p.WaitUntil(wait, func() bool { return p.checked("登录时启动 Spruce") })
	if svc.Gets() != gets+1 {
		t.Errorf("read the settings %d times for one change", svc.Gets()-gets)
	}
	p.tab("health")
	if got := p.valueText("提醒阈值"); got != "75%" {
		t.Fatalf("threshold reads %q", got)
	}

	// Go clamps the 25 sent to 20, the minimum: the slider shows what Go stored.
	svc.SetClamp(func(next enginesettings.AppSettings) enginesettings.AppSettings {
		next.Health.CPUThreshold = max(next.Health.CPUThreshold-55, 20)
		return next
	})
	p.AccessibilityAction(p.slider("提醒阈值"), core.ActionIncrement)
	p.Advance(debounce)
	if got := p.updates(1).Health.CPUThreshold; got != 80 {
		t.Fatalf("sent %d", got)
	}
	p.WaitUntil(wait, func() bool { return p.valueText("提醒阈值") == "25%" })
	if got := p.text("cpu-threshold-note"); got != "已调整为 25%" {
		t.Errorf("note %q", got)
	}
}

func TestRefusedSaveShowsTheReasonAndTheStoredValue(t *testing.T) {
	svc := settingsfake.New(dataDir)
	svc.UpdateErr = errors.New("无法更改「登录时启动」，其他设置没有保存")
	p := start(t, options{svc: svc})
	p.Press("role:switch 登录时启动 Spruce")
	got := p.WaitFor("id:section-general >> id:launch-at-login-error", wait)
	if got.Text != "无法更改「登录时启动」，其他设置没有保存" {
		t.Errorf("error %q", got.Text)
	}
	// The stored settings come back.
	p.WaitUntil(wait, func() bool { return !p.checked("登录时启动 Spruce") })
}

func TestOnlyTheLatestSaveCounts(t *testing.T) {
	svc := settingsfake.New(dataDir)
	off, on := make(chan struct{}), make(chan struct{})
	svc.Wait = func(next enginesettings.AppSettings) {
		if next.Background.Enabled {
			<-on
		} else {
			<-off
		}
	}
	svc.Fail = func(next enginesettings.AppSettings) error {
		if !next.Background.Enabled {
			return errors.New("无法保存设置，详情已记录到日志")
		}
		return nil
	}
	p := start(t, options{svc: svc})
	p.Press("role:switch 在后台定期检查")
	p.Press("role:switch 在后台定期检查")
	p.WaitUntil(wait, func() bool { return len(svc.Updates()) == 2 })
	if !p.checked("在后台定期检查") {
		t.Fatal("the switch does not show the latest change")
	}
	// The newer save answers first, then the older one fails: neither its error nor its value shows.
	close(on)
	p.WaitUntil(wait, func() bool { return svc.Answered() == 1 && p.checked("在后台定期检查") })
	close(off)
	p.WaitUntil(wait, func() bool { return svc.Answered() == 2 })
	// The older answer is posted right after Update returns; nothing it brings may show.
	for deadline := time.Now().Add(200 * time.Millisecond); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		p.Settle()
		if p.Exists("id:background-check-error") {
			t.Fatalf("the older save's error shows: %q", p.text("background-check-error"))
		}
		if !p.checked("在后台定期检查") {
			t.Fatal("the older save's value shows")
		}
	}
}

func TestSlidersSaveOnceTheUserStops(t *testing.T) {
	p := start(t, options{})
	p.tab("health")
	v := p.Find(p.slider("提醒阈值")).Props.Value
	if v.Min != 20 || v.Max != 400 || v.Text != "40%" {
		t.Fatalf("threshold %+v", v)
	}
	p.AccessibilityAction(p.slider("提醒阈值"), core.ActionIncrement)
	p.AccessibilityAction(p.slider("提醒阈值"), core.ActionIncrement)
	if got := p.valueText("提醒阈值"); got != "50%" {
		t.Errorf("threshold reads %q", got)
	}
	p.Find("id:cpu-threshold >> text:50%")
	p.Advance(debounce - time.Millisecond)
	p.Settle()
	if n := len(p.svc.Updates()); n != 0 {
		t.Fatalf("saved %d times before the user stopped", n)
	}
	p.Advance(time.Millisecond)
	if got := p.updates(1).Health; got != (enginesettings.HealthSettings{CPUThreshold: 50, SustainMinutes: 10, Enabled: true}) {
		t.Fatalf("health %+v", got)
	}

	s := p.Find(p.slider("持续时间")).Props.Value
	if s.Min != 1 || s.Max != 120 {
		t.Fatalf("sustain %+v", s)
	}
	for range 7 {
		p.AccessibilityAction(p.slider("持续时间"), core.ActionDecrement)
	}
	p.Advance(debounce)
	if got := p.updates(2).Health; got != (enginesettings.HealthSettings{CPUThreshold: 50, SustainMinutes: 3, Enabled: true}) {
		t.Fatalf("health %+v", got)
	}
	if got := p.valueText("持续时间"); got != "3 分钟" {
		t.Errorf("sustain reads %q", got)
	}
}

func TestClampedValueSaysSo(t *testing.T) {
	svc := settingsfake.New(dataDir)
	svc.Clamp = func(next enginesettings.AppSettings) enginesettings.AppSettings {
		next.Health.CPUThreshold = 400
		return next
	}
	p := start(t, options{svc: svc})
	p.tab("health")
	p.AccessibilityAction(p.slider("提醒阈值"), core.ActionIncrement)
	p.Advance(debounce)
	note := p.WaitFor("id:section-health >> id:cpu-threshold-note", wait)
	if note.Text != "已调整为 400%" {
		t.Errorf("note %q", note.Text)
	}
	if got := p.valueText("提醒阈值"); got != "400%" {
		t.Errorf("threshold reads %q", got)
	}
}

func TestNumbersAreValidatedBeforeTheyAreSaved(t *testing.T) {
	p := start(t, options{})
	p.tab("history")
	if got := p.field("retention-days"); got != "180" {
		t.Fatalf("retention shows %q", got)
	}
	p.Clear("id:retention-days-field")
	p.Type("id:retention-days-field", "3天")
	if got := p.text("retention-days-error"); got != "请输入 7 到 3650 之间的整数" {
		t.Errorf("error %q", got)
	}
	if !p.Find("id:retention-days-field").Props.Invalid {
		t.Error("the field is not marked invalid")
	}
	p.Clear("id:retention-days-field")
	p.Type("id:retention-days-field", "30")
	if p.Exists("id:retention-days-error") {
		t.Errorf("error %q stays", p.text("retention-days-error"))
	}
	p.Advance(debounce)
	want := settingsfake.Defaults()
	want.History.RetentionDays = 30
	if got := p.updates(1); !equal(got, want) {
		t.Fatalf("update %+v", got)
	}

	// Leaving the field saves at once.
	p.tab("cleanup")
	p.Clear("id:large-file-min-field")
	p.Type("id:large-file-min-field", "2000")
	p.Focus("id:dev-artifact-min-age-field")
	if got := p.updates(2).Cleanup.LargeFileMinMB; got != 2000 {
		t.Fatalf("large files %d", got)
	}
	if got := p.field("large-file-min"); got != "2000" {
		t.Errorf("large files shows %q", got)
	}

	p.Clear("id:dev-artifact-min-age-field")
	p.Type("id:dev-artifact-min-age-field", "0")
	p.Advance(debounce)
	got := p.updates(3)
	if got.Cleanup.DevArtifactMinAgeDays != 0 || got.Cleanup.LargeFileMinMB != 2000 || got.History.RetentionDays != 30 {
		t.Fatalf("cleanup %+v history %+v", got.Cleanup, got.History)
	}
}

func TestInvalidTextIsNotSavedOnBlur(t *testing.T) {
	p := start(t, options{})
	p.tab("history")
	p.Clear("id:retention-days-field")
	p.Type("id:retention-days-field", "5")
	p.Key("Tab")
	p.Advance(debounce)
	p.Settle()
	if n := len(p.svc.Updates()); n != 0 {
		t.Fatalf("saved %d times", n)
	}
	if got := p.field("retention-days"); got != "5" {
		t.Errorf("retention shows %q", got)
	}
	p.Find("id:retention-days-error")
}

func TestLeavingThePageSendsThePendingSave(t *testing.T) {
	p := start(t, options{})
	p.tab("history")
	p.Clear("id:retention-days-field")
	p.Type("id:retention-days-field", "90")
	p.Post(func() { p.shown.Set(false) })
	p.WaitForGone("id:settings-page", wait)
	if got := p.updates(1).History.RetentionDays; got != 90 {
		t.Fatalf("retention %d", got)
	}
}

// Another tab is another part of the page: a save still waiting goes out as its tab goes.
func TestSwitchingTabsSendsThePendingSave(t *testing.T) {
	p := start(t, options{})
	p.tab("history")
	p.Clear("id:retention-days-field")
	p.Type("id:retention-days-field", "90")
	p.tab("about")
	if got := p.updates(1).History.RetentionDays; got != 90 {
		t.Fatalf("retention %d", got)
	}
}

func TestFolderPickerAddsRefusesDuplicatesIgnoresCancelAndRemoves(t *testing.T) {
	p := start(t, options{})
	p.tab("cleanup")
	p.Find("id:exclusions-list >> text:\"/Users/me/Keep\"")
	p.pick("/Users/me/Movies", "/users/ME/keep", "")

	p.Press("id:exclusions-add")
	if got := p.updates(1).Cleanup.Exclusions; !slices.Equal(got, []string{"/Users/me/Keep", "/Users/me/Movies"}) {
		t.Fatalf("exclusions %v", got)
	}
	p.WaitFor("id:exclusions-list >> text:\"/Users/me/Movies\"", wait)

	p.Press("id:exclusions-add")
	if got := p.WaitFor("id:exclusions-error", wait).Text; got != "这个文件夹已经在列表中了" {
		t.Errorf("error %q", got)
	}

	p.Press("id:exclusions-add")
	p.WaitUntil(wait, func() bool { return len(p.DirectoryPicks) == 3 })
	p.Settle()
	if n := len(p.svc.Updates()); n != 1 {
		t.Fatalf("Update was called %d times", n)
	}
	if p.Exists("id:exclusions-error") {
		t.Error("the duplicate's error stays after another pick")
	}

	p.Press("id:exclusions-list >> role:button 移除 /Users/me/Keep")
	if got := p.updates(2).Cleanup.Exclusions; !slices.Equal(got, []string{"/Users/me/Movies"}) {
		t.Fatalf("exclusions %v", got)
	}
	p.WaitForGone("id:exclusions-list >> text:\"/Users/me/Keep\"", wait)
}

func TestFolderListsWriteTheirOwnFieldAndStopAtTheMaximum(t *testing.T) {
	p := start(t, options{})
	p.tab("cleanup")
	p.pick("/Users/me/code", "/Volumes/Data/Installers")
	p.Press("id:dev-project-roots-add")
	got := p.updates(1)
	if !slices.Equal(got.Cleanup.DevProjectRoots, []string{"/Users/me/Projects", "/Users/me/code"}) ||
		!slices.Equal(got.Cleanup.Exclusions, []string{"/Users/me/Keep"}) {
		t.Fatalf("cleanup %+v", got.Cleanup)
	}

	p.Press("id:installer-roots-add")
	if got := p.updates(2).Cleanup.InstallerRoots; len(got) != 3 {
		t.Fatalf("installers %v", got)
	}
	// MaxPaths is 3 in these limits.
	p.WaitUntil(wait, func() bool { return !p.Enabled("id:installer-roots-add") })
	if got := p.text("installer-roots-note"); got != "最多 3 项" {
		t.Errorf("note %q", got)
	}
}

func TestEmptyFolderList(t *testing.T) {
	svc := settingsfake.New(dataDir)
	svc.Stored.Cleanup.Exclusions = nil
	p := start(t, options{svc: svc})
	p.tab("cleanup")
	p.Find("id:exclusions-list >> text:还没有添加文件夹")
}

func TestClearHistoryAfterConfirming(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Runs = []history.Run{{ID: "1", Kind: history.KindMaintenance}}
	p := start(t, options{fakes: fakes})
	p.tab("history")
	gen := p.stores.History.Generation()
	p.Press("id:clear-history-button")
	p.WaitFor("id:clear-dialog", wait)
	p.Find("id:clear-dialog >> text:清空操作历史？")
	p.Press("id:confirm-ok")
	p.WaitForGone("id:clear-dialog", wait)
	if runs, _ := fakes.Deps().History.List(10); len(runs) != 0 {
		t.Errorf("runs %v", runs)
	}
	if p.stores.History.Generation() == gen {
		t.Error("the history's generation did not change")
	}
}

func TestClearHistoryCancel(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Runs = []history.Run{{ID: "1", Kind: history.KindMaintenance}}
	p := start(t, options{fakes: fakes})
	p.tab("history")
	p.Press("id:clear-history-button")
	p.WaitFor("id:clear-dialog", wait)
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:clear-dialog", wait)
	if runs, _ := fakes.Deps().History.List(10); len(runs) != 1 {
		t.Errorf("runs %v", runs)
	}
}

// Escape while the history is being cleared does nothing; a failure then shows in the dialog, which stays.
func TestEscapeWhileClearingKeepsTheDialog(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Runs = []history.Run{{ID: "1", Kind: history.KindMaintenance}}
	fakes.ClearErr = errors.New("无法清空操作历史，详情已记录到日志")
	release := make(chan struct{})
	fakes.ClearRelease = release
	p := start(t, options{fakes: fakes})
	p.tab("history")
	p.Press("id:clear-history-button")
	p.WaitFor("id:clear-dialog", wait)
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return p.Find("id:confirm-ok").Props.Disabled })
	p.Key("Escape")
	p.Settle()
	if !p.Exists("id:clear-dialog") || !p.stores.Block.Blocked() {
		t.Fatalf("after Escape: dialog %v, shell blocked %v", p.Exists("id:clear-dialog"), p.stores.Block.Blocked())
	}
	close(release)
	if got := p.WaitFor("id:clear-error", wait).Text; got != "无法清空操作历史，详情已记录到日志" {
		t.Errorf("error %q", got)
	}
	p.Find("id:clear-dialog")
	// Idle again, Escape is 取消.
	p.Key("Escape")
	p.WaitForGone("id:clear-dialog", wait)
	if p.stores.Block.Blocked() {
		t.Error("the shell stays blocked")
	}
}

func TestFullDiskAccessGuide(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Permission = maintenance.PermissionDenied
	p := start(t, options{fakes: fakes})
	p.WaitFor("id:section-permissions >> text:未授权", wait)
	p.Find("id:section-permissions >> text:/Safari 和邮件的数据库、读取" + format.TrashName() + "和查找部分应用的残留文件/")
	p.Press("id:section-permissions >> id:grant-full-disk-access")
	p.WaitFor("id:section-permissions >> text:已授权", wait)
	p.WaitForGone("id:grant-full-disk-access", wait)
	if _, guides := fakes.Counts(); guides != 1 {
		t.Errorf("guides %d", guides)
	}
}

// Until the first check answers the section says 正在检查… and offers no guide; it reads the state again each time the page opens, since it changes in System Settings.
func TestFullDiskAccessIsCheckedAsThePageOpens(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Permission = maintenance.PermissionDenied
	release := make(chan struct{})
	fakes.CheckRelease = release
	p := start(t, options{fakes: fakes})
	p.Find("id:section-permissions >> text:正在检查…")
	if p.Exists("id:grant-full-disk-access") {
		t.Error("去授权 offered before the first check answered")
	}
	close(release)
	p.WaitFor("id:section-permissions >> text:未授权", wait)
	p.WaitFor("id:grant-full-disk-access", wait)

	// Granted in System Settings while the app runs: the page shows it as it opens again.
	fakes.SetPermission(maintenance.PermissionGranted)
	p.Post(func() { p.shown.Set(false) })
	p.WaitForGone("id:settings-page", wait)
	p.Post(func() { p.shown.Set(true) })
	p.WaitFor("id:section-permissions >> text:已授权", wait)
	p.WaitForGone("id:grant-full-disk-access", wait)
}

func TestFullDiskAccessGuideFailure(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Permission = maintenance.PermissionDenied
	fakes.GuideErr = errors.New("open")
	p := start(t, options{fakes: fakes})
	p.WaitFor("id:section-permissions >> text:未授权", wait)
	p.Press("id:grant-full-disk-access")
	if got := p.WaitFor("id:full-disk-access-error", wait).Text; got != "无法打开系统设置" {
		t.Errorf("error %q", got)
	}
}

func TestNoPermissionsSectionWhereNotRequired(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Permission = maintenance.PermissionNotRequired
	p := start(t, options{fakes: fakes})
	p.WaitForGone("id:section-permissions", wait)
	p.Find("role:switch 在后台定期检查")
}

func TestAboutShowsTheVersionAndRevealsTheDataFolder(t *testing.T) {
	p := start(t, options{})
	p.Find("role:header 设置")
	p.Find("text:调整提醒、清理范围、排除的文件夹和历史记录")
	p.tab("about")
	p.Find("id:section-about >> text:版本 0.1.0")
	p.Find("id:section-about >> text:\"" + dataDir + "\"")
	for _, l := range settings.Licenses {
		p.Find("id:license-list >> text:" + l.Name)
	}
	p.Press("id:reveal-data-dir")
	p.WaitUntil(wait, func() bool { return len(p.files.Revealed()) == 1 })
	if got := p.files.Revealed(); got[0] != dataDir {
		t.Errorf("revealed %v", got)
	}
}

func TestRevealFailureShows(t *testing.T) {
	files := &settingsfake.Files{RevealErr: errors.New("这个项目已经不存在了")}
	p := start(t, options{files: files})
	p.tab("about")
	p.Press("id:reveal-data-dir")
	if got := p.WaitFor("id:data-dir-error", wait).Text; got != "这个项目已经不存在了" {
		t.Errorf("error %q", got)
	}
}

func TestDevelopmentBuild(t *testing.T) {
	svc := settingsfake.New("")
	svc.AboutInfo.Version = ""
	p := start(t, options{svc: svc})
	p.tab("about")
	p.Find("id:section-about >> text:版本 开发版")
	if p.Enabled("id:reveal-data-dir") {
		t.Error("reveal is enabled without a data folder")
	}
}

func TestResetRestoresTheDefaultsAfterConfirming(t *testing.T) {
	svc := settingsfake.New(dataDir)
	svc.Stored.History.RetentionDays = 30
	svc.Stored.Health.CPUThreshold = 90
	p := start(t, options{svc: svc})
	p.tab("history")
	if got := p.field("retention-days"); got != "30" {
		t.Fatalf("retention shows %q", got)
	}

	p.tab("general")
	p.Press("id:reset")
	p.WaitFor("id:reset-dialog", wait)
	p.Find("id:reset-dialog >> text:恢复默认设置？")
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:reset-dialog", wait)
	if svc.Resets() != 0 {
		t.Fatal("reset without confirming")
	}

	// A draft is not saved on the way.
	p.tab("cleanup")
	p.Clear("id:large-file-min-field")
	p.Type("id:large-file-min-field", "abc")
	p.tab("general")
	p.Press("id:reset")
	p.WaitFor("id:reset-dialog", wait)
	p.Press("id:confirm-ok")
	p.WaitForGone("id:reset-dialog", wait)
	if svc.Resets() != 1 {
		t.Fatalf("resets %d", svc.Resets())
	}
	p.tab("history")
	p.WaitUntil(wait, func() bool { return p.field("retention-days") == "180" })
	p.tab("health")
	if got := p.valueText("提醒阈值"); got != "40%" {
		t.Errorf("threshold reads %q", got)
	}
	p.tab("cleanup")
	if got := p.field("large-file-min"); got != "500" {
		t.Errorf("large files shows %q", got)
	}
	if p.Exists("id:large-file-min-error") {
		t.Error("the draft's error stays")
	}
	if n := len(svc.Updates()); n != 0 {
		t.Errorf("Update was called %d times", n)
	}
}

func TestResetFailureStaysInTheDialog(t *testing.T) {
	svc := settingsfake.New(dataDir)
	svc.ResetErr = errors.New("无法恢复默认设置，详情已记录到日志")
	p := start(t, options{svc: svc})
	p.Press("id:reset")
	p.WaitFor("id:reset-dialog", wait)
	p.Press("id:confirm-ok")
	if got := p.WaitFor("id:reset-error", wait).Text; got != "无法恢复默认设置，详情已记录到日志" {
		t.Errorf("error %q", got)
	}
	p.Find("id:reset-dialog")
}

// Escape while the defaults are being restored does nothing, so a failure still shows in the dialog; idle again, Escape closes it and 恢复默认设置 opens it again.
func TestEscapeWhileResettingKeepsTheDialog(t *testing.T) {
	svc := settingsfake.New(dataDir)
	svc.ResetErr = errors.New("无法恢复默认设置，详情已记录到日志")
	release := make(chan struct{})
	svc.ResetRelease = release
	p := start(t, options{svc: svc})
	p.Press("id:reset")
	p.WaitFor("id:reset-dialog", wait)
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return svc.Resets() == 1 })
	p.Key("Escape")
	p.Settle()
	if !p.Exists("id:reset-dialog") || !p.stores.Block.Blocked() {
		t.Fatalf("after Escape: dialog %v, shell blocked %v", p.Exists("id:reset-dialog"), p.stores.Block.Blocked())
	}
	close(release)
	if got := p.WaitFor("id:reset-error", wait).Text; got != "无法恢复默认设置，详情已记录到日志" {
		t.Errorf("error %q", got)
	}
	p.Key("Escape")
	p.WaitForGone("id:reset-dialog", wait)
	p.Press("id:reset")
	p.WaitFor("id:reset-dialog", wait)
}

func equal(a, b enginesettings.AppSettings) bool {
	return a.Version == b.Version && a.Health == b.Health && a.History == b.History && a.General == b.General &&
		a.Cleanup.DevArtifactMinAgeDays == b.Cleanup.DevArtifactMinAgeDays && a.Cleanup.LargeFileMinMB == b.Cleanup.LargeFileMinMB &&
		slices.Equal(a.Cleanup.Exclusions, b.Cleanup.Exclusions) && slices.Equal(a.Cleanup.DevProjectRoots, b.Cleanup.DevProjectRoots) &&
		slices.Equal(a.Cleanup.InstallerRoots, b.Cleanup.InstallerRoots)
}

// A link elsewhere that names a setting opens the page on its tab, once: the next visit opens on 通用.
func TestOpensOnTheTabALinkAskedFor(t *testing.T) {
	var n *nav.Navigator
	p := &page{t: t, svc: settingsfake.New(dataDir), files: &settingsfake.Files{}, fakes: storefake.New(t.TempDir())}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, p.svc.Use(p.fakes.Deps()))
		settings.DepsKey.Provide(cx, settings.Deps{Files: p.files})
		n = nav.NewNavigator(cx, nav.Health)
		nav.Key.Provide(cx, n)
		return core.Dynamic(func(*core.Scope) core.Node {
			if n.Current() != nav.Settings {
				return core.Text{Content: "elsewhere"}
			}
			return core.Func(settings.Page)
		})
	}, nsgotest.Size(1052, 2600))
	p.WaitFor("text:elsewhere", wait)

	p.Post(func() { n.GoSettings(nav.SettingsCleanup) })
	p.WaitFor("id:settings-tab-cleanup", wait)
	if !p.Find("id:tab-cleanup").Props.Selected {
		t.Error("清理 is not the selected tab")
	}

	p.Post(func() { n.Go(nav.Health) })
	p.WaitFor("text:elsewhere", wait)
	p.Post(func() { n.Go(nav.Settings) })
	p.WaitFor("id:settings-tab-general", wait)
}

func TestGeneralTabOnMacOS(t *testing.T) {
	p := start(t, options{})
	if p.Exists("id:confirm-before-clean") || p.Exists("text:清理前确认") {
		t.Error("清理前确认 is still there")
	}
	if got := p.Find("id:launch-at-login >> text:登录系统后在菜单栏安静运行，不打开窗口。"); got == nil {
		t.Error("登录时启动 does not say it stays in the menu bar")
	}
	p.Find("id:close-to-tray >> text:Spruce 会在后台定期检查，并在空间不足时提醒你。关闭后，关闭窗口就会退出 Spruce。")
	if !p.checked("关闭窗口后继续在菜单栏运行") || !p.checked("在后台定期检查") || p.checked("空间不足时提醒我") {
		t.Error("the new switches do not start at their defaults")
	}
	p.Find("id:section-background >> text:每隔几小时检查一次可以释放的空间。只在接通电源、电脑不忙时进行，只读取，不会删除任何东西。")
	p.Find("id:section-alerts >> text:系统盘可用空间低于下面的值时发一条通知。")
	if got := p.field("low-disk-threshold"); got != "20" {
		t.Errorf("threshold shows %q", got)
	}
	p.Find("id:reset-row >> text:所有设置回到初始值，「登录时启动」和自动清理规则会关闭。操作历史不受影响。")

	// Without the menu bar, logging in opens the window.
	p.Press("role:switch 关闭窗口后继续在菜单栏运行")
	p.updates(1)
	p.WaitFor("id:launch-at-login >> text:登录系统后自动打开 Spruce。", wait)

	p.Press("id:reset")
	p.WaitFor("id:reset-dialog >> text:所有设置回到初始值，「登录时启动」和自动清理规则会关闭。操作历史不受影响。", wait)
}

func TestGeneralTabOnWindows(t *testing.T) {
	p := start(t, options{goos: "windows"})
	if p.Exists("id:close-to-tray") {
		t.Error("Windows shows 关闭窗口后继续在菜单栏运行")
	}
	p.Find("id:launch-at-login >> text:登录系统后自动打开 Spruce。")
	p.Find("id:section-background >> text:Spruce 打开时每隔几小时检查一次可以释放的空间。只在接通电源、电脑不忙时进行，只读取，不会删除任何东西。")
	p.Find("id:low-disk-alert")
}

func TestLowDiskAlertAsksForNotificationsAndSaysWhenDenied(t *testing.T) {
	p := start(t, options{})
	// Opening the page reads the permission without asking.
	p.Settle()
	if p.NotificationPermission != core.NotificationsNotAsked {
		t.Fatalf("permission %v as the page opened", p.NotificationPermission)
	}
	p.NotificationAnswer = core.NotificationsDenied
	p.Press("role:switch 空间不足时提醒我")
	if got := p.updates(1).General; !got.LowDiskAlert || got.LowDiskGB != 20 {
		t.Fatalf("general %+v", got)
	}
	if got := p.WaitFor("id:notify-denied", wait).Text; got != "通知没有开启，可以在系统设置里允许 Spruce 发送通知" {
		t.Errorf("denied line %q", got)
	}
	if p.NotificationPermission != core.NotificationsDenied {
		t.Errorf("permission %v: turning the alert on did not ask", p.NotificationPermission)
	}
	// The switch stays on; turning it off takes the line away.
	if !p.checked("空间不足时提醒我") {
		t.Error("the alert turned itself off")
	}
	p.Press("role:switch 空间不足时提醒我")
	p.updates(2)
	p.WaitForGone("id:notify-denied", wait)

	p.Clear("id:low-disk-threshold-field")
	p.Type("id:low-disk-threshold-field", "0")
	if got := p.text("low-disk-threshold-error"); got != "请输入 1 到 500 之间的整数" {
		t.Errorf("error %q", got)
	}
	p.Clear("id:low-disk-threshold-field")
	p.Type("id:low-disk-threshold-field", "50")
	p.Advance(debounce)
	if got := p.updates(3).General.LowDiskGB; got != 50 {
		t.Errorf("threshold saved as %d", got)
	}
}

func TestLowDiskAlertGranted(t *testing.T) {
	p := start(t, options{})
	p.Press("role:switch 空间不足时提醒我")
	p.updates(1)
	p.WaitUntil(wait, func() bool { return p.NotificationPermission == core.NotificationsGranted })
	p.Settle()
	if p.Exists("id:notify-denied") {
		t.Error("notify-denied with the permission granted")
	}
}

func TestAutoTabRulesAndTheirStatus(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	ov := storefake.NewOverview(app.OverviewState{Phase: app.OverviewIdle})
	fakes.Overview = ov
	p := start(t, options{fakes: fakes})
	p.tab("auto")
	p.Find("id:settings-tab-auto >> text:自动清理只会把文件移到废纸篓，不会运行命令，也不会直接删除。每次运行都会记在操作历史里，在清空废纸篓之前可以放回原处。只在接通电源、电脑不忙时运行。你放回原处的项目不会再被自动清理。")
	for _, r := range []struct{ id, title string }{
		{"app-caches", "旧的应用缓存"}, {"installers", "旧安装包"}, {"build-output", "闲置项目的构建产物"}, {"incomplete-downloads", "未完成的下载"},
	} {
		p.Find("id:section-auto-rules >> id:rule-" + r.id)
		if p.checked(r.title) {
			t.Errorf("%s is on by default", r.title)
		}
		if got := p.text("rule-" + r.id + "-status"); got != "检查后会显示符合条件的项目" {
			t.Errorf("%s status %q without a result", r.id, got)
		}
	}
	p.Find("id:rule-installers >> text:每天把「下载」里超过 7 天、对应的应用已经装好的安装包和磁盘映像（.dmg、.pkg）移到废纸篓。压缩包、系统映像和其他文件夹里的不会处理，正在挂载或无法确认是否挂载的也不会处理。")
	if !p.checked("每周汇总") {
		t.Error("每周汇总 is off by default")
	}
	p.Find("id:auto-summary-notify >> text:每周一告诉你自动清理了多少。")

	ov.Set(app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{},
		Rules: []app.RuleStatus{{ID: "app-caches", Matching: 12, MatchingBytes: 3 << 30}, {ID: "installers", LastRun: time.Now().Add(-3 * time.Hour), LastTrashed: 1 << 30}}})
	p.WaitUntil(wait, func() bool { return p.text("rule-app-caches-status") == "现在符合条件的：12 项，"+format.Bytes(3<<30) })

	// Turning a rule on is the user's action: it asks for notifications once, for the weekly summary, and says when it runs.
	p.Press("role:switch 旧的应用缓存")
	if got := p.updates(1).Automation; !got.AppCaches || got.Installers || !got.WeeklySummary {
		t.Fatalf("automation %+v", got)
	}
	p.WaitUntil(wait, func() bool { return p.text("rule-app-caches-status") == "还没有运行过，下次后台检查时运行" })
	p.WaitUntil(wait, func() bool { return p.NotificationPermission == core.NotificationsGranted })

	p.Press("role:switch 旧安装包")
	p.updates(2)
	want := "上次运行：" + format.RelativeTime(time.Now().Add(-3*time.Hour), time.Now()) + "，移到废纸篓 " + format.Bytes(1<<30)
	p.WaitUntil(wait, func() bool { return p.text("rule-installers-status") == want })
}

func TestWeeklySummarySaysWhenNotificationsAreOff(t *testing.T) {
	svc := settingsfake.New(dataDir)
	svc.Stored.Automation.WeeklySummary = false
	p := start(t, options{svc: svc})
	p.tab("auto")
	p.NotificationAnswer = core.NotificationsDenied
	p.Press("role:switch 每周汇总")
	if got := p.updates(1).Automation; !got.WeeklySummary {
		t.Fatalf("automation %+v", got)
	}
	p.WaitFor("id:auto-summary-notify >> id:notify-denied", wait)
}

func TestAutoTabOnWindows(t *testing.T) {
	p := start(t, options{goos: "windows"})
	p.tab("auto")
	p.Find("id:auto-unsupported >> text:Windows 版暂不提供自动清理。Windows 在回收站放不下某个项目时会询问是否直接删除，无人值守时无法保证只移到回收站。")
	if p.Exists("id:section-auto-rules") || p.Exists("id:auto-summary-notify") || len(p.Query("id:settings-tab-auto >> role:switch")) != 0 {
		t.Error("Windows shows the rules")
	}
}

func TestTabsAndDeveloperGroup(t *testing.T) {
	p := start(t, options{})
	var ids []string
	for _, tab := range p.Query("role:tab") {
		ids = append(ids, tab.Props.TestID)
	}
	if want := []string{"tab-general", "tab-auto", "tab-cleanup", "tab-health", "tab-history", "tab-about"}; !slices.Equal(ids, want) {
		t.Errorf("tabs %v, want %v", ids, want)
	}
	p.tab("cleanup")
	if got := p.Find("id:section-dev-junk").Props.Label; got != "开发者" {
		t.Errorf("group %q", got)
	}
}
