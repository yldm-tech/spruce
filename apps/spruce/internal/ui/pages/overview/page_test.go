package overview_test

import (
	"context"
	"errors"
	"reflect"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/growth"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/cleanflow/cleanflowfake"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/overview"
	"spruce/apps/spruce/internal/ui/store"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

const (
	// wait is real time; the page is the largest one and its ticking waits run many frames, which under -race on a loaded machine took longer than 5 s.
	wait = 20 * time.Second
	mb   = 1_000_000
	gb   = 1000 * mb
	home = "/Users/me"
)

// now is the fixed clock of every test.
var now = time.Date(2026, 11, 14, 12, 0, 0, 0, time.Local)

func item(path string, size int64, selected bool, age time.Duration) cleanup.Item {
	return cleanup.Item{ID: cleanup.ItemID(path), Path: path, Size: size, Selected: selected, ModTime: now.Add(-age), Dir: true}
}

func category(id, title, desc, icon string, del cleanup.DeletionMode, items ...cleanup.Item) cleanup.Category {
	src, _, _ := strings.Cut(id, ":")
	return cleanup.Category{ID: id, Title: title, Description: desc, Icon: icon, Kind: cleanup.KindCache, Deletion: del, Source: src, Items: items}
}

func artifact(project, name string, size int64, git cleanup.GitState, hold string) cleanup.Item {
	i := item(project+"/"+name, size, hold == "", 100*24*time.Hour)
	i.Project, i.Git, i.Hold = project, git, hold
	return i
}

// scan is a check with something of most kinds: caches deleted for good, a command, build output partly in 文稿 (mixed), an installer, an incomplete download and logs, things left unchecked, and the Trash.
func scan() cleanup.ScanResult {
	trash := category("system-junk:trash", "废纸篓", "废纸篓里的全部项目", "trash", cleanup.DeletePermanent, item(home+"/.Trash", 2*gb, false, 0))
	trash.Kind, trash.Warning = cleanup.KindTrash, "清空后无法恢复，默认不选"
	npm := category("dev-junk:tool-npm", "npm 缓存", "运行 npm cache clean --force，清空 npm 下载的包", "node", cleanup.DeletePermanent,
		item(home+"/.npm/_cacache", 1340*mb, true, 3*24*time.Hour))
	npm.Command = "npm cache clean --force"
	running := category("system-junk:app-caches-running", "正在运行的应用的缓存", "这些应用正在运行", "cache", cleanup.DeletePermanent,
		item(home+"/Library/Caches/com.running", 300*mb, false, time.Hour))
	running.Warning = "这些应用正在运行，建议先退出再清理"
	res := cleanup.NewResult([]cleanup.Category{
		category("system-junk:app-caches", "应用缓存", "应用的缓存文件", "cache", cleanup.DeletePermanent,
			item(home+"/Library/Caches/com.spotify.client", 1200*mb, true, 40*24*time.Hour),
			item(home+"/Library/Caches/com.apple.Safari", 800*mb, true, 40*24*time.Hour)),
		running,
		category("system-junk:logs", "日志", "应用的日志", "log", cleanup.DeletePermanent, item(home+"/Library/Logs/a.log", 200*mb, true, 60*24*time.Hour)),
		category("system-junk:incomplete-downloads", "未完成的下载", "中断的下载", "temp", cleanup.DeleteTrash,
			item(home+"/Downloads/big.crdownload", 300*mb, true, 5*24*time.Hour)),
		trash,
		category("installers:disk-images", "磁盘映像", "下载的磁盘映像", "installer", cleanup.DeleteTrash,
			item(home+"/Downloads/Foo 1.0.dmg", 600*mb, true, 20*24*time.Hour)),
		npm,
		category("dev-junk:artifact-node-modules", "node_modules", "依赖", "node", cleanup.DeletePermanent,
			artifact(home+"/ws/a", "node_modules", 1*gb, cleanup.GitClean, ""),
			artifact(home+"/ws/c", "node_modules", 900*mb, cleanup.GitDirty, "项目有未提交的改动")),
		category("dev-junk:artifact-node-modules-trash", "node_modules", "依赖", "node", cleanup.DeleteTrash,
			artifact(home+"/Documents/d", "node_modules", 400*mb, cleanup.GitNone, "")),
	}, now)
	res.Sources = []cleanup.SourceState{
		{ID: "system-junk", Status: cleanup.SourceDone}, {ID: "dev-junk", Status: cleanup.SourceDone}, {ID: "installers", Status: cleanup.SourceDone},
	}
	return res
}

// result is scan as 概览's controller publishes it, actionable, checked 5 minutes ago.
func result(res cleanup.ScanResult) *app.OverviewResult {
	recs := advice.Derive(res, advice.Inputs{Now: now, Home: home, InstalledApps: []string{"Foo"}, InstallerRoots: []string{home + "/Downloads"}})
	return &app.OverviewResult{CheckedAt: now.Add(-5 * time.Minute), Mode: app.CheckForeground, Actionable: true, Scan: res, Recommendations: recs,
		Headline: advice.Headline(recs)}
}

// ready is a state with a result to act on, on a disk with room.
func ready() app.OverviewState {
	return app.OverviewState{Phase: app.OverviewIdle, Result: result(scan()), CanUndo: true,
		Disk: &app.DiskState{Volume: "Macintosh HD", Free: 120 * gb, Total: 1000 * gb, At: now}}
}

type page struct {
	*nsgotest.App
	fakes  *storefake.Services
	ov     *storefake.Overview
	stores *store.Stores
	nav    *nav.Navigator
	files  *cleanflowfake.Files
	tb     testing.TB
}

func newFakes(t *testing.T) *storefake.Services {
	f := storefake.New(t.TempDir())
	f.Permission = maintenance.PermissionGranted
	return f
}

// start opens 概览 on the controller ov (nil leaves it unwired), with fakes' stores.
func start(t *testing.T, fakes *storefake.Services, ov *storefake.Overview, before ...func(*nav.Navigator)) *page {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
	fakes.Overview = ov
	p := &page{fakes: fakes, ov: ov, files: &cleanflowfake.Files{}, tb: t}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		p.nav = nav.NewNavigator(cx, nav.Overview)
		nav.Key.Provide(cx, p.nav)
		for _, f := range before {
			f(p.nav)
		}
		cleanflow.Provide(cx)
		overview.Provide(cx, overview.Deps{Files: p.files, Home: home})
		return core.Dynamic(func(*core.Scope) core.Node {
			switch p.nav.Current() {
			case nav.Overview:
				return core.Func(overview.Page)
			case nav.SystemJunk:
				// 系统垃圾 as the app shows it, to check what it shares with the cards.
				return core.Func(func(*core.Scope) core.Node {
					return cleanflow.DrillPage{Route: nav.SystemJunk, Toast: overview.CleanToast, ListLabel: "可以清理的系统垃圾", SizeLabel: "已选大小", Reveal: p.files.Reveal}
				})
			}
			return core.Group{}
		})
	}, nsgotest.Size(1052, 1400), func(a *nsgotest.App) { a.Clock.SetWallTime(now) })
	p.WaitUntil(wait, func() bool { return p.stores.Settings.Loaded() })
	// The page has the controller's state before the test acts on it.
	if ov != nil {
		p.WaitUntil(wait, func() bool { return reflect.DeepEqual(p.stores.Overview.State(), ov.State()) })
	}
	return p
}

// open starts 概览 on st.
func open(t *testing.T, st app.OverviewState) *page {
	return start(t, newFakes(t), storefake.NewOverview(st))
}

