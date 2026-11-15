package cleanflow

import (
	"github.com/yldm-tech/spruce/internal/engine/advice"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// Selection is which items are checked: category ID to the set of its checked item IDs. A category without an entry has none checked. A Selection is a value: the functions here return a new one and never change the one they are given, so a signal can hold it.
type Selection map[string]map[string]struct{}

// NothingSelected is the empty selection.
var NothingSelected = Selection{}

// Has reports whether item itemID of category categoryID is checked.
func (s Selection) Has(categoryID, itemID string) bool {
	_, ok := s[categoryID][itemID]
	return ok
}

// Count returns how many items of category categoryID are checked.
func (s Selection) Count(categoryID string) int { return len(s[categoryID]) }

// CheckState is how much of a category is checked, for its tri-state checkbox.
type CheckState uint8

// The check states: none, some or all of a category's items.
const (
	CheckNone CheckState = iota
	CheckSome
	CheckAll
)

// set returns ids as a set.
func set(ids []string) map[string]struct{} {
	m := make(map[string]struct{}, len(ids))
	for _, id := range ids {
		m[id] = struct{}{}
	}
	return m
}

// clone returns a shallow copy of s: the category entries are shared, which is safe because no function changes an entry in place.
func (s Selection) clone() Selection {
	out := make(Selection, len(s)+1)
	for k, v := range s {
		out[k] = v
	}
	return out
}

// DefaultSelection checks what the scan says starts checked: every item of a category marked selected, and each item marked selected on its own (a category may start checked in part). A held item (Item.Hold) never starts checked.
func DefaultSelection(categories []cleanup.Category) Selection {
	sel := Selection{}
	for _, c := range categories {
		var picked []string
		for _, it := range c.Items {
			if (c.Selected || it.Selected) && it.Hold == "" {
				picked = append(picked, it.ID)
			}
		}
		if len(picked) > 0 {
			sel[c.ID] = set(picked)
		}
	}
	return sel
}

// CategoryState says whether none, some or all of a category's items that may be checked are checked; a category with none that may be (empty, or every item held) is CheckNone.
func CategoryState(c cleanup.Category, sel Selection) CheckState {
	n, all := sel.Count(c.ID), Selectable(c)
	if n == 0 || all == 0 {
		return CheckNone
	}
	if n >= all {
		return CheckAll
	}
	return CheckSome
}

// Selectable counts the items of c that may be checked: every one but those held (Item.Hold), which the cleaners refuse.
func Selectable(c cleanup.Category) int {
	n := 0
	for _, it := range c.Items {
		if it.Hold == "" {
			n++
		}
	}
	return n
}

// itemIDs returns the IDs of c's items that may be checked.
func itemIDs(c cleanup.Category) []string {
	ids := make([]string, 0, len(c.Items))
	for _, it := range c.Items {
		if it.Hold == "" {
			ids = append(ids, it.ID)
		}
	}
	return ids
}

// SetCategory checks or unchecks every item of a category that may be checked.
func SetCategory(sel Selection, c cleanup.Category, on bool) Selection {
	next := sel.clone()
	if ids := itemIDs(c); on && len(ids) > 0 {
		next[c.ID] = set(ids)
	} else {
		delete(next, c.ID)
	}
	return next
}

// SetItem checks or unchecks one item of a category; a held item stays unchecked.
func SetItem(sel Selection, c cleanup.Category, itemID string, on bool) Selection {
	if on {
		for _, it := range c.Items {
			if it.ID == itemID && it.Hold != "" {
				return sel
			}
		}
	}
	items := make(map[string]struct{}, len(sel[c.ID])+1)
	for id := range sel[c.ID] {
		items[id] = struct{}{}
	}
	if on {
		items[itemID] = struct{}{}
	} else {
		delete(items, itemID)
	}
	next := sel.clone()
	if len(items) == 0 {
		delete(next, c.ID)
	} else {
		next[c.ID] = items
	}
	return next
}

// SelectAll checks every item of every category that may be checked.
func SelectAll(categories []cleanup.Category) Selection {
	sel := Selection{}
	for _, c := range categories {
		if ids := itemIDs(c); len(ids) > 0 {
			sel[c.ID] = set(ids)
		}
	}
	return sel
}

// Reconcile is sel over a newer scan of categories: the checks of items still there and not held stay, every other one goes. Categories not among categories keep nothing.
func Reconcile(sel Selection, categories []cleanup.Category) Selection {
	out := Selection{}
	for _, c := range categories {
		ids := sel[c.ID]
		if len(ids) == 0 {
			continue
		}
		var kept []string
		for _, it := range c.Items {
			if _, ok := ids[it.ID]; ok && it.Hold == "" {
				kept = append(kept, it.ID)
			}
		}
		if len(kept) > 0 {
			out[c.ID] = set(kept)
		}
	}
	return out
}

// Refs are the checked items of categories, in their order, as advice names items of a merged scan.
func Refs(categories []cleanup.Category, sel Selection) []advice.ItemRef {
	var out []advice.ItemRef
	for _, c := range categories {
		ids := sel[c.ID]
		if len(ids) == 0 {
			continue
		}
		for _, it := range c.Items {
			if _, ok := ids[it.ID]; ok {
				out = append(out, advice.ItemRef{CategoryID: c.ID, ItemID: it.ID})
			}
		}
	}
	return out
}

// ItemsRequest is the request for the checked items of categories with every item named, so a clean acts on exactly what the view showed when the user asked, whatever a newer check found since.
func ItemsRequest(categories []cleanup.Category, sel Selection) cleanup.CleanRequest {
	req := cleanup.CleanRequest{CategoryIDs: []string{}}
	for _, ref := range Refs(categories, sel) {
		if n := len(req.CategoryIDs); n == 0 || req.CategoryIDs[n-1] != ref.CategoryID {
			req.CategoryIDs = append(req.CategoryIDs, ref.CategoryID)
		}
		req.ItemIDs = append(req.ItemIDs, ref.ItemID)
	}
	return req
}

// WithinGroup applies next, a selection a CategoryList over group made, to group's categories only, keeping the checks of every other category: a list that shows part of a scan has 全选 and 全不选 act on its own part.
func WithinGroup(all Selection, group []cleanup.Category, next Selection) Selection {
	ids := make(map[string]bool, len(group))
	for _, c := range group {
		ids[c.ID] = true
	}
	out := Selection{}
	for id, items := range all {
		if !ids[id] {
			out[id] = items
		}
	}
	for id, items := range next {
		if ids[id] {
			out[id] = items
		}
	}
	return out
}

// Totals are the checked items' count and size.
type Totals struct {
	Count int
	Bytes int64
}

// SelectedTotals sums the checked items' count and size.
func SelectedTotals(categories []cleanup.Category, sel Selection) Totals {
	var t Totals
	for _, c := range categories {
		ids := sel[c.ID]
		if len(ids) == 0 {
			continue
		}
		for _, it := range c.Items {
			if _, ok := ids[it.ID]; ok {
				t.Count++
				t.Bytes += it.Size
			}
		}
	}
	return t
}
