package kit

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// Link is an underlined text link (.link-button on a router link), such as 去设置 or 查看详情. To, when set, is the page it goes to; OnPress runs otherwise. Start aligns it to the start of its column with no left padding, as the cards of 智能扫描 place theirs.
type Link struct {
	Text    func() string
	To      func() nav.Route
	OnPress func()
	Start   bool
	TestID  string
}

// Render implements core.Node.
func (l Link) Render(cx *core.Scope) core.Node {
	text := TextFunc(l.Text, FontBody, Regular, core.CurrentColor)
	text.Style.TextDecoration = core.TextDecorationUnderline
	st := core.Style{Padding: core.Symmetric(2, 4), Color: ColorTextSecondary, Cursor: core.CursorPointer,
		Hover:        &core.Style{Color: ColorTextLinkHover},
		FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: ColorText}}
	if l.Start {
		st.AlignSelf, st.FlexShrink, st.Padding.Left = core.AlignStart, 0, 0
	}
	return core.View{Role: core.RoleLink, LabelFunc: l.Text, TestID: l.TestID, Style: st,
		OnPress: func(core.PressEvent) {
			switch {
			case l.To != nil:
				nav.Use(cx).Go(l.To())
			case l.OnPress != nil:
				l.OnPress()
			}
		},
		Children: []core.Node{text}}
}