// waitText waits until selector shows want.
func (p *page) waitText(selector, want string) {
	p.tb.Helper()
	p.WaitUntil(wait, func() bool {
		vs := p.Query(selector)
		return len(vs) == 1 && textOf(vs[0]) == want
	})
}

// textOf is a view's text: its own, else its descendants' in order (a button's).
func textOf(v *nsgotest.View) string {
	if v.Text != "" {
		return v.Text
	}
	var b strings.Builder
	for _, c := range v.Children {
		b.WriteString(textOf(c))
	}
	return b.String()
}

// ticking waits for cond, moving the manual clock so timers come through.
func (p *page) ticking(cond func() bool) {
	p.tb.Helper()
	p.WaitUntil(wait, func() bool {
		if cond() {
			return true
		}
		p.Advance(50 * time.Millisecond)
		return false
	})
}

func (p *page) disabled(selector string) bool { return p.Find(selector).Props.Disabled }

func (p *page) checks() int { c, _, _, _, _ := p.ov.Calls(); return c }

func (p *page) cleans() []storefake.CleanCall { _, _, c, _, _ := p.ov.Calls(); return c }

// toast is the toast shown, nil for none.
func (p *page) toast() *store.Toast {
	return p.stores.Toasts.Current()
}

// set publishes st as the controller's state and waits for the page to take it.
func (p *page) set(st app.OverviewState) {
	p.ov.Set(st)
	p.Settle()
}

func ids(recs []advice.Recommendation) []string {
	out := make([]string, len(recs))
	for i, r := range recs {
		out[i] = r.ID
	}
	return out
}

func TestFixtureCards(t *testing.T) {
	got := ids(result(scan()).Recommendations)
	want := []string{"app-caches", "projects-idle", "tool-npm", "installers", "incomplete-downloads", "logs"}
	if !slices.Equal(got, want) {
		t.Fatalf("cards %v, want %v", got, want)
	}
}

// State C: the conclusion, the cards, the disk, what needs judgement, the Trash and the sources.
func TestReady(t *testing.T) {
	p := open(t, ready())
	p.Find("role:header 概览")
	p.Find("text:Spruce 会在后台定期检查，告诉你哪些空间可以放心释放")
	p.waitText("id:conclusion-label", "可以放心释放")
	p.waitText("id:conclusion-value", "5.8 GB")
	p.waitText("id:conclusion-sub", "6 条建议，其中 900 MB 会先移到废纸篓")
	p.waitText("id:checked-at", "上次检查：5分钟前")
	p.waitText("id:recheck", "重新检查")
	p.waitText("id:disk-text", "Macintosh HD · 可用 120 GB，共 1 TB")
	if v := p.Find("id:disk-meter").Props.Value; v.Now != 88 {
		t.Errorf("used %+v", v)
	}
	if p.Exists("id:disk-low") || p.Exists("id:low-disk-offer") {
		t.Error("a low disk on a disk with room")
	}
	if got := len(p.Query("id:recs >> role:region")); got != 5 {
		t.Errorf("%d cards shown, want 5", got)
	}
	p.waitText("id:recs-more", "再显示 1 条建议")
	p.Press("id:recs-more")
	p.WaitFor("id:rec-logs", wait)

	p.waitText("id:rec-app-caches-title", "应用缓存")
	p.waitText("id:rec-app-caches-reason", "2 个应用的缓存，最大的是 com.spotify.client")
	p.waitText("id:rec-app-caches-size", "2 GB")
	p.waitText("id:rec-app-caches-consequence", "直接删除，应用下次打开时会重新生成，第一次可能稍慢")
	p.WaitFor("id:rec-app-caches-badge >> text:无法撤销", wait)
	p.waitText("id:rec-app-caches-act", "删除…")
	p.WaitFor("id:rec-tool-npm-badge >> text:运行命令", wait)
	p.waitText("id:rec-tool-npm-act", "运行清理…")
	p.waitText("id:rec-tool-npm-reason", "最近一次使用在 3天前")
	p.WaitFor("id:rec-projects-idle-badge >> text:部分无法撤销", wait)
	p.waitText("id:rec-projects-idle-act", "清理…")
	p.waitText("id:rec-projects-idle-held", "1 个有未提交改动的项目没有选入")
	p.WaitFor("id:rec-installers-badge >> text:可撤销", wait)
	p.waitText("id:rec-installers-act", "移到废纸篓")
	p.waitText("id:rec-installers-title", "下载里的安装包")
	p.waitText("id:rec-installers-reason", "1 个安装包，对应的应用都已装好，在下载里")

	p.waitText("id:review-text", "还有 2 项默认不选（1.2 GB），比如正在运行的应用的缓存、最近下载的安装包、有未提交改动的项目，可能还要用")
	p.WaitFor("id:review-link-system-junk", wait)
	p.WaitFor("id:review-link-dev-junk", wait)
	if p.Exists("id:review-link-installers") {
		t.Error("a link to 安装包, which leaves nothing unchecked")
	}
	p.waitText("id:trash-text", "废纸篓里有 2 GB。移到废纸篓的文件，清空废纸篓后才真正腾出空间。")
	p.waitText("id:browse-size-system-junk", "4.8 GB")
	p.waitText("id:browse-note-system-junk", "6 项，建议清理 4 项（2.5 GB）")
	p.waitText("id:browse-note-dev-junk", "4 项，建议清理 3 项（2.7 GB）")
	p.WaitFor("id:status-cpu", wait)
	p.WaitFor("id:status-maintenance", wait)
	if p.checks() != 0 {
		t.Errorf("an actionable result was checked again on open (%d)", p.checks())
	}
	if p.Exists("id:auto-summary") || p.Exists("id:check-waiting") || p.Exists("id:denied-notice") || p.Exists("id:skipped-notice") {
		t.Error("a note that does not apply")
	}
}

// State A: the first check, with no result yet.
func TestFirstCheck(t *testing.T) {
	st := app.OverviewState{Phase: app.OverviewChecking, CanUndo: true, Progress: &cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 47, Total: 100,
		Sources: []cleanup.SourceState{
			{ID: "system-junk", Status: cleanup.SourceScanning, Done: 6, Total: 19, Bytes: 1_200_000_000},
			{ID: "dev-junk", Status: cleanup.SourceScanning},
			{ID: "installers", Status: cleanup.SourceDone, Bytes: 374_800_000, Items: 5},
		}}}
	p := open(t, st)
	p.WaitFor("id:check-progress", wait)
	p.Find("id:check-progress >> text:正在检查哪些空间可以放心释放…")
	p.Find("id:check-progress >> text:只读取，不会删除任何东西，通常不到一分钟。")
	if v := p.Find("id:check-ring").Props.Value; v.Now != 47 {
		t.Errorf("ring %+v", v)
	}
	p.waitText("id:row-status-system-junk", "正在检查… 已找到 1.2 GB")
	p.waitText("id:row-status-installers", "完成，找到 5 项，共 374.8 MB")
	p.WaitFor("id:sweep-dev-junk", wait)
	p.waitText("id:recheck", "正在检查…")
	if !p.Find("id:recheck").Props.Busy {
		t.Error("重新检查 is not busy during a check")
	}
	for _, id := range []string{"recs", "review", "browse", "growth", "conclusion"} {
		if p.Exists("id:" + id) {
			t.Errorf("%s shows during the first check", id)
		}
	}
	p.WaitFor("id:status", wait)
	if p.checks() != 0 {
		t.Error("a check started over a running one")
	}
	p.Press("id:check-stop")
	p.WaitUntil(wait, func() bool { _, stops, _, _, _ := p.ov.Calls(); return stops == 1 })
}

