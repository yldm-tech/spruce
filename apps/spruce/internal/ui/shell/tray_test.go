package shell

import (
	"errors"
	"slices"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/rules"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

const (
	gb = int64(1) << 30
	// sep stands for a separator in the expected menus.
	sep = "—"
)

// shown is each item of menu as its label, "—" for a separator, with "(off)" after a disabled one.
func shown(menu []desktop.MenuItem) []string {
	out := make([]string, len(menu))
	for i, it := range menu {
		switch {
		case it.Separator:
			out[i] = sep
		case it.Disabled:
			out[i] = it.Label + " (off)"
		default:
			out[i] = it.Label
		}
	}
	return out
}

func TestTrayMenu(t *testing.T) {
	disk := &app.DiskState{Volume: "Macintosh HD", Free: 120 * gb, Total: 500 * gb}
	low := &app.DiskState{Volume: "Macintosh HD", Free: 8 * gb, Total: 500 * gb, Low: true}
	free, total, lowFree, headline := format.Bytes(120*gb), format.Bytes(500*gb), format.Bytes(8*gb), format.Bytes(18*gb)
	tail := func(check, auto, quit string) []string {
		return []string{sep, "打开 Spruce", check, sep, auto, sep, quit}
	}
	for _, c := range []struct {
		name string
		s    TrayState
		want []string
	}{
		{"ready", TrayState{Disk: disk, HasResult: true, Headline: 18 * gb},
			append([]string{"可用 " + free + "（共 " + total + "） (off)", "可以放心释放 " + headline + "…"}, tail("立即检查", "自动清理：未开启", "退出 Spruce")...)},
		{"checking with a result keeps the number", TrayState{Disk: disk, Checking: true, HasResult: true, Headline: 18 * gb},
			append([]string{"可用 " + free + "（共 " + total + "） (off)", "可以放心释放 " + headline + "…"}, tail("立即检查 (off)", "自动清理：未开启", "退出 Spruce")...)},
		{"first check", TrayState{Disk: disk, Checking: true},
			append([]string{"可用 " + free + "（共 " + total + "） (off)", "正在检查… (off)"}, tail("立即检查 (off)", "自动清理：未开启", "退出 Spruce")...)},
		{"nothing to clean", TrayState{Disk: low, HasResult: true, RulesOn: 2},
			append([]string{"空间不足：只剩 " + lowFree + " (off)", "没有需要清理的内容 (off)"}, tail("立即检查", "自动清理：已开启 2 条", "退出 Spruce")...)},
		{"no result and no disk yet", TrayState{},
			append([]string{"查看检查结果…"}, tail("立即检查", "自动清理：未开启", "退出 Spruce")...)},
		{"cleaning", TrayState{Disk: disk, HasResult: true, Headline: 18 * gb, Cleaning: true, RulesOn: 4},
			append([]string{"可用 " + free + "（共 " + total + "） (off)", "可以放心释放 " + headline + "…"},
				tail("立即检查 (off)", "自动清理：已开启 4 条", "退出 Spruce（正在进行的清理会在当前项目完成后停止）")...)},
	} {
		if got := shown(TrayMenu(c.s)); !slices.Equal(got, c.want) {
			t.Errorf("%s:\n got %q\nwant %q", c.name, got, c.want)
		}
	}
}

func TestTrayMenuActions(t *testing.T) {
	var did []string
	s := TrayState{HasResult: true, Headline: gb, RulesOn: len(rules.Catalog()),
		Open: func() { did = append(did, "open") }, Overview: func() { did = append(did, "overview") }, Check: func() { did = append(did, "check") },
		Auto: func() { did = append(did, "auto") }, Quit: func() { did = append(did, "quit") }}
	for _, it := range TrayMenu(s) {
		if !it.Separator && !it.Disabled {
			it.OnSelect()
		}
	}
	if want := []string{"overview", "open", "check", "auto", "quit"}; !slices.Equal(did, want) {
		t.Errorf("actions %v, want %v", did, want)
	}
}

func TestTrayTooltip(t *testing.T) {
	for _, c := range []struct {
		d        *app.DiskState
		checking bool
		want     string
	}{
		{nil, false, "Spruce"},
		{&app.DiskState{Free: 120 * gb}, false, "Spruce · 可用 " + format.Bytes(120*gb)},
		{&app.DiskState{Free: 8 * gb, Low: true}, false, "Spruce · 空间不足，只剩 " + format.Bytes(8*gb)},
		{&app.DiskState{Free: 8 * gb, Low: true}, true, "Spruce · 正在检查…"},
		{nil, true, "Spruce · 正在检查…"},
	} {
		if got := trayTooltip(c.d, c.checking); got != c.want {
			t.Errorf("trayTooltip(%+v, %v) = %q, want %q", c.d, c.checking, got, c.want)
		}
	}
}

func TestNoticeText(t *testing.T) {
	trash := format.TrashName()
	for _, c := range []struct {
		name        string
		n           app.Notice
		title, body string
	}{
		{"low disk with something to free", app.Notice{Kind: app.NoticeLowDisk, ID: "low-disk", Volume: "Macintosh HD", Free: 8 * gb, Recommended: 18 * gb},
			"磁盘空间不多了", "Macintosh HD 只剩 " + format.Bytes(8*gb) + "。Spruce 找到 " + format.Bytes(18*gb) + " 可以放心释放，点按查看。"},
		{"low disk with nothing to free", app.Notice{Kind: app.NoticeLowDisk, ID: "low-disk", Volume: "Macintosh HD", Free: 8 * gb},
			"磁盘空间不多了", "Macintosh HD 只剩 " + format.Bytes(8*gb) + "，点按看看是什么占用了空间。"},
		{"summary of one rule", app.Notice{Kind: app.NoticeAutoSummary, ID: "auto-summary:2026-W46", Trashed: 3 * gb,
			Rules: []rules.RuleTotal{{Rule: rules.AppCaches, Items: 40, Bytes: 3 * gb}}},
			"本周自动清理了 " + format.Bytes(3*gb), "旧的应用缓存 " + format.Bytes(3*gb) + " 已移到" + trash + "。点按查看，需要时可以放回原处。"},
		{"summary of two rules", app.Notice{Kind: app.NoticeAutoSummary, ID: "auto-summary:2026-W46", Trashed: 5 * gb,
			Rules: []rules.RuleTotal{{Rule: rules.BuildOutput, Bytes: 4 * gb}, {Rule: rules.Installers, Bytes: gb}}},
			"本周自动清理了 " + format.Bytes(5*gb),
			"闲置项目的构建产物 " + format.Bytes(4*gb) + "、旧安装包 " + format.Bytes(gb) + " 已移到" + trash + "。点按查看，需要时可以放回原处。"},
		{"summary of more rules ends with 等", app.Notice{Kind: app.NoticeAutoSummary, ID: "auto-summary:2026-W46", Trashed: 6 * gb,
			Rules: []rules.RuleTotal{{Rule: rules.BuildOutput, Bytes: 4 * gb}, {Rule: rules.AppCaches, Bytes: gb}, {Rule: rules.IncompleteDownloads, Bytes: gb}}},
			"本周自动清理了 " + format.Bytes(6*gb),
			"闲置项目的构建产物 " + format.Bytes(4*gb) + "、旧的应用缓存 " + format.Bytes(gb) + " 等已移到" + trash + "。点按查看，需要时可以放回原处。"},
	} {
		title, body := NoticeText(c.n)
		if title != c.title || body != c.body {
			t.Errorf("%s:\n got %q / %q\nwant %q / %q", c.name, title, body, c.title, c.body)
		}
	}
}

func TestCheckNoticeText(t *testing.T) {
	for _, c := range []struct {
		failed      string
		headline    int64
		title, body string
	}{
		{"", 18 * gb, "检查完成", "可以放心释放 " + format.Bytes(18*gb) + "，点按查看。"},
		{"", 0, "检查完成", "没有需要清理的内容。"},
		{"正在清理，完成后再检查", 0, "检查没有完成", "正在清理，完成后再检查"},
	} {
		if title, body := CheckNoticeText(c.failed, c.headline); title != c.title || body != c.body {
			t.Errorf("CheckNoticeText(%q, %d) = %q / %q, want %q / %q", c.failed, c.headline, title, body, c.title, c.body)
		}
	}
}

// startTrayCheck runs trayCheck over a fake controller in state st, with the window away or not, and returns its 立即检查.
func startTrayCheck(t *testing.T, st app.OverviewState, away *bool) (*nsgotest.App, *storefake.Overview, func()) {
	t.Helper()
	fakes := storefake.New(t.TempDir())
	ov := storefake.NewOverview(st)
	fakes.Overview = ov
	var check func()
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		stores := storefake.Provide(cx, fakes.Deps())
		check = trayCheck(cx, stores.Overview, func() bool { return *away })
		return core.Text{Content: "Spruce"}
	})
	a.NotificationPermission = core.NotificationsGranted
	return a, ov, check
}

