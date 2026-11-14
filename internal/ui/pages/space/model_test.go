package space

import (
	"encoding/json"
	"os"
	"runtime"
	"testing"

	"github.com/yldm-tech/spruce/internal/app"
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
	if runtime.GOOS != "windows" && u.Text != "可用 120 GB，共 500 GB" {
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
