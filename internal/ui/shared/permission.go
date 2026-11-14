package shared

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// NeedsFullDiskAccess reports whether a page should offer the Full Disk Access guide: "denied" and "unknown" both mean a task or a scan may be skipped.
func NeedsFullDiskAccess(st maintenance.PermissionState) bool {
	return st == maintenance.PermissionDenied || st == maintenance.PermissionUnknown
}

// PermissionAction is the Full Disk Access prompt: Text, then 去授权, which opens System Settings at Full Disk Access and shows 等待授权… while the guide waits there, and an error line when System Settings could not be opened. Start aligns it to the start of its container (inside a card) rather than centering it (on a done card). OnGuided receives the state the guide ended with.
type PermissionAction struct {
	Text     string
	Start    bool
	OnGuided func(maintenance.PermissionState)
	TestID   string
}

// Render implements core.Node.
func (p PermissionAction) Render(cx *core.Scope) core.Node {
	perm := store.Use(cx).Permission
	failed := core.NewSignal(cx, false)
	justify := core.JustifyCenter
	if p.Start {
		justify = core.JustifyStart
	}
	return core.View{TestID: p.TestID,
		Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, JustifyContent: justify, RowGap: 8, ColumnGap: 12,
			Margin: core.Edges{Top: 6}},
		Children: []core.Node{
			core.Text{Content: p.Text, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: kit.ColorTextMuted}},
			kit.Button{Variant: kit.ButtonAccent, TestID: "grant-full-disk-access",
				TextFunc: func() string {
					if perm.Guiding() {
						return "等待授权…"
					}
					return "去授权"
				},
				Disabled: perm.Guiding,
				OnPress: func() {
					failed.Set(false)
					perm.Guide(func(st maintenance.PermissionState, err error) {
						failed.Set(err != nil)
						if p.OnGuided != nil && err == nil {
							p.OnGuided(st)
						}
					})
				}},
			kit.ErrorLine{Text: func() string {
				if failed.Get() {
					return "无法打开系统设置"
				}
				return ""
			}},
		}}
}
