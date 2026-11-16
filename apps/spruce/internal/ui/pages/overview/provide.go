package overview

import (
	"maps"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/store"
)

// kept is what 概览 keeps in the app's scope from one visit to the next, besides what is checked (cleanflow.Kept): the cards open, how many items each card's last clean could not handle, and whether every card shows.
type kept struct {
	open   *core.Signal[map[string]bool]
	failed *core.Signal[map[string]int]
	more   *core.Signal[bool]
}

// keptKey provides 概览's kept.
var keptKey = core.NewKey[*kept]("spruce.overview.kept", nil)

// Provide gives the page its Deps in cx, the app's own scope, which outlives every showing of the page: what the page keeps from one visit to the next is made here too.
func Provide(cx *core.Scope, d Deps) {
	DepsKey.Provide(cx, d)
	k := &kept{open: core.NewSignalFunc(cx, map[string]bool{}, nil), failed: core.NewSignalFunc(cx, map[string]int{}, nil),
		more: core.NewSignal(cx, false)}
	// What a card's last clean could not handle is told of the check it cleaned: a newer check lists those items again or finds them gone, so the counts go with it.
	stores := store.Use(cx)
	ov := stores.Overview
	checked := core.NewMemoFunc(cx, func() time.Time {
		if r := ov.State().Result; r != nil {
			return r.CheckedAt
		}
		return time.Time{}
	}, time.Time.Equal)
	cx.Effect(func() {
		checked.Get()
		cx.Untrack(func() {
			if len(k.failed.GetUntracked()) > 0 {
				k.failed.Set(map[string]int{})
			}
		})
	})
	// Free space is read again as the window comes back and after anything changed the Trash (a clean, a move in 空间透视, an uninstall, 撤销), whichever page shows: 概览 and the menu bar show it as it is, not as the controller's 15-minute reading left it.
	n := nav.Use(cx)
	shown, trash := core.Untracked(cx, n.Shown), core.Untracked(cx, stores.Trash.Generation)
	cx.Effect(func() {
		s, g := n.Shown(), stores.Trash.Generation()
		if s == shown && g == trash {
			return
		}
		shown, trash = s, g
		cx.Untrack(ov.ReadDisk)
	})
	keptKey.Provide(cx, k)
}

// isOpen reports whether the card id shows its items; reading it subscribes.
func (k *kept) isOpen(id string) bool { return k.open.Get()[id] }

// setOpen opens or closes the card id.
func (k *kept) setOpen(id string, on bool) { put(k.open, id, on) }

// failedOf is how many items the last clean of the card id could not handle; reading it subscribes.
func (k *kept) failedOf(id string) int { return k.failed.Get()[id] }

// setFailed records how many items the last clean of the card id could not handle.
func (k *kept) setFailed(id string, n int) { put(k.failed, id, n) }

// put sets the entry id of the map s holds to v, a zero v removing it, and sets s only when that changes something.
func put[V comparable](s *core.Signal[map[string]V], id string, v V) {
	var zero V
	cur := s.GetUntracked()
	if cur[id] == v {
		return
	}
	next := maps.Clone(cur)
	if v == zero {
		delete(next, id)
	} else {
		next[id] = v
	}
	s.Set(next)
}
