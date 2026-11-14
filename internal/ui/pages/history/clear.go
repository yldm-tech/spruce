package history

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// ClearDialog asks before clearing the history, which cannot be undone, and clears on 清空 through the history store. 操作历史 and 设置 both open it. An error keeps it open with the reason; it closes (OnClose) after the history was cleared or on 取消.
type ClearDialog struct {
	Open func() bool
	// Count is how many runs there are, nil when the caller does not know.
	Count       func() int
	OnClose     func()
	ReturnFocus *core.Ref
}

// Render implements core.Node.
func (d ClearDialog) Render(cx *core.Scope) core.Node {
	hist := store.Use(cx).History
	busy := core.NewSignal(cx, false)
	failure := core.NewSignal(cx, "")
	closeDialog := func() {
		failure.Set("")
		if d.OnClose != nil {
			d.OnClose()
		}
	}
	what := func() string {
		if d.Count == nil {
			return "全部操作记录"
		}
		return "全部 " + strconv.Itoa(d.Count()) + " 条操作记录"
	}
	message := core.View{Children: []core.Node{
		core.Text{ContentFunc: func() string { return "将删除" + what() + "，无法恢复。只删除记录，不会撤销或改动已经做过的清理和维护。" }, TestID: "clear-message"},
		core.Show{When: func() bool { return failure.Get() != "" }, Then: func(*core.Scope) core.Node {
			return core.Text{ContentFunc: failure.Get, Role: core.RoleAlert, LiveRegion: core.LiveRegionPolite, TestID: "clear-error",
				Style: core.Style{Margin: core.Edges{Top: 8}, Color: kit.ColorWarning}}
		}},
	}}
	return kit.ConfirmDialog{Open: d.Open, Title: "清空操作历史？", Message: message, ConfirmLabel: "清空", Danger: true, Busy: busy.Get,
		ReturnFocus: d.ReturnFocus, TestID: "clear-dialog", OnCancel: closeDialog,
		OnConfirm: func() {
			busy.Set(true)
			failure.Set("")
			hist.Clear(func(err error) {
				busy.Set(false)
				if err != nil {
					failure.Set(err.Error())
					return
				}
				closeDialog()
			})
		}}
}