// State B: a cached result while a check updates it; nothing can be acted on until it ends.
func TestCachedWhileChecking(t *testing.T) {
	st := ready()
	st.Result.FromCache, st.Result.Actionable = true, false
	p := open(t, st)
	// Opening on a cached result checks again.
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })
	p.waitText("id:rec-app-caches-act", "重新检查后才能清理")
	if !p.disabled("id:rec-app-caches-act") {
		t.Error("a card of the cache can act")
	}
	st.Phase = app.OverviewChecking
	p.set(st)
	p.waitText("id:checked-at", "上次检查：5分钟前 · 正在更新…")
	p.WaitFor("id:conclusion-value", wait)
	p.waitText("id:rec-app-caches-act", "正在核对…")
	if !p.disabled("id:rec-app-caches-act") || !p.disabled("id:empty-trash") {
		t.Error("acting while checking")
	}
	fresh := ready()
	p.set(fresh)
	p.waitText("id:rec-app-caches-act", "删除…")
	if p.disabled("id:rec-app-caches-act") {
		t.Error("the fresh result cannot be acted on")
	}
}

// State D: a result past StaleAfter warns and checks again on open.
func TestStale(t *testing.T) {
	st := ready()
	st.Result.Stale, st.Result.Actionable, st.Result.CheckedAt = true, false, now.Add(-30*time.Hour)
	p := open(t, st)
	p.waitText("id:checked-at", "上次检查：昨天，结果可能已经变化")
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })
}

// State D on a window shown again after it was hidden: 概览 stayed mounted, and decides again on the state then.
func TestWindowShownAgainChecks(t *testing.T) {
	p := open(t, ready())
	p.WaitFor("id:rec-app-caches", wait)
	// A result that can still be acted on is not checked again.
	p.Post(p.nav.WindowShown)
	p.Settle()
	if p.checks() != 0 {
		t.Fatalf("%d checks on showing a fresh result again", p.checks())
	}
	// Hidden for a day, the result went stale: showing the window checks.
	st := ready()
	st.Result.Stale, st.Result.Actionable, st.Result.CheckedAt = true, false, now.Add(-30*time.Hour)
	p.set(st)
	p.waitText("id:checked-at", "上次检查：昨天，结果可能已经变化")
	if p.checks() != 0 {
		t.Fatalf("%d checks before the window came back", p.checks())
	}
	p.Post(p.nav.WindowShown)
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })
	// Not while a check runs.
	st.Phase = app.OverviewChecking
	p.set(st)
	p.WaitUntil(wait, func() bool { return p.stores.Overview.State().Phase == app.OverviewChecking })
	p.Post(p.nav.WindowShown)
	p.Settle()
	if p.checks() != 1 {
		t.Errorf("%d checks on showing the window during a check", p.checks())
	}
}

// A change of the exclusions or folders while 概览 is open checks again at once.
func TestSettingsChangeChecksAgain(t *testing.T) {
	p := open(t, ready())
	p.WaitFor("id:rec-app-caches", wait)
	next := p.stores.Settings.Value()
	next.Cleanup.Exclusions = append(slices.Clone(next.Cleanup.Exclusions), home+"/keep")
	p.Post(func() { p.stores.Settings.Save(next, nil) })
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })
	// Settings that do not change what a check finds do not.
	other := p.stores.Settings.Value()
	other.General.LowDiskGB = 30
	p.Post(func() { p.stores.Settings.Save(other, nil) })
	p.Settle()
	if p.checks() != 1 {
		t.Errorf("%d checks after a change of the threshold", p.checks())
	}
}

// State E: no cards.
func TestEmpty(t *testing.T) {
	res := cleanup.NewResult([]cleanup.Category{}, now)
	st := ready()
	st.Result = result(res)
	p := open(t, st)
	p.WaitFor("id:overview-empty >> text:没有需要清理的内容", wait)
	p.Find("id:overview-empty >> text:系统垃圾、开发者垃圾和安装包都很干净。Spruce 会定期再看看。")
	p.waitText("id:checked-at", "上次检查：5分钟前")
	if p.Exists("id:conclusion-value") || p.Exists("id:recs") || p.Exists("id:review") || p.Exists("id:trash-row") || p.Exists("id:browse") {
		t.Error("a part without content")
	}
	// Something found that no card takes (a running app's cache) brings 按类别查看 back, all three cards of it.
	res = cleanup.NewResult([]cleanup.Category{category("system-junk:app-caches-running", "正在运行的应用的缓存", "这些应用正在运行", "cache", cleanup.DeletePermanent,
		item(home+"/Library/Caches/com.running", 300*mb, false, time.Hour))}, now)
	st.Result = result(res)
	p.set(st)
	p.WaitFor("id:browse", wait)
	for _, id := range []string{"system-junk", "dev-junk", "installers"} {
		p.Find("id:browse-" + id)
	}
	// So does a source that failed.
	res = cleanup.NewResult([]cleanup.Category{}, now)
	res.Sources = []cleanup.SourceState{{ID: "installers", Status: cleanup.SourceFailed, Error: "无法读取下载文件夹"}}
	st.Result = result(res)
	p.set(st)
	p.waitText("id:browse-note-installers", "无法读取下载文件夹")
}

// State F: a failed check keeps the old result, outdated, with 重试; without one it is the error alone.
func TestFailed(t *testing.T) {
	st := ready()
	st.Error = "需要权限"
	p := open(t, st)
	p.waitText("id:check-error", "这次检查没有完成：需要权限")
	p.waitText("id:checked-at", "上次检查：5分钟前，结果可能已经变化")
	p.WaitFor("id:conclusion-value", wait)
	p.Press("id:check-retry")
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })

	q := open(t, app.OverviewState{Phase: app.OverviewIdle, Error: "没能读取", CanUndo: true})
	q.waitText("id:conclusion >> id:check-error", "这次检查没有完成：没能读取")
	if q.Exists("id:conclusion-value") || q.Exists("id:recs") {
		t.Error("a conclusion without a result")
	}
	if q.checks() != 0 {
		t.Error("a failed check was retried on open")
	}
	q.Press("id:check-retry")
	q.WaitUntil(wait, func() bool { return q.checks() == 1 })
}

// State G: no controller wired, the stopped state, and 开始检查 says why it cannot.
func TestStoppedWithoutController(t *testing.T) {
	p := start(t, newFakes(t), nil)
	p.WaitFor("text:检查已停止", wait)
	p.Find("text:随时可以重新开始，检查只读取，不会删除任何东西。")
	p.Press("id:check-start")
	p.waitText("id:check-error", "概览还没有接入")
	if p.Exists("id:disk") {
		t.Error("a disk row without a disk")
	}
	p.WaitFor("id:status", wait)
}

// State G with a controller: a stopped check offers 开始检查.
// A loaded controller's first answer with nothing in it (Windows: no undo, background checks off, the disk unread, no cache) still starts the check on open.
func TestBareFirstAnswerChecks(t *testing.T) {
	p := open(t, app.OverviewState{Phase: app.OverviewIdle})
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })
}

func TestStopped(t *testing.T) {
	fakes := newFakes(t)
	ov := storefake.NewOverview(app.OverviewState{Phase: app.OverviewChecking, CanUndo: true})
	p := start(t, fakes, ov)
	p.WaitFor("id:check-progress", wait)
	p.set(app.OverviewState{Phase: app.OverviewIdle, CanUndo: true})
	p.WaitFor("id:check-start", wait)
	p.Press("id:check-start")
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })
}

