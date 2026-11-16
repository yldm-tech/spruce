package history

import (
	"github.com/nsgo-dev/nsgo/core"

	enginehist "spruce/apps/spruce/internal/engine/history"
)

// kept is what 操作历史 keeps from one visit to the next: the filter, the runs shown open, and how many of GoHistory's requests the page has taken, so a request made while it was away is taken as it opens and nothing else resets it.
type kept struct {
	filter *core.Signal[enginehist.Kind]
	open   *core.Signal[map[string]bool]
	asked  int
}

func newKept(cx *core.Scope) *kept {
	return &kept{filter: core.NewSignal[enginehist.Kind](cx, ""), open: core.NewSignalFunc(cx, map[string]bool{}, nil)}
}

// keptKey provides what Provide keeps.
var keptKey = core.NewKey[*kept]("spruce.history.kept", nil)

// Provide gives the page its Deps in cx, the app's own scope, which outlives every showing of the page: what the page keeps from one visit to the next is made here too, the filter and the runs shown open.
func Provide(cx *core.Scope, d Deps) {
	DepsKey.Provide(cx, d)
	keptKey.Provide(cx, newKept(cx))
}
