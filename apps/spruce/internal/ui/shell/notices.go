package shell

import (
	"context"
	"strings"
	"time"

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

// CheckNoticeText is the title and body of the notification at the end of a check the menu bar icon started while the window was out of sight: why it did not finish (failed, "" when it did), else what it found to free (headline), or that there is nothing to clean.
func CheckNoticeText(failed string, headline int64) (title, body string) {
	switch {
	case failed != "":
		return "检查没有完成", failed
	case headline > 0:
		return "检查完成", "可以放心释放 " + format.Bytes(headline) + "，点按查看。"
	}
	return "检查完成", "没有需要清理的内容。"
}

// trayCheck returns what the menu bar icon's 立即检查 runs: a foreground check, which, started while the window is out of sight (away), ends with a notification (CheckNoticeText), as the window would have shown how it went; a refused check says why at once. It asks for the permission as the user starts it, the moment the system's prompt makes sense; without it nothing shows.
func trayCheck(cx *core.Scope, o *store.Overview, away func() bool) func() {
	// watching is set from a check started while away until it ends; seen once it ran (the phase went to checking), or its result came (checkedAt moved on).
	var watching, seen bool
	var checkedAt time.Time
	notify := func(failed string, headline int64) {
		title, body := CheckNoticeText(failed, headline)
		_ = cx.ShowNotification(core.Notification{ID: noticeCheck, Title: title, Body: body}, nil)
	}
	cx.Effect(func() {
		st := o.State()
		if !watching {
			return
		}
		if st.Phase == app.OverviewChecking || st.Result != nil && !st.Result.CheckedAt.Equal(checkedAt) {
			seen = true
		}
		if st.Phase == app.OverviewChecking || !seen {
			return
		}
		watching = false
		var headline int64
		if st.Result != nil {
			headline = st.Result.Headline
		}
		cx.Untrack(func() { notify(st.Error, headline) })
	})
	return func() {
		watching, seen = false, false
		if away() {
			st := core.Untracked(cx, o.State)
			watching, checkedAt = true, time.Time{}
			if st.Result != nil {
				checkedAt = st.Result.CheckedAt
			}
			cx.RequestNotificationPermission(func(core.NotificationPermission) {})
		}
		o.CheckNow(func(err error) {
			if err != nil && watching {
				watching = false
				notify(err.Error(), 0)
			}
		})
	}
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