// State H: locations Spruce may not read, with the Full Disk Access guide, which checks again once granted.
func TestDenied(t *testing.T) {
	fakes := newFakes(t)
	fakes.Permission = maintenance.PermissionDenied
	st := ready()
	st.Result.Scan.Denied = 3
	p := start(t, fakes, storefake.NewOverview(st))
	p.WaitFor("id:denied-notice >> text:有 3 个位置没有读取权限，结果可能不完整", wait)
	p.Press("id:denied-notice >> id:grant-full-disk-access")
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })
}

// State I: a low disk changes the conclusion, links to 空间透视 and offers the alert, which turns the setting on.
func TestLowDisk(t *testing.T) {
	st := ready()
	st.Disk = &app.DiskState{Volume: "Macintosh HD", Free: 12 * gb, Total: 500 * gb, Low: true}
	p := open(t, st)
	p.waitText("id:conclusion-label", "空间不多了，可以放心释放")
	p.WaitFor("id:disk-low >> text:空间不多了", wait)
	p.WaitFor("id:low-disk-offer >> text:空间不足时让 Spruce 提醒你", wait)
	if v := p.Find("id:disk-meter").Props.Value; v.Now != 98 {
		t.Errorf("used %+v", v)
	}
	p.Press("id:low-disk-offer >> text:开启提醒")
	p.WaitUntil(wait, func() bool { return p.stores.Settings.Value().General.LowDiskAlert })
	p.WaitForGone("id:low-disk-offer", wait)
	p.Press("id:disk-low >> role:link")
	if p.nav.Current() != nav.Space {
		t.Errorf("看看是什么占用了空间 went to %q", p.nav.Current())
	}
}

// A failed source, the folders a background check skipped and why a scheduled check waits.
func TestNotices(t *testing.T) {
	st := ready()
	st.Result.Scan.Sources[2] = cleanup.SourceState{ID: "installers", Status: cleanup.SourceFailed, Error: "无法读取下载文件夹"}
	st.Result.Skipped = []string{home + "/Downloads", home + "/Desktop", home + "/Library/Mobile Documents", "/Volumes/T7"}
	st.Waiting = "正在使用电池，接通电源后再检查"
	p := open(t, st)
	p.waitText("id:source-failed-installers", "安装包没能检查完成：无法读取下载文件夹")
	p.waitText("id:skipped-notice", "后台检查没有读取下载、桌面、iCloud 云盘、外接磁盘，这些位置要在你使用 Spruce 时才能检查。")
	p.waitText("id:check-waiting", "· 正在使用电池，接通电源后再检查")
	p.waitText("id:browse-note-installers", "无法读取下载文件夹")
	p.Press("id:check-skipped")
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })
}

// An automatic run just moved things: said once, with 查看 to its records; and that rules are on.
func TestAutoRules(t *testing.T) {
	fakes := newFakes(t)
	fakes.Settings.Automation.AppCaches = true
	st := ready()
	st.LastAuto = &app.AutoRun{RunIDs: []string{"a1"}, Trashed: 4_200_000_000, At: now.Add(-time.Hour)}
	p := start(t, fakes, storefake.NewOverview(st))
	p.WaitFor("id:auto-just-ran", wait)
	p.WaitFor("text: · 自动清理刚刚移走了 4.2 GB", wait)
	p.WaitFor("id:auto-summary >> text:自动清理已开启", wait)
	if p.Exists("id:rec-app-caches-auto") {
		t.Error("以后自动处理 under a rule that is on")
	}
	p.WaitFor("id:rec-installers-auto", wait)
	p.Press("id:auto-just-ran")
	if p.nav.Current() != nav.History {
		t.Fatalf("查看 went to %q", p.nav.Current())
	}
	if k, run := p.nav.TakeHistoryFilter(); k != history.KindAuto || run != "" {
		t.Errorf("filter %q %q", k, run)
	}
	p.nav.Go(nav.Overview)
	p.WaitFor("id:conclusion-value", wait)
	if p.Exists("id:auto-just-ran") {
		t.Error("the automatic run is told again once seen")
	}
	p.Press("id:auto-summary")
	if p.nav.Current() != nav.Settings || p.nav.TakeSettingsTab() != nav.SettingsAuto {
		t.Error("自动清理已开启 does not open 设置 → 自动清理")
	}
}

// 以后自动处理 asks first and then turns the rule on.
func TestAutoOffer(t *testing.T) {
	p := open(t, ready())
	p.Press("id:rec-installers-auto")
	p.WaitFor("id:rule-confirm", wait)
	p.Find("id:rule-confirm >> text:开启「旧安装包」？")
	p.Find("id:rule-confirm >> text:每天把「下载」里超过 7 天、对应的应用已经装好的安装包和磁盘映像（.dmg、.pkg）移到废纸篓。压缩包、系统映像和其他文件夹里的不会处理，正在挂载或无法确认是否挂载的也不会处理。")
	p.Press("id:rule-confirm >> id:confirm-cancel")
	p.WaitForGone("id:rule-confirm", wait)
	if p.stores.Settings.Value().Automation.Installers {
		t.Fatal("取消 turned the rule on")
	}
	p.Press("id:rec-installers-auto")
	p.Press("id:rule-confirm >> id:confirm-ok")
	p.WaitUntil(wait, func() bool { return p.stores.Settings.Value().Automation.Installers })
	p.WaitForGone("id:rec-installers-auto", wait)
	if len(p.cleans()) != 0 {
		t.Error("turning a rule on cleaned")
	}
	// Windows has no rules and no undo.
	st := ready()
	st.CanUndo = false
	q := open(t, st)
	q.WaitFor("id:rec-installers", wait)
	if q.Exists("id:rec-installers-auto") {
		t.Error("以后自动处理 without rules")
	}
}

// A move to the Trash goes at once, then the toast offers 撤销; the card goes.
func TestTrashCard(t *testing.T) {
	p := open(t, ready())
	p.ov.CleanRelease = make(chan struct{})
	p.ov.OnClean = func(_ context.Context, req cleanup.CleanRequest, _ app.CleanOrigin, progress func(cleanup.Progress)) (app.OverviewOutcome, error) {
		return app.OverviewOutcome{RunID: "r1", Undoable: true, Result: cleanup.CleanResult{Trashed: 600 * mb, Removed: 1}}, nil
	}
	p.Press("id:rec-installers-act")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	if p.Exists("id:rec-confirm") {
		t.Fatal("a move to the Trash asked first")
	}
	c := p.cleans()[0]
	if c.Origin.Recommendation != "installers" || !slices.Equal(c.Req.CategoryIDs, []string{"installers:disk-images"}) ||
		!slices.Equal(c.Req.ItemIDs, []string{cleanup.ItemID(home + "/Downloads/Foo 1.0.dmg")}) {
		t.Errorf("clean %+v", c)
	}
	p.waitText("id:rec-installers-act", "正在移到废纸篓…")
	if !p.Find("id:rec-installers-act").Props.Busy {
		t.Error("the card's button is not busy")
	}
	// One thing at a time.
	if !p.disabled("id:rec-app-caches-act") || !p.disabled("id:recheck") || !p.disabled("id:empty-trash") {
		t.Error("another action while one runs")
	}
	if l := p.Find("id:rec-app-caches-act").Props.Label; l != "正在清理另一项，完成后再试" {
		t.Errorf("other card's label %q", l)
	}
	if l := p.Find("id:recheck").Props.Label; l != "正在清理，完成后再检查" {
		t.Errorf("recheck label %q", l)
	}
	// Past 600 ms the progress shows.
	p.ticking(func() bool { return p.Exists("id:rec-installers-progress") })
	p.WaitFor("id:rec-installers-stop", wait)
	// The controller drops the card before the clean returns; the card stays until it ends.
	next := ready()
	next.Result.Recommendations = slices.DeleteFunc(slices.Clone(next.Result.Recommendations), func(r advice.Recommendation) bool { return r.ID == "installers" })
	p.set(next)
	p.WaitFor("id:rec-installers", wait)
	close(p.ov.CleanRelease)
	p.ticking(func() bool { return p.toast() != nil })
	got := p.toast()
	if got.Text != "已移到废纸篓 600 MB" || got.UndoRun != "r1" || got.ViewRun != "" || got.Duration != 10*time.Second {
		t.Errorf("toast %+v", got)
	}
	p.ticking(func() bool { return !p.Exists("id:rec-installers") })
	if p.disabled("id:rec-app-caches-act") {
		t.Error("the other cards stay disabled")
	}
	// While 撤销 shows, emptying the Trash waits.
	if !p.disabled("id:empty-trash") || p.Find("id:empty-trash").Props.Label != "请等撤销提示消失后再清空" {
		t.Error("清空废纸篓 while 撤销 shows")
	}
}

