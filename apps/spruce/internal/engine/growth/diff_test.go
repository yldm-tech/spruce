package growth

import (
	"path/filepath"
	"slices"
	"testing"
	"time"
)

const gb = 1 << 30

var t0 = time.Date(2026, 11, 10, 9, 0, 0, 0, time.UTC)

func TestKeep(t *testing.T) {
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	var times []time.Time
	// Two a day for 100 days.
	for d := range 100 {
		day := now.AddDate(0, 0, -d)
		times = append(times, day.Add(-1*time.Hour), day.Add(-3*time.Hour))
	}
	r := Retention{Daily: 14, Weekly: 12, MaxFiles: 40, MaxBytes: 8 << 20}
	keep := Keep(times, now, r)
	if !slices.IsSortedFunc(keep, func(a, b time.Time) int { return a.Compare(b) }) {
		t.Fatalf("not oldest first: %v", keep)
	}
	if got := keep[len(keep)-1]; !got.Equal(now.Add(-time.Hour)) {
		t.Errorf("newest %v", got)
	}
	days := map[string]int{}
	for _, k := range keep {
		if now.Sub(k) > MaxAge {
			t.Errorf("kept %v, older than 90 days", k)
		}
		days[k.Format("2006-01-02")]++
	}
	for d, n := range days {
		if n > 1 {
			t.Errorf("%d kept on %s", n, d)
		}
	}
	// The 14 days, then one a week for the rest of the 12 weeks.
	for d := range 14 {
		if days[now.AddDate(0, 0, -d).Format("2006-01-02")] != 1 {
			t.Errorf("day %d not kept", d)
		}
	}
	if len(keep) < 14+8 || len(keep) > 14+12 {
		t.Errorf("kept %d: %v", len(keep), keep)
	}
	if got := Keep(times, now, Retention{Daily: 14, Weekly: 12, MaxFiles: 5}); len(got) != 5 || !got[4].Equal(now.Add(-time.Hour)) {
		t.Errorf("max files: %v", got)
	}
	// The newest stays even when every bucket is empty.
	old := []time.Time{now.AddDate(0, 0, -30)}
	if got := Keep(old, now, Retention{Daily: 1}); len(got) != 1 {
		t.Errorf("the only snapshot went: %v", got)
	}
	if got := Keep([]time.Time{now.AddDate(0, 0, -91)}, now, DefaultRetention()); len(got) != 0 {
		t.Errorf("a snapshot older than 90 days was kept: %v", got)
	}
}

func TestBaseline(t *testing.T) {
	now := t0
	times := []time.Time{now.Add(-10 * 24 * time.Hour), now.Add(-6 * 24 * time.Hour), now.Add(-30 * time.Hour), now.Add(-2 * time.Hour)}
	cases := []struct {
		ago  time.Duration
		want time.Time
	}{
		{24 * time.Hour, times[2]},
		{7 * 24 * time.Hour, times[0]},
		{30 * 24 * time.Hour, times[0]},
		{time.Hour, times[3]},
	}
	for _, c := range cases {
		got, ok := Baseline(times, now, c.ago)
		if !ok || !got.Equal(c.want) {
			t.Errorf("Baseline(%v) = %v, want %v", c.ago, got, c.want)
		}
	}
	if _, ok := Baseline(nil, now, time.Hour); ok {
		t.Error("a baseline among none")
	}
}

func snap(at time.Time, locs ...Location) Snapshot {
	return Snapshot{Version: Version, At: at, Locations: locs}
}

func TestDiffThresholds(t *testing.T) {
	old := snap(t0,
		Location{ID: "downloads", Path: "/u/Downloads", Bytes: 10 * gb},
		Location{ID: "movies", Path: "/u/Movies", Bytes: 100 * gb},
		Location{ID: "npm", Path: "/u/.npm", Bytes: gb},
		Location{ID: "go", Path: "/u/go", Bytes: 5 * gb},
	)
	cur := snap(t0.Add(48*time.Hour),
		// +2 GB of 10: listed.
		Location{ID: "downloads", Path: "/u/Downloads", Bytes: 12 * gb},
		// +5 GB of 100: under 10%.
		Location{ID: "movies", Path: "/u/Movies", Bytes: 105 * gb},
		// +0.9 GB: under 1 GB.
		Location{ID: "npm", Path: "/u/.npm", Bytes: gb + 900<<20},
		// Smaller.
		Location{ID: "go", Path: "/u/go", Bytes: gb},
	)
	got := Diff(old, cur, 0)
	if len(got) != 1 {
		t.Fatalf("rows %+v", got)
	}
	g := got[0]
	if g.Path != "/u/Downloads" || g.Label != "下载" || g.Icon != "installer" || g.Before != 10*gb || g.After != 12*gb || !g.Since.Equal(t0) || g.AtLeast {
		t.Errorf("row %+v", g)
	}
}

