package cleanflow

import (
	"fmt"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/format"
)

// Confirmation is the question asked before a clean.
type Confirmation struct {
	Message string
	// Danger is set when anything is deleted for good.
	Danger bool
}

// CleanConfirmation is the question asked before a clean: how much goes, and whether it can be undone.
func CleanConfirmation(categories []cleanup.Category, sel Selection) Confirmation {
	t := SelectedTotals(categories, sel)
	permanent := 0
	for _, c := range categories {
		if c.Deletion != cleanup.DeletePermanent {
			continue
		}
		ids := sel[c.ID]
		for _, it := range c.Items {
			if _, ok := ids[it.ID]; ok {
				permanent++
			}
		}
	}
	bin := format.TrashName()
	var tail string
	switch permanent {
	case 0:
		tail = "它们会移到" + bin + "，可以随时放回。"
	case t.Count:
		tail = "删除后无法恢复。"
	default:
		tail = fmt.Sprintf("其中 %d 项会永久删除，无法恢复；其余移到%s。", permanent, bin)
	}
	return Confirmation{Message: fmt.Sprintf("将清理 %d 项，共 %s。%s", t.Count, format.Bytes(t.Bytes), tail), Danger: permanent > 0}
}