// Leaving 概览 while a card cleans neither stops the clean nor loses its toast with 撤销: nothing holds the sidebar during a card's clean.
func TestCardCleanOutlivesThePage(t *testing.T) {
	p := open(t, ready())
	p.ov.CleanRelease = make(chan struct{})
	stopped := make(chan bool, 1)
	p.ov.OnClean = func(ctx context.Context, _ cleanup.CleanRequest, _ app.CleanOrigin, _ func(cleanup.Progress)) (app.OverviewOutcome, error) {
		stopped <- ctx.Err() != nil
		return app.OverviewOutcome{RunID: "r1", Undoable: true, Result: cleanup.CleanResult{Trashed: 600 * mb, Removed: 1}}, nil
	}
	p.Press("id:rec-installers-act")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	p.Post(func() { p.nav.Go(nav.Space) })
	p.WaitUntil(wait, func() bool { return !p.Exists("id:rec-installers") })
	close(p.ov.CleanRelease)
	if <-stopped {
		t.Fatal("leaving the page stopped the clean")
	}
	p.ticking(func() bool { return p.toast() != nil })
	if got := p.toast(); got.Text != "已移到废纸篓 600 MB" || got.UndoRun != "r1" {
		t.Errorf("toast %+v", got)
	}
	// Back on 概览, nothing is left locked.
	p.ticking(func() bool { return !p.stores.Overview.Busy() })
	p.Post(func() { p.nav.Go(nav.Overview) })
	p.WaitFor("id:rec-app-caches-act", wait)
	if p.disabled("id:rec-app-caches-act") {
		t.Error("a card stays disabled after the clean ended elsewhere")
	}
}

// Back on 概览 while a card's clean still runs, the card shows it as its own, with its progress and 停止, which stops it.
func TestCardCleanSeenAfterComingBack(t *testing.T) {
	p := open(t, ready())
	p.ov.CleanRelease = make(chan struct{})
	stopped := make(chan bool, 1)
	p.ov.OnClean = func(ctx context.Context, _ cleanup.CleanRequest, _ app.CleanOrigin, _ func(cleanup.Progress)) (app.OverviewOutcome, error) {
		stopped <- ctx.Err() != nil
		return app.OverviewOutcome{Result: cleanup.CleanResult{Interrupted: true}}, nil
	}
	p.Press("id:rec-installers-act")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	p.Post(func() { p.nav.Go(nav.Space) })
	p.WaitUntil(wait, func() bool { return !p.Exists("id:rec-installers") })
	p.Post(func() { p.nav.Go(nav.Overview) })
	p.WaitFor("id:rec-installers-progress", wait)
	if l := p.Find("id:rec-app-caches-act").Props.Label; l != "正在清理另一项，完成后再试" || !p.disabled("id:rec-app-caches-act") {
		t.Errorf("other card's label %q", l)
	}
	p.Press("id:rec-installers-stop")
	if !<-stopped {
		t.Fatal("停止 did not stop the clean")
	}
	p.ticking(func() bool { return !p.stores.Overview.Busy() })
	p.WaitForGone("id:rec-installers-stop", wait)
	if p.disabled("id:rec-app-caches-act") {
		t.Error("a card stays disabled after the clean stopped")
	}
}

// A clean that leaves items keeps the card with how many, and the toast offers 查看.
func TestTrashCardPartlyFailed(t *testing.T) {
	p := open(t, ready())
	p.ov.OnClean = func(context.Context, cleanup.CleanRequest, app.CleanOrigin, func(cleanup.Progress)) (app.OverviewOutcome, error) {
		return app.OverviewOutcome{RunID: "r2", Undoable: true, NotUndoable: 1, Result: cleanup.CleanResult{Trashed: 200 * mb,
			Failed: []cleanup.Failure{{ItemID: "x", Reason: "没有权限"}, {ItemID: "y", InUse: true}}}}, nil
	}
	p.Press("id:rec-incomplete-downloads-act")
	p.ticking(func() bool { return p.toast() != nil })
	got := p.toast()
	if got.Text != "已移到废纸篓 200 MB，其中 1 项不能撤销，1 项未能处理，1 项正在使用，已跳过" || got.UndoRun != "r2" || got.ViewRun != "r2" {
		t.Errorf("toast %+v", got)
	}
	p.waitText("id:rec-incomplete-downloads-failed", "2 项未能处理")
	p.ticking(func() bool { return p.Exists("id:rec-incomplete-downloads") })
}

// Deleting for good always asks, naming what and how much; 取消 does nothing.
func TestDeleteCard(t *testing.T) {
	p := open(t, ready())
	p.ov.OnClean = func(context.Context, cleanup.CleanRequest, app.CleanOrigin, func(cleanup.Progress)) (app.OverviewOutcome, error) {
		return app.OverviewOutcome{RunID: "r3", Result: cleanup.CleanResult{Freed: 2 * gb, Removed: 2}}, nil
	}
	p.Press("id:rec-app-caches-act")
	p.WaitFor("id:rec-confirm", wait)
	p.Find("id:rec-confirm >> text:删除应用缓存？")
	p.Find("id:rec-confirm >> text:2 GB（2 项）会直接删除，不经过废纸篓，无法撤销。应用下次打开时会重新生成，第一次可能稍慢。")
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:rec-confirm", wait)
	if len(p.cleans()) != 0 {
		t.Fatal("取消 cleaned")
	}
	p.Press("id:rec-app-caches-act")
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	p.ticking(func() bool { return p.toast() != nil })
	if got := p.toast(); got.Text != "已释放 2 GB" || got.UndoRun != "" || got.Duration != 6*time.Second {
		t.Errorf("toast %+v", got)
	}
}

