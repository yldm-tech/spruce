package devjunk

import (
	"strconv"
	"strings"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
	"github.com/yldm-tech/spruce/internal/ui/format"
)

// SplitGroups splits a scan into the page's two sections: the tool caches (every kind but KindProjectArtifact) and the build output found in projects (KindProjectArtifact), each in scan order.
func SplitGroups(categories []cleanup.Category) (tools, projects []cleanup.Category) {
	for _, c := range categories {
		if c.Kind == cleanup.KindProjectArtifact {
			projects = append(projects, c)
		} else {
			tools = append(tools, c)
		}
	}
	return tools, projects
}

// Plan is what a clean of the selection will do, counted the way the confirmation tells it.
type Plan struct {
	Count int
	Bytes int64
	// Commands are the commands that will run, as typed in a terminal.
	Commands []string
	// Permanent counts the items deleted for good, Trashed the items moved to the Trash.
	Permanent, Trashed int
}

// CleanPlan splits the selection into commands to run (categories with a command), items deleted for good and items moved to the Trash.
func CleanPlan(categories []cleanup.Category, sel cleanflow.Selection) Plan {
	t := cleanflow.SelectedTotals(categories, sel)
	p := Plan{Count: t.Count, Bytes: t.Bytes}
	for _, c := range categories {
		ids := sel[c.ID]
		if len(ids) == 0 {
			continue
		}
		n := 0
		for _, it := range c.Items {
			if _, ok := ids[it.ID]; ok {
				n++
			}
		}
		switch {
		case n == 0:
		case c.Command != "":
			p.Commands = append(p.Commands, c.Command)
		case c.Deletion == cleanup.DeleteTrash:
			p.Trashed += n
		default:
			p.Permanent += n
		}
	}
	return p
}

// Confirmation is a plan worded for the confirmation dialog.
type Confirmation struct {
	// Summary is the total; Lines one line per way things go.
	Summary string
	Lines   []string
	// Danger is set when anything cannot be undone: an item deleted for good or a command run.
	Danger bool
}

// ConfirmationLines words a plan for the confirmation dialog: the total, then one line per way things go.
func ConfirmationLines(p Plan) Confirmation {
	bin := format.TrashName()
	var lines []string
	if len(p.Commands) > 0 {
		lines = append(lines, "运行 "+strconv.Itoa(len(p.Commands))+" 个清理命令："+strings.Join(p.Commands, "、"))
	}
	if p.Permanent > 0 {
		lines = append(lines, "永久删除 "+strconv.Itoa(p.Permanent)+" 项，无法恢复")
	}
	if p.Trashed > 0 {
		lines = append(lines, strconv.Itoa(p.Trashed)+" 项移到"+bin+"，可以放回")
	}
	return Confirmation{
		Summary: "将清理 " + strconv.Itoa(p.Count) + " 项，共 " + format.Bytes(p.Bytes) + "。",
		Lines:   lines,
		Danger:  p.Permanent > 0 || len(p.Commands) > 0,
	}
}

// OverlaySub is the line under the cleaning overlay's title: the count, and when a command runs, that stopping lets it finish first.
func OverlaySub(p Plan) string {
	sub := "共 " + strconv.Itoa(p.Count) + " 项"
	if len(p.Commands) > 0 {
		sub += "；停止后，正在运行的命令会先完成"
	}
	return sub
}

// RootsNotice is the line in place of the projects' section when the scan found no build output: whether there was nowhere to look (no project folder set, once the settings are loaded) or nothing in the folders.
func RootsNotice(roots []string, loaded bool) string {
	if loaded && len(roots) == 0 {
		return "没有设置项目文件夹，所以没有查找项目的构建产物。"
	}
	return "项目文件夹里没有找到构建产物。"
}

// DeniedNotice is the warning above the results, or above the empty state, when the scan could not read denied places; empty when it read them all.
func DeniedNotice(denied int64) string {
	if denied <= 0 {
		return ""
	}
	return "有 " + strconv.FormatInt(denied, 10) + " 个位置没有权限读取，结果可能不完整。"
}
