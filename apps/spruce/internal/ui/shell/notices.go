package shell

import (
	"context"
	"strings"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/rules"
	"spruce/apps/spruce/internal/ui/async"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/store"
)

// summaryRules is how many rules the weekly summary names before it ends with 等.
const summaryRules = 2

// NoticeText is the title and body of the system notification for n: low space, with what 概览 can free when it found something, or the weekly summary of the automatic rules.
func NoticeText(n app.Notice) (title, body string) {
	if n.Kind == app.NoticeAutoSummary {
		names := make([]string, 0, summaryRules)
		for i, r := range n.Rules {
			if i == summaryRules {
				break
			}
			names = append(names, ruleTitle(r.Rule)+" "+format.Bytes(r.Bytes))
		}
		moved := strings.Join(names, "、") + " "
		if len(n.Rules) > summaryRules {
			moved += "等"
		}
		return "本周自动清理了 " + format.Bytes(n.Trashed),
			moved + "已移到" + format.TrashName() + "。点按查看，需要时可以放回原处。"
	}
	title = "磁盘空间不多了"
	if n.Recommended > 0 {
		return title, n.Volume + " 只剩 " + format.Bytes(n.Free) + "。Spruce 找到 " + format.Bytes(n.Recommended) + " 可以放心释放，点按查看。"
	}
	return title, n.Volume + " 只剩 " + format.Bytes(n.Free) + "，点按看看是什么占用了空间。"
}

// ruleTitle is the label of the automatic rule id, as 设置 → 自动清理 shows it.
func ruleTitle(id rules.ID) string {
	for _, s := range rules.Catalog() {
		if s.ID == id {
			return s.Title
		}
	}
	return string(id)
}

// postNotices posts the controller's notifications until ctx ends. Only when the user already let Spruce show notifications: background work never asks, so a notification without the permission is dropped. One of an ID replaces the one before.
func postNotices(cx *core.Scope, ctx context.Context, o *store.Overview) {
	ch, stop := o.Notices()
	if ch == nil {
		stop()
		return
	}
	async.Subscribe(cx, ctx, ch, stop, func(n app.Notice) {
		title, body := NoticeText(n)
		cx.NotificationPermission(func(p core.NotificationPermission) {
			if p == core.NotificationsGranted {
				_ = cx.ShowNotification(core.Notification{ID: n.ID, Title: title, Body: body}, nil)
			}
		})
	})
}
