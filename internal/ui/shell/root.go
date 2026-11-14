package shell

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Root is the window's root view: the sidebar and the page area over the page background. It hides the system's title bar (the sidebar and the page headers drag the window) and reports the window's frame to keepFrame as it moves.
func Root(cx *core.Scope, block *store.Block, keepFrame func(core.WindowFrame)) core.Node {
	desktop.SetTitleBar(cx, desktop.TitleBarHidden, 0)
	navigator := nav.NewNavigator(cx, nav.Start)
	nav.Key.Provide(cx, navigator)
	cx.Effect(func() {
		if f := desktop.Frame(cx); f.Width > 0 && keepFrame != nil {
			keepFrame(f)
		}
	})
	main := core.View{TestID: "main",
		Style: core.Style{FlexGrow: 999, FlexShrink: 1, FlexBasis: core.Pt(kit.MainMinWidth), MinWidth: core.Pt(0), Overflow: core.Hidden,
			Position: core.Relative},
		Children: []core.Node{core.Dynamic(func(cx *core.Scope) core.Node {
			// core.Func mounts the page untracked: what the page reads as it is made does not remake it.
			return core.Func(pages[navigator.Current()])
		})}}
	return kit.PageBackground{Children: []core.Node{Sidebar{Nav: navigator, Block: block}, main}}
}
