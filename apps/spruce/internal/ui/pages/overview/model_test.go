package overview

import (
	"math"
	"reflect"
	"runtime"
	"slices"
	"testing"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/growth"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/settings"
	"spruce/apps/spruce/internal/ui/cleanflow"
)

var now = time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)

func skipOnWindows(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
}

// Ported from sources.test.ts: waits before the first event.
func TestSourceRowsBeforeTheFirstEvent(t *testing.T) {
	for _, r := range SourceRows(cleanup.Progress{}) {
		if r.Status != StatusWaiting || r.Known {
			t.Errorf("%s %+v", r.ID, r)
		}
		if got := RowStatusText(r); got != "等待开始…" {
			t.Errorf("%s %q", r.ID, got)
		}
	}
	if _, known := OverallFraction(cleanup.Progress{}); known {
		t.Error("overall known before the first event")
	}
}

// Ported from sources.test.ts: reads each source's state from the event.
func TestSourceRows(t *testing.T) {
	p := cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 1250, Total: 3000, Bytes: 10, Sources: []cleanup.SourceState{
		{ID: "system-junk", Status: cleanup.SourceDone, Done: 4, Total: 4, Bytes: 10, Items: 2},
		{ID: "dev-junk", Status: cleanup.SourceScanning, Done: 1, Total: 4},
		{ID: "installers", Status: cleanup.SourceScanning},
	}}
	type row struct {
		id       app.CleanerID
		status   RowStatus
		fraction float64
		known    bool
	}
	var got []row
	for _, r := range SourceRows(p) {
		got = append(got, row{r.ID, r.Status, r.Fraction, r.Known})
	}
	want := []row{
		{app.CleanerSystemJunk, "done", 1, true},
		{app.CleanerDevJunk, "scanning", 0.25, true},
		{app.CleanerInstallers, "scanning", 0, false},
	}
	if !reflect.DeepEqual(got, want) {
		t.Errorf("rows\n got %+v\nwant %+v", got, want)
	}
	if f, known := OverallFraction(p); !known || math.Abs(f-1250.0/3000) > 1e-9 {
		t.Errorf("overall %v %v", f, known)
	}
}

func TestRowStatusText(t *testing.T) {
	skipOnWindows(t)
	for _, tc := range []struct {
		row  SourceRow
		want string
	}{
		{SourceRow{Status: "scanning"}, "正在检查… 已找到 0 B"},
		{SourceRow{Status: "done", Items: 4, Bytes: 2_000_000}, "完成，找到 4 项，共 2 MB"},
		{SourceRow{Status: "done"}, "完成，没有找到可以清理的内容"},
		{SourceRow{Status: "failed", Error: "需要权限"}, "需要权限"},
		{SourceRow{Status: "failed"}, "没能检查完成"},
	} {
		if got := RowStatusText(tc.row); got != tc.want {
			t.Errorf("%+v: %q, want %q", tc.row, got, tc.want)
		}
	}
}

func TestConclusion(t *testing.T) {
	skipOnWindows(t)
	if ConclusionLabel(false) != "可以放心释放" || ConclusionLabel(true) != "空间不多了，可以放心释放" {
		t.Error("label")
	}
	recs := []advice.Recommendation{{Action: advice.ActionTrash, Bytes: 2_100_000_000}, {Action: advice.ActionDelete, Bytes: 17e9}, {Action: advice.ActionTrash, Bytes: 1e9}}
	if got := ConclusionSub(recs); got != "3 条建议，其中 3.1 GB 会先移到废纸篓" {
		t.Errorf("sub %q", got)
	}
	if got := ConclusionSub(recs[1:2]); got != "1 条建议" {
		t.Errorf("sub without trash %q", got)
	}
	at := now.Add(-3 * time.Hour)
	for _, tc := range []struct {
		stale, checking bool
		progress        cleanup.Progress
		want            string
	}{
		{false, false, cleanup.Progress{}, "上次检查：3小时前"},
		{true, false, cleanup.Progress{}, "上次检查：3小时前，结果可能已经变化"},
		{true, true, cleanup.Progress{}, "上次检查：3小时前 · 正在更新…"},
		{false, true, cleanup.Progress{Done: 47, Total: 100}, "上次检查：3小时前 · 正在更新… 47%"},
	} {
		if got := CheckedAtText(at, tc.stale, tc.checking, tc.progress, now); got != tc.want {
			t.Errorf("%+v: %q", tc, got)
		}
	}
	// A result that arrived after the page's clock last moved is 「刚刚」, not a date.
	if got := CheckedAtText(now.Add(30*time.Second), false, false, cleanup.Progress{}, now); got != "上次检查：刚刚" {
		t.Errorf("a result later than the clock: %q", got)
	}
}

