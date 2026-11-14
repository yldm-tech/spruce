package cleanflow

import (
	"fmt"
	"strings"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/format"
)

// maxFailureLines is how many failed items the done card names one by one.
const maxFailureLines = 8

// CleanOutcome is how a clean ended: its result, or the sentence of a clean that could not start.
type CleanOutcome struct {
	// Result is nil when the clean could not start; Err then says why.
	Result *cleanup.CleanResult
	Err    string
}

// Summary is what the done card of a clean shows.
type Summary struct {
	Title, Big string
	Lines      []string
	Succeeded  bool
}

// CleanSummary turns a clean's outcome into the done card: the space given back, a line per category, the items left in place, and what the Trash means.
func CleanSummary(o CleanOutcome) Summary {
	if o.Result == nil {
		return Summary{Title: "清理没有进行", Big: format.Bytes(0), Lines: []string{o.Err}}
	}
	r := *o.Result
	bin := format.TrashName()
	lines := []string{}
	for _, c := range r.Categories {
		var parts []string
		if c.Removed > 0 {
			if c.Trashed > 0 {
				parts = append(parts, fmt.Sprintf("%d 项已移到%s（%s）", c.Removed, bin, format.Bytes(c.Trashed)))
			} else {
				parts = append(parts, fmt.Sprintf("已清理 %d 项，释放 %s", c.Removed, format.Bytes(c.Freed)))
			}
		}
		if c.Failed > 0 {
			parts = append(parts, fmt.Sprintf("%d 项未能处理", c.Failed))
		}
		if len(parts) > 0 {
			lines = append(lines, c.Title+"："+strings.Join(parts, "，"))
		}
	}
	// A failure without a path stands for a whole category (智能扫描 reports a part it could not hand over that way); its reason names it.
	for i, f := range r.Failed {
		if i == maxFailureLines {
			break
		}
		if f.Path == "" {
			lines = append(lines, f.Reason)
		} else {
			lines = append(lines, format.BaseName(f.Path)+"："+f.Reason)
		}
	}
	if len(r.Failed) > maxFailureLines {
		lines = append(lines, fmt.Sprintf("另有 %d 项未能处理", len(r.Failed)-maxFailureLines))
	}
	if r.Skipped > 0 {
		lines = append(lines, fmt.Sprintf("%d 项在扫描后已不存在，已跳过", r.Skipped))
	}
	if r.Trashed > 0 {
		lines = append(lines, fmt.Sprintf("移到%s的项目在清空%s后才会释放空间", bin, bin))
	}
	if r.Interrupted {
		lines = append(lines, "清理在中途停止，其余项目没有改动")
	}
	title := "清理完成"
	switch {
	case r.Interrupted:
		title = "清理已中断"
	case r.Removed == 0:
		title = "清理未完成"
	}
	return Summary{Title: title, Big: format.Bytes(r.Freed + r.Trashed), Lines: lines, Succeeded: r.Removed > 0}
}

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
