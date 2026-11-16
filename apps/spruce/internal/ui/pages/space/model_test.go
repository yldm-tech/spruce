package space

import (
	"encoding/json"
	"os"
	"testing"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/growth"
	"spruce/apps/spruce/internal/ui/pages/space/spacefake"
	"spruce/apps/spruce/internal/ui/store"
)

// The labels (labels.ts).

func TestEntryLabel(t *testing.T) {
	cases := []struct {
		e    app.SpaceEntry
		want string
	}{
		{app.SpaceEntry{Kind: app.SpaceDir, Name: "Projects"}, "Projects"},
		{app.SpaceEntry{Kind: app.SpaceFile, Name: "big.iso"}, "big.iso"},
		{app.SpaceEntry{Kind: app.SpaceOther, Count: 4}, "其他 4 项"},
		{app.SpaceEntry{Kind: app.SpaceFiles, Count: 30}, "30 个小文件"},
		{app.SpaceEntry{Kind: app.SpaceOther}, "其他 0 项"},
	}
	for _, c := range cases {
		if got := EntryLabel(c.e); got != c.want {
			t.Errorf("EntryLabel(%+v) = %q, want %q", c.e, got, c.want)
		}
	}
	if !IsGroup(app.SpaceEntry{Kind: app.SpaceOther}) || !IsGroup(app.SpaceEntry{Kind: app.SpaceFiles}) {
		t.Error("folded groups are not groups")
	}
	if IsGroup(app.SpaceEntry{Kind: app.SpaceDir}) || IsGroup(app.SpaceEntry{Kind: app.SpaceFile}) {
		t.Error("a folder or a file is a group")
	}
}

func TestShares(t *testing.T) {
	for _, c := range []struct {
		part, whole int64
		want        float64
	}{{3, 4, 0.75}, {5, 0, 0}, {-1, 10, 0}, {12, 10, 1}} {
		if got := ShareOf(c.part, c.whole); got != c.want {
			t.Errorf("ShareOf(%d, %d) = %v, want %v", c.part, c.whole, got, c.want)
		}
	}
	for _, c := range []struct {
		share float64
		want  string
	}{{0, "0%"}, {0.0004, "<0.1%"}, {0.004, "0.4%"}, {0.05, "5%"}, {0.0951, "9.5%"}, {0.42, "42%"}, {0.125, "13%"}, {1, "100%"}} {
		if got := FormatShare(c.share); got != c.want {
			t.Errorf("FormatShare(%v) = %q, want %q", c.share, got, c.want)
		}
	}
}

func TestVolumeUsageAndFileCount(t *testing.T) {
	u := UsageOf(app.SpaceVolume{Total: 500e9, Free: 120e9})
	if u.Used != 380e9 || u.Share != 0.76 {
		t.Errorf("usage %+v", u)
	}
	if want := "可用 " + spacefake.Size(120e9) + "，共 " + spacefake.Size(500e9); u.Text != want {
		t.Errorf("usage text %q", u.Text)
	}
	if got := UsedText(u.Share); got != "已用 76%" {
		t.Errorf("UsedText = %q", got)
	}
	if u := UsageOf(app.SpaceVolume{Total: 10, Free: 20}); u.Used != 0 || u.Share != 0 {
		t.Errorf("more free than total %+v", u)
	}
	if got := FileCount(1204); got != "1,204 个文件" {
		t.Errorf("FileCount = %q", got)
	}
}

// The treemap's layout (treemapLayout.test.ts).

type item struct {
	name string
	size int64
}

func sizeOf(i item) int64 { return i.size }

func overlaps(a, b Cell[item]) bool {
	return a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height
}

func TestLayoutTreemapInsideWithoutOverlaps(t *testing.T) {
	cells := LayoutTreemap([]item{{"c", 10}, {"a", 60}, {"b", 30}}, sizeOf, 400, 300)
	var names []string
	for _, c := range cells {
		names = append(names, c.Item.name)
		if c.X < 0 || c.Y < 0 || c.X+c.Width > 400 || c.Y+c.Height > 300 {
			t.Errorf("cell %+v outside the area", c)
		}
		if c.X != float64(int(c.X)) || c.Width != float64(int(c.Width)) {
			t.Errorf("cell %+v not on whole points", c)
		}
	}
	if len(names) != 3 || names[0] != "a" || names[1] != "b" || names[2] != "c" {
		t.Errorf("order %v", names)
	}
	for i := range cells {
		for j := i + 1; j < len(cells); j++ {
			if overlaps(cells[i], cells[j]) {
				t.Errorf("%+v overlaps %+v", cells[i], cells[j])
			}
		}
	}
}