// A command asks first, naming it in the monospaced font.
func TestCommandCard(t *testing.T) {
	p := open(t, ready())
	p.Press("id:rec-tool-npm-act")
	p.WaitFor("id:rec-confirm", wait)
	p.Find("id:rec-confirm >> text:运行 npm cache？")
	msg := p.Find("id:rec-confirm-message")
	if msg.Text != "将运行 npm cache clean --force，清空 npm 下载的包（约 1.3 GB），无法撤销。停止后，正在运行的命令会先完成。" {
		t.Errorf("message %q", msg.Text)
	}
	if len(msg.Spans) != 3 {
		t.Errorf("spans %+v", msg.Spans)
	}
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	if c := p.cleans()[0]; !slices.Equal(c.Req.CategoryIDs, []string{"dev-junk:tool-npm"}) || c.Origin.Recommendation != "tool-npm" {
		t.Errorf("clean %+v", c)
	}
}

// A mixed card asks with the plan; unchecking what is deleted for good makes it a move to the Trash, with its size on the button.
func TestMixedCardAndPartialSelection(t *testing.T) {
	p := open(t, ready())
	p.Press("id:rec-projects-idle-act")
	p.WaitFor("id:rec-confirm", wait)
	p.Find("id:rec-confirm >> text:确认清理")
	p.WaitFor("id:rec-confirm >> id:plan-permanent", wait)
	p.WaitFor("id:rec-confirm >> id:plan-trash", wait)
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:rec-confirm", wait)

	p.Press("id:rec-projects-idle-expand")
	p.WaitFor("id:rec-projects-idle >> text:已选 2/2 项 · 1.4 GB", wait)
	p.Press("id:item-" + cleanup.ItemID(home+"/ws/a/node_modules"))
	p.waitText("id:rec-projects-idle-act", "移到废纸篓 · 400 MB")
	p.WaitFor("id:rec-projects-idle-badge >> text:可撤销", wait)
	p.Press("id:rec-projects-idle-act")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	if c := p.cleans()[0]; !slices.Equal(c.Req.ItemIDs, []string{cleanup.ItemID(home + "/Documents/d/node_modules")}) ||
		!slices.Equal(c.Req.CategoryIDs, []string{"dev-junk:artifact-node-modules-trash"}) {
		t.Errorf("clean %+v", c)
	}
}

// On Windows a move to the Recycle Bin asks first and cannot be undone from Spruce.
func TestRecycleBinCard(t *testing.T) {
	st := ready()
	st.CanUndo = false
	p := open(t, st)
	p.WaitFor("id:rec-installers-badge >> text:移到回收站", wait)
	p.ov.OnClean = func(context.Context, cleanup.CleanRequest, app.CleanOrigin, func(cleanup.Progress)) (app.OverviewOutcome, error) {
		return app.OverviewOutcome{RunID: "r4", Result: cleanup.CleanResult{Trashed: 600 * mb}}, nil
	}
	p.Press("id:rec-installers-act")
	p.WaitFor("id:rec-confirm", wait)
	p.Find("id:rec-confirm >> text:把下载里的安装包移到回收站？")
	p.Find("id:rec-confirm >> text:600 MB（1 项）会移到回收站，需要时可以在回收站里还原。")
	p.Press("id:confirm-ok")
	p.ticking(func() bool { return p.toast() != nil })
	if got := p.toast(); got.Text != "已移到回收站 600 MB，需要时可以在回收站里还原" || got.UndoRun != "" || got.Duration != 6*time.Second {
		t.Errorf("toast %+v", got)
	}
}

// A card the user changed keeps the change through a newer check and leaves what it adds unchecked; one the user did not follows the check.
func TestNewerResultKeepsTheSelection(t *testing.T) {
	p := open(t, ready())
	p.Press("id:rec-app-caches-expand")
	p.Press("id:item-" + cleanup.ItemID(home+"/Library/Caches/com.apple.Safari"))
	p.waitText("id:rec-app-caches-act", "删除… · 1.2 GB")

	res := scan()
	for i := range res.Categories {
		if res.Categories[i].ID == "system-junk:app-caches" {
			res.Categories[i].Items = append(res.Categories[i].Items, item(home+"/Library/Caches/com.new", 500*mb, true, 40*24*time.Hour))
		}
		if res.Categories[i].ID == "system-junk:logs" {
			res.Categories[i].Items = append(res.Categories[i].Items, item(home+"/Library/Logs/b.log", 100*mb, true, 60*24*time.Hour))
		}
	}
	res = cleanup.NewResult(res.Categories, now)
	res.Sources = scan().Sources
	st := ready()
	st.Result = result(res)
	p.set(st)
	p.waitText("id:rec-app-caches-added", "检查后新增 1 项，未选中")
	p.waitText("id:rec-app-caches-act", "删除… · 1.2 GB")
	if !p.Find("id:rec-app-caches-expand").Props.Expanded {
		t.Error("the card closed")
	}
	p.Press("id:recs-more")
	p.Press("id:rec-logs-expand")
	p.WaitFor("id:rec-logs >> text:已选 2/2 项 · 300 MB", wait)
	if p.Exists("id:rec-logs-added") {
		t.Error("an untouched card says items were added")
	}
}

// withItem is scan with one more item, checked, in the category id.
func withItem(id string, it cleanup.Item) cleanup.ScanResult {
	res := scan()
	for i := range res.Categories {
		if res.Categories[i].ID == id {
			res.Categories[i].Items = append(res.Categories[i].Items, it)
		}
	}
	out := cleanup.NewResult(res.Categories, now)
	out.Sources = res.Sources
	return out
}

// A newer result that lands while a confirmation is open does not change what the confirmed clean acts on: the card, untouched, now holds one more item, which the confirmation never named.
func TestConfirmCleansWhatItShowed(t *testing.T) {
	p := open(t, ready())
	p.Press("id:rec-app-caches-act")
	p.WaitFor("id:rec-confirm", wait)
	p.Find("id:rec-confirm >> text:2 GB（2 项）会直接删除，不经过废纸篓，无法撤销。应用下次打开时会重新生成，第一次可能稍慢。")

	st := ready()
	st.Result = result(withItem("system-junk:app-caches", item(home+"/Library/Caches/com.new", 500*mb, true, 40*24*time.Hour)))
	p.set(st)
	p.waitText("id:rec-app-caches-size", "2.5 GB")
	p.WaitFor("id:rec-confirm", wait)
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	want := []string{cleanup.ItemID(home + "/Library/Caches/com.spotify.client"), cleanup.ItemID(home + "/Library/Caches/com.apple.Safari")}
	if c := p.cleans()[0]; !slices.Equal(c.Req.ItemIDs, want) || !slices.Equal(c.Req.CategoryIDs, []string{"system-junk:app-caches"}) {
		t.Errorf("clean %+v, want the confirmed %v", c, want)
	}
}

// A confirmation goes with the card that asked: a newer result without the card closes it, and nothing runs.
func TestConfirmClosesWithItsCard(t *testing.T) {
	p := open(t, ready())
	p.Press("id:rec-app-caches-act")
	p.WaitFor("id:rec-confirm", wait)
	next := ready()
	next.Result.Recommendations = slices.DeleteFunc(slices.Clone(next.Result.Recommendations), func(r advice.Recommendation) bool { return r.ID == "app-caches" })
	p.set(next)
	p.WaitForGone("id:rec-app-caches", wait)
	p.WaitForGone("id:rec-confirm", wait)
	if len(p.cleans()) != 0 {
		t.Fatal("a closed confirmation cleaned")
	}
	if p.disabled("id:rec-installers-act") || p.disabled("id:recheck") {
		t.Error("the page stays locked")
	}
}