func TestTrayCheckTellsHowItEndedWhileTheWindowIsAway(t *testing.T) {
	a, ov, check := startTrayCheck(t, app.OverviewState{Phase: app.OverviewIdle}, new(true))
	a.Post(check)
	a.WaitUntil(5*time.Second, func() bool { checks, _, _, _, _ := ov.Calls(); return checks == 1 })
	ov.Set(app.OverviewState{Phase: app.OverviewChecking})
	a.Settle()
	if len(a.Notifications) != 0 {
		t.Fatalf("a notification while the check runs: %v", a.Notifications)
	}
	ov.Set(app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{CheckedAt: time.Now(), Headline: 18 * gb}})
	a.WaitUntil(5*time.Second, func() bool { return len(a.Notifications) == 1 })
	if n := a.Notifications[0]; n.ID != noticeCheck || n.Title != "检查完成" || n.Body != "可以放心释放 "+format.Bytes(18*gb)+"，点按查看。" {
		t.Errorf("notification %+v", n)
	}

	// A later state of the same result says nothing again.
	ov.Set(app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{CheckedAt: time.Now(), Headline: 18 * gb}})
	a.Settle()
	if len(a.Notifications) != 1 {
		t.Errorf("%d notifications after the check ended once", len(a.Notifications))
	}
}

