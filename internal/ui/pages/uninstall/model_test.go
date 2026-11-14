package uninstall

import (
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

var now = time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)

func daysAgo(n int) time.Time { return now.Add(-time.Duration(n) * 24 * time.Hour) }

func testApps() []app.UninstallApp {
	return []app.UninstallApp{
		{ID: "a", Name: "Zed", BundleID: "dev.zed.Zed"},
		{ID: "b", Name: "Arc", BundleID: "company.thebrowser.Browser", InstallDate: time.Date(2024, 1, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "c", Name: "微信", BundleID: "com.tencent.xinWeChat", Publisher: "Tencent", InstallDate: time.Date(2025, 6, 1, 0, 0, 0, 0, time.UTC)},
		{ID: "d", Name: "Blender", BundleID: "org.blenderfoundation.blender"},
	}
}

func testDetails() map[string]AppDetails {
	return map[string]AppDetails{
		"a": {Size: 300, HasSize: true, Usage: app.UninstallUsed, LastUsed: daysAgo(2)},
		"b": {Size: 900, HasSize: true, Usage: app.UninstallNeverUsed},
		"c": {Usage: app.UninstallUsed, LastUsed: daysAgo(30)},
		"d": {Size: 300, HasSize: true, Usage: app.UninstallUsageUnknown},
	}
}

func ids(apps []app.UninstallApp) string {
	out := make([]string, len(apps))
	for i, a := range apps {
		out[i] = a.ID
	}
	return strings.Join(out, ",")
}

func TestVisibleAppsSorts(t *testing.T) {
	apps, details := testApps(), testDetails()
	for _, tc := range []struct {
		sort    SortKey
		windows bool
		want    string
	}{
		{SortName, false, "c,b,d,a"},
		{SortSize, false, "b,d,a,c"},
		{SortRecent, false, "a,c,b,d"},
		// Windows orders by install date.
		{SortRecent, true, "c,b,d,a"},
	} {
		if got := ids(VisibleApps(apps, details, "", tc.sort, tc.windows)); got != tc.want {
			t.Errorf("sort %d windows %v = %s, want %s", tc.sort, tc.windows, got, tc.want)
		}
	}
}

func TestVisibleAppsSearches(t *testing.T) {
	apps, details := testApps(), testDetails()
	for query, want := range map[string]string{" ZED ": "a", "thebrowser": "b", "tencent": "c", "nothing": ""} {
		if got := ids(VisibleApps(apps, details, query, SortName, false)); got != want {
			t.Errorf("search %q = %s, want %s", query, got, want)
		}
	}
}

func TestVisibleAppsKeepsItsInput(t *testing.T) {
	input := testApps()
	VisibleApps(input, testDetails(), "", SortSize, false)
	if ids(input) != ids(testApps()) {
		t.Errorf("input reordered: %s", ids(input))
	}
}

func TestNameOrderFollowsTheChineseCollator(t *testing.T) {
	apps := []app.UninstallApp{}
	for _, n := range []string{"aDrive", "指尖冒险 TapTapGo", "app10", "水杉词典", "AIME Settings", "app9", "汽水音乐", "指尖冒险", "火山方舟", "Foo A", "Foo 微"} {
		apps = append(apps, app.UninstallApp{ID: n, Name: n})
	}
	want := "火山方舟,汽水音乐,水杉词典,指尖冒险,指尖冒险 TapTapGo,aDrive,AIME Settings,app9,app10,Foo 微,Foo A"
	if got := ids(VisibleApps(apps, nil, "", SortName, false)); got != want {
		t.Errorf("order %s", got)
	}
}

func TestMergeDetailsFieldByField(t *testing.T) {
	zero := int64(0)
	m := MergeDetails(nil, []app.UninstallAppDetails{{ID: "a", Usage: app.UninstallNeverUsed}})
	before := m
	m = MergeDetails(m, []app.UninstallAppDetails{{ID: "a", Icon: "data:x"}, {ID: "a", Size: &zero}})
	if got := m["a"]; got != (AppDetails{Usage: app.UninstallNeverUsed, Icon: "data:x", Size: 0, HasSize: true}) {
		t.Errorf("merged %+v", got)
	}
	if before["a"].Icon != "" {
		t.Error("the earlier map changed")
	}
}

func TestUsageAndSizeText(t *testing.T) {
	apps, details := testApps(), testDetails()
	if got := UsageText(apps[0], details["a"], false, now); !strings.HasSuffix(got, "使用过") {
		t.Errorf("used %q", got)
	}
	for _, tc := range []struct {
		app     app.UninstallApp
		d       AppDetails
		windows bool
		want    string
	}{
		{apps[1], details["b"], false, "从未使用"},
		{apps[3], details["d"], false, ""},
		{apps[3], AppDetails{}, false, ""},
		{apps[0], AppDetails{}, true, ""},
	} {
		if got := UsageText(tc.app, tc.d, tc.windows, now); got != tc.want {
			t.Errorf("usage of %s = %q, want %q", tc.app.Name, got, tc.want)
		}
	}
	if got := UsageText(apps[1], AppDetails{}, true, now); !strings.HasPrefix(got, "安装于 ") {
		t.Errorf("installed %q", got)
	}
	for _, tc := range []struct {
		d         AppDetails
		measuring bool
		want      string
	}{
		{AppDetails{HasSize: true}, true, "0 B"},
		{AppDetails{}, true, "正在计算…"},
		{AppDetails{}, false, "大小未知"},
	} {
		if got := SizeText(tc.d, tc.measuring, false); got != tc.want {
			t.Errorf("size %q, want %q", got, tc.want)
		}
	}
}

func leftover(id string, size int64, selected bool) app.UninstallLeftover {
	return app.UninstallLeftover{ID: id, Path: "/Users/me/Library/Caches/" + id, Kind: app.UninstallKindCaches, Size: size, Dir: true, Selected: selected}
}

func TestLeftovers(t *testing.T) {
	ls := []app.UninstallLeftover{leftover("x", 1000, true), leftover("y", 2000, false), leftover("z", 3000, true)}
	checked := DefaultChecked(ls)
	if got := CheckedIDs(ls, checked); !slices.Equal(got, []string{"x", "z"}) {
		t.Errorf("checked %v", got)
	}
	if n, size := CheckedTotals(ls, checked); n != 2 || size != 4000 {
		t.Errorf("totals %d %d", n, size)
	}
	if got := ConfirmMessage("Foo", 2, 1_500_000, false); got != "「Foo」和勾选的 2 个相关文件（共 1.5 MB）会移到废纸篓，可以随时放回。" {
		t.Errorf("message %q", got)
	}
	if got := ConfirmMessage("Foo", 0, 1000, false); got != "「Foo」（共 1 KB）会移到废纸篓，可以随时放回。" {
		t.Errorf("message %q", got)
	}
}

func result(r cleanup.CleanResult) cleanup.CleanResult {
	if r.Failed == nil {
		r.Failed = []cleanup.Failure{}
	}
	r.StartedAt, r.Duration = time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC), time.Second
	return r
}

