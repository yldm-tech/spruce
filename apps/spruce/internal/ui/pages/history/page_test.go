package history_test

import (
	"errors"
	"reflect"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	enginehist "spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/taskresult"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/history"
	"spruce/apps/spruce/internal/ui/pages/history/historyfake"
	"spruce/apps/spruce/internal/ui/store"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

const wait = 5 * time.Second

var catalog = historyfake.Catalog{
	{ID: "dns", Title: "刷新 DNS 缓存", Icon: "network", DefaultOn: true, NeedsAdmin: true, EstimatedSeconds: 5},
	{ID: "databases", Title: "优化应用数据库", Icon: "database", EstimatedSeconds: 45},
}

// daysAgo is a local time days before today, so the grouping does not depend on the time zone the tests run in.
func daysAgo(days, h, m, s int) time.Time {
	now := time.Now()
	return time.Date(now.Year(), now.Month(), now.Day()-days, h, m, s, 0, time.Local)
}

func dayKey(days int) string { return daysAgo(days, 12, 0, 0).Format("2006-01-02") }

// span is a run starting at start (stored in UTC, as runs read back from the file are) and lasting seconds.
func span(id string, kind enginehist.Kind, start time.Time, seconds int) enginehist.Run {
	return enginehist.Run{ID: id, Kind: kind, StartedAt: start.UTC(), EndedAt: start.Add(time.Duration(seconds) * time.Second).UTC()}
}

func maintenanceRun() enginehist.Run {
	r := span("m1", enginehist.KindMaintenance, daysAgo(0, 10, 15, 3), 28)
	r.Results = []taskresult.Result{
		{TaskID: "dns", Outcome: taskresult.Applied, Message: "已刷新 DNS 缓存"},
		{TaskID: "databases", Outcome: taskresult.Skipped, Message: "Safari 仍在运行，已跳过"},
	}
	return r
}

func junkRun() enginehist.Run {
	r := span("j1", enginehist.KindSystemJunk, daysAgo(0, 9, 0, 0), 5)
	r.Summary = &enginehist.Totals{Freed: 1_200_000_000, Items: 34, Failed: 1}
	r.Details = []string{"系统缓存：已清理 34 项", "~/Library/Caches/x：文件正在使用，已跳过"}
	return r
}

func uninstallRun() enginehist.Run {
	r := span("u1", enginehist.KindUninstall, daysAgo(1, 20, 45, 0), 3)
	r.Summary = &enginehist.Totals{Freed: 300_000_000, Trashed: 2_000_000_000, Items: 3}
	r.Details = []string{"Xcode：已移到废纸篓 3 项"}
	return r
}

func runs() []enginehist.Run { return []enginehist.Run{maintenanceRun(), junkRun(), uninstallRun()} }

type page struct {
	*nsgotest.App
	svc    *historyfake.Service
	stores *store.Stores
	nav    *nav.Navigator
}

// options change how start mounts the page: 概览's controller (nil leaves it unwired, as in the app before it is) and a navigator call made before the page opens.
type options struct {
	overview *storefake.Overview
	before   func(n *nav.Navigator)
	// kept mounts the page as the shell does, with Provide, and only while the navigator is on 操作历史, so a test can leave it and come back.
	kept bool
	// files reveals the paths of the details.
	files *historyfake.Files
	// mac runs the page on a macOS desktop, where rows have context menus.
	mac bool
}

func start(t *testing.T, svc *historyfake.Service) *page { return startWith(t, svc, options{}) }

func startWith(t *testing.T, svc *historyfake.Service, o options) *page {
	t.Helper()
	fakes := storefake.New(t.TempDir())
	fakes.Overview = o.overview
	p := &page{svc: svc}
	opts := []nsgotest.Option{nsgotest.Size(1052, 820)}
	if o.mac {
		opts = append(opts, nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		d := fakes.Deps()
		d.History = svc
		p.stores = storefake.Provide(cx, d)
		p.nav = nav.NewNavigator(cx, nav.History)
		if o.before != nil {
			o.before(p.nav)
		}
		nav.Key.Provide(cx, p.nav)
		deps := history.Deps{Catalog: catalog, Files: o.files, Home: "/Users/me"}
		if !o.kept {
			history.DepsKey.Provide(cx, deps)
			return history.Page(cx)
		}
		history.Provide(cx, deps)
		return core.Dynamic(func(*core.Scope) core.Node {
			if p.nav.Current() != nav.History {
				return core.Text{Content: "elsewhere"}
			}
			return core.Func(history.Page)
		})
	}, opts...)
	return p
}

// rows lists the IDs of the rows inside selector, in order, by their heads' buttons. rows("role:region") lists every row shown.
func (p *page) rows(selector string) []string {
	var ids []string
	for _, v := range p.Query(selector + " >> role:button") {
		if id, ok := strings.CutSuffix(v.Props.TestID, "-head"); ok {
			ids = append(ids, strings.TrimPrefix(id, "run-"))
		}
	}
	return ids
}

// texts lists the texts of the views selector matches, in order.
func (p *page) texts(selector string) []string {
	var out []string
	for _, v := range p.Query(selector) {
		out = append(out, v.Text)
	}
	return out
}

func TestListGroupsByDayAndTotals(t *testing.T) {
	p := start(t, historyfake.New(runs()...))
	today := "id:day-" + dayKey(0)
	p.WaitFor(today, wait)
	p.Find("text:操作历史")
	p.Find("text:Spruce 做过的每一次维护、清理和卸载")
	if got := p.Find(today).Props.Label; got != "今天" {
		t.Errorf("today's region is %q", got)
	}
	if got := p.Find("id:day-" + dayKey(1)).Props.Label; got != "昨天" {
		t.Errorf("yesterday's region is %q", got)
	}
	if got := p.svc.Limits(); !reflect.DeepEqual(got, []int{500}) {
		t.Errorf("List limits %v", got)
	}
	if got := p.rows(today); !reflect.DeepEqual(got, []string{"m1", "j1"}) {
		t.Errorf("today's rows %v", got)
	}
	p.Find("id:run-m1 >> text:维护")
	p.Find("id:run-m1 >> text:完成 1 项，1 项未完成")
	p.Find("id:run-m1 >> text:10:15")
	p.Find("id:run-j1 >> text:系统垃圾")
	p.Find("id:run-j1 >> text:释放 " + format.Bytes(1_200_000_000) + "，34 项，1 项未能处理")
	p.Find("id:day-" + dayKey(1) + " >> text:应用卸载")
	p.Find("id:run-u1 >> text:释放 " + format.Bytes(300_000_000) + "，移到" + format.TrashName() + " " + format.Bytes(2_000_000_000) + "，3 项")

	// 1.2 GB + 300 MB freed; what went to the Trash is not counted.
	if got := strings.Join(p.texts("id:history-total >> text:/./"), ""); got != "累计释放 "+format.Bytes(1_500_000_000) {
		t.Errorf("total %q", got)
	}
	p.WaitUntil(wait, func() bool { return p.Find("id:history-count").Text == "共 3 条记录，保留最近 180 天" })
	if !p.Exists("id:clear-history") {
		t.Error("清空历史 is missing")
	}
}

func TestExpandShowsTasksOrLines(t *testing.T) {
	p := start(t, historyfake.New(runs()...))
	p.WaitFor("id:run-m1", wait)
	head := "id:run-m1-head"
	if p.Find(head).Props.Expanded {
		t.Fatal("the row starts expanded")
	}
	p.Press(head)
	if !p.Find(head).Props.Expanded {
		t.Fatal("the row did not expand")
	}
	p.WaitFor("id:run-m1 >> id:task-dns", wait)
	// The skipped task first, as the 维护 done card lists it, then the completed one.
	var tasks []string
	cells := p.Find("id:run-m1 >> id:run-tasks").Children
	for i := 0; i+2 < len(cells); i += 3 {
		tasks = append(tasks, cells[i].Text+"|"+cells[i+1].Text+"|"+cells[i+2].Text)
	}
	want := []string{"优化应用数据库|已跳过|Safari 仍在运行", "刷新 DNS 缓存|已完成|已刷新 DNS 缓存"}
	if !reflect.DeepEqual(tasks, want) || len(cells) != 6 {
		t.Errorf("tasks %q (%d cells)", tasks, len(cells))
	}
	// One grid: every status starts at the same x, whatever the title's length.
	if a, b := p.Find("id:task-databases-status").Frame.Origin.X, p.Find("id:task-dns-status").Frame.Origin.X; a != b || a == 0 {
		t.Errorf("statuses at x %v and %v", a, b)
	}
	if got := p.Find("id:run-m1 >> id:run-times").Text; got != "开始 10:15:03 · 结束 10:15:31 · 用时 28 秒" {
		t.Errorf("times %q", got)
	}
	p.Press(head)
	p.WaitForGone("id:run-m1 >> id:run-details", wait)

	p.Press("id:run-j1-head")
	p.Find("id:run-j1 >> text:系统缓存：已清理 34 项")
	p.Find("id:run-j1 >> text:~/Library/Caches/x：文件正在使用，已跳过")
	if got := p.Find("id:run-j1 >> id:run-times").Text; got != "开始 09:00:00 · 结束 09:00:05 · 用时 5 秒" {
		t.Errorf("times %q", got)
	}
}

func TestFiltersByTheKindsPresent(t *testing.T) {
	p := start(t, historyfake.New(runs()...))
	p.WaitFor("id:chip-all", wait)
	var labels []string
	for _, v := range p.Query("role:toolbar 按类型筛选 >> role:button") {
		labels = append(labels, v.Props.Label)
	}
	if want := []string{"全部", "系统垃圾", "应用卸载", "维护"}; !reflect.DeepEqual(labels, want) {
		t.Errorf("chips %q", labels)
	}
	if !p.Find("id:chip-all").Props.Pressed {
		t.Error("全部 is not pressed")
	}
	p.Press("id:chip-uninstall")
	if !p.Find("id:chip-uninstall").Props.Pressed || p.Find("id:chip-all").Props.Pressed {
		t.Error("应用卸载 is not the pressed chip")
	}
	p.WaitForGone("id:day-"+dayKey(0), wait)
	if got := p.rows("id:day-" + dayKey(1)); !reflect.DeepEqual(got, []string{"u1"}) {
		t.Errorf("filtered rows %v", got)
	}
	if len(p.rows("role:region")) != 1 {
		t.Error("other rows remain")
	}
	// The total stays over every clean-up.
	p.Find("id:history-total >> text:" + format.Bytes(1_500_000_000))
	p.Press("id:chip-all")
	p.WaitUntil(wait, func() bool { return len(p.rows("role:region")) == 3 })
}

// The filter and the runs shown open survive going to another page and back; a GoHistory request made meanwhile still wins.
func TestFilterAndOpenRunsOutliveTheVisit(t *testing.T) {
	p := startWith(t, historyfake.New(runs()...), options{kept: true})
	p.WaitFor("id:chip-uninstall", wait)
	p.Press("id:chip-uninstall")
	p.Press("id:run-u1-head")
	p.WaitFor("id:run-u1 >> id:run-details", wait)
	away := func() {
		p.Post(func() { p.nav.Go(nav.Space) })
		p.WaitFor("text:elsewhere", wait)
	}
	away()
	p.Post(func() { p.nav.Go(nav.History) })
	p.WaitFor("id:run-u1 >> id:run-details", wait)
	if got := p.rows("role:region"); !reflect.DeepEqual(got, []string{"u1"}) {
		t.Errorf("rows %v after coming back", got)
	}
	if !p.Find("id:chip-uninstall").Props.Pressed {
		t.Error("应用卸载 is not the pressed chip")
	}

	away()
	p.Post(func() { p.nav.GoHistory("", "j1") })
	p.WaitFor("id:run-j1 >> id:run-details", wait)
	if got := p.rows("role:region"); len(got) != 3 {
		t.Errorf("rows %v after a request for every kind", got)
	}
	p.Find("id:run-u1 >> id:run-details")
}

// A run's details can be selected, and a line that starts with a path reveals it or copies it from its right-click menu.
func TestDetailsSelectAndOfferTheirPaths(t *testing.T) {
	files := &historyfake.Files{}
	p := startWith(t, historyfake.New(runs()...), options{files: files, mac: true})
	p.WaitFor("id:run-j1-head", wait)
	p.Press("id:run-j1-head")
	p.WaitFor("id:run-j1 >> id:run-details", wait)
	if !p.Find("id:run-j1 >> id:run-details").Props.SelectionArea {
		t.Error("the details cannot be selected")
	}
	p.RightClick("id:run-line-1")
	var labels []string
	for _, it := range p.PopupMenu {
		labels = append(labels, it.Label)
	}
	if want := []string{"在" + format.FileManagerName() + "中显示", "拷贝路径"}; !reflect.DeepEqual(labels, want) {
		t.Fatalf("menu %q", labels)
	}
	p.ChoosePopup("拷贝路径")
	if p.Clipboard != "/Users/me/Library/Caches/x" {
		t.Errorf("clipboard %q", p.Clipboard)
	}
	p.RightClick("id:run-line-1")
	p.ChoosePopup("在" + format.FileManagerName() + "中显示")
	p.WaitUntil(wait, func() bool { return len(files.Revealed()) == 1 })
	if got := files.Revealed()[0]; got != "/Users/me/Library/Caches/x" {
		t.Errorf("revealed %q", got)
	}
}

// A line naming where an item was moved from offers only 拷贝路径 while the run can put the item back, since it is in the Trash; once it cannot, the item may be back in its place, so 在访达中显示 returns.
func TestMovedItemsAreNotRevealedFromTheTrash(t *testing.T) {
	moved := span("s1", enginehist.KindSpace, daysAgo(0, 8, 0, 0), 2)
	moved.Summary = &enginehist.Totals{Trashed: 4_000_000_000, Items: 1}
	moved.Details = []string{"已移到废纸篓：~/Movies/a.mov"}
	svc := historyfake.New(moved)
	svc.SetRestorable("s1")
	p := startWith(t, svc, options{files: &historyfake.Files{}, mac: true})
	p.WaitFor("id:run-s1-restore", wait)
	p.Press("id:run-s1-head")
	p.WaitFor("id:run-s1 >> id:run-details", wait)
	menu := func() []string {
		p.RightClick("id:run-line-0")
		var labels []string
		for _, it := range p.PopupMenu {
			labels = append(labels, it.Label)
		}
		return labels
	}
	if got := menu(); !reflect.DeepEqual(got, []string{"拷贝路径"}) {
		t.Errorf("menu while the run can put it back %q", got)
	}
	p.ChoosePopup("拷贝路径")
	if p.Clipboard != "/Users/me/Movies/a.mov" {
		t.Errorf("clipboard %q", p.Clipboard)
	}
	svc.SetRestorable()
	p.Post(p.stores.History.Bump)
	p.WaitForGone("id:run-s1-restore", wait)
	if got := menu(); len(got) != 2 {
		t.Errorf("menu once the run cannot put it back %q", got)
	}
}

// A run 放回原处 can put back offers it on a right click too; another run has no menu.
func TestRestoreFromTheRowMenu(t *testing.T) {
	svc := historyfake.New(runs()...)
	svc.SetRestorable("u1")
	ov := storefake.NewOverview(app.OverviewState{})
	p := startWith(t, svc, options{overview: ov, mac: true})
	p.WaitFor("id:run-u1-restore", wait)
	p.RightClick("id:run-j1-head")
	if p.PopupMenu != nil {
		t.Errorf("a run without anything to put back has a menu %v", p.PopupMenu)
	}
	p.RightClick("id:run-u1-head")
	p.ChoosePopup("放回原处")
	p.WaitUntil(wait, func() bool { _, _, _, undos, _ := ov.Calls(); return len(undos) == 1 && undos[0] == "u1" })
}

func TestRefreshesWhenARunIsRecorded(t *testing.T) {
	svc := historyfake.New(runs()...)
	p := start(t, svc)
	p.WaitFor("id:run-m1", wait)
	fresh := junkRun()
	fresh.ID, fresh.Kind = "j2", enginehist.KindDevJunk
	fresh.StartedAt, fresh.EndedAt = daysAgo(0, 11, 0, 0).UTC(), daysAgo(0, 11, 0, 1).UTC()
	svc.Record(fresh)
	p.WaitFor("id:run-j2", wait)
	p.Find("id:run-j2 >> text:开发者")
	if got := p.rows("id:day-" + dayKey(0)); !reflect.DeepEqual(got, []string{"j2", "m1", "j1"}) {
		t.Errorf("rows %v", got)
	}
	if got := len(svc.Limits()); got != 2 {
		t.Errorf("List called %d times", got)
	}
}

func TestEmptyStateWithoutClearOrFilters(t *testing.T) {
	p := start(t, historyfake.New())
	p.WaitFor("id:history-empty", wait)
	p.Find("text:还没有操作记录")
	p.Find("text:运行维护或清理后，每一次的结果都会记在这里。")
	if p.Exists("id:clear-history") || p.Exists("id:chip-all") || p.Exists("id:history-count") {
		t.Error("清空历史, the filters or the count show without runs")
	}
}

func TestRetryAfterAFailedLoad(t *testing.T) {
	svc := historyfake.New(runs()...)
	svc.FailList(errors.New("读取失败"))
	p := start(t, svc)
	p.WaitFor("id:history-error", wait)
	if got := p.Find("role:alert").Text; got != "无法读取操作历史：读取失败" {
		t.Errorf("alert %q", got)
	}
	if p.Exists("id:history-empty") {
		t.Error("the empty state shows after a failed load")
	}
	p.Press("id:history-retry")
	p.WaitFor("id:day-"+dayKey(0), wait)
	p.WaitForGone("id:history-error", wait)
}

func TestClearAsksFirstThenClears(t *testing.T) {
	svc := historyfake.New(runs()...)
	p := start(t, svc)
	p.WaitFor("id:clear-history", wait)
	p.Press("id:clear-history")
	p.WaitFor("id:clear-dialog", wait)
	p.Find("text:清空操作历史？ >> nth=0")
	if got := p.Find("id:clear-message").Text; !strings.Contains(got, "将删除全部 3 条操作记录，无法恢复") ||
		!strings.Contains(got, "之前移到"+format.TrashName()+"的项目也不能再在这里放回原处，记下的空间变化和上次检查的结果也会一并删除。") {
		t.Errorf("message %q", got)
	}
	if !p.stores.Block.Blocked() {
		t.Error("the shell is not blocked under the dialog")
	}
	// A danger dialog starts on 取消.
	p.WaitUntil(wait, func() bool { return p.Find("id:confirm-cancel").Focused })
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:clear-dialog", wait)
	if svc.Clears() != 0 {
		t.Error("取消 cleared the history")
	}
	p.WaitUntil(wait, func() bool { return p.Find("id:clear-history").Focused })

	p.Press("id:clear-history")
	p.WaitFor("id:clear-dialog", wait)
	p.Press("id:confirm-ok")
	p.WaitFor("id:history-empty", wait)
	p.WaitForGone("id:clear-dialog", wait)
	if svc.Clears() != 1 {
		t.Errorf("Clear called %d times", svc.Clears())
	}
	if p.stores.Block.Blocked() {
		t.Error("the shell is still blocked")
	}
}

func TestClearFailureKeepsTheDialog(t *testing.T) {
	svc := historyfake.New(runs()...)
	svc.FailClear(errors.New("无法清空操作历史"))
	p := start(t, svc)
	p.WaitFor("id:clear-history", wait)
	p.Press("id:clear-history")
	p.WaitFor("id:clear-dialog", wait)
	p.Press("id:confirm-ok")
	p.WaitFor("id:clear-error", wait)
	if got := p.Find("id:clear-error").Text; got != "无法清空操作历史" {
		t.Errorf("error %q", got)
	}
	if !p.Exists("id:clear-dialog") || !p.Enabled("id:confirm-ok") {
		t.Error("the dialog closed or stayed busy")
	}
	if got := len(p.rows("role:region")); got != 3 {
		t.Errorf("%d rows", got)
	}
	// 取消 closes it, and opening it again starts without the old error.
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:clear-dialog", wait)
	p.Press("id:clear-history")
	p.WaitFor("id:clear-dialog", wait)
	if p.Exists("id:clear-error") {
		t.Error("the old error shows again")
	}
}

func TestUnknownTasksAndKinds(t *testing.T) {
	odd := span("x1", "future", daysAgo(0, 8, 0, 0), 1)
	m := maintenanceRun()
	m.Results = append(m.Results, taskresult.Result{TaskID: "gone", Outcome: taskresult.Unavailable, Message: "这台电脑上没有这项"})
	p := start(t, historyfake.New(m, odd))
	p.WaitFor("id:run-x1", wait)
	p.Find("id:run-x1 >> text:其他操作")
	p.Find("id:run-x1 >> text:没有记录结果")
	p.Press("id:run-m1-head")
	p.WaitFor("id:run-m1 >> id:task-gone", wait)
	if got := p.Find("id:run-m1 >> id:task-gone").Text; got != "gone" {
		t.Errorf("unknown task titled %q", got)
	}
}

func TestFiveHundredRuns(t *testing.T) {
	var all []enginehist.Run
	for i := range enginehist.MaxRuns {
		r := junkRun()
		r.ID = "r" + strconv.Itoa(i)
		start := daysAgo(i/20, 23, 0, 0).Add(-time.Duration(i%20) * time.Minute)
		r.StartedAt, r.EndedAt = start.UTC(), start.Add(time.Second).UTC()
		all = append(all, r)
	}
	began := time.Now()
	p := start(t, historyfake.New(all...))
	p.WaitUntil(wait, func() bool { return len(p.rows("role:region")) == enginehist.MaxRuns })
	t.Logf("500 rows in %v", time.Since(began))
	p.Press("id:run-r499-head")
	p.WaitFor("id:run-r499 >> id:run-details", wait)
}