func TestDiffChildReplacesItsLocation(t *testing.T) {
	old := snap(t0, Location{ID: "library-caches", Path: "/u/Library/Caches", Bytes: 20 * gb,
		Children: []Entry{{"com.apple.Safari", 2 * gb}, {"com.spotify.client", gb}}})
	cur := snap(t0.Add(72*time.Hour), Location{ID: "library-caches", Path: "/u/Library/Caches", Bytes: 27 * gb,
		Children: []Entry{{"com.apple.Safari", 8 * gb}, {"com.spotify.client", 2 * gb}}})
	got := Diff(old, cur, 0)
	if len(got) != 1 {
		t.Fatalf("rows %+v", got)
	}
	if g := got[0]; g.Path != filepath.Join("/u/Library/Caches", "com.apple.Safari") || g.Before != 2*gb || g.After != 8*gb || g.Icon != "cache" || g.Label != "" {
		t.Errorf("row %+v", g)
	}

	// No child explains most of it: the location lists.
	cur = snap(t0.Add(72*time.Hour), Location{ID: "library-caches", Path: "/u/Library/Caches", Bytes: 24 * gb,
		Children: []Entry{{"com.apple.Safari", 2*gb + 900<<20}, {"com.spotify.client", gb + 900<<20}, {"a", 700 << 20}, {"b", 700 << 20}}})
	got = Diff(old, cur, 0)
	if len(got) != 1 || got[0].Path != "/u/Library/Caches" {
		t.Errorf("rows %+v", got)
	}
}

func TestDiffChildrenOfALocationThatDidNotGrowEnough(t *testing.T) {
	// Application Support is large: 3 GB more is under 10% of it, but one child doubled.
	old := snap(t0, Location{ID: "library-application-support", Path: "/u/AS", Bytes: 200 * gb, Children: []Entry{{"Steam", 3 * gb}, {"Other", 100 * gb}}})
	cur := snap(t0.Add(time.Hour), Location{ID: "library-application-support", Path: "/u/AS", Bytes: 203 * gb, Children: []Entry{{"Steam", 6 * gb}, {"Other", 100 * gb}}})
	got := Diff(old, cur, 0)
	if len(got) != 1 || got[0].Path != filepath.Join("/u/AS", "Steam") || got[0].Before != 3*gb {
		t.Errorf("rows %+v", got)
	}
}

func TestDiffAtLeast(t *testing.T) {
	old := snap(t0, Location{ID: "dev-root:0", Path: "/u/code", Bytes: 5 * gb, Children: []Entry{{"big", 5 * gb}}})
	cur := snap(t0.Add(time.Hour), Location{ID: "dev-root:0", Path: "/u/code", Bytes: 8 * gb, Children: []Entry{{"big", 5 * gb}, {"new", 3 * gb}}})
	got := Diff(old, cur, 0)
	if len(got) != 1 {
		t.Fatalf("rows %+v", got)
	}
	if g := got[0]; g.Path != filepath.Join("/u/code", "new") || !g.AtLeast || g.Before != ChildMin || g.After != 3*gb || g.Icon != "build" {
		t.Errorf("row %+v", g)
	}

	// With MaxChildren listed before, a child new to the list was below the smallest of them.
	var children []Entry
	for i := range MaxChildren {
		children = append(children, Entry{Name: string(rune('a'+i%26)) + string(rune('a'+i/26)), Bytes: int64(MaxChildren-i) * 100 << 20})
	}
	smallest := children[len(children)-1].Bytes
	old = snap(t0, Location{ID: "dev-root:0", Path: "/u/code", Bytes: 600 * gb, Children: children})
	cur = snap(t0.Add(time.Hour), Location{ID: "dev-root:0", Path: "/u/code", Bytes: 600*gb + 2*gb, Children: append(slices.Clone(children), Entry{"new", 2 * gb})})
	got = Diff(old, cur, 0)
	if len(got) != 1 || got[0].Before != smallest || !got[0].AtLeast {
		t.Errorf("rows %+v", got)
	}
}