// ReasonWith's time comes out in the page's words: relative for recent use, the date for the oldest log.
func TestReasonText(t *testing.T) {
	r := advice.Recommendation{Reason: "最近一次使用在 {time}", ReasonTime: now.Add(-3 * 24 * time.Hour), ReasonTimeKind: advice.TimeRelative}
	if got := ReasonText(r, now); got != "最近一次使用在 3天前" {
		t.Errorf("relative %q", got)
	}
	r = advice.Recommendation{Reason: "2 个文件，最早的来自 {time}", ReasonTime: time.Date(2026, 3, 2, 9, 0, 0, 0, time.Local), ReasonTimeKind: advice.TimeDate}
	if got := ReasonText(r, now); got != "2 个文件，最早的来自 3月2日" {
		t.Errorf("date %q", got)
	}
	r.ReasonTime = time.Date(2025, 3, 2, 9, 0, 0, 0, time.Local)
	if got := ReasonText(r, now); got != "2 个文件，最早的来自 2025年3月2日" {
		t.Errorf("date of another year %q", got)
	}
	if got := ReasonText(advice.Recommendation{Reason: "没有时间"}, now); got != "没有时间" {
		t.Errorf("plain %q", got)
	}
}

// The four actions: the button, its busy text, the badge and whether it asks.
func TestActions(t *testing.T) {
	skipOnWindows(t)
	type want struct {
		label, partial, busy string
		badge                Badge
		confirm, winConfirm  bool
	}
	for a, w := range map[advice.Action]want{
		advice.ActionTrash:   {"移到废纸篓", "移到废纸篓 · 3.1 GB", "正在移到废纸篓…", Badge{Text: "可撤销", Glyph: "check"}, false, true},
		advice.ActionDelete:  {"删除…", "删除… · 3.1 GB", "正在删除…", Badge{Text: "无法撤销", Glyph: "alert"}, true, true},
		advice.ActionCommand: {"运行清理…", "运行清理… · 3.1 GB", "正在运行…", Badge{Text: "运行命令", Glyph: "alert", Warning: true}, true, true},
		advice.ActionMixed:   {"清理…", "清理… · 3.1 GB", "正在清理…", Badge{Text: "部分无法撤销", Glyph: "alert"}, true, true},
	} {
		if got := ActionLabel(a, false, 3_100_000_000); got != w.label {
			t.Errorf("%s label %q", a, got)
		}
		if got := ActionLabel(a, true, 3_100_000_000); got != w.partial {
			t.Errorf("%s partial %q", a, got)
		}
		if got := BusyLabel(a); got != w.busy {
			t.Errorf("%s busy %q", a, got)
		}
		if got := BadgeFor(a, true); got != w.badge {
			t.Errorf("%s badge %+v", a, got)
		}
		if NeedsConfirm(a, true) != w.confirm || NeedsConfirm(a, false) != w.winConfirm {
			t.Errorf("%s confirm", a)
		}
	}
	if got := BadgeFor(advice.ActionTrash, false); got != (Badge{Text: "移到回收站", Glyph: "trash"}) {
		t.Errorf("windows badge %+v", got)
	}
}

func TestConfirmations(t *testing.T) {
	skipOnWindows(t)
	r := advice.Recommendation{Title: "Xcode 编译缓存", Consequence: "直接删除，下次编译会慢一些，Xcode 会重新建立索引"}
	d := DeleteConfirm(r, 1, 17_200_000_000)
	if d.Title != "删除Xcode 编译缓存？" || d.Message != "17.2 GB（1 项）会直接删除，不经过废纸篓，无法撤销。下次编译会慢一些，Xcode 会重新建立索引。" || !d.Danger || d.ConfirmLabel != "删除" {
		t.Errorf("delete %+v", d)
	}
	c := CommandConfirm("brew cleanup -s --prune=all", "运行 brew cleanup -s --prune=all：清理下载的安装包和旧版本", 2_000_000_000)
	if c.Title != "运行 brew cleanup？" || c.Message+c.Command+c.After != "将运行 brew cleanup -s --prune=all，清理下载的安装包和旧版本（约 2 GB），无法撤销。停止后，正在运行的命令会先完成。" ||
		!c.Danger || c.ConfirmLabel != "运行" {
		t.Errorf("command %+v", c)
	}
	for desc, want := range map[string]string{"运行 npm cache clean --force，清空 npm 下载的包": "清空 npm 下载的包", "下载过的包": "下载过的包"} {
		if got := commandEffect(desc); got != want {
			t.Errorf("effect of %q: %q", desc, got)
		}
	}
	w := RecycleConfirm(advice.Recommendation{Title: "下载里的安装包"}, 3, 2_100_000_000)
	if w.Title != "把下载里的安装包移到回收站？" || w.Message != "2.1 GB（3 项）会移到回收站，需要时可以在回收站里还原。" || w.Danger || w.ConfirmLabel != "移到回收站" {
		t.Errorf("recycle %+v", w)
	}
}

