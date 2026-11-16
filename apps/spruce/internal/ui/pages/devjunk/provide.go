package devjunk

import (
	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/nav"
)

// kept is what the view keeps in the app's scope from one visit to the next, besides what is checked and open (cleanflow.Kept): its tab, and the 项目 filter and order.
type kept struct {
	tab    *core.Signal[nav.DevTab]
	filter *core.Signal[ProjectFilter]
	sort   *core.Signal[ProjectSort]
}

// keptKey provides the view's kept.
var keptKey = core.NewKey[*kept]("spruce.devjunk.kept", nil)

// Provide gives the page its Deps in cx, the app's own scope, which outlives every showing of the page: what the page keeps from one visit to the next is made here too.
func Provide(cx *core.Scope, d Deps) {
	DepsKey.Provide(cx, d)
	keptKey.Provide(cx, &kept{tab: core.NewSignal(cx, nav.DevProjects), filter: core.NewSignal(cx, FilterAll), sort: core.NewSignal(cx, SortSize)})
}