// A card that a newer result moves past the first ones while its clean runs does not lock the page: the clean goes on, and when it ends the toast shows and the other cards can act again.
func TestCardMovedAwayWhileCleaning(t *testing.T) {
	p := open(t, ready())
	p.ov.CleanRelease = make(chan struct{})
	p.ov.OnClean = func(context.Context, cleanup.CleanRequest, app.CleanOrigin, func(cleanup.Progress)) (app.OverviewOutcome, error) {
		return app.OverviewOutcome{RunID: "r5", Undoable: true, Result: cleanup.CleanResult{Trashed: 600 * mb, Removed: 1}}, nil
	}
	p.Press("id:rec-installers-act")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	p.waitText("id:rec-installers-act", "正在移到废纸篓…")

	// The controller ranks installers last now, past the first five cards.
	next := ready()
	recs := next.Result.Recommendations
	i := slices.IndexFunc(recs, func(r advice.Recommendation) bool { return r.ID == "installers" })
	moved := recs[i]
	recs = append(slices.Delete(slices.Clone(recs), i, i+1), moved)
	next.Result.Recommendations = recs
	p.set(next)
	p.WaitForGone("id:rec-installers", wait)

	close(p.ov.CleanRelease)
	p.ticking(func() bool { return p.toast() != nil })
	if got := p.toast(); got.Text != "已移到废纸篓 600 MB" || got.UndoRun != "r5" {
		t.Errorf("toast %+v", got)
	}
	p.WaitUntil(wait, func() bool { return !p.disabled("id:rec-app-caches-act") && !p.disabled("id:recheck") })
}

// 全选 and 全不选 act on the card; with nothing checked it cannot act.
func TestSelectAllNone(t *testing.T) {
	p := open(t, ready())
	p.Press("id:rec-app-caches-expand")
	p.Press("id:rec-app-caches-none")
	p.WaitFor("id:rec-app-caches >> text:已选 0/2 项 · 0 B", wait)
	if !p.disabled("id:rec-app-caches-act") {
		t.Error("acting on nothing")
	}
	p.Press("id:rec-app-caches-all")
	p.waitText("id:rec-app-caches-act", "删除…")
}

// 在{来源}中查看全部, 按类别查看 and 需要你判断 open the drill-down views.
func TestDrillLinks(t *testing.T) {
	p := open(t, ready())
	p.Press("id:rec-tool-npm-expand")
	p.waitText("id:rec-tool-npm-more", "在开发者中查看全部")
	p.Press("id:rec-tool-npm-more")
	if p.nav.Current() != nav.DevJunk || p.nav.TakeDevTab() != nav.DevTools || p.nav.TakeReveal() != "dev-junk:tool-npm" {
		t.Error("在开发者中查看全部 does not open 工具缓存 on npm")
	}
	p.nav.Go(nav.Overview)
	p.WaitFor("id:browse-dev-junk", wait)
	p.Press("id:browse-dev-junk")
	if p.nav.Current() != nav.DevJunk || p.nav.TakeDevTab() != nav.DevProjects {
		t.Error("按类别查看 does not open 项目")
	}
	p.nav.Go(nav.Overview)
	p.WaitFor("id:browse-installers", wait)
	p.Press("id:browse-installers")
	if p.nav.Current() != nav.Installers {
		t.Errorf("安装包 went to %q", p.nav.Current())
	}
	p.nav.Go(nav.Overview)
	p.WaitFor("id:review-link-system-junk", wait)
	p.Press("id:review-link-system-junk")
	if p.nav.Current() != nav.SystemJunk {
		t.Errorf("在系统垃圾中查看 went to %q", p.nav.Current())
	}
}

// 清空废纸篓… reads the Trash first and asks with what it holds now.
func TestEmptyTrash(t *testing.T) {
	p := open(t, ready())
	p.ov.TrashInfoValue = app.TrashInfo{Items: 42, Bytes: 2_100_000_000, Restorable: 3}
	p.ov.OnClean = func(context.Context, cleanup.CleanRequest, app.CleanOrigin, func(cleanup.Progress)) (app.OverviewOutcome, error) {
		return app.OverviewOutcome{RunID: "t1", Result: cleanup.CleanResult{Freed: 2_100_000_000}}, nil
	}
	p.Press("id:empty-trash")
	p.WaitFor("id:empty-trash-confirm", wait)
	p.Find("id:empty-trash-confirm >> text:清空废纸篓？")
	p.Find("id:empty-trash-confirm >> text:废纸篓里的 42 项（2.1 GB）会永久删除，无法恢复。其中 3 项是 Spruce 移到废纸篓的，清空后不能再放回原处。")
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	if c := p.cleans()[0]; !slices.Equal(c.Req.CategoryIDs, []string{"system-junk:trash"}) || c.Origin.Source != "system-junk" {
		t.Errorf("clean %+v", c)
	}
	p.ticking(func() bool { return p.toast() != nil })
	if got := p.toast(); got.Text != "已释放 2.1 GB" {
		t.Errorf("toast %+v", got)
	}

	q := open(t, ready())
	q.ov.TrashErr = errors.New("无法读取")
	q.Press("id:empty-trash")
	q.WaitFor("id:empty-trash-confirm >> text:废纸篓里的全部项目会永久删除，无法恢复。", wait)
}

// Without Full Disk Access the Trash row says why it cannot be read and offers nothing.
func TestTrashUnreadable(t *testing.T) {
	res := scan()
	for i := range res.Categories {
		if res.Categories[i].ID == "system-junk:trash" {
			res.Categories[i].Items, res.Categories[i].Size = nil, 0
			res.Categories[i].Warning = "需要为 Spruce 开启「完全磁盘访问权限」才能读取废纸篓，开启后重新检查"
		}
	}
	st := ready()
	st.Result = result(res)
	p := open(t, st)
	p.waitText("id:trash-text", "需要为 Spruce 开启「完全磁盘访问权限」才能读取废纸篓，开启后重新检查")
	if p.Exists("id:empty-trash") {
		t.Error("清空废纸篓 without a readable Trash")
	}
}