func TestCleanToast(t *testing.T) {
	skipOnWindows(t)
	inUse := cleanup.Failure{ItemID: "u", InUse: true}
	failed := cleanup.Failure{ItemID: "f", Reason: "没有权限"}
	for _, tc := range []struct {
		name    string
		a       advice.Action
		out     app.OverviewOutcome
		canUndo bool
		want    string
		undo    string
		view    string
		long    bool
	}{
		{"trash", advice.ActionTrash, app.OverviewOutcome{RunID: "r", Undoable: true, Result: cleanup.CleanResult{Trashed: 3_100_000_000}}, true,
			"已移到废纸篓 3.1 GB", "r", "", true},
		{"trash not undoable", advice.ActionTrash, app.OverviewOutcome{RunID: "r", Undoable: true, NotUndoable: 1, Result: cleanup.CleanResult{Trashed: 3_100_000_000}}, true,
			"已移到废纸篓 3.1 GB，其中 1 项不能撤销", "r", "", true},
		{"trash none undoable", advice.ActionTrash, app.OverviewOutcome{RunID: "r", NotUndoable: 2, Result: cleanup.CleanResult{Trashed: 1e9}}, true,
			"已移到废纸篓 1 GB，其中 2 项不能撤销", "", "", true},
		{"trash failed", advice.ActionTrash, app.OverviewOutcome{RunID: "r", Undoable: true, Result: cleanup.CleanResult{Trashed: 1e9, Failed: []cleanup.Failure{failed, inUse}}}, true,
			"已移到废纸篓 1 GB，1 项未能处理，1 项正在使用，已跳过", "r", "r", true},
		{"recycle bin", advice.ActionTrash, app.OverviewOutcome{RunID: "r", Result: cleanup.CleanResult{Trashed: 1e9}}, false,
			"已移到回收站 1 GB，需要时可以在回收站里还原", "", "", false},
		{"recycle bin failed", advice.ActionTrash, app.OverviewOutcome{RunID: "r", Result: cleanup.CleanResult{Trashed: 1e9, Failed: []cleanup.Failure{failed}}}, false,
			"已移到回收站 1 GB，需要时可以在回收站里还原，1 项未能处理", "", "r", false},
		{"delete", advice.ActionDelete, app.OverviewOutcome{RunID: "r", Result: cleanup.CleanResult{Freed: 17e9}}, true, "已释放 17 GB", "", "", false},
		{"command failed", advice.ActionCommand, app.OverviewOutcome{RunID: "r", Result: cleanup.CleanResult{Freed: 1e9, Failed: []cleanup.Failure{failed}}}, true,
			"已释放 1 GB，1 项未能处理", "", "r", false},
		{"mixed", advice.ActionMixed, app.OverviewOutcome{RunID: "r", Undoable: true, Result: cleanup.CleanResult{Freed: 1e9, Trashed: 400e6}}, true,
			"已释放 1 GB，移到废纸篓 400 MB", "r", "", true},
	} {
		got := CleanToast(tc.a, tc.out, tc.canUndo)
		long := got.Duration == toastLong
		if got.Text != tc.want || got.UndoRun != tc.undo || got.ViewRun != tc.view || long != tc.long || (!long && got.Duration != toastShort) {
			t.Errorf("%s: %+v", tc.name, got)
		}
	}
}

func ref(cat, id string) advice.ItemRef { return advice.ItemRef{CategoryID: cat, ItemID: id} }

