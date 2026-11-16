package cleanflow

import (
	"reflect"
	"runtime"
	"slices"
	"strconv"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
)

// macWording skips a test whose expectations are the macOS wording and SI sizes (废纸篓, 1000-byte megabytes).
func macWording(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
}

func item(id string, size int64) cleanup.Item {
	return cleanup.Item{ID: id, Path: "/x/" + id, Size: size, ModTime: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)}
}

func category(id string, selected bool, items ...cleanup.Item) cleanup.Category {
	c := cleanup.Category{ID: id, Title: id, Icon: "cache", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent, Items: items, Selected: selected}
	for _, it := range items {
		c.Size += it.Size
	}
	return c
}

func ids(m map[string]struct{}) []string {
	out := make([]string, 0, len(m))
	for id := range m {
		out = append(out, id)
	}
	slices.Sort(out)
	return out
}

// Ported from selection.test.ts.
func TestSelection(t *testing.T) {
	cats := []cleanup.Category{
		category("caches", true, item("a", 100), item("b", 50)),
		category("logs", false, item("c", 10)),
		category("empty", true),
	}

	t.Run("starts from the scan's defaults", func(t *testing.T) {
		sel := DefaultSelection(cats)
		for i, want := range []CheckState{CheckAll, CheckNone, CheckNone} {
			if got := CategoryState(cats[i], sel); got != want {
				t.Errorf("%s: %v", cats[i].ID, got)
			}
		}
		if got := SelectedTotals(cats, sel); got != (Totals{Count: 2, Bytes: 150}) {
			t.Errorf("totals %+v", got)
		}
	})

	t.Run("checks the items the scan marked on their own, a category in part", func(t *testing.T) {
		old := item("old", 30)
		old.Selected = true
		part := category("support", false, old, item("newest", 40))
		sel := DefaultSelection([]cleanup.Category{part})
		if CategoryState(part, sel) != CheckSome {
			t.Error("not partly checked")
		}
		if got := ids(sel["support"]); !reflect.DeepEqual(got, []string{"old"}) {
			t.Errorf("checked %v", got)
		}
	})

	t.Run("toggles items and categories", func(t *testing.T) {
		start := DefaultSelection(cats)
		sel := SetItem(start, cats[0], "b", false)
		if CategoryState(cats[0], sel) != CheckSome {
			t.Error("not partly checked")
		}
		if CategoryState(cats[0], start) != CheckAll {
			t.Error("SetItem changed the selection it was given")
		}
		sel = SetItem(sel, cats[0], "a", false)
		if _, ok := sel["caches"]; ok {
			t.Error("an emptied category keeps its entry")
		}
		sel = SetCategory(sel, cats[1], true)
		if got := SelectedTotals(cats, sel); got != (Totals{Count: 1, Bytes: 10}) {
			t.Errorf("totals %+v", got)
		}
		if CategoryState(cats[1], SetCategory(sel, cats[1], false)) != CheckNone {
			t.Error("unchecking the category left items checked")
		}
		if got := SelectedTotals(cats, SelectAll(cats)); got != (Totals{Count: 3, Bytes: 160}) {
			t.Errorf("all %+v", got)
		}
		if got := SelectedTotals(cats, NothingSelected); got != (Totals{}) {
			t.Errorf("none %+v", got)
		}
	})

	t.Run("applies a group's selection to the group only", func(t *testing.T) {
		all := DefaultSelection(cats)
		got := WithinGroup(all, cats[1:2], SelectAll(cats))
		if CategoryState(cats[0], got) != CheckAll || CategoryState(cats[1], got) != CheckAll {
			t.Errorf("select all in the group %v", got)
		}
		got = WithinGroup(got, cats[1:2], NothingSelected)
		if CategoryState(cats[0], got) != CheckAll || CategoryState(cats[1], got) != CheckNone {
			t.Errorf("select none in the group %v", got)
		}
	})
}

func planCategory(id string, deletion cleanup.DeletionMode, sizes ...int64) cleanup.Category {
	c := cleanup.Category{ID: id, Title: id + " 标题", Icon: "cache", Kind: cleanup.KindCache, Deletion: deletion, Selected: true}
	for i, s := range sizes {
		c.Items = append(c.Items, cleanup.Item{ID: id + "-" + strconv.Itoa(i), Path: "/x/" + id + "/" + strconv.Itoa(i), Size: s})
		c.Size += s
	}
	return c
}

