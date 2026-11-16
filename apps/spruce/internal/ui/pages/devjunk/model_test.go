package devjunk_test

import (
	"reflect"
	"slices"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	engdev "spruce/apps/spruce/internal/engine/devjunk"
	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/pages/devjunk"
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

// projectScan is build output of four projects, as a merged scan holds it: clean and idle, dirty (held), unknown, and one whose output holds tracked files (held), the first two with two folders.
func projectScan(now time.Time) cleanup.ScanResult {
	art := func(project, name string, size int64, git cleanup.GitState, hold string, idle time.Duration) cleanup.Item {
		p := project + "/" + name
		return cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: size, ModTime: now.Add(-idle), Project: project, Git: git, Hold: hold, Selected: hold == "" && git != cleanup.GitUnknown,
			Note: projectNote(project)}
	}
	day := 24 * time.Hour
	nm := cleanup.Category{ID: "dev-junk:artifact-node-modules", Source: "dev-junk", Title: "node_modules", Kind: cleanup.KindProjectArtifact, Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{
		art("/w/shop", "node_modules", 500, cleanup.GitClean, "", 100*day),
		art("/w/blog", "node_modules", 400, cleanup.GitDirty, "项目有未提交的改动", 100*day),
		art("/w/tool", "node_modules", 300, cleanup.GitUnknown, "", 5*day),
		art("/w/lib", "node_modules", 50, cleanup.GitClean, "构建产物文件夹里有纳入版本控制的文件", 200*day),
	}}
	next := cleanup.Category{ID: "dev-junk:artifact-next", Source: "dev-junk", Title: ".next", Kind: cleanup.KindProjectArtifact, Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{
		art("/w/shop", ".next", 200, cleanup.GitClean, "", 100*day),
		art("/w/blog", ".next", 100, cleanup.GitDirty, "项目有未提交的改动", 100*day),
	}}
	return cleanup.NewResult([]cleanup.Category{nm, next}, now)
}

func projectNote(project string) string {
	if project == "/w/lib" {
		return "packages/lib"
	}
	return project[len("/w/"):]
}

// The 项目 helpers: filters, the Git line, why a project is held, its check state, and checking projects never checks held output.
func TestProjects(t *testing.T) {
	now := time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)
	res := projectScan(now)
	ps := engdev.GroupProjects(res)
	byPath := map[string]engdev.Project{}
	for _, p := range ps {
		byPath[p.Path] = p
	}
	names := func(list []engdev.Project) []string {
		var out []string
		for _, p := range list {
			out = append(out, p.Path)
		}
		return out
	}
	if got := names(devjunk.FilterProjects(ps, devjunk.FilterAll, 30, now)); !reflect.DeepEqual(got, []string{"/w/shop", "/w/blog", "/w/tool", "/w/lib"}) {
		t.Errorf("all %v", got)
	}
	if got := names(devjunk.FilterProjects(ps, devjunk.FilterIdle, 30, now)); !reflect.DeepEqual(got, []string{"/w/shop", "/w/blog", "/w/lib"}) {
		t.Errorf("idle %v", got)
	}
	if got := names(devjunk.FilterProjects(ps, devjunk.FilterDirty, 30, now)); !reflect.DeepEqual(got, []string{"/w/blog"}) {
		t.Errorf("dirty %v", got)
	}
	if devjunk.IdleLabel(30) != "闲置超过 30 天" || devjunk.Idle(engdev.Project{}, 0, now) {
		t.Error("idle label or a project without activity")
	}
	for path, want := range map[string]devjunk.GitLabel{
		"/w/shop": {}, "/w/blog": {Text: "有未提交的改动", Badge: true}, "/w/tool": {Text: "无法确定 Git 状态"}, "/w/lib": {Text: "构建产物里有纳入版本控制的文件"},
	} {
		if got := devjunk.GitLabelOf(byPath[path]); got != want {
			t.Errorf("%s git %+v", path, got)
		}
	}
	if got := devjunk.GitLabelOf(engdev.Project{Git: cleanup.GitNone}); got.Text != "不是 Git 仓库" {
		t.Errorf("none %+v", got)
	}
	if devjunk.HoldTip(byPath["/w/blog"]) != "有未提交的改动。提交或丢弃改动后才能清理。" ||
		devjunk.HoldTip(byPath["/w/lib"]) != "构建产物文件夹里有纳入版本控制的文件，删除后仓库会出现改动。" || devjunk.HoldTip(byPath["/w/tool"]) != "" {
		t.Error("hold tips")
	}
	if devjunk.Checkable(byPath["/w/blog"], res.Categories) || !devjunk.Checkable(byPath["/w/tool"], res.Categories) {
		t.Error("checkable")
	}

	sel := cleanflow.DefaultSelection(res.Categories)
	if devjunk.ProjectState(byPath["/w/shop"], res.Categories, sel) != cleanflow.CheckAll || devjunk.ProjectState(byPath["/w/tool"], res.Categories, sel) != cleanflow.CheckNone {
		t.Error("default states")
	}
	if devjunk.SelectedProjects(ps, sel) != 1 {
		t.Errorf("selected %d", devjunk.SelectedProjects(ps, sel))
	}
	all := devjunk.SetProjects(sel, ps, res.Categories, true)
	if devjunk.SelectedProjects(ps, all) != 2 || devjunk.ProjectState(byPath["/w/blog"], res.Categories, all) != cleanflow.CheckNone {
		t.Errorf("checking every project checked held output: %v", all)
	}
	part := cleanflow.SetItem(all, res.Categories[1], cleanup.ItemID("/w/shop/.next"), false)
	if devjunk.ProjectState(byPath["/w/shop"], res.Categories, part) != cleanflow.CheckSome {
		t.Error("part of a project")
	}
	if got := devjunk.SetProjects(all, ps, res.Categories, false); len(got) != 0 {
		t.Errorf("unchecking every project %v", got)
	}
	if got := devjunk.ProjectSub(byPath["/w/shop"]); got != "node_modules · .next" {
		t.Errorf("sub %q", got)
	}
	if got := devjunk.ProjectSub(byPath["/w/lib"]); got != "packages/lib · node_modules" {
		t.Errorf("sub of a nested project %q", got)
	}
	if got := devjunk.ProjectItems(byPath["/w/shop"], res.Categories[:1]); len(got) != 1 || got[0].Item.Path != "/w/shop/node_modules" {
		t.Errorf("items %+v", got)
	}
}

