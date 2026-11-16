package cleanflow

import (
	"slices"

	"spruce/apps/spruce/internal/engine/cleanup"
)

// PlanLine is one category's share of a clean: how many of its items are checked and their size.
type PlanLine struct {
	ID, Title string
	Count     int
	Bytes     int64
	// Command is the tool command that cleans the category (Category.Command), for the commands group.
	Command string
}

// Plan is a clean split by what happens to the checked items, one line per category in list order.
type Plan struct {
	Permanent, Trash, Commands []PlanLine
}

// Count returns how many items the plan cleans.
func (p Plan) Count() int {
	n := 0
	for _, group := range [][]PlanLine{p.Permanent, p.Trash, p.Commands} {
		for _, l := range group {
			n += l.Count
		}
	}
	return n
}

// CleanPlan splits the checked items by what happens to them: cleaned by the tool's own command, deleted for good, or moved to the Trash.
func CleanPlan(categories []cleanup.Category, sel Selection) Plan {
	var p Plan
	for _, c := range categories {
		ids := sel[c.ID]
		if len(ids) == 0 {
			continue
		}
		line := PlanLine{ID: c.ID, Title: c.Title}
		for _, it := range c.Items {
			if _, ok := ids[it.ID]; ok {
				line.Count++
				line.Bytes += it.Size
			}
		}
		if line.Count == 0 {
			continue
		}
		switch {
		case c.Command != "":
			line.Command = c.Command
			p.Commands = append(p.Commands, line)
		case c.Deletion == cleanup.DeletePermanent:
			p.Permanent = append(p.Permanent, line)
		default:
			p.Trash = append(p.Trash, line)
		}
	}
	return p
}

// SetInUseAside takes the items another program held open out of a clean's failures: they are not errors the user must act on but files to try again later, counted on their own (inUse). The categories' failure counts drop to match. r is left as it is; without in-use items it comes back unchanged.
func SetInUseAside(r cleanup.CleanResult) (out cleanup.CleanResult, inUse int) {
	byCategory := map[string]int{}
	for _, f := range r.Failed {
		if f.InUse {
			inUse++
			byCategory[f.CategoryID]++
		}
	}
	if inUse == 0 {
		return r, 0
	}
	out = r
	out.Failed = slices.DeleteFunc(slices.Clone(r.Failed), func(f cleanup.Failure) bool { return f.InUse })
	out.Categories = slices.Clone(r.Categories)
	for i := range out.Categories {
		c := &out.Categories[i]
		c.Failed = max(0, c.Failed-byCategory[c.CategoryID])
	}
	return out, inUse
}
