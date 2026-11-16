package kit

import (
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"
)

// RowMenu returns a view's OnContextMenu that shows the platform's own context menu where the user clicked, its items made as it opens (items), so they follow the row's state then; nil items or none show nothing. cx is the scope of the row, whose items run under it. Rows of files and apps take it with RevealItem and CopyPathItem.
func RowMenu(cx *core.Scope, items func() []desktop.MenuItem) func(core.ContextMenuEvent) {
	return func(e core.ContextMenuEvent) {
		if list := items(); len(list) > 0 {
			desktop.PopupMenu(cx, list, e.PagePosition, nil)
		}
	}
}

// RevealItem is the menu item 在<fileManager>中显示, which calls reveal: pages pass format.FileManagerName(), and a page that keeps the platform in its state (应用卸载) passes the name it shows on the row, so the menu and the row's button agree.
func RevealItem(fileManager string, reveal func()) desktop.MenuItem {
	return desktop.MenuItem{Label: "在" + fileManager + "中显示", OnSelect: reveal}
}

// CopyPathItem is the menu item 拷贝路径, which puts path on the clipboard.
func CopyPathItem(cx *core.Scope, path string) desktop.MenuItem {
	return desktop.MenuItem{Label: "拷贝路径", OnSelect: func() { cx.SetClipboard(path) }}
}
