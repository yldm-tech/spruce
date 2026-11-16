package cleanflow

import (
	"maps"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/store"
)

// Kept is what 概览's cards and the drill-down views keep in the app's scope, so leaving a page loses none of it: one Choice over the latest check, which both read and change, so an item unchecked on a card is unchecked in its drill-down view and the other way round, and which follows each newer check (Choice.Reconcile) whether or not a page shows; and which rows the drill-down views' lists have open.
type Kept struct {
	// Choice is what is checked.
	Choice *core.Signal[Choice]
	// open are the rows open in the drill-down views' lists, by category ID or ProjectKey.
	open *core.Signal[map[string]bool]
}

// keptKey provides the app's Kept.
var keptKey = core.NewKey[*Kept]("spruce.cleanflow.kept", nil)

// UseKept returns the Kept that Provide made.
func UseKept(cx *core.Scope) *Kept { return keptKey.Use(cx) }

// Provide makes, in cx (the app's own scope, which outlives every showing of a page), what 概览's cards and the drill-down views keep from one visit to the next (Kept); the shell calls it with the pages' Provide, after it provides the stores.
func Provide(cx *core.Scope) {
	ov := store.Use(cx).Overview
	k := &Kept{Choice: core.NewSignalFunc(cx, Choice{Sel: NothingSelected}, nil), open: core.NewSignalFunc(cx, map[string]bool{}, nil)}
	// The store publishes a copy of the result with every state, a check's progress too; its categories are the same slice until a newer check or a clean replaces them, so the choice follows only those.
	categories := core.NewMemoFunc(cx, func() []cleanup.Category {
		r := ov.State().Result
		switch {
		case r == nil:
			return nil
		case r.Scan.Categories == nil:
			// A result with nothing found is told apart from none.
			return []cleanup.Category{}
		}
		return r.Scan.Categories
	}, sameCategories)
	cx.Effect(func() {
		cats := categories.Get()
		if cats == nil {
			return
		}
		cx.Untrack(func() { k.Choice.Set(k.Choice.GetUntracked().Reconcile(cats)) })
	})
	keptKey.Provide(cx, k)
}

// sameCategories reports whether a and b are the same slice of the same scan, not merely equal ones; nil (no result) is none.
func sameCategories(a, b []cleanup.Category) bool {
	if len(a) != len(b) || (a == nil) != (b == nil) {
		return false
	}
	return len(a) == 0 || &a[0] == &b[0]
}

// ProjectKey is the key of the 开发者 project id among the open rows (Kept.Open), apart from the categories'.
func ProjectKey(id string) string { return "project:" + id }

// Open reports whether the row key (a category ID or a ProjectKey) is open; reading it subscribes.
func (k *Kept) Open(key string) bool { return k.open.Get()[key] }

// SetOpen opens or closes the row key.
func (k *Kept) SetOpen(key string, on bool) {
	if k.open.GetUntracked()[key] == on {
		return
	}
	next := maps.Clone(k.open.GetUntracked())
	if on {
		next[key] = true
	} else {
		delete(next, key)
	}
	k.open.Set(next)
}