// The window coming back while the check runs shows how it went, so no notification follows.
func TestTrayCheckSaysNothingOnceTheWindowIsBack(t *testing.T) {
	away := true
	a, ov, check := startTrayCheck(t, app.OverviewState{Phase: app.OverviewIdle}, &away)
	a.Post(check)
	a.WaitUntil(5*time.Second, func() bool { checks, _, _, _, _ := ov.Calls(); return checks == 1 })
	ov.Set(app.OverviewState{Phase: app.OverviewChecking})
	a.Settle()
	a.Post(func() { away = false })
	a.Settle()
	ov.Set(app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{CheckedAt: time.Now(), Headline: 18 * gb}})
	a.Settle()
	a.Advance(time.Second)
	if len(a.Notifications) != 0 {
		t.Errorf("notifications with the window back: %v", a.Notifications)
	}
}

func TestTrayCheckSaysWhyItDidNotFinish(t *testing.T) {
	a, ov, check := startTrayCheck(t, app.OverviewState{Phase: app.OverviewIdle}, new(true))
	a.Post(check)
	a.WaitUntil(5*time.Second, func() bool { checks, _, _, _, _ := ov.Calls(); return checks == 1 })
	ov.Set(app.OverviewState{Phase: app.OverviewChecking})
	a.Settle()
	ov.Set(app.OverviewState{Phase: app.OverviewIdle, Error: "无法读取磁盘"})
	a.WaitUntil(5*time.Second, func() bool { return len(a.Notifications) == 1 })
	if n := a.Notifications[0]; n.Title != "检查没有完成" || n.Body != "无法读取磁盘" {
		t.Errorf("notification %+v", n)
	}

	// Refused at once (a clean runs): it says so without waiting.
	b, ov2, check2 := startTrayCheck(t, app.OverviewState{Phase: app.OverviewIdle}, new(true))
	ov2.CheckErr = errors.New("正在清理，完成后再检查")
	b.Post(check2)
	b.WaitUntil(5*time.Second, func() bool { return len(b.Notifications) == 1 })
	if n := b.Notifications[0]; n.Title != "检查没有完成" || n.Body != "正在清理，完成后再检查" {
		t.Errorf("refusal notification %+v", n)
	}
}

func TestTrayCheckWithTheWindowInSightSaysNothing(t *testing.T) {
	a, ov, check := startTrayCheck(t, app.OverviewState{Phase: app.OverviewIdle}, new(false))
	a.Post(check)
	a.WaitUntil(5*time.Second, func() bool { checks, _, _, _, _ := ov.Calls(); return checks == 1 })
	ov.Set(app.OverviewState{Phase: app.OverviewChecking})
	a.Settle()
	ov.Set(app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{CheckedAt: time.Now()}})
	a.Settle()
	a.Advance(time.Second)
	if len(a.Notifications) != 0 {
		t.Errorf("notifications with the window in sight: %v", a.Notifications)
	}
}