// A card the user did not change follows each newer result; a changed one keeps what is still there and counts what the newer one adds.
func TestPick(t *testing.T) {
	cat := func(id string, items ...string) cleanup.Category {
		c := cleanup.Category{ID: id, Source: "system-junk", Selected: true}
		for _, it := range items {
			c.Items = append(c.Items, cleanup.Item{ID: it, Selected: true})
		}
		return c
	}
	scan := cleanup.ScanResult{Categories: []cleanup.Category{cat("a", "1", "2"), cat("b", "3")}}
	r := advice.Recommendation{ID: "c", Categories: []string{"a", "b"}, Items: []advice.ItemRef{ref("a", "1"), ref("a", "2"), ref("b", "3")}}
	ch := cleanflow.Choice{Sel: cleanflow.NothingSelected}.Reconcile(scan.Categories)
	if got := CardPick(r, ch, scan); !slices.Equal(got.Refs(r), r.Items) || got.Added != 0 {
		t.Fatalf("default %+v", got)
	}
	newerScan := cleanup.ScanResult{Categories: []cleanup.Category{cat("a", "1"), cat("b", "3", "4")}}
	newer := r
	newer.Items = []advice.ItemRef{ref("a", "1"), ref("b", "3"), ref("b", "4")}
	if got := CardPick(newer, ch.Reconcile(newerScan.Categories), newerScan); got.Added != 0 || !slices.Equal(got.Refs(newer), newer.Items) {
		t.Errorf("untouched %+v", got)
	}
	changed := ChangeCard(ch, r, scan, cleanflow.SetItem(CardPick(r, ch, scan).Sel, cleanup.Category{ID: "a"}, "1", false))
	if got := CardPick(r, changed, scan).Refs(r); !slices.Equal(got, []advice.ItemRef{ref("a", "2"), ref("b", "3")}) {
		t.Fatalf("changed %v", got)
	}
	changed = changed.Reconcile(newerScan.Categories)
	got := CardPick(newer, changed, newerScan)
	if got.Added != 1 || !slices.Equal(got.Refs(newer), []advice.ItemRef{ref("b", "3")}) {
		t.Errorf("touched %+v %v", got, got.Refs(newer))
	}
	// Added counts from the last change, again on the next result.
	if again := CardPick(newer, changed.Reconcile(newerScan.Categories), newerScan); again.Added != 1 {
		t.Errorf("again %+v", again)
	}
	if req := CleanRequest(newer, got); !slices.Equal(req.CategoryIDs, []string{"b"}) || !slices.Equal(req.ItemIDs, []string{"3"}) {
		t.Errorf("request %+v", req)
	}
}

func scanFor(t *testing.T) cleanup.ScanResult {
	t.Helper()
	trash := cleanup.Category{ID: "system-junk:trash", Kind: cleanup.KindTrash, Items: []cleanup.Item{{ID: "t", Size: 9e9}}}
	res := cleanup.NewResult([]cleanup.Category{
		{ID: "system-junk:app-caches", Source: "system-junk", Selected: true, Items: []cleanup.Item{{ID: "a", Size: 100}, {ID: "b", Size: 50}}},
		{ID: "system-junk:running", Source: "system-junk", Items: []cleanup.Item{{ID: "c", Size: 30}}},
		trash,
		{ID: "dev-junk:artifact-x", Source: "dev-junk", Items: []cleanup.Item{{ID: "d", Size: 200, Selected: true}, {ID: "e", Size: 300, Hold: "有改动"}}},
	}, now)
	res.Sources = []cleanup.SourceState{{ID: "system-junk", Status: cleanup.SourceDone}, {ID: "dev-junk", Status: cleanup.SourceDone},
		{ID: "installers", Status: cleanup.SourceFailed}}
	return res
}

