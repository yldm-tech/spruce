package devjunk_test

import (
	"reflect"
	"slices"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/pages/devjunk"
)

// modelCategory is devJunk.test.ts's category: one item of 100 B per name.
func modelCategory(id string, kind cleanup.CategoryKind, deletion cleanup.DeletionMode, items ...string) cleanup.Category {
	c := cleanup.Category{ID: id, Title: id, Icon: "cache", Kind: kind, Deletion: deletion, Size: int64(len(items)) * 100}
	for _, i := range items {
		c.Items = append(c.Items, cleanup.Item{ID: i, Path: "/x/" + i, Size: 100})
	}
	return c
}

// modelCategories are devJunk.test.ts's CATEGORIES.
func modelCategories() []cleanup.Category {
	npm := modelCategory("tool-npm", cleanup.KindDevCache, cleanup.DeletePermanent, "npm")
	npm.Command = "npm cache clean --force"
	return []cleanup.Category{
		npm,
		modelCategory("tool-xcode-device-support", cleanup.KindDevCache, cleanup.DeletePermanent, "ios16", "ios17"),
		modelCategory("tool-xcode-archives", cleanup.KindDevCache, cleanup.DeleteTrash, "arch"),
		modelCategory("artifact-node-modules", cleanup.KindProjectArtifact, cleanup.DeletePermanent, "nm-old", "nm-new"),
		modelCategory("artifact-node-modules-trash", cleanup.KindProjectArtifact, cleanup.DeleteTrash, "nm-docs"),
	}
}

// selectionFromIDs checks exactly the items whose IDs are in ids, which may check a category in part.
func selectionFromIDs(categories []cleanup.Category, ids ...string) cleanflow.Selection {
	sel := cleanflow.Selection{}
	for _, c := range categories {
		for _, it := range c.Items {
			if slices.Contains(ids, it.ID) {
				if sel[c.ID] == nil {
					sel[c.ID] = map[string]struct{}{}
				}
				sel[c.ID][it.ID] = struct{}{}
			}
		}
	}
	return sel
}

func ids(categories []cleanup.Category) []string {
	out := make([]string, len(categories))
	for i, c := range categories {
		out[i] = c.ID
	}
	return out
}

func keys[V any](m map[string]V) []string {
	out := make([]string, 0, len(m))
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

// Ported from devJunk.test.ts: puts tool caches and project build output apart.
func TestSplitGroups(t *testing.T) {
	tools, projects := devjunk.SplitGroups(modelCategories())
	if got := ids(tools); !reflect.DeepEqual(got, []string{"tool-npm", "tool-xcode-device-support", "tool-xcode-archives"}) {
		t.Errorf("tools %v", got)
	}
	if got := ids(projects); !reflect.DeepEqual(got, []string{"artifact-node-modules", "artifact-node-modules-trash"}) {
		t.Errorf("projects %v", got)
	}
}

// Ported from devJunk.test.ts: withinGroup changes one section and keeps the other's checks.
func TestWithinGroup(t *testing.T) {
	cats := modelCategories()
	tools, projects := devjunk.SplitGroups(cats)
	all := selectionFromIDs(cats, "npm", "nm-old")
	// 全不选 in the projects list sends the empty selection.
	next := cleanflow.WithinGroup(all, projects, cleanflow.NothingSelected)
	if got := keys(next); !reflect.DeepEqual(got, []string{"tool-npm"}) {
		t.Errorf("after 全不选 %v", got)
	}
	// 全选 in the tools list sends only the tools.
	again := cleanflow.WithinGroup(all, tools, cleanflow.SelectAll(tools))
	want := []string{"artifact-node-modules", "tool-npm", "tool-xcode-archives", "tool-xcode-device-support"}
	if got := keys(again); !reflect.DeepEqual(got, want) {
		t.Errorf("after 全选 %v", got)
	}
	if got := keys(again["artifact-node-modules"]); !reflect.DeepEqual(got, []string{"nm-old"}) {
		t.Errorf("node_modules %v", got)
	}
}

// Ported from devJunk.test.ts: counts commands, permanent deletions and the Trash apart.
func TestCleanPlanAndConfirmationLines(t *testing.T) {
	cats := modelCategories()
	plan := devjunk.CleanPlan(cats, selectionFromIDs(cats, "npm", "ios16", "arch", "nm-old", "nm-new", "nm-docs"))
	want := devjunk.Plan{Count: 6, Bytes: 600, Commands: []string{"npm cache clean --force"}, Permanent: 3, Trashed: 2}
	if !reflect.DeepEqual(plan, want) {
		t.Errorf("plan %+v", plan)
	}
	text := devjunk.ConfirmationLines(plan)
	if text.Summary != "将清理 6 项，共 600 B。" {
		t.Errorf("summary %q", text.Summary)
	}
	lines := []string{"运行 1 个清理命令：npm cache clean --force", "永久删除 3 项，无法恢复", "2 项移到" + format.TrashName() + "，可以放回"}
	if !reflect.DeepEqual(text.Lines, lines) {
		t.Errorf("lines %q", text.Lines)
	}
	if !text.Danger {
		t.Error("not danger")
	}
}

// Ported from devJunk.test.ts: is no danger when everything goes to the Trash.
func TestConfirmationAllToTheTrash(t *testing.T) {
	cats := modelCategories()
	text := devjunk.ConfirmationLines(devjunk.CleanPlan(cats, selectionFromIDs(cats, "arch")))
	if want := []string{"1 项移到" + format.TrashName() + "，可以放回"}; !reflect.DeepEqual(text.Lines, want) {
		t.Errorf("lines %q", text.Lines)
	}
	if text.Danger {
		t.Error("danger")
	}
}

// Ported from devJunk.test.ts: treats a command category as a command, not a deletion.
func TestCommandIsNotADeletion(t *testing.T) {
	cats := modelCategories()
	plan := devjunk.CleanPlan(cats, selectionFromIDs(cats, "npm"))
	if plan.Permanent != 0 || plan.Trashed != 0 || len(plan.Commands) != 1 {
		t.Errorf("plan %+v", plan)
	}
	if !devjunk.ConfirmationLines(plan).Danger {
		t.Error("a command is not danger")
	}
}

func TestOverlaySub(t *testing.T) {
	if got := devjunk.OverlaySub(devjunk.Plan{Count: 3, Commands: []string{"npm cache clean --force"}}); got != "共 3 项；停止后，正在运行的命令会先完成" {
		t.Errorf("with a command %q", got)
	}
	if got := devjunk.OverlaySub(devjunk.Plan{Count: 2}); got != "共 2 项" {
		t.Errorf("without %q", got)
	}
}

func TestRootsNotice(t *testing.T) {
	for _, tc := range []struct {
		roots  []string
		loaded bool
		want   string
	}{
		{nil, true, "没有设置项目文件夹，所以没有查找项目的构建产物。"},
		{nil, false, "项目文件夹里没有找到构建产物。"},
		{[]string{"/p"}, true, "项目文件夹里没有找到构建产物。"},
	} {
		if got := devjunk.RootsNotice(tc.roots, tc.loaded); got != tc.want {
			t.Errorf("%v %v: %q", tc.roots, tc.loaded, got)
		}
	}
}

func TestDeniedNotice(t *testing.T) {
	if got := devjunk.DeniedNotice(0); got != "" {
		t.Errorf("none denied: %q", got)
	}
	if got := devjunk.DeniedNotice(2); got != "有 2 个位置没有权限读取，结果可能不完整。" {
		t.Errorf("two denied: %q", got)
	}
}
