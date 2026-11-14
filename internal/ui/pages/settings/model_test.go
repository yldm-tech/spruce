package settings

import (
	"testing"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/settings"
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
	if !got.General.LaunchAtLogin || got.General.ConfirmBeforeClean != base.General.ConfirmBeforeClean {
		t.Error("launch at login")
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