func TestLayoutTreemapProportions(t *testing.T) {
	cells := LayoutTreemap([]item{{"big", 75}, {"small", 25}}, sizeOf, 400, 200)
	area := func(name string) float64 {
		for _, c := range cells {
			if c.Item.name == name {
				return c.Width * c.Height
			}
		}
		t.Fatalf("no cell %s", name)
		return 0
	}
	// The gap takes a little of the area; the ratio stays close to 3.
	if r := area("big") / area("small"); r <= 2.7 || r >= 3.3 {
		t.Errorf("ratio %v", r)
	}
	// Two cells side by side leave exactly one gap between them.
	first, second := cells[0], cells[1]
	if second.X < first.X || (second.X == first.X && second.Y < first.Y) {
		first, second = second, first
	}
	gap := second.Y - (first.Y + first.Height)
	if first.Y == second.Y {
		gap = second.X - (first.X + first.Width)
	}
	if gap != TreemapGap {
		t.Errorf("gap %v", gap)
	}
}

func TestLayoutTreemapEmpty(t *testing.T) {
	if got := LayoutTreemap([]item{{"zero", 0}}, sizeOf, 400, 300); len(got) != 0 {
		t.Errorf("zero size %+v", got)
	}
	if got := LayoutTreemap([]item{{"a", 5}}, sizeOf, 0, 300); len(got) != 0 {
		t.Errorf("no width %+v", got)
	}
	if got := LayoutTreemap(nil, sizeOf, 400, 300); len(got) != 0 {
		t.Errorf("no items %+v", got)
	}
	cells := LayoutTreemap([]item{{"a", 5}, {"none", 0}}, sizeOf, 100, 100)
	if len(cells) != 1 || cells[0].X != 0 || cells[0].Y != 0 || cells[0].Width != 100 || cells[0].Height != 100 {
		t.Errorf("one cell %+v", cells)
	}
}

func TestCellShowsLabel(t *testing.T) {
	if !CellShowsLabel(120, 60) || CellShowsLabel(40, 60) || CellShowsLabel(120, 20) {
		t.Error("label thresholds")
	}
}

// TestLayoutTreemapMatchesD3 compares the port with what d3-hierarchy 3 lays out for the same sizes (testdata/treemap_d3.json, made with d3's treemap().tile(treemapSquarify).paddingInner(3).round(true)), up to the 302 cells of a large folder.
func TestLayoutTreemapMatchesD3(t *testing.T) {
	data, err := os.ReadFile("testdata/treemap_d3.json")
	if err != nil {
		t.Fatal(err)
	}
	var cases []struct {
		Sizes []float64    `json:"sizes"`
		W     float64      `json:"w"`
		H     float64      `json:"h"`
		Cells [][5]float64 `json:"cells"`
	}
	if err := json.Unmarshal(data, &cases); err != nil {
		t.Fatal(err)
	}
	for n, c := range cases {
		items := make([]int, len(c.Sizes))
		for i := range items {
			items[i] = i
		}
		got := LayoutTreemap(items, func(i int) int64 { return int64(c.Sizes[i]) }, c.W, c.H)
		if len(got) != len(c.Cells) {
			t.Fatalf("case %d: %d cells, d3 has %d", n, len(got), len(c.Cells))
		}
		for i, want := range c.Cells {
			g := got[i]
			if float64(g.Item) != want[0] || g.X != want[1] || g.Y != want[2] || g.Width != want[3] || g.Height != want[4] {
				t.Errorf("case %d cell %d: %+v, d3 %v", n, i, g, want)
			}
		}
	}
}

