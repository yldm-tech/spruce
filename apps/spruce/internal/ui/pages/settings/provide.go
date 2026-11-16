package settings

import (
	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/nav"
)

// tabKey provides the tab 设置 showed last, which the next visit opens on.
var tabKey = core.NewKey[*core.Signal[nav.SettingsTab]]("spruce.settings.tab", nil)

// Provide gives the page its Deps in cx, the app's own scope, which outlives every showing of the page: what the page keeps from one visit to the next is made here too, the tab it showed last.
func Provide(cx *core.Scope, d Deps) {
	DepsKey.Provide(cx, d)
	tabKey.Provide(cx, core.NewSignal(cx, nav.SettingsGeneral))
}
