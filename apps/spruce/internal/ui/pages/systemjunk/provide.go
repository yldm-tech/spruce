package systemjunk

import "github.com/nsgo-dev/nsgo/core"

// Provide gives the page its Deps in cx, the app's own scope, which outlives every showing of the page: what the page keeps from one visit to the next is made here too.
func Provide(cx *core.Scope, d Deps) { DepsKey.Provide(cx, d) }