func TestTrashToast(t *testing.T) {
	moved := app.SpaceTrashResult{Trashed: 3.1e9, Items: 1, Failed: []app.SpaceFailure{}, RunID: "run-1", Undoable: true}
	movedText := "已移到" + spacefake.Trash + " " + spacefake.Size(3.1e9)
	restore := "，需要时可以在" + spacefake.Trash + "里还原"
	cases := []struct {
		name string
		res  app.SpaceTrashResult
		undo bool
		want store.Toast
	}{
		{"undoable", moved, true, store.Toast{Text: movedText, UndoRun: "run-1", Duration: 10 * time.Second}},
		{"some not undoable", with(moved, func(r *app.SpaceTrashResult) { r.NotUndoable = 1 }), true,
			store.Toast{Text: movedText + "，其中 1 项不能撤销", UndoRun: "run-1", Duration: 10 * time.Second}},
		{"none undoable", with(moved, func(r *app.SpaceTrashResult) { r.NotUndoable, r.Undoable = 1, false }), true,
			store.Toast{Text: movedText + "，其中 1 项不能撤销", Duration: 10 * time.Second}},
		{"some left", with(moved, func(r *app.SpaceTrashResult) {
			r.Failed = []app.SpaceFailure{{Path: "/a", Reason: "x"}, {Path: "/b", Reason: "y"}}
		}), true,
			store.Toast{Text: movedText + "，2 项未能处理", UndoRun: "run-1", ViewRun: "run-1", Duration: 10 * time.Second}},
		{"recycle bin", with(moved, func(r *app.SpaceTrashResult) { r.Undoable = false }), false,
			store.Toast{Text: movedText + restore, Duration: 6 * time.Second}},
		{"recycle bin, some left", with(moved, func(r *app.SpaceTrashResult) {
			r.Undoable, r.Failed = false, []app.SpaceFailure{{Path: "/a", Reason: "x"}}
		}), false,
			store.Toast{Text: movedText + restore, ViewRun: "run-1", Duration: 6 * time.Second}},
	}
	for _, c := range cases {
		if got := TrashToast(c.res, c.undo); got != c.want {
			t.Errorf("%s: %+v, want %+v", c.name, got, c.want)
		}
	}
}

func with[T any](v T, fn func(*T)) T {
	fn(&v)
	return v
}

func TestGrowthText(t *testing.T) {
	now := time.Date(2026, 11, 15, 14, 0, 0, 0, time.Local)
	cases := []struct {
		since time.Time
		want  string
	}{
		{now.Add(-30 * time.Minute), "1 小时前"},
		{now.Add(-6 * time.Hour), "6 小时前"},
		{now.Add(-5 * 24 * time.Hour), "周二"},
		{now.Add(-20 * 24 * time.Hour), "10月26日"},
		{now.AddDate(-1, 0, -1), "2025年11月14日"},
	}
	for _, c := range cases {
		if got := GrowthWhen(c.since, now); got != c.want {
			t.Errorf("GrowthWhen(%v) = %q, want %q", c.since, got, c.want)
		}
	}
	g := growth.Growth{Path: "/Users/me/Library/Caches/com.apple.Safari", Before: 1e9, After: 7e9, Since: now.Add(-5 * 24 * time.Hour)}
	if got := GrowthSentence(g, now); got != "从周二到现在涨了 "+spacefake.Size(6e9) {
		t.Errorf("sentence %q", got)
	}
	g.AtLeast = true
	if got := GrowthSentence(g, now); got != "从周二到现在至少涨了 "+spacefake.Size(6e9) {
		t.Errorf("sentence %q", got)
	}
	if got := GrowthName(g, "/Users/me"); got != spacefake.Native("~/Library/Caches/com.apple.Safari") {
		t.Errorf("name %q", got)
	}
	if got := GrowthName(growth.Growth{Path: "/Users/me", Label: ""}, "/Users/me"); got != "~" {
		t.Errorf("home %q", got)
	}
	if got := GrowthName(growth.Growth{Path: "/Users/me/Downloads", Label: "下载"}, "/Users/me"); got != "下载" {
		t.Errorf("label %q", got)
	}
	if got := GrowthName(growth.Growth{Path: "/Users/meme/x"}, "/Users/me"); got != "/Users/meme/x" {
		t.Errorf("outside home %q", got)
	}
	for w, want := range map[GrowthWindow]string{GrowthDay: "最近 24 小时没有明显变大的位置。", GrowthWeek: "最近 7 天没有明显变大的位置。", GrowthMonth: "最近 30 天没有明显变大的位置。"} {
		if got := GrowthNone(w); got != want {
			t.Errorf("GrowthNone(%v) = %q", w, got)
		}
	}
}
