package maintenance

import "github.com/nsgo-dev/nsgo/core"

// choicesKey provides the user's choices on the cards (State's overrides), kept from one visit to the next.
var choicesKey = core.NewKey[*core.Signal[map[string]bool]]("spruce.maintenance.choices", nil)

// Provide gives the page its Deps in cx, the app's own scope, which outlives every showing of the page: what the page keeps from one visit to the next is made here too, the user's choices on the cards.
func Provide(cx *core.Scope, d Deps) {
	DepsKey.Provide(cx, d)
	choicesKey.Provide(cx, core.NewSignalFunc(cx, map[string]bool{}, nil))
}