// 空间变化 lists what grew most, with 去清理 when a card covers it.
func TestGrowth(t *testing.T) {
	st := ready()
	ov := storefake.NewOverview(st)
	ov.GrowthList = []growth.Growth{
		{Path: home + "/Library/Caches/com.spotify.client", Before: 1 * gb, After: 7 * gb, Since: now.Add(-3 * 24 * time.Hour)},
		{Path: home + "/Movies", Label: "影片", Icon: "video", Before: 10 * gb, After: 12 * gb, Since: now.Add(-6 * time.Hour), AtLeast: true},
		{Path: home + "/a", Before: 1 * gb, After: 2 * gb, Since: now.Add(-3 * 24 * time.Hour)},
		{Path: home + "/b", Before: 1 * gb, After: 2 * gb, Since: now.Add(-3 * 24 * time.Hour)},
	}
	p := start(t, newFakes(t), ov)
	p.WaitFor("id:growth", wait)
	p.Find("id:growth >> text:最近 7 天")
	if got := len(p.Query("id:growth >> id:growth-row-3")); got != 0 {
		t.Error("more than 3 rows")
	}
	p.waitText("id:growth-row-0-text", "从"+[]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}[now.Add(-3*24*time.Hour).Weekday()]+"到现在涨了 6 GB")
	p.Find("id:growth-row-0 >> text:~/Library/Caches/com.spotify.client")
	p.Find("id:growth-row-0 >> text:+6 GB")
	p.Find("id:growth-row-0 >> text:1 GB → 7 GB")
	p.waitText("id:growth-row-1-text", "从6 小时前到现在至少涨了 2 GB")
	p.Find("id:growth-row-1 >> text:影片")
	p.WaitFor("id:growth-row-1-space", wait)
	if _, _, _, _, w := ov.Calls(); len(w) == 0 || w[0] != 7*24*time.Hour {
		t.Errorf("windows %v", w)
	}
	p.Press("id:growth-row-1-space")
	if p.nav.Current() != nav.Space || p.nav.TakeSpacePath() != home+"/Movies" {
		t.Error("在空间透视中查看 does not open 空间透视 on the folder")
	}
	p.nav.Go(nav.Overview)
	p.WaitFor("id:growth-row-0-clean", wait)
	p.Press("id:growth-row-0-clean")
	p.WaitUntil(wait, func() bool { return p.Find("id:rec-app-caches-expand").Props.Expanded })

	q := open(t, ready())
	r := start(t, newFakes(t), func() *storefake.Overview {
		o := storefake.NewOverview(ready())
		o.GrowthErr = growth.ErrNoHistory
		return o
	}())
	r.waitText("id:growth-empty", "Spruce 会定期记下主要位置的大小，过一两天这里就会显示哪些位置在变大。")
	q.waitText("id:growth-empty", "最近 7 天没有明显变大的位置。")
	// Snapshots that cannot be read are said to be so, not to be coming.
	u := start(t, newFakes(t), func() *storefake.Overview {
		o := storefake.NewOverview(ready())
		o.GrowthErr = errors.New("read growth: permission denied")
		return o
	}())
	u.waitText("id:growth-error", "无法读取空间变化的记录")
	if got := len(u.Query("id:growth-empty")); got != 0 {
		t.Error("unreadable snapshots read as too few")
	}
}

// GoOverviewCard opens 概览 with the card shown and expanded, also past the first five.
func TestGoOverviewCard(t *testing.T) {
	p := start(t, newFakes(t), storefake.NewOverview(ready()), func(n *nav.Navigator) { n.GoOverviewCard("logs") })
	p.WaitUntil(wait, func() bool { return p.Exists("id:rec-logs-expand") && p.Find("id:rec-logs-expand").Props.Expanded })
	if p.Exists("id:recs-more") {
		t.Error("再显示 still shows")
	}
}

// Installers outside 下载 make the card 旧安装包, naming the folders.
func TestInstallersTitle(t *testing.T) {
	res := scan()
	for i := range res.Categories {
		if res.Categories[i].ID == "installers:disk-images" {
			res.Categories[i].Items = append(res.Categories[i].Items, item(home+"/Desktop/Bar.dmg", 300*mb, true, 30*24*time.Hour))
		}
	}
	res = cleanup.NewResult(res.Categories, now)
	st := ready()
	st.Result = result(res)
	p := open(t, st)
	p.waitText("id:rec-installers-title", "旧安装包")
	p.waitText("id:rec-installers-reason", "2 个安装包，都下载超过 7 天了，在下载、桌面里")
}

// The times read again every minute, on the scope's clock.
func TestTimesFollowTheClock(t *testing.T) {
	p := open(t, ready())
	p.waitText("id:checked-at", "上次检查：5分钟前")
	p.Advance(time.Minute)
	p.waitText("id:checked-at", "上次检查：6分钟前")
}

// Leaving 概览 keeps what the user did on it: a card's unchecked item stays unchecked and its card open, with 再显示 still showing every card.
func TestCardsComeBackAsLeft(t *testing.T) {
	p := open(t, ready())
	safari := "id:item-" + cleanup.ItemID(home+"/Library/Caches/com.apple.Safari")
	p.Press("id:recs-more")
	p.Press("id:rec-app-caches-expand")
	p.Press(safari)
	p.waitText("id:rec-app-caches-act", "删除… · 1.2 GB")

	p.nav.Go(nav.DevJunk)
	p.WaitForGone("id:overview-page", wait)
	p.nav.Go(nav.Overview)
	p.WaitFor("id:rec-app-caches", wait)
	p.waitText("id:rec-app-caches-act", "删除… · 1.2 GB")
	if !p.Find("id:rec-app-caches-expand").Props.Expanded {
		t.Error("the card closed")
	}
	if p.Find(safari).Props.Checked {
		t.Error("the unchecked item came back checked")
	}
	p.WaitFor("id:rec-logs", wait)
	p.Press("id:rec-app-caches-act")
	p.WaitFor("id:rec-confirm", wait)
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	if got := p.cleans()[0].Req.ItemIDs; !slices.Equal(got, []string{cleanup.ItemID(home + "/Library/Caches/com.spotify.client")}) {
		t.Errorf("cleaned %v", got)
	}
}

// A card and its drill-down view check the same items: 在系统垃圾中查看全部 shows what the card unchecked as unchecked, and what changes there shows on the card.
func TestCardAndDrillShareTheSelection(t *testing.T) {
	p := open(t, ready())
	safari, spotify := "id:item-"+cleanup.ItemID(home+"/Library/Caches/com.apple.Safari"), "id:item-"+cleanup.ItemID(home+"/Library/Caches/com.spotify.client")
	p.Press("id:rec-app-caches-expand")
	p.Press(safari)
	p.Press("id:rec-app-caches-more")
	p.WaitFor("id:category-list", wait)
	p.WaitFor(safari, wait)
	if p.Find(safari).Props.Checked || !p.Find(spotify).Props.Checked {
		t.Error("the drill-down view does not show the card's selection")
	}
	if !p.Find("id:check-system-junk:app-caches").Props.Indeterminate {
		t.Error("应用缓存 is not checked in part")
	}
	p.Press(spotify)
	p.Press("id:back")
	p.WaitFor("id:rec-app-caches", wait)
	p.WaitFor("id:rec-app-caches >> text:已选 0/2 项 · 0 B", wait)
	if !p.disabled("id:rec-app-caches-act") {
		t.Error("the card acts on items unchecked in its drill-down view")
	}
}

// A check over a result shows how far it is under the conclusion, and the header stops it instead of waiting: the result stays, and the cards can act once the controller is idle again. The menu's 重新检查 (⌘R) checks again as the header does, and waits while a check runs.
func TestStopACheckOverAResult(t *testing.T) {
	p := open(t, ready())
	p.waitText("id:recheck", "重新检查")
	if !p.nav.CanRefresh() {
		t.Fatal("⌘R cannot check again")
	}
	p.Post(func() { p.nav.Commands().Refresh() })
	p.WaitUntil(wait, func() bool { return p.checks() == 1 })

	st := ready()
	// The controller locks the result while the check runs.
	st.Phase, st.Progress = app.OverviewChecking, &cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 47, Total: 100}
	st.Result.Actionable = false
	p.set(st)
	p.waitText("id:checked-at", "上次检查：5分钟前 · 正在更新… 47%")
	p.waitText("id:stop-check", "停止检查")
	if p.Exists("id:recheck") || p.nav.CanRefresh() {
		t.Error("重新检查 is offered while a check runs")
	}
	p.waitText("id:rec-app-caches-act", "正在核对…")
	p.Press("id:stop-check")
	p.WaitUntil(wait, func() bool { _, stops, _, _, _ := p.ov.Calls(); return stops == 1 })

	p.set(ready())
	p.waitText("id:recheck", "重新检查")
	p.waitText("id:conclusion-value", "5.8 GB")
	p.waitText("id:rec-app-caches-act", "删除…")
	if p.disabled("id:rec-app-caches-act") {
		t.Error("the cards cannot act once the check stopped")
	}
}
