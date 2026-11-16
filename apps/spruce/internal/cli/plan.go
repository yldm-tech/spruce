package cli

import (
	"slices"
	"time"

	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
)

// Selection is what clean was asked to take from a check.
type Selection struct {
	// Cards are recommendation IDs: each card's items, commands included.
	Cards []string
	// Categories are merged category IDs: every item of each, held ones aside.
	Categories []string
	// OlderThan keeps only items whose newest change (Item.Newest, else ModTime) is at least this old; 0 keeps every age.
	OlderThan time.Duration
}

// explicit reports whether s names cards or categories; without, clean takes every card that runs no command.
func (s Selection) explicit() bool { return len(s.Cards)+len(s.Categories) > 0 }

// Plan is what a clean would do: the request, and the items by category in scan order.
type Plan struct {
	Request cleanup.CleanRequest `json:"request"`
	Items   int                  `json:"items"`
	Bytes   int64                `json:"bytes"`
	// Trash is the part of Bytes that goes to the Trash; the rest is deleted for good or left to a command.
	Trash      int64          `json:"trash"`
	Categories []PlanCategory `json:"categories"`
	// Held are items the selection names that carry Item.Hold: never cleaned.
	Held []PlanItem `json:"held,omitzero"`
	// Missing are named cards and categories this check found nothing of.
	Missing []string `json:"missing,omitzero"`
}

// PlanCategory is one category of a plan.
type PlanCategory struct {
	ID       string               `json:"id"`
	Title    string               `json:"title"`
	Kind     cleanup.CategoryKind `json:"kind"`
	Deletion cleanup.DeletionMode `json:"deletion"`
	Command  string               `json:"command,omitempty"`
	Bytes    int64                `json:"bytes"`
	Items    []PlanItem           `json:"items"`
}

// PlanItem is one item of a plan.
type PlanItem struct {
	CategoryID string `json:"categoryId"`
	ID         string `json:"id"`
	Path       string `json:"path"`
	Size       int64  `json:"size"`
	Hold       string `json:"hold,omitempty"`
}

// Empty reports whether the plan cleans nothing.
func (p Plan) Empty() bool { return p.Items == 0 }

// buildPlan picks from res what sel names: the cards' items (recs is advice.Derive of res), every unheld item of the categories, or, when sel names neither, every card's items in categories that run no command; then only items at least sel.OlderThan old at now. An item with Hold is never in the request, whatever names it: it is listed in Held. Pure.
func buildPlan(res cleanup.ScanResult, recs []advice.Recommendation, sel Selection, now time.Time) Plan {
	picked := map[advice.ItemRef]bool{}
	byID := map[string]*cleanup.Category{}
	for i := range res.Categories {
		byID[res.Categories[i].ID] = &res.Categories[i]
	}
	var missing []string
	if !sel.explicit() {
		for _, r := range recs {
			if r.Action == advice.ActionCommand {
				continue
			}
			for _, ref := range r.Items {
				if c := byID[ref.CategoryID]; c != nil && c.Command == "" {
					picked[ref] = true
				}
			}
		}
	}
	for _, id := range sel.Cards {
		i := slices.IndexFunc(recs, func(r advice.Recommendation) bool { return r.ID == id })
		if i < 0 {
			missing = appendNew(missing, id)
			continue
		}
		for _, ref := range recs[i].Items {
			picked[ref] = true
		}
	}
	for _, id := range sel.Categories {
		c := byID[id]
		if c == nil || len(c.Items) == 0 {
			missing = appendNew(missing, id)
			continue
		}
		for _, it := range c.Items {
			picked[advice.ItemRef{CategoryID: c.ID, ItemID: it.ID}] = true
		}
	}

	plan := Plan{Request: cleanup.CleanRequest{CategoryIDs: []string{}}, Categories: []PlanCategory{}, Missing: missing}
	cutoff := now.Add(-sel.OlderThan)
	for _, c := range res.Categories {
		pc := PlanCategory{ID: c.ID, Title: c.Title, Kind: c.Kind, Deletion: c.Deletion, Command: c.Command, Items: []PlanItem{}}
		for _, it := range c.Items {
			if !picked[advice.ItemRef{CategoryID: c.ID, ItemID: it.ID}] {
				continue
			}
			if sel.OlderThan > 0 {
				if t := latest(it); t.IsZero() || t.After(cutoff) {
					continue
				}
			}
			pi := PlanItem{CategoryID: c.ID, ID: it.ID, Path: it.Path, Size: it.Size, Hold: it.Hold}
			if it.Hold != "" {
				plan.Held = append(plan.Held, pi)
				continue
			}
			pc.Items = append(pc.Items, pi)
			pc.Bytes += it.Size
		}
		if len(pc.Items) == 0 {
			continue
		}
		plan.Categories = append(plan.Categories, pc)
		plan.Request.CategoryIDs = append(plan.Request.CategoryIDs, c.ID)
		for _, pi := range pc.Items {
			plan.Request.ItemIDs = append(plan.Request.ItemIDs, pi.ID)
		}
		plan.Items += len(pc.Items)
		plan.Bytes += pc.Bytes
		if c.Deletion == cleanup.DeleteTrash && c.Command == "" && c.Kind != cleanup.KindTrash {
			plan.Trash += pc.Bytes
		}
	}
	return plan
}

// latest is when an item last changed: the newest change inside a folder, else its own modification time (a build output item's is its project's last activity).
func latest(it cleanup.Item) time.Time {
	if !it.Newest.IsZero() {
		return it.Newest
	}
	return it.ModTime
}

// appendNew appends s to list unless it is there already.
func appendNew(list []string, s string) []string {
	if slices.Contains(list, s) {
		return list
	}
	return append(list, s)
}
