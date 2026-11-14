package cleanflow

import "github.com/yldm-tech/spruce/internal/engine/cleanup"

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

// DefaultSelection checks what the scan says starts checked: every item of a category marked selected, and each item marked selected on its own (a category may start checked in part).
func DefaultSelection(categories []cleanup.Category) Selection {
	sel := Selection{}
	for _, c := range categories {
		var picked []string
		for _, it := range c.Items {
			if c.Selected || it.Selected {
				picked = append(picked, it.ID)
			}
		}
		if len(picked) > 0 {
			sel[c.ID] = set(picked)
		}
	}
	return sel
}

// CategoryState says whether none, some or all of a category's items are checked; an empty category is CheckNone.
func CategoryState(c cleanup.Category, sel Selection) CheckState {
	n := sel.Count(c.ID)
	if n == 0 || len(c.Items) == 0 {
		return CheckNone
	}
	if n >= len(c.Items) {
		return CheckAll
	}
	return CheckSome
}

// itemIDs returns the IDs of c's items.
func itemIDs(c cleanup.Category) []string {
	ids := make([]string, len(c.Items))
	for i, it := range c.Items {
		ids[i] = it.ID
	}
	return ids
}

// SetCategory checks or unchecks every item of a category.
func SetCategory(sel Selection, c cleanup.Category, on bool) Selection {
	next := sel.clone()
	if on && len(c.Items) > 0 {
		next[c.ID] = set(itemIDs(c))
	} else {
		delete(next, c.ID)
	}
	return next
}

// SetItem checks or unchecks one item of a category.
func SetItem(sel Selection, c cleanup.Category, itemID string, on bool) Selection {
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

// SelectAll checks every item of every category.
func SelectAll(categories []cleanup.Category) Selection {
	sel := Selection{}
	for _, c := range categories {
		if len(c.Items) > 0 {
			sel[c.ID] = set(itemIDs(c))
		}
	}
	return sel
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

// ToCleanRequest turns a selection into the request Cleanup.Clean takes: every category with a checked item, and the item IDs only when some category is checked in part (the Go side then deletes exactly those items). IDs no longer in the categories are left out.
func ToCleanRequest(categories []cleanup.Category, sel Selection) cleanup.CleanRequest {
	req := cleanup.CleanRequest{CategoryIDs: []string{}}
	var itemIDs []string
	partial := false
	for _, c := range categories {
		ids := sel[c.ID]
		if len(ids) == 0 {
			continue
		}
		picked := 0
		for _, it := range c.Items {
			if _, ok := ids[it.ID]; ok {
				itemIDs = append(itemIDs, it.ID)
				picked++
			}
		}
		if picked == 0 {
			continue
		}
		req.CategoryIDs = append(req.CategoryIDs, c.ID)
		if picked < len(c.Items) {
			partial = true
		}
	}
	if partial {
		req.ItemIDs = itemIDs
	}
	return req
}
