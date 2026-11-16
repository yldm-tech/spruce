package shared

import (
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/health"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/ui/format"
)

func TestAlertTitle(t *testing.T) {
	a := health.Alert{Name: "Xcode", CPUPercent: 47.6, Duration: 12*time.Minute + 40*time.Second}
	if got := AlertTitle(a); got != "Xcode 已经持续占用 48% CPU 有 12 分钟" {
		t.Errorf("AlertTitle = %q", got)
	}
	a.Duration = 20 * time.Second
	if got := AlertTitle(a); got != "Xcode 已经持续占用 48% CPU 有 1 分钟" {
		t.Errorf("short alert = %q", got)
	}
}

func TestAlertSub(t *testing.T) {
	mac := monitorFor("darwin")
	if got := AlertSub(0, mac); got != "如果不是你正在用它，可以在活动监视器里看看。Spruce 不会结束进程，只提醒你。" {
		t.Errorf("mac = %q", got)
	}
	win := monitorFor("windows")
	if got := AlertSub(2, win); got != "另有 2 个进程也在高占用。占用按单个处理器核心计算，任务管理器按全部核心显示，数值会更低。如果不是你正在用它，可以在任务管理器里看看。Spruce 不会结束进程，只提醒你。" {
		t.Errorf("windows = %q", got)
	}
}

func TestNeedsFullDiskAccess(t *testing.T) {
	for st, want := range map[maintenance.PermissionState]bool{
		maintenance.PermissionDenied: true, maintenance.PermissionUnknown: true,
		maintenance.PermissionGranted: false, maintenance.PermissionNotRequired: false, "": false,
	} {
		if got := NeedsFullDiskAccess(st); got != want {
			t.Errorf("NeedsFullDiskAccess(%q) = %v", st, got)
		}
	}
}

func TestLastCleanText(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)
	run := history.Run{EndedAt: now.Add(-72 * time.Hour), Summary: &history.Totals{Freed: 1_200_000_000, Trashed: 3_000_000}}
	want := "上次清理：3天前，释放 " + format.Bytes(1_200_000_000) + "，移到" + format.TrashName() + " " + format.Bytes(3_000_000)
	if got := LastCleanText(run, now); got != want {
		t.Errorf("LastCleanText = %q, want %q", got, want)
	}
	if got := LastCleanText(history.Run{StartedAt: now.Add(-time.Minute * 5)}, now); got != "上次清理：5分钟前" {
		t.Errorf("without summary = %q", got)
	}
}
