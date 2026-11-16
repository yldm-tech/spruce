package kit_test

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/ui/kit"
)

// A right click on a row shows its menu where the pointer is; 拷贝路径 fills the clipboard and the reveal item, named after the file manager it is given, reveals the row.
func TestRowMenu(t *testing.T) {
	const path = "/Users/demo/Library/Caches/com.example"
	reveals := 0
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Style: core.Style{Width: core.Pt(400), Padding: core.All(20)}, Children: []core.Node{
			core.View{TestID: "row", Style: core.Style{Height: core.Pt(40)},
				OnContextMenu: kit.RowMenu(cx, func() []desktop.MenuItem {
					return []desktop.MenuItem{kit.RevealItem("资源管理器", func() { reveals++ }), kit.CopyPathItem(cx, path)}
				})},
			core.View{TestID: "empty", Style: core.Style{Height: core.Pt(40)},
				OnContextMenu: kit.RowMenu(cx, func() []desktop.MenuItem { return nil })},
		}}
	}, nsgotest.Size(440, 200), macDesktop)

	a.RightClick("id:row")
	var labels []string
	for _, it := range a.PopupMenu {
		labels = append(labels, it.Label)
	}
	if len(labels) != 2 || labels[0] != "在资源管理器中显示" || labels[1] != "拷贝路径" {
		t.Fatalf("menu %q", labels)
	}
	if a.PopupAt.Y < 20 || a.PopupAt.Y > 60 {
		t.Errorf("menu at %v, not at the row", a.PopupAt)
	}
	a.ChoosePopup("拷贝路径")
	if a.Clipboard != path {
		t.Errorf("clipboard %q", a.Clipboard)
	}
	a.RightClick("id:row")
	a.ChoosePopup("在资源管理器中显示")
	if reveals != 1 {
		t.Errorf("reveals %d", reveals)
	}

	a.RightClick("id:empty")
	if a.PopupMenu != nil {
		t.Error("a menu without items shows")
	}
}