// Ported from cleanPlan.test.ts.
func TestCleanPlan(t *testing.T) {
	t.Run("splits the checked items by deletion mode, in list order", func(t *testing.T) {
		cats := []cleanup.Category{
			planCategory("a", cleanup.DeletePermanent, 10, 20),
			planCategory("b", cleanup.DeleteTrash, 5),
			planCategory("c", cleanup.DeletePermanent, 1),
		}
		sel := Selection{"a": {"a-1": {}}, "b": {"b-0": {}}, "c": {}}
		want := Plan{
			Permanent: []PlanLine{{ID: "a", Title: "a 标题", Count: 1, Bytes: 20}},
			Trash:     []PlanLine{{ID: "b", Title: "b 标题", Count: 1, Bytes: 5}},
		}
		got := CleanPlan(cats, sel)
		if !reflect.DeepEqual(got, want) {
			t.Errorf("plan %+v", got)
		}
		if got.Count() != 2 {
			t.Errorf("count %d", got.Count())
		}
	})

	t.Run("puts a category its tool's command cleans apart", func(t *testing.T) {
		npm := planCategory("npm", cleanup.DeletePermanent, 7)
		npm.Command = "npm cache clean --force"
		got := CleanPlan([]cleanup.Category{npm}, Selection{"npm": {"npm-0": {}}})
		want := Plan{Commands: []PlanLine{{ID: "npm", Title: "npm 标题", Count: 1, Bytes: 7, Command: "npm cache clean --force"}}}
		if !reflect.DeepEqual(got, want) {
			t.Errorf("plan %+v", got)
		}
	})
}

func inUseResult() cleanup.CleanResult {
	return cleanup.CleanResult{Freed: 100, Removed: 1, Partial: true,
		Failed: []cleanup.Failure{
			{ItemID: "1", CategoryID: "temp", Path: "/t/1", Reason: "有 2 个文件未能删除，可能正在使用", InUse: true},
			{ItemID: "2", CategoryID: "temp", Path: "/t/2", Reason: "没有权限删除"},
		},
		Categories: []cleanup.CategoryOutcome{{CategoryID: "temp", Title: "临时文件", Removed: 1, Freed: 100, Failed: 2}},
		StartedAt:  time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC), Duration: time.Second}
}

// Ported from cleanPlan.test.ts.
func TestSetInUseAside(t *testing.T) {
	t.Run("counts the in-use items and leaves the other failures", func(t *testing.T) {
		base := inUseResult()
		got, inUse := SetInUseAside(base)
		if inUse != 1 || len(got.Failed) != 1 || got.Failed[0].ItemID != "2" || got.Categories[0].Failed != 1 {
			t.Errorf("result %+v, in use %d", got, inUse)
		}
		if !reflect.DeepEqual(base, inUseResult()) {
			t.Error("the result it was given changed")
		}
	})

	t.Run("returns the result as it is without in-use items", func(t *testing.T) {
		plain := inUseResult()
		plain.Failed = plain.Failed[1:]
		got, inUse := SetInUseAside(plain)
		if inUse != 0 || !reflect.DeepEqual(got, plain) {
			t.Errorf("result %+v, in use %d", got, inUse)
		}
	})
}

// Ported from cleanSummary.test.ts.
func TestCleanConfirmation(t *testing.T) {
	macWording(t)
	cat := func(id string, d cleanup.DeletionMode) cleanup.Category {
		return cleanup.Category{ID: id, Title: id, Icon: "cache", Kind: cleanup.KindCache, Deletion: d, Selected: true, Size: 1_000_000,
			Items: []cleanup.Item{{ID: id + "-1", Path: "/x/" + id, Size: 1_000_000}}}
	}
	cats := []cleanup.Category{cat("a", cleanup.DeletePermanent), cat("b", cleanup.DeleteTrash)}
	if got := CleanConfirmation(cats, Selection{"b": {"b-1": {}}}); got != (Confirmation{Message: "将清理 1 项，共 1 MB。它们会移到废纸篓，可以随时放回。"}) {
		t.Errorf("trash %+v", got)
	}
	if got := CleanConfirmation(cats, Selection{"a": {"a-1": {}}}); got.Message != "将清理 1 项，共 1 MB。删除后无法恢复。" || !got.Danger {
		t.Errorf("permanent %+v", got)
	}
	both := Selection{"a": {"a-1": {}}, "b": {"b-1": {}}}
	if got := CleanConfirmation(cats, both); got != (Confirmation{Message: "将清理 2 项，共 2 MB。其中 1 项会永久删除，无法恢复；其余移到废纸篓。", Danger: true}) {
		t.Errorf("both %+v", got)
	}
}

