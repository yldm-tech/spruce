package devjunk

import (
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	engdev "spruce/apps/spruce/internal/engine/devjunk"
	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/format"
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

// ProjectFilter is a chip of the 项目 tab.
type ProjectFilter string

const (
	FilterAll   ProjectFilter = "all"
	FilterIdle  ProjectFilter = "idle"
	FilterDirty ProjectFilter = "dirty"
)

// IdleLabel is the 闲置 chip for projects idle at least days days.
func IdleLabel(days int) string { return "闲置超过 " + strconv.Itoa(days) + " 天" }

// Idle reports whether p was last active at least days days before now; a project whose activity is unknown is not.
func Idle(p engdev.Project, days int, now time.Time) bool {
	return !p.LastActive.IsZero() && now.Sub(p.LastActive) >= time.Duration(days)*24*time.Hour
}

// FilterProjects keeps the projects of the filter f, in their order (largest first).
func FilterProjects(projects []engdev.Project, f ProjectFilter, days int, now time.Time) []engdev.Project {
	out := []engdev.Project{}
	for _, p := range projects {
		switch {
		case f == FilterIdle && !Idle(p, days, now):
		case f == FilterDirty && p.Git != cleanup.GitDirty:
		default:
			out = append(out, p)
		}
	}
	return out
}

// GitLabel is a project row's Git status: nothing for a clean project, a warning badge for uncommitted changes, otherwise a quiet line.
type GitLabel struct {
	Text  string
	Badge bool
}

// GitLabelOf words p's Git status; a project held for tracked files in its build output says so.
func GitLabelOf(p engdev.Project) GitLabel {
	switch {
	case p.Git == cleanup.GitDirty:
		return GitLabel{Text: "有未提交的改动", Badge: true}
	case p.Hold != "":
		return GitLabel{Text: "构建产物里有纳入版本控制的文件"}
	case p.Git == cleanup.GitUnknown:
		return GitLabel{Text: "无法确定 Git 状态"}
	case p.Git == cleanup.GitNone:
		return GitLabel{Text: "不是 Git 仓库"}
	}
	return GitLabel{}
}

// HoldTip is why a held project cannot be checked, "" for one that can.
func HoldTip(p engdev.Project) string {
	switch {
	case p.Hold == "":
		return ""
	case p.Git == cleanup.GitDirty:
		return "有未提交的改动。提交或丢弃改动后才能清理。"
	}
	return "构建产物文件夹里有纳入版本控制的文件，删除后仓库会出现改动。"
}

// ProjectItems are the build output items of p in categories (a merged scan's), in p's order, each with its category; items no longer there are left out.
func ProjectItems(p engdev.Project, categories []cleanup.Category) []ProjectArtifact {
	byID := make(map[string]*cleanup.Category, len(categories))
	for i := range categories {
		byID[categories[i].ID] = &categories[i]
	}
	var out []ProjectArtifact
	for _, ref := range p.Items {
		c := byID[ref.CategoryID]
		if c == nil {
			continue
		}
		for _, it := range c.Items {
			if it.ID == ref.ItemID {
				out = append(out, ProjectArtifact{Category: *c, Item: it})
				break
			}
		}
	}
	return out
}

// ProjectArtifact is one build output folder of a project with the category it is in.
type ProjectArtifact struct {
	Category cleanup.Category
	Item     cleanup.Item
}

// ProjectState is how much of p's build output that may be checked is checked; a project with nothing that may be is CheckNone.
func ProjectState(p engdev.Project, categories []cleanup.Category, sel cleanflow.Selection) cleanflow.CheckState {
	n, all := 0, 0
	for _, a := range ProjectItems(p, categories) {
		if a.Item.Hold != "" {
			continue
		}
		all++
		if sel.Has(a.Category.ID, a.Item.ID) {
			n++
		}
	}
	switch {
	case n == 0 || all == 0:
		return cleanflow.CheckNone
	case n == all:
		return cleanflow.CheckAll
	}
	return cleanflow.CheckSome
}

// Checkable reports whether any of p's build output may be checked.
func Checkable(p engdev.Project, categories []cleanup.Category) bool {
	if p.Hold != "" {
		return false
	}
	for _, a := range ProjectItems(p, categories) {
		if a.Item.Hold == "" {
			return true
		}
	}
	return false
}

// SetProjects checks or unchecks the build output of projects; held items stay unchecked.
func SetProjects(sel cleanflow.Selection, projects []engdev.Project, categories []cleanup.Category, on bool) cleanflow.Selection {
	for _, p := range projects {
		if on && p.Hold != "" {
			continue
		}
		for _, a := range ProjectItems(p, categories) {
			sel = cleanflow.SetItem(sel, a.Category, a.Item.ID, on)
		}
	}
	return sel
}

// SelectedProjects counts the projects with any build output checked.
func SelectedProjects(projects []engdev.Project, sel cleanflow.Selection) int {
	n := 0
	for _, p := range projects {
		for _, ref := range p.Items {
			if sel.Has(ref.CategoryID, ref.ItemID) {
				n++
				break
			}
		}
	}
	return n
}

// ProjectSub is the line under a project's name: its path under its root when that says more than the name, and its kinds of build output.
func ProjectSub(p engdev.Project) string {
	var parts []string
	if p.Label != "" && p.Label != filepath.Base(p.Path) {
		parts = append(parts, p.Label)
	}
	if len(p.Kinds) > 0 {
		parts = append(parts, strings.Join(p.Kinds, " · "))
	}
	return strings.Join(parts, " · ")
}
