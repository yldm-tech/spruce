package advice

import (
	"path/filepath"
	"strings"

	"spruce/apps/spruce/internal/engine/cleanup"
)

// Score is bytes × the action's safety weight: trash 1.0, delete 0.9, mixed 0.85, command 0.75. Pure.
func Score(bytes int64, a Action) float64 {
	w := 0.0
	switch a {
	case ActionTrash:
		w = 1.0
	case ActionDelete:
		w = 0.9
	case ActionMixed:
		w = 0.85
	case ActionCommand:
		w = 0.75
	}
	return float64(bytes) * w
}

// ActionOf is the action of the items refs names in res (a card's current selection): ActionCommand when they all belong to categories cleaned by a command, ActionDelete when they are all deleted for good without one, ActionTrash when they all go to the Trash, ActionMixed for any mix. Refs res does not have are left out; "" when none is left. Pure.
func ActionOf(res cleanup.ScanResult, refs []ItemRef) Action {
	cats := make(map[string]*cleanup.Category, len(res.Categories))
	for i := range res.Categories {
		cats[res.Categories[i].ID] = &res.Categories[i]
	}
	seen := map[Action]bool{}
	for _, r := range refs {
		c := cats[r.CategoryID]
		if c == nil || !hasItem(c, r.ItemID) {
			continue
		}
		switch {
		case c.Command != "":
			seen[ActionCommand] = true
		case c.Deletion == cleanup.DeleteTrash:
			seen[ActionTrash] = true
		default:
			seen[ActionDelete] = true
		}
	}
	switch {
	case len(seen) > 1:
		return ActionMixed
	case seen[ActionTrash]:
		return ActionTrash
	case seen[ActionDelete]:
		return ActionDelete
	case seen[ActionCommand]:
		return ActionCommand
	}
	return ""
}

func hasItem(c *cleanup.Category, id string) bool {
	for _, it := range c.Items {
		if it.ID == id {
			return true
		}
	}
	return false
}

// Covering returns the ID of the first card (in recs order) with an item whose Path equals path, lies inside it, or that path lies inside; "" when none. 空间变化 rows use it for 去清理. Pure.
func Covering(recs []Recommendation, res cleanup.ScanResult, path string) string {
	if path == "" {
		return ""
	}
	path = filepath.Clean(path)
	paths := make(map[ItemRef]string, res.Items)
	for _, c := range res.Categories {
		for _, it := range c.Items {
			paths[ItemRef{CategoryID: c.ID, ItemID: it.ID}] = it.Path
		}
	}
	for _, r := range recs {
		for _, ref := range r.Items {
			p, ok := paths[ref]
			if ok && p != "" && (within(p, path) || within(path, p)) {
				return r.ID
			}
		}
	}
	return ""
}

// within reports whether p is root or lies inside it.
func within(p, root string) bool {
	rel, err := filepath.Rel(root, filepath.Clean(p))
	return err == nil && !filepath.IsAbs(rel) && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator))
}