// A held item (Item.Hold) is never checked: not by the defaults, its category, 全选 or by itself, and a category whose other items are checked counts as all checked.
func TestSelectionLeavesHeldItems(t *testing.T) {
	held := item("h", 70)
	held.Hold, held.Selected = "项目有未提交的改动", true
	c := category("nm", true, item("a", 100), held)
	cats := []cleanup.Category{c, category("all-held", false, held)}
	sel := DefaultSelection(cats)
	if got := ids(sel["nm"]); !reflect.DeepEqual(got, []string{"a"}) || CategoryState(c, sel) != CheckAll {
		t.Errorf("defaults %v %v", got, CategoryState(c, sel))
	}
	if got := ids(SelectAll(cats)["nm"]); !reflect.DeepEqual(got, []string{"a"}) || len(SelectAll(cats)["all-held"]) != 0 {
		t.Errorf("select all %v", got)
	}
	if got := SetCategory(NothingSelected, cats[1], true); len(got) != 0 {
		t.Errorf("a category of held items %v", got)
	}
	if got := SetItem(sel, c, "h", true); !reflect.DeepEqual(got, sel) {
		t.Errorf("checked the held item %v", got)
	}
	if Selectable(c) != 1 || Selectable(cats[1]) != 0 || CategoryState(cats[1], SelectAll(cats)) != CheckNone {
		t.Error("selectable")
	}
}

// Reconcile keeps what is still there and not held; Refs and ItemsRequest name every checked item in list order.
func TestReconcileAndRequest(t *testing.T) {
	old := []cleanup.Category{category("caches", true, item("a", 1), item("b", 2)), category("logs", true, item("c", 3))}
	sel := SelectAll(old)
	held := item("b", 2)
	held.Hold = "x"
	next := []cleanup.Category{category("caches", false, item("a", 1), held, item("n", 5))}
	got := Reconcile(sel, next)
	if !reflect.DeepEqual(got, Selection{"caches": {"a": {}}}) {
		t.Errorf("reconcile %v", got)
	}
	cats := []cleanup.Category{category("caches", true, item("a", 1), item("b", 2)), category("logs", false, item("c", 3)), category("tmp", true, item("d", 4))}
	sel = SetItem(SelectAll(cats), cats[0], "b", false)
	if got := ItemsRequest(cats, sel); !reflect.DeepEqual(got, cleanup.CleanRequest{CategoryIDs: []string{"caches", "logs", "tmp"}, ItemIDs: []string{"a", "c", "d"}}) {
		t.Errorf("request %+v", got)
	}
	if got := ItemsRequest(cats, NothingSelected); !reflect.DeepEqual(got, cleanup.CleanRequest{CategoryIDs: []string{}}) {
		t.Errorf("empty request %+v", got)
	}
	if got := Refs(cats[:1], sel); len(got) != 1 || got[0].CategoryID != "caches" || got[0].ItemID != "a" {
		t.Errorf("refs %+v", got)
	}
}

// Only a move to the Trash that can be undone runs without asking.
func TestNeedsConfirm(t *testing.T) {
	trash := Plan{Trash: []PlanLine{{ID: "t", Count: 1}}}
	for _, c := range []struct {
		plan    Plan
		canUndo bool
		want    bool
	}{
		{trash, true, false},
		{trash, false, true},
		{Plan{Permanent: []PlanLine{{ID: "p", Count: 1}}}, true, true},
		{Plan{Commands: []PlanLine{{ID: "c", Count: 1}}, Trash: trash.Trash}, true, true},
	} {
		if got := NeedsConfirm(c.plan, c.canUndo); got != c.want {
			t.Errorf("%+v %v: %v", c.plan, c.canUndo, got)
		}
	}
}

// The status row says why the view may not clean.
func TestStaleText(t *testing.T) {
	for _, c := range []struct {
		checkErr, err string
		has           bool
		want          string
	}{
		{"正在清理，完成后再检查", "x", true, "正在清理，完成后再检查"},
		{"", "没有权限", true, "这次检查没有完成：没有权限"},
		{"", "", false, "还没有检查结果"},
		{"", "", true, "重新检查后才能清理"},
	} {
		if got := StaleText(c.checkErr, c.err, c.has); got != c.want {
			t.Errorf("%+v: %q", c, got)
		}
	}
	if DeniedText(0) != "" || DeniedText(3) != "有 3 个位置没有读取权限，结果可能不完整" {
		t.Error("denied")
	}
}