func TestReviewAndBrowse(t *testing.T) {
	skipOnWindows(t)
	res := scanFor(t)
	r := ReviewOf(res)
	if r.Count != 2 || r.Bytes != 330 || !slices.Equal(r.Sources, []string{"system-junk", "dev-junk"}) {
		t.Errorf("review %+v", r)
	}
	if got := ReviewText(Review{Count: 2, Bytes: 3e9}); got != "还有 2 项默认不选（3 GB），比如正在运行的应用的缓存、最近下载的安装包、有未提交改动的项目，可能还要用" {
		t.Errorf("review text %q", got)
	}
	recs := []advice.Recommendation{
		{ID: "app-caches", Items: []advice.ItemRef{ref("system-junk:app-caches", "a"), ref("system-junk:app-caches", "b")}},
		{ID: "small", Items: []advice.ItemRef{ref("dev-junk:artifact-x", "d")}},
	}
	b := BrowseOf(app.OverviewResult{Scan: res, Recommendations: recs})
	want := []Browse{
		{Source: "system-junk", Bytes: 9_000_000_180, Items: 4, RecItems: 2, RecBytes: 150},
		{Source: "dev-junk", Bytes: 500, Items: 2, RecItems: 1, RecBytes: 200},
		{Source: "installers", Error: "没能检查完成"},
	}
	if !reflect.DeepEqual(b, want) {
		t.Errorf("browse\n got %+v\nwant %+v", b, want)
	}
	if got := BrowseNote(b[1]); got != "2 项，建议清理 1 项（200 B）" {
		t.Errorf("note %q", got)
	}
	if got := BrowseNote(Browse{}); got != "没有找到可以清理的内容" {
		t.Errorf("empty note %q", got)
	}
	if got := BrowseNote(b[2]); got != "没能检查完成" {
		t.Errorf("failed note %q", got)
	}
}

func TestCardCategories(t *testing.T) {
	res := scanFor(t)
	r := advice.Recommendation{Categories: []string{"dev-junk:artifact-x", "system-junk:app-caches"},
		Items: []advice.ItemRef{ref("dev-junk:artifact-x", "d"), ref("system-junk:app-caches", "b")}}
	cats := cardCategories(res, r)
	if len(cats) != 2 || cats[0].ID != "dev-junk:artifact-x" || len(cats[0].Items) != 1 || cats[0].Size != 200 || cats[1].Items[0].ID != "b" {
		t.Errorf("categories %+v", cats)
	}
	if Sum(res, r.Items) != 250 {
		t.Errorf("sum %d", Sum(res, r.Items))
	}
}

func TestSkippedNames(t *testing.T) {
	home := "/Users/me"
	got := SkippedNames(home, []string{home + "/Downloads", home + "/Desktop", home + "/Documents", home + "/Library/Mobile Documents",
		home + "/Library/CloudStorage/Dropbox-Personal", "/Volumes/T7", "/Volumes/Backup", home + "/Downloads"})
	want := []string{"下载", "桌面", "文稿", "iCloud 云盘", "Dropbox", "外接磁盘"}
	if !slices.Equal(got, want) {
		t.Errorf("names %v", got)
	}
	if got := SkippedText(want[:2]); got != "后台检查没有读取下载、桌面，这些位置要在你使用 Spruce 时才能检查。" {
		t.Errorf("text %q", got)
	}
}

func TestDisk(t *testing.T) {
	skipOnWindows(t)
	d := app.DiskState{Volume: "Macintosh HD", Free: 120e9, Total: 1000e9}
	if got := DiskText(d); got != "Macintosh HD · 可用 120 GB，共 1 TB" {
		t.Errorf("text %q", got)
	}
	if got := UsedFraction(d); math.Abs(got-0.88) > 1e-9 {
		t.Errorf("used %v", got)
	}
	if UsedFraction(app.DiskState{}) != 0 {
		t.Error("used of nothing")
	}
}

func TestGrowthWords(t *testing.T) {
	skipOnWindows(t)
	tuesday := time.Date(2026, 11, 10, 9, 0, 0, 0, time.Local) // a Tuesday
	for _, tc := range []struct {
		since time.Time
		want  string
	}{
		{now.Add(-3 * time.Hour), "3 小时前"},
		{now.Add(-10 * time.Minute), "1 小时前"},
		{tuesday, "周二"},
		{now.Add(-20 * 24 * time.Hour), "10月25日"},
	} {
		if got := GrowthWhen(tc.since, now); got != tc.want {
			t.Errorf("%v: %q, want %q", tc.since, got, tc.want)
		}
	}
	g := growth.Growth{Path: "/Users/me/Library/Caches/com.apple.Safari", Before: 1e9, After: 7e9, Since: tuesday}
	if got := GrowthSentence(g, now); got != "从周二到现在涨了 6 GB" {
		t.Errorf("sentence %q", got)
	}
	g.AtLeast = true
	if got := GrowthSentence(g, now); got != "从周二到现在至少涨了 6 GB" {
		t.Errorf("at least %q", got)
	}
	if got := GrowthName(g, "/Users/me"); got != "~/Library/Caches/com.apple.Safari" {
		t.Errorf("name %q", got)
	}
	g.Label = "下载"
	if got := GrowthName(g, "/Users/me"); got != "下载" {
		t.Errorf("label %q", got)
	}
	if got := GrowthName(growth.Growth{Path: "/opt/x"}, "/Users/me"); got != "/opt/x" {
		t.Errorf("outside home %q", got)
	}
}