func TestRemoveSummaryReportsTheAppAndItsLeftovers(t *testing.T) {
	s := RemoveSummary(app.UninstallOutcome{App: "Foo", BundleRemoved: true, Result: result(cleanup.CleanResult{
		Trashed: 5_000_000, Removed: 3,
		Categories: []cleanup.CategoryOutcome{
			{CategoryID: "app", Title: "app", Removed: 1, Trashed: 4_000_000},
			{CategoryID: "caches", Title: "caches", Removed: 2, Trashed: 1_000_000, Failed: 1},
		},
		Failed: []cleanup.Failure{{ItemID: "f", CategoryID: "caches", Path: "/x/Caches/com.foo", Reason: "没有权限删除"}},
	})}, false)
	if s.Title != "已卸载「Foo」" || s.Big != "5 MB" || !s.Succeeded {
		t.Errorf("summary %+v", s)
	}
	want := []string{"「Foo」已移到废纸篓", "相关文件：2 项已移到废纸篓", "相关文件：1 项未能处理", "com.foo：没有权限删除", "移到废纸篓的项目在清空废纸篓后才会释放空间"}
	if !slices.Equal(s.Lines, want) {
		t.Errorf("lines %q", s.Lines)
	}
}

func TestRemoveSummaryExplainsAnAppThatStayed(t *testing.T) {
	s := RemoveSummary(app.UninstallOutcome{App: "Foo", BundleFailure: "需要管理员权限。", Result: result(cleanup.CleanResult{})}, false)
	want := DoneSummary{Title: "没能卸载「Foo」", Big: "未卸载", Lines: []string{"需要管理员权限。", "它的相关文件也没有改动。"}}
	if s.Title != want.Title || s.Big != want.Big || s.Succeeded || !slices.Equal(s.Lines, want.Lines) {
		t.Errorf("summary %+v", s)
	}
}

func TestRemoveSummarySaysWhenARemovalStopped(t *testing.T) {
	s := RemoveSummary(app.UninstallOutcome{App: "Foo", BundleRemoved: true, Result: result(cleanup.CleanResult{Interrupted: true})}, false)
	if s.Title != "卸载已中断" || !slices.Contains(s.Lines, "卸载在中途停止，其余项目没有改动") {
		t.Errorf("summary %+v", s)
	}
}

func TestRemoveSummaryLimitsTheFailures(t *testing.T) {
	var failed []cleanup.Failure
	for i := range 10 {
		failed = append(failed, cleanup.Failure{Path: "/x/" + string(rune('a'+i)), Reason: "被占用"})
	}
	s := RemoveSummary(app.UninstallOutcome{App: "Foo", BundleRemoved: true, Result: result(cleanup.CleanResult{Failed: failed, Skipped: 2})}, false)
	if !slices.Contains(s.Lines, "另有 2 项未能处理") || !slices.Contains(s.Lines, "2 项在查找后已不存在，已跳过") || slices.Contains(s.Lines, "i：被占用") {
		t.Errorf("lines %q", s.Lines)
	}
}

func TestWindowsSummaries(t *testing.T) {
	s := RemoveSummary(app.UninstallOutcome{App: "Foo", Result: result(cleanup.CleanResult{Trashed: 2000, Removed: 1,
		Categories: []cleanup.CategoryOutcome{{CategoryID: "appData", Title: "appData", Removed: 1, Trashed: 2000}}})}, true)
	if len(s.Lines) < 2 || s.Lines[0] != "卸载程序已移除「Foo」" || s.Lines[1] != "相关文件：1 项已移到回收站" || !s.Succeeded {
		t.Errorf("summary %+v", s)
	}
	if got := UninstalledSummary("Foo", 0).Lines; !slices.Equal(got, []string{"卸载程序已移除「Foo」", "没有找到它留下的文件"}) {
		t.Errorf("lines %q", got)
	}
	if got := UninstalledSummary("Foo", 3).Lines[1]; got != "留下的 3 个相关文件没有改动" {
		t.Errorf("line %q", got)
	}
}

func TestProgressLine(t *testing.T) {
	if got := ProgressLine(cleanup.Progress{Done: 1, Total: 2, Bytes: 100_000_000}, false); got != "已处理 1/2 · 已释放 100 MB" {
		t.Errorf("line %q", got)
	}
	if got := ProgressLine(cleanup.Progress{}, false); got != "正在准备…" {
		t.Errorf("line %q", got)
	}
}
