package settings

import (
	"testing"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/engine/rules"
	"spruce/apps/spruce/internal/engine/settings"
	"spruce/apps/spruce/internal/ui/format"
)

func TestParseWhole(t *testing.T) {
	for _, tc := range []struct {
		text string
		want int
		ok   bool
	}{
		{"30", 30, true},
		{" 180 ", 180, true},
		{"0", 0, true},
		{"007", 7, true},
		{"9007199254740991", 9007199254740991, true},
		{"9007199254740992", 0, false},
		{"99999999999999999999", 0, false},
		{"", 0, false},
		{"  ", 0, false},
		{"3天", 0, false},
		{"-1", 0, false},
		{"+1", 0, false},
		{"1.5", 0, false},
		{"1e3", 0, false},
		{"1 000", 0, false},
		{"３０", 0, false},
	} {
		got, ok := ParseWhole(tc.text)
		if got != tc.want || ok != tc.ok {
			t.Errorf("ParseWhole(%q) = %d, %v; want %d, %v", tc.text, got, ok, tc.want, tc.ok)
		}
	}
}

func TestRange(t *testing.T) {
	r := app.SettingRange{Min: 7, Max: 3650}
	for n, want := range map[int]bool{6: false, 7: true, 180: true, 3650: true, 3651: false} {
		if InRange(n, r) != want {
			t.Errorf("InRange(%d) = %v", n, !want)
		}
	}
	if got := RangeText(r); got != "请输入 7 到 3650 之间的整数" {
		t.Errorf("RangeText = %q", got)
	}
}

func TestLists(t *testing.T) {
	if !SameList([]string{"/a", "/b"}, []string{"/a", "/b"}) || SameList([]string{"/a", "/b"}, []string{"/b", "/a"}) ||
		SameList([]string{"/a"}, []string{"/a", "/b"}) || !SameList(nil, []string{}) {
		t.Error("SameList compares wrongly")
	}
	if !ContainsFold([]string{"/Users/me/Keep"}, "/users/ME/keep") || ContainsFold([]string{"/Users/me/Keep"}, "/Users/me/Keeps") {
		t.Error("ContainsFold compares wrongly")
	}
	list := []string{"/a", "/b"}
	if got := With(list, "/c"); !SameList(got, []string{"/a", "/b", "/c"}) || len(list) != 2 {
		t.Errorf("With = %v, list %v", got, list)
	}
	if got := Without(list, "/a"); !SameList(got, []string{"/b"}) || !SameList(list, []string{"/a", "/b"}) {
		t.Errorf("Without = %v, list %v", got, list)
	}
}

func TestFieldsWriteTheirOwnPart(t *testing.T) {
	base := settings.Defaults("/Users/me")
	got := FieldRetentionDays.Put(base, 30)
	if got.History.RetentionDays != 30 || base.History.RetentionDays == 30 || FieldRetentionDays.Get(got) != 30 {
		t.Error("retention")
	}
	got = FieldCPUThreshold.Put(base, 75)
	if got.Health.CPUThreshold != 75 || got.Health.SustainMinutes != base.Health.SustainMinutes {
		t.Error("threshold")
	}
	got = FieldExclusions.Put(base, []string{"/x"})
	if !SameList(got.Cleanup.Exclusions, []string{"/x"}) || !SameList(got.Cleanup.DevProjectRoots, base.Cleanup.DevProjectRoots) {
		t.Error("exclusions")
	}
	got = FieldLaunchAtLogin.Put(base, true)
	if !got.General.LaunchAtLogin || got.General.ConfirmBeforeClean != base.General.ConfirmBeforeClean || got.General.KeepInTray != base.General.KeepInTray {
		t.Error("launch at login")
	}
	got = FieldKeepInTray.Put(base, false)
	if got.General.KeepInTray || !base.General.KeepInTray || got.General.LaunchAtLogin != base.General.LaunchAtLogin {
		t.Error("keep in tray")
	}
	got = FieldBackgroundEnabled.Put(base, false)
	if got.Background.Enabled || got.General != base.General {
		t.Error("background")
	}
	got = FieldLowDiskAlert.Put(FieldLowDiskGB.Put(base, 50), true)
	if !got.General.LowDiskAlert || got.General.LowDiskGB != 50 || FieldLowDiskGB.Get(got) != 50 || got.General.KeepInTray != base.General.KeepInTray {
		t.Error("low disk")
	}
	got = FieldWeeklySummary.Put(base, false)
	if got.Automation.WeeklySummary || got.Automation.AppCaches != base.Automation.AppCaches {
		t.Error("weekly summary")
	}
	// Each rule's switch writes its own flag and nothing else.
	for _, spec := range rules.Catalog() {
		got := RuleField(spec.ID).Put(base, true)
		on := rules.Enabled(got.Automation)
		if len(on) != 1 || on[0] != spec.ID || !RuleField(spec.ID).Get(got) || got.Automation.WeeklySummary != base.Automation.WeeklySummary {
			t.Errorf("rule %s turns on %v", spec.ID, on)
		}
		if RuleDescription(spec.ID) == "" {
			t.Errorf("rule %s has no description", spec.ID)
		}
	}
}