func TestRules(t *testing.T) {
	var a settings.AutomationSettings
	for _, id := range []string{"app-caches", "installers", "build-output", "incomplete-downloads"} {
		if _, ok := Rules[id]; !ok {
			t.Errorf("no copy for %s", id)
		}
		if RuleOn(a, id) {
			t.Errorf("%s on", id)
		}
		on := WithRule(a, id)
		if !RuleOn(on, id) || !AnyRuleOn(on) {
			t.Errorf("%s not turned on", id)
		}
	}
	if AnyRuleOn(a) || AnyRuleOn(settings.AutomationSettings{WeeklySummary: true}) {
		t.Error("a rule on without one")
	}
	if got := WithRule(a, "nope"); got != a {
		t.Error("an unknown rule changed the settings")
	}
}

func TestShouldCheck(t *testing.T) {
	fresh := &app.OverviewResult{Actionable: true}
	for _, tc := range []struct {
		name string
		st   app.OverviewState
		want bool
	}{
		{"no result", app.OverviewState{Phase: app.OverviewIdle}, true},
		{"failed", app.OverviewState{Phase: app.OverviewIdle, Error: "x"}, false},
		{"checking", app.OverviewState{Phase: app.OverviewChecking}, false},
		{"cleaning", app.OverviewState{Phase: app.OverviewCleaning, Result: &app.OverviewResult{FromCache: true}}, false},
		{"fresh", app.OverviewState{Phase: app.OverviewIdle, Result: fresh}, false},
		{"cache", app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{FromCache: true}}, true},
		{"stale", app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{Stale: true, Actionable: true}}, true},
		{"settings changed", app.OverviewState{Phase: app.OverviewIdle, Result: &app.OverviewResult{}}, true},
	} {
		if got := shouldCheck(tc.st); got != tc.want {
			t.Errorf("%s: %v", tc.name, got)
		}
	}
	// A loaded controller's answer with nothing else in it (Windows: no undo, no rules, no disk) is an answer.
	if !unanswered(app.OverviewState{Phase: app.OverviewIdle, CanUndo: true}) || unanswered(app.OverviewState{Phase: app.OverviewIdle, Loaded: true}) {
		t.Error("unanswered")
	}
	if viewOf(app.OverviewState{Phase: app.OverviewChecking}) != viewProgress || viewOf(app.OverviewState{Error: "x"}) != viewFailed ||
		viewOf(app.OverviewState{}) != viewStopped || viewOf(app.OverviewState{Phase: app.OverviewChecking, Result: fresh}) != viewResult {
		t.Error("views")
	}
}

// Ported from sources.test.ts: offers 维护 when it never ran or ran more than 30 days ago.
func TestMaintenanceDue(t *testing.T) {
	run := func(kind history.Kind, ended time.Time) history.Run {
		return history.Run{ID: ended.String(), Kind: kind, StartedAt: ended, EndedAt: ended}
	}
	runs := []history.Run{
		run(history.KindSystemJunk, time.Date(2026, 11, 13, 0, 0, 0, 0, time.UTC)),
		run(history.KindMaintenance, time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)),
	}
	last := LastMaintenance(runs)
	if last == nil || !last.EndedAt.Equal(time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)) {
		t.Fatalf("last %+v", last)
	}
	if MaintenanceDue(last, now) {
		t.Error("due 13 days after")
	}
	old := run(history.KindMaintenance, time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC))
	if !MaintenanceDue(&old, now) || !MaintenanceDue(nil, now) {
		t.Error("not due")
	}
	if LastMaintenance(runs[:1]) != nil {
		t.Error("a 维护 run among cleans")
	}
	if MaintenanceNote(nil, true) != "做一次轻量保养，不会动你的文档和照片" || MaintenanceNote(&old, true) != "已经超过 30 天没有维护了，可以再做一次" ||
		MaintenanceValue(nil, now) != "还没有做过维护" {
		t.Error("words")
	}
	recent := run(history.KindMaintenance, now.Add(-3*24*time.Hour))
	if got := MaintenanceValue(&recent, now); got != "上次维护：3天前" {
		t.Errorf("value %q", got)
	}
}