// The cleaning overlay counts what the confirmation counts, leaving the Trash to its own words.
func TestCleanSub(t *testing.T) {
	macWording(t)
	cats := []cleanup.Category{
		{ID: "c", Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{{ID: "a"}, {ID: "b"}}},
		{ID: "t", Kind: cleanup.KindTrash, Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{{ID: "trash"}}},
	}
	for _, c := range []struct {
		sel  Selection
		want string
	}{
		{Selection{"c": {"a": {}, "b": {}}}, "共 2 项"},
		{Selection{"c": {"a": {}}, "t": {"trash": {}}}, "共 1 项，并清空废纸篓"},
		{Selection{"t": {"trash": {}}}, "清空废纸篓"},
	} {
		if got := CleanSub(cats, c.sel); got != c.want {
			t.Errorf("CleanSub(%v) = %q, want %q", c.sel, got, c.want)
		}
	}
}

// A category the user did not change follows each newer check; one changed on a card keeps what is still there and counts what the check adds; a change in a drill-down view covers every category of its source, also one a newer check adds.
func TestChoice(t *testing.T) {
	src := func(c cleanup.Category, source string) cleanup.Category {
		c.ID, c.Source = source+":"+c.ID, source
		return c
	}
	caches := src(category("caches", true, item("a", 1), item("b", 2)), "system-junk")
	logs := src(category("logs", true, item("c", 3)), "system-junk")
	disk := src(category("dmg", true, item("e", 5)), "installers")
	ch := Choice{Sel: NothingSelected}.Reconcile([]cleanup.Category{caches, logs, disk})
	if !reflect.DeepEqual(ch.Sel, DefaultSelection([]cleanup.Category{caches, logs, disk})) {
		t.Fatalf("defaults %v", ch.Sel)
	}
	// A card over caches unchecks a.
	card := []advice.ItemRef{{CategoryID: caches.ID, ItemID: "a"}, {CategoryID: caches.ID, ItemID: "b"}}
	ch = ch.Change(WithinRefs(ch.Sel, card, Selection{caches.ID: {"b": {}}}), []cleanup.Category{caches}, "")
	if ch.Sel.Has(caches.ID, "a") || !ch.Sel.Has(caches.ID, "b") || !ch.Sel.Has(logs.ID, "c") || !ch.Sel.Has(disk.ID, "e") {
		t.Fatalf("card change %v", ch.Sel)
	}
	// A newer check adds n to caches and d to logs: the changed caches leaves n unchecked, the untouched logs checks d.
	caches2 := src(category("caches", true, item("a", 1), item("b", 2), item("n", 4)), "system-junk")
	logs2 := src(category("logs", true, item("c", 3), item("d", 1)), "system-junk")
	newer := []cleanup.Category{caches2, logs2, disk}
	ch = ch.Reconcile(newer)
	if ch.Sel.Has(caches2.ID, "a") || ch.Sel.Has(caches2.ID, "n") || !ch.Sel.Has(caches2.ID, "b") || !ch.Sel.Has(logs2.ID, "d") {
		t.Errorf("reconciled %v", ch.Sel)
	}
	refs := []advice.ItemRef{{CategoryID: caches2.ID, ItemID: "a"}, {CategoryID: caches2.ID, ItemID: "b"}, {CategoryID: caches2.ID, ItemID: "n"}, {CategoryID: logs2.ID, ItemID: "d"}}
	if got := ch.Added(newer, refs); got != 1 {
		t.Errorf("added %d, want 1 (n)", got)
	}
	// 全不选 in 系统垃圾 changes the whole source: a category a newer check adds there starts unchecked, other sources keep their defaults.
	ch = ch.Change(WithinGroup(ch.Sel, newer[:2], NothingSelected), newer[:2], "system-junk")
	tmp := src(category("tmp", true, item("t", 1)), "system-junk")
	disk2 := src(category("dmg", true, item("e", 5), item("f", 6)), "installers")
	ch = ch.Reconcile([]cleanup.Category{caches2, logs2, tmp, disk2})
	if ch.Sel.Count(tmp.ID) != 0 || ch.Sel.Count(caches2.ID) != 0 || !ch.Sel.Has(disk2.ID, "f") {
		t.Errorf("after the drill-down view's change %v", ch.Sel)
	}
	// Categories that went are forgotten.
	ch = ch.Reconcile([]cleanup.Category{disk2})
	if _, ok := ch.Touched[caches2.ID]; ok || len(ch.Sel) != 1 {
		t.Errorf("gone categories kept %+v", ch)
	}
}
