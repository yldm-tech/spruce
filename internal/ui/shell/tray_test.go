package shell

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/rules"
	"github.com/yldm-tech/spruce/internal/ui/format"
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
		d    *app.DiskState
		want string
	}{
		{nil, "Spruce"},
		{&app.DiskState{Free: 120 * gb}, "Spruce · 可用 " + format.Bytes(120*gb)},
		{&app.DiskState{Free: 8 * gb, Low: true}, "Spruce · 空间不足，只剩 " + format.Bytes(8*gb)},
	} {
		if got := trayTooltip(c.d); got != c.want {
			t.Errorf("trayTooltip(%+v) = %q, want %q", c.d, got, c.want)
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
