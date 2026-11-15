package shell

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/format"
)

// TrayState is what the menu bar icon's menu shows and does, read as the user clicks the icon.
type TrayState struct {
	// Disk is the system volume's space; nil until the controller first reads it.
	Disk *app.DiskState
	// Checking is set while a check runs; Cleaning while a clean runs, 概览's or any other of this process.
	Checking, Cleaning bool
	// HasResult is set once a check has a conclusion; Headline is what it says can be freed.
	HasResult bool
	Headline  int64
	// RulesOn counts the automatic rules turned on.
	RulesOn int

	// Open shows the window as it was; Overview shows it on 概览; Check starts a foreground check; Auto shows 设置 → 自动清理; Quit ends Spruce.
	Open, Overview, Check, Auto, Quit func()
}

// TrayMenu is the menu bar icon's menu for s: the free space, the conclusion, 打开 Spruce and 立即检查, the automatic rules, and 退出 Spruce, which says what happens to a clean that runs. No item cleans anything: they open the window or start a read-only check.
func TrayMenu(s TrayState) []desktop.MenuItem {
	sep := desktop.MenuItem{Separator: true}
	var items []desktop.MenuItem
	if d := s.Disk; d != nil {
		label := fmt.Sprintf("可用 %s（共 %s）", format.Bytes(d.Free), format.Bytes(d.Total))
		if d.Low {
			label = "空间不足：只剩 " + format.Bytes(d.Free)
		}
		items = append(items, desktop.MenuItem{Label: label, Disabled: true})
	}
	switch {
	case s.HasResult && s.Headline > 0:
		items = append(items, desktop.MenuItem{Label: "可以放心释放 " + format.Bytes(s.Headline) + "…", OnSelect: s.Overview})
	case s.Checking:
		items = append(items, desktop.MenuItem{Label: "正在检查…", Disabled: true})
	case s.HasResult:
		items = append(items, desktop.MenuItem{Label: "没有需要清理的内容", Disabled: true})
	default:
		items = append(items, desktop.MenuItem{Label: "查看检查结果…", OnSelect: s.Overview})
	}
	auto := "自动清理：未开启"
	if s.RulesOn > 0 {
		auto = fmt.Sprintf("自动清理：已开启 %d 条", s.RulesOn)
	}
	quit := "退出 Spruce"
	if s.Cleaning {
		quit = "退出 Spruce（正在进行的清理会在当前项目完成后停止）"
	}
	return append(items, sep,
		desktop.MenuItem{Label: "打开 Spruce", OnSelect: s.Open},
		desktop.MenuItem{Label: "立即检查", Disabled: s.Checking || s.Cleaning, OnSelect: s.Check},
		sep,
		desktop.MenuItem{Label: auto, OnSelect: s.Auto},
		sep,
		desktop.MenuItem{Label: quit, OnSelect: s.Quit},
	)
}

// trayTooltip is what shows as the pointer rests on the icon: the free space, or that it is low.
func trayTooltip(d *app.DiskState) string {
	switch {
	case d == nil:
		return "Spruce"
	case d.Low:
		return "Spruce · 空间不足，只剩 " + format.Bytes(d.Free)
	}
	return "Spruce · 可用 " + format.Bytes(d.Free)
}
