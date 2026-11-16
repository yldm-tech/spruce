package cleanflow

import "github.com/nsgo-dev/nsgo/core"

// Provide makes, in cx (the app's own scope, which outlives every showing of a page), what 概览's cards and the drill-down views keep from one visit to the next; the shell calls it with the pages' Provide. Nothing is kept yet.
func Provide(cx *core.Scope) {}