func TestDiffLeavesOutWhatCannotCompare(t *testing.T) {
	old := snap(t0,
		Location{ID: "desktop", Path: "/u/Desktop", Skipped: true},
		Location{ID: "documents", Path: "/u/Documents", Bytes: gb},
		Location{ID: "trash", Path: "/u/.Trash", Denied: 1},
		Location{ID: "library-caches", Path: "/u/Library/Caches", Bytes: 5 * gb, Denied: 40},
		Location{ID: "dev-root:0", Path: "/u/code", Bytes: gb},
	)
	cur := snap(t0.Add(time.Hour),
		// Skipped before.
		Location{ID: "desktop", Path: "/u/Desktop", Bytes: 50 * gb},
		// Skipped now.
		Location{ID: "documents", Path: "/u/Documents", Skipped: true},
		// Not readable before.
		Location{ID: "trash", Path: "/u/.Trash", Bytes: 9 * gb},
		// Fewer folders denied now: read more fully, not grown.
		Location{ID: "library-caches", Path: "/u/Library/Caches", Bytes: 15 * gb, Denied: 2},
		// Another folder under the same ID.
		Location{ID: "dev-root:0", Path: "/u/other", Bytes: 9 * gb},
		// New.
		Location{ID: "npm", Path: "/u/.npm", Bytes: 9 * gb},
	)
	if got := Diff(old, cur, 0); len(got) != 0 {
		t.Errorf("rows %+v", got)
	}
}

func TestDiffNestedAndOrder(t *testing.T) {
	dev := "/u/Library/Developer"
	dd := filepath.Join(dev, "Xcode", "DerivedData")
	old := snap(t0,
		Location{ID: "library-developer", Path: dev, Bytes: 20 * gb, Children: []Entry{{"Xcode", 15 * gb}, {"CoreSimulator", 5 * gb}}},
		Location{ID: "xcode-derived-data", Path: dd, Bytes: 10 * gb},
		Location{ID: "downloads", Path: "/u/Downloads", Bytes: 2 * gb},
		Location{ID: "npm", Path: "/u/.npm", Bytes: 2 * gb},
	)
	cur := snap(t0.Add(time.Hour),
		Location{ID: "library-developer", Path: dev, Bytes: 30 * gb, Children: []Entry{{"Xcode", 25 * gb}, {"CoreSimulator", 5 * gb}}},
		Location{ID: "xcode-derived-data", Path: dd, Bytes: 19 * gb},
		Location{ID: "downloads", Path: "/u/Downloads", Bytes: 5 * gb},
		Location{ID: "npm", Path: "/u/.npm", Bytes: 4 * gb},
	)
	got := Diff(old, cur, 0)
	var paths []string
	for _, g := range got {
		paths = append(paths, g.Path)
	}
	if want := []string{dd, "/u/Downloads", "/u/.npm"}; !slices.Equal(paths, want) {
		t.Fatalf("paths %v, want %v", paths, want)
	}
	if got[0].Label != "Xcode 编译缓存" || got[0].Icon != "xcode" {
		t.Errorf("derived data %+v", got[0])
	}
	if got := Diff(old, cur, 2); len(got) != 2 {
		t.Errorf("n=2 gave %d", len(got))
	}
}

func TestDiffPathListsOnce(t *testing.T) {
	// %TEMP% is a child of %LOCALAPPDATA% and a watch of its own.
	local := `/u/AppData/Local`
	temp := filepath.Join(local, "Temp")
	old := snap(t0, Location{ID: "local-appdata", Path: local, Bytes: 10 * gb, Children: []Entry{{"Temp", 2 * gb}}}, Location{ID: "temp", Path: temp, Bytes: 2 * gb})
	cur := snap(t0.Add(time.Hour), Location{ID: "local-appdata", Path: local, Bytes: 14 * gb, Children: []Entry{{"Temp", 6 * gb}}}, Location{ID: "temp", Path: temp, Bytes: 6 * gb})
	got := Diff(old, cur, 0)
	if len(got) != 1 || got[0].Path != temp || got[0].Label != "临时文件" {
		t.Errorf("rows %+v", got)
	}
}
