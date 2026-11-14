package kit_test

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/ui/kit"
)

// TestPageBackgroundKeepsWideContentInTheWindow lays out the shell's row (a 228-point sidebar and a main area that may shrink) over the background with a page whose content cannot be narrower than 1100 points, as 空间透视's map and list are with a long file name in the list. The main area must keep the window's remaining width and clip the page, not grow past the window's right edge.
func TestPageBackgroundKeepsWideContentInTheWindow(t *testing.T) {
	app := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return kit.PageBackground{Children: []core.Node{
			core.View{TestID: "sidebar", Style: core.Style{Width: core.Pt(228), FlexShrink: 0}},
			core.View{TestID: "main", Style: core.Style{FlexGrow: 999, FlexShrink: 1, FlexBasis: core.Pt(kit.MainMinWidth), MinWidth: core.Pt(0), Overflow: core.Hidden},
				Children: []core.Node{core.View{Style: core.Style{MinWidth: core.Pt(1100), Height: core.Pt(40)}}}},
		}}
	}, nsgotest.Size(1280, 820))
	if got := app.Find("id:main").Frame.Size.Width; got != 1280-228 {
		t.Errorf("main area is %v points wide, want %v", got, 1280-228)
	}
}