// ArtifactsOf keeps only the build output of the projects given; ProjectNames names the checked ones, five at most.
func TestArtifactsOfAndProjectNames(t *testing.T) {
	cats := []cleanup.Category{
		modelCategory("nm", cleanup.KindProjectArtifact, cleanup.DeletePermanent, "a", "b", "c"),
		modelCategory("next", cleanup.KindProjectArtifact, cleanup.DeletePermanent, "d"),
	}
	project := func(path string, refs ...engdev.ProjectItem) engdev.Project {
		return engdev.Project{ID: path, Path: path, Items: refs}
	}
	a := project("/w/a", engdev.ProjectItem{CategoryID: "nm", ItemID: "a"}, engdev.ProjectItem{CategoryID: "next", ItemID: "d"})
	b := project("/w/b", engdev.ProjectItem{CategoryID: "nm", ItemID: "b"})
	got := devjunk.ArtifactsOf([]engdev.Project{b}, cats)
	if len(got) != 1 || got[0].ID != "nm" || len(got[0].Items) != 1 || got[0].Items[0].ID != "b" || got[0].Size != 100 {
		t.Errorf("artifacts of b %+v", got)
	}
	if got := devjunk.ArtifactsOf([]engdev.Project{a, b}, cats); len(got) != 2 || len(got[0].Items) != 2 {
		t.Errorf("artifacts of a and b %+v", got)
	}
	sel := cleanflow.Selection{"nm": {"b": {}}, "next": {"d": {}}}
	if got := devjunk.ProjectNames([]engdev.Project{a, b}, sel); got != "项目：a、b" {
		t.Errorf("names %q", got)
	}
	if got := devjunk.ProjectNames([]engdev.Project{a, b}, cleanflow.NothingSelected); got != "" {
		t.Errorf("no names %q", got)
	}
	var many []engdev.Project
	all := cleanflow.Selection{"nm": {}}
	for _, n := range []string{"p1", "p2", "p3", "p4", "p5", "p6", "p7"} {
		many = append(many, project("/w/"+n, engdev.ProjectItem{CategoryID: "nm", ItemID: n}))
		all["nm"][n] = struct{}{}
	}
	if got := devjunk.ProjectNames(many, all); got != "项目：p1、p2、p3、p4、p5 等 7 个" {
		t.Errorf("many %q", got)
	}
}