func TestRuleStatusText(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)
	st := &app.RuleStatus{ID: "app-caches", Matching: 12, MatchingBytes: 3 << 30, LastRun: now.Add(-2 * time.Hour), LastTrashed: 1 << 30}
	trash := format.TrashName()
	lastRun := "上次运行：" + format.RelativeTime(st.LastRun, now) + "，移到" + trash + " " + format.Bytes(1<<30)
	for _, c := range []struct {
		on        bool
		st        *app.RuleStatus
		hasResult bool
		want      string
	}{
		{false, nil, false, "检查后会显示符合条件的项目"},
		{false, nil, true, "检查后会显示符合条件的项目"},
		{false, st, false, "检查后会显示符合条件的项目"},
		{false, st, true, "现在符合条件的：12 项，" + format.Bytes(3<<30)},
		{true, nil, false, "还没有运行过，下次后台检查时运行"},
		{true, &app.RuleStatus{ID: "app-caches", Matching: 3}, true, "还没有运行过，下次后台检查时运行"},
		{true, st, true, lastRun},
		{true, st, false, lastRun},
	} {
		if got := RuleStatusText(c.on, c.st, c.hasResult, now); got != c.want {
			t.Errorf("RuleStatusText(%v, %+v, %v) = %q, want %q", c.on, c.st, c.hasResult, got, c.want)
		}
	}

	// A rule that is on and has run shows its last run before any check result arrives (at startup, or with no readable cache).
	state := app.OverviewState{Rules: []app.RuleStatus{*st}}
	got := RuleStatusOf(state, rules.AppCaches)
	if got == nil || got.Matching != 12 {
		t.Fatalf("status without a result %+v", got)
	}
	if text := RuleStatusText(true, got, state.Result != nil, now); text != lastRun {
		t.Errorf("on, run, no result: %q, want %q", text, lastRun)
	}
	state.Result = &app.OverviewResult{}
	if got := RuleStatusOf(state, rules.AppCaches); got == nil || got.Matching != 12 {
		t.Errorf("status %+v", got)
	}
	if RuleStatusOf(state, rules.Installers) != nil {
		t.Error("a status of a rule the controller did not report")
	}
}

func TestTexts(t *testing.T) {
	if Version("0.1.0") != "版本 0.1.0" || Version("") != "版本 开发版" {
		t.Error("Version")
	}
	for st, want := range map[maintenance.PermissionState]string{
		maintenance.PermissionGranted: "已授权", maintenance.PermissionDenied: "未授权",
		maintenance.PermissionUnknown: "无法确定", maintenance.PermissionNotRequired: "不需要",
	} {
		if got := PermissionText(st); got != want {
			t.Errorf("PermissionText(%v) = %q", st, got)
		}
	}
}