// A clean of several items counts them; one item reports nothing until it ends, so the row says how much it handles and the bar goes by bytes.
func TestCleanProgress(t *testing.T) {
	skipOnWindows(t)
	const gb = 1_000_000_000
	for _, tc := range []struct {
		a     advice.Action
		pr    cleanup.Progress
		total int64
		text  string
		frac  float64
	}{
		{advice.ActionDelete, cleanup.Progress{Done: 3, Total: 10, Bytes: 2 * gb}, 17 * gb, "已处理 3/10", 0.3},
		{advice.ActionDelete, cleanup.Progress{Done: 0, Total: 1}, 17 * gb, "已释放 0 B / 17 GB", 0},
		{advice.ActionCommand, cleanup.Progress{}, 1 * gb, "已释放 0 B / 1 GB", 0},
		{advice.ActionMixed, cleanup.Progress{Done: 1, Total: 1, Bytes: gb / 2}, 1 * gb, "已释放 500 MB / 1 GB", 0.5},
		{advice.ActionTrash, cleanup.Progress{Done: 0, Total: 1}, 600_000_000, "已移到废纸篓 0 B / 600 MB", 0},
	} {
		if got := CleanProgressText(tc.a, tc.pr, tc.total); got != tc.text {
			t.Errorf("%+v: text %q, want %q", tc, got, tc.text)
		}
		if got := CleanProgressFraction(tc.pr, tc.total); math.Abs(got-tc.frac) > 1e-9 {
			t.Errorf("%+v: fraction %v, want %v", tc, got, tc.frac)
		}
	}
}

// 全部清理 takes every card's checked items, merged by category in the cards' order, but leaves out a command's, which runs from its own card.
func TestAllOf(t *testing.T) {
	cat := func(id, command string, items ...string) cleanup.Category {
		c := cleanup.Category{ID: id, Source: "system-junk", Selected: true, Command: command}
		for _, it := range items {
			c.Items = append(c.Items, cleanup.Item{ID: it, Size: 10, Selected: true})
		}
		return c
	}
	scan := cleanup.ScanResult{Categories: []cleanup.Category{cat("a", "", "1", "2", "3"), cat("b", "", "4"), cat("tool", "tool clean", "5")}}
	recs := []advice.Recommendation{
		{ID: "one", Categories: []string{"a"}, Items: []advice.ItemRef{ref("a", "1"), ref("a", "2")}},
		{ID: "two", Categories: []string{"b", "a"}, Items: []advice.ItemRef{ref("b", "4"), ref("a", "3")}},
		{ID: "cmd", Action: advice.ActionCommand, Categories: []string{"tool"}, Items: []advice.ItemRef{ref("tool", "5")}},
		{ID: "none", Categories: []string{"b"}, Items: []advice.ItemRef{ref("b", "4")}},
	}
	ch := cleanflow.Choice{Sel: cleanflow.NothingSelected}.Reconcile(scan.Categories)
	// The card two leaves 4 unchecked, which the card none (over the same item) then shares.
	ch = ChangeCard(ch, recs[1], scan, cleanflow.SetItem(CardPick(recs[1], ch, scan).Sel, scan.Categories[1], "4", false))
	all := AllOf(recs, ch, scan)
	if !slices.Equal(all.Cards, []string{"one", "two"}) {
		t.Errorf("cards %v", all.Cards)
	}
	ids := make([]string, len(all.Categories))
	for i, c := range all.Categories {
		ids[i] = c.ID
		if c.ID == "a" && len(c.Items) != 3 {
			t.Errorf("category a merged %+v", c.Items)
		}
	}
	if !slices.Equal(ids, []string{"a"}) {
		t.Errorf("categories %v", ids)
	}
	if got := cleanflow.Refs(all.Categories, all.Sel); !slices.Equal(got, []advice.ItemRef{ref("a", "1"), ref("a", "2"), ref("a", "3")}) {
		t.Errorf("refs %v", got)
	}
	failed := []cleanup.Failure{{CategoryID: "a", ItemID: "3"}, {CategoryID: "a", ItemID: "1"}, {CategoryID: "a", ItemID: "2"}, {CategoryID: "x", ItemID: "9"}}
	if got := FailedByCard(recs, all.Cards, failed); !reflect.DeepEqual(got, map[string]int{"one": 2, "two": 1}) {
		t.Errorf("failed by card %v", got)
	}
}
