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

	enginehist "github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/pages/history"
	"github.com/yldm-tech/spruce/internal/ui/pages/history/historyfake"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
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
}

func start(t *testing.T, svc *historyfake.Service) *page {
	t.Helper()
	fakes := storefake.New(t.TempDir())
	p := &page{svc: svc}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		d := fakes.Deps()
		d.History = svc
		p.stores = storefake.Provide(cx, d)
		history.DepsKey.Provide(cx, history.Deps{Catalog: catalog})
		return history.Page(cx)
	}, nsgotest.Size(1052, 820))
	return p
}

// rows lists the IDs of the rows inside selector, in order: the day sections hold no other buttons than the rows' heads. rows("role:region") lists every row shown.
func (p *page) rows(selector string) []string {
	var ids []string
	for _, v := range p.Query(selector + " >> role:button") {
		ids = append(ids, strings.TrimSuffix(strings.TrimPrefix(v.Props.TestID, "run-"), "-head"))
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
	for _, v := range p.Find("id:run-m1 >> id:run-tasks").Children {
		var line string
		for _, c := range v.Children {
			line += c.Text
		}
		tasks = append(tasks, line)
	}
	want := []string{"优化应用数据库已跳过Safari 仍在运行，已跳过", "刷新 DNS 缓存已完成已刷新 DNS 缓存"}
	if !reflect.DeepEqual(tasks, want) {
		t.Errorf("tasks %q", tasks)
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
	if got := p.Find("id:clear-message").Text; !strings.Contains(got, "将删除全部 3 条操作记录，无法恢复") {
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
	p.Find("id:run-m1 >> id:task-gone >> text:gone")
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
