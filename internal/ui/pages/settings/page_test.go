package settings_test

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	enginesettings "github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/ui/format"
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
		settings.DepsKey.Provide(cx, settings.Deps{Files: o.files})
		p.shown = core.NewSignal(cx, true)
		return core.Show{When: p.shown.Get, Then: func(*core.Scope) core.Node { return core.Func(settings.Page) }}
	}, nsgotest.Size(1052, 2600))
	p.WaitFor("id:section-about", wait)
	return p
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

	p.Press("role:switch 清理前确认")
	if got := p.updates(2).General; got != (enginesettings.GeneralSettings{LaunchAtLogin: true, ConfirmBeforeClean: false}) {
		t.Fatalf("general %+v", got)
	}

	p.Press("role:switch 启用高 CPU 提醒")
	if got := p.updates(3).Health; got != (enginesettings.HealthSettings{CPUThreshold: 40, SustainMinutes: 10, Enabled: false}) {
		t.Fatalf("health %+v", got)
	}
	if p.checked("启用高 CPU 提醒") {
		t.Error("启用高 CPU 提醒 is still on")
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
		if next.General.ConfirmBeforeClean {
			<-on
		} else {
			<-off
		}
	}
	svc.Fail = func(next enginesettings.AppSettings) error {
		if !next.General.ConfirmBeforeClean {
			return errors.New("无法保存设置，详情已记录到日志")
		}
		return nil
	}
	p := start(t, options{svc: svc})
	p.Press("role:switch 清理前确认")
	p.Press("role:switch 清理前确认")
	p.WaitUntil(wait, func() bool { return len(svc.Updates()) == 2 })
	if !p.checked("清理前确认") {
		t.Fatal("the switch does not show the latest change")
	}
	// The newer save answers first, then the older one fails: neither its error nor its value shows.
	close(on)
	p.WaitUntil(wait, func() bool { return svc.Answered() == 1 && p.checked("清理前确认") })
	close(off)
	p.WaitUntil(wait, func() bool { return svc.Answered() == 2 })
	// The older answer is posted right after Update returns; nothing it brings may show.
	for deadline := time.Now().Add(200 * time.Millisecond); time.Now().Before(deadline); time.Sleep(10 * time.Millisecond) {
		p.Settle()
		if p.Exists("id:confirm-before-clean-error") {
			t.Fatalf("the older save's error shows: %q", p.text("confirm-before-clean-error"))
		}
		if !p.checked("清理前确认") {
			t.Fatal("the older save's value shows")
		}
	}
}

func TestSlidersSaveOnceTheUserStops(t *testing.T) {
	p := start(t, options{})
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
	p.Clear("id:retention-days-field")
	p.Type("id:retention-days-field", "5")
	p.Focus("id:large-file-min-field")
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
	p.Clear("id:retention-days-field")
	p.Type("id:retention-days-field", "90")
	p.Post(func() { p.shown.Set(false) })
	p.WaitForGone("id:section-about", wait)
	if got := p.updates(1).History.RetentionDays; got != 90 {
		t.Fatalf("retention %d", got)
	}
}

func TestFolderPickerAddsRefusesDuplicatesIgnoresCancelAndRemoves(t *testing.T) {
	p := start(t, options{})
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
	p.Find("id:exclusions-list >> text:还没有添加文件夹")
}

func TestClearHistoryAfterConfirming(t *testing.T) {
	fakes := storefake.New(t.TempDir())
	fakes.Runs = []history.Run{{ID: "1", Kind: history.KindMaintenance}}
	p := start(t, options{fakes: fakes})
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
	p.Press("id:clear-history-button")
	p.WaitFor("id:clear-dialog", wait)
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:clear-dialog", wait)
	if runs, _ := fakes.Deps().History.List(10); len(runs) != 1 {
		t.Errorf("runs %v", runs)
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
	p.Find("role:button 在" + format.FileManagerName() + "中显示")
}

func TestAboutShowsTheVersionAndRevealsTheDataFolder(t *testing.T) {
	p := start(t, options{})
	p.Find("role:header 设置")
	p.Find("text:调整提醒、清理范围、排除的文件夹和历史记录")
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
	p.Press("id:reveal-data-dir")
	if got := p.WaitFor("id:data-dir-error", wait).Text; got != "这个项目已经不存在了" {
		t.Errorf("error %q", got)
	}
}

func TestDevelopmentBuild(t *testing.T) {
	svc := settingsfake.New("")
	svc.AboutInfo.Version = ""
	p := start(t, options{svc: svc})
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
	if got := p.field("retention-days"); got != "30" {
		t.Fatalf("retention shows %q", got)
	}

	p.Press("id:reset")
	p.WaitFor("id:reset-dialog", wait)
	p.Find("id:reset-dialog >> text:恢复默认设置？")
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:reset-dialog", wait)
	if svc.Resets() != 0 {
		t.Fatal("reset without confirming")
	}

	// A draft goes with the reset.
	p.Clear("id:large-file-min-field")
	p.Type("id:large-file-min-field", "abc")
	p.Press("id:reset")
	p.WaitFor("id:reset-dialog", wait)
	p.Press("id:confirm-ok")
	p.WaitForGone("id:reset-dialog", wait)
	if svc.Resets() != 1 {
		t.Fatalf("resets %d", svc.Resets())
	}
	p.WaitUntil(wait, func() bool { return p.field("retention-days") == "180" })
	if got := p.valueText("提醒阈值"); got != "40%" {
		t.Errorf("threshold reads %q", got)
	}
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

func equal(a, b enginesettings.AppSettings) bool {
	return a.Version == b.Version && a.Health == b.Health && a.History == b.History && a.General == b.General &&
		a.Cleanup.DevArtifactMinAgeDays == b.Cleanup.DevArtifactMinAgeDays && a.Cleanup.LargeFileMinMB == b.Cleanup.LargeFileMinMB &&
		slices.Equal(a.Cleanup.Exclusions, b.Cleanup.Exclusions) && slices.Equal(a.Cleanup.DevProjectRoots, b.Cleanup.DevProjectRoots) &&
		slices.Equal(a.Cleanup.InstallerRoots, b.Cleanup.InstallerRoots)
}
