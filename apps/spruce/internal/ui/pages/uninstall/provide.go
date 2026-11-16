package uninstall

import (
	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/store"
)

// stateKey provides the page's State, made once by Provide.
var stateKey = core.NewKey[*State]("spruce.uninstall.state", nil)

// Provide gives the page its Deps in cx, the app's own scope, which outlives every showing of the page, and makes there the State the page keeps from one visit to the next: the list and its details, the search, the order, the picked app and its checked leftovers. The shell's stores must be provided in cx first.
func Provide(cx *core.Scope, d Deps) {
	DepsKey.Provide(cx, d)
	stateKey.Provide(cx, NewState(cx, d, store.Use(cx)))
}
