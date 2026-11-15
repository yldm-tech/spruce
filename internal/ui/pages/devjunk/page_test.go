package devjunk_test

import (
	"fmt"
	"reflect"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	engdev "github.com/yldm-tech/spruce/internal/engine/devjunk"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow/cleanflowfake"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/pages/devjunk"
	"github.com/yldm-tech/spruce/internal/ui/store"
	"github.com/yldm-tech/spruce/internal/ui/store/storefake"
)

// wait is real time; the race detector on a loaded machine slows every frame.
const wait = 20 * time.Second

var now = time.Date(2026, 11, 14, 12, 0, 0, 0, time.UTC)

// tools are the check's tool caches: npm cleaned by its command (checked), Xcode archives that go to the Trash (unchecked) and device support checked in part.
func tools() []cleanup.Category {
	item := func(path string, size int64, selected bool) cleanup.Item {
		return cleanup.Item{ID: cleanup.ItemID(path), Path: path, Size: size, ModTime: now.Add(-3 * 24 * time.Hour), Selected: selected, Dir: true}
	}
	npm := cleanup.Category{ID: "dev-junk:tool-npm", Source: "dev-junk", Title: "npm 缓存", Description: "运行 npm cache clean --force，清空 npm 下载的包", Icon: "node",
		Kind: cleanup.KindDevCache, Deletion: cleanup.DeletePermanent, Command: "npm cache clean --force", Items: []cleanup.Item{item("/Users/me/.npm/_cacache", 1000, true)}}
	archives := cleanup.Category{ID: "dev-junk:tool-xcode-archives", Source: "dev-junk", Title: "Xcode 归档", Description: "Xcode 打包时留下的归档", Icon: "archive",
		Kind: cleanup.KindDevCache, Deletion: cleanup.DeleteTrash, Warning: "包含已发布版本的符号文件", Items: []cleanup.Item{item("/Users/me/Library/Developer/Xcode/Archives/a", 500, false)}}
	support := cleanup.Category{ID: "dev-junk:tool-xcode-device-support", Source: "dev-junk", Title: "设备支持文件", Description: "连接真机调试时复制的系统符号", Icon: "simulator",
		Kind: cleanup.KindDevCache, Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{
			item("/Users/me/Library/Developer/Xcode/iOS DeviceSupport/18.2", 3000, false), item("/Users/me/Library/Developer/Xcode/iOS DeviceSupport/17.5", 2000, true)}}
	return []cleanup.Category{npm, archives, support}
}

// artifacts are four projects' build output: shop (clean, idle, two folders, checked), blog (dirty, held), tool (Git unknown, recent, unchecked) and lib (tracked files, held).
func artifacts() []cleanup.Category {
	day := 24 * time.Hour
	art := func(project, name string, size int64, git cleanup.GitState, hold string, idle time.Duration, selected bool) cleanup.Item {
		p := project + "/" + name
		return cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: size, ModTime: now.Add(-idle), Project: project, Git: git, Hold: hold, Selected: selected, Dir: true,
			Note: strings.TrimPrefix(project, "/Users/me/Workspace/")}
	}
	nm := cleanup.Category{ID: "dev-junk:artifact-node-modules", Source: "dev-junk", Title: "node_modules", Description: "安装的 npm 依赖", Icon: "node",
		Kind: cleanup.KindProjectArtifact, Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{
			art("/Users/me/Workspace/shop", "node_modules", 5000, cleanup.GitClean, "", 100*day, true),
			art("/Users/me/Workspace/blog", "node_modules", 4000, cleanup.GitDirty, "项目有未提交的改动", 100*day, false),
			art("/Users/me/Workspace/tool", "node_modules", 3000, cleanup.GitUnknown, "", 5*day, false),
			art("/Users/me/Workspace/lib", "node_modules", 800, cleanup.GitClean, "构建产物文件夹里有纳入版本控制的文件", 200*day, false),
		}}
	next := cleanup.Category{ID: "dev-junk:artifact-next", Source: "dev-junk", Title: ".next", Description: "Next.js 的构建输出", Icon: "build",
		Kind: cleanup.KindProjectArtifact, Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{
			art("/Users/me/Workspace/shop", ".next", 2000, cleanup.GitClean, "", 100*day, true),
		}}
	return []cleanup.Category{nm, next}
}

// stateOf is an actionable check over cats, with its projects grouped as 概览's controller does.
func stateOf(cats ...cleanup.Category) app.OverviewState {
	res := cleanflowfake.Result(cats...)
	res.Sources = []cleanup.SourceState{{ID: "dev-junk", Status: cleanup.SourceDone}}
	return app.OverviewState{Phase: app.OverviewIdle, CanUndo: true,
		Result: &app.OverviewResult{CheckedAt: now, Mode: app.CheckForeground, Actionable: true, Scan: res, Projects: engdev.GroupProjects(res)}}
}

func ready() app.OverviewState { return stateOf(append(tools(), artifacts()...)...) }

func id(path string) string { return cleanup.ItemID(path) }

type page struct {
	*nsgotest.App
	ov     *storefake.Overview
	files  *cleanflowfake.Files
	stores *store.Stores
	nav    *nav.Navigator
	tb     testing.TB
}

// start opens 开发者 on st with the project folders roots.
func start(t *testing.T, st app.OverviewState, roots []string, before ...func(*nav.Navigator)) *page {
	t.Helper()
	return startSized(t, st, roots, nsgotest.Size(1052, 1200), before...)
}

// startSized is start in a window of size.
func startSized(t *testing.T, st app.OverviewState, roots []string, size nsgotest.Option, before ...func(*nav.Navigator)) *page {
	t.Helper()
	if runtime.GOOS == "windows" {
		t.Skip("expectations use the macOS wording and SI sizes")
	}
	t.Cleanup(devjunk.SetNow(now))
	fakes := storefake.New(t.TempDir())
	fakes.Settings.Cleanup.DevProjectRoots = roots
	fakes.Settings.Cleanup.DevArtifactMinAgeDays = 30
	ov := storefake.NewOverview(st)
	fakes.Overview = ov
	p := &page{ov: ov, files: &cleanflowfake.Files{}, tb: t}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.stores = storefake.Provide(cx, fakes.Deps())
		p.nav = nav.NewNavigator(cx, nav.Overview)
		nav.Key.Provide(cx, p.nav)
		for _, f := range before {
			f(p.nav)
		}
		devjunk.DepsKey.Provide(cx, devjunk.Deps{Files: p.files})
		return core.Dynamic(func(*core.Scope) core.Node {
			if p.nav.Current() != nav.DevJunk {
				return core.Group{}
			}
			return core.Func(devjunk.Page)
		})
	}, size)
	p.WaitUntil(wait, func() bool {
		return p.stores.Settings.Loaded() && reflect.DeepEqual(p.stores.Overview.State(), ov.State())
	})
	return p
}

var roots = []string{"/Users/me/Workspace"}

func (p *page) checked(selector string) bool { return p.Find(selector).Props.Checked }

func (p *page) disabled(selector string) bool { return p.Find(selector).Props.Disabled }

func (p *page) cleans() []storefake.CleanCall { _, _, c, _, _ := p.ov.Calls(); return c }

// stat returns the figure beside 清理 under label.
func (p *page) stat(label string) string {
	for _, v := range p.Query("text:" + label) {
		kids := v.Parent.Children
		for i, k := range kids {
			if k == v && i+1 < len(kids) {
				return kids[i+1].Text
			}
		}
	}
	return ""
}

func (p *page) project(path string) string { return "id:project-" + id(path) }

// textOf is a view's text: its own, else its descendants' in order.
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

// 项目 is the first tab: the projects largest first, each with its Git status, the defaults of the check, held projects disabled and one of unknown Git state left to the user.
func TestProjectsTab(t *testing.T) {
	p := start(t, ready(), roots, func(n *nav.Navigator) { n.Go(nav.DevJunk) })
	p.Find("id:drill-dev-junk")
	p.Find("role:header 开发者")
	p.Find("id:back")
	if !p.Find("id:tab-projects").Props.Selected {
		t.Error("项目 is not the first tab")
	}
	rows := p.Query("id:projects >> role:group")
	var order []string
	for _, r := range rows {
		if strings.HasPrefix(r.Props.TestID, "project-") && !strings.HasSuffix(r.Props.TestID, "-items") {
			order = append(order, r.Props.Label)
		}
	}
	if !reflect.DeepEqual(order, []string{"shop", "blog", "tool", "lib"}) {
		t.Errorf("order %v", order)
	}
	shop, blog, tool, lib := "/Users/me/Workspace/shop", "/Users/me/Workspace/blog", "/Users/me/Workspace/tool", "/Users/me/Workspace/lib"
	p.Find(p.project(shop) + " >> text:node_modules · .next")
	p.Find(p.project(shop) + " >> text:7 KB")
	if p.Exists(p.project(shop) + "-git") {
		t.Error("a clean project shows a Git status")
	}
	if got := textOf(p.Find(p.project(blog) + "-git")); got != "有未提交的改动" {
		t.Errorf("blog git %q", got)
	}
	if got := p.Find(p.project(tool) + "-git").Text; got != "无法确定 Git 状态" {
		t.Errorf("tool git %q", got)
	}
	if got := p.Find(p.project(lib) + "-git").Text; got != "构建产物里有纳入版本控制的文件" {
		t.Errorf("lib git %q", got)
	}
	if !p.checked(p.project(shop)+"-check") || p.checked(p.project(tool)+"-check") {
		t.Error("defaults")
	}
	if !p.disabled(p.project(blog)+"-check") || !p.disabled(p.project(lib)+"-check") || p.disabled(p.project(tool)+"-check") {
		t.Error("held projects can be checked, or the unknown one cannot")
	}
	if got := p.stat("已选择"); got != "1 个项目" {
		t.Errorf("已选择 %q", got)
	}
	if got := p.stat("已选大小"); got != "7 KB" {
		t.Errorf("已选大小 %q", got)
	}
	// Git state unknown: never checked by default, but the user may.
	p.Press(p.project(tool) + "-check")
	if !p.checked(p.project(tool)+"-check") || p.stat("已选择") != "2 个项目" {
		t.Error("the unknown project could not be checked")
	}
	// 全选 leaves held projects alone.
	p.Press("id:select-none")
	p.Press("id:select-all")
	if p.checked(p.project(blog)+"-check") || p.checked(p.project(lib)+"-check") || p.stat("已选择") != "2 个项目" {
		t.Error("全选 checked a held project")
	}
	// Expanding shows each folder, held ones disabled.
	p.Press(p.project(blog) + "-expand")
	p.WaitFor(p.project(blog)+"-items", wait)
	if !p.disabled("id:item-" + id(blog+"/node_modules")) {
		t.Error("a held folder can be checked")
	}
	p.Press(p.project(shop) + "-reveal")
	p.WaitUntil(wait, func() bool { return len(p.files.Revealed()) == 1 })
	if got := p.files.Revealed()[0]; got != shop {
		t.Errorf("revealed %q", got)
	}
}

// The chips filter the projects: idle at least the setting's days, or with uncommitted changes.
func TestProjectFilters(t *testing.T) {
	p := start(t, ready(), roots, func(n *nav.Navigator) { n.Go(nav.DevJunk) })
	if got := textOf(p.Find("id:projects-filter-idle")); got != "闲置超过 30 天" {
		t.Errorf("idle chip %q", got)
	}
	p.Find("text:按大小排序")
	count := func() int { return len(p.Query("id:projects >> role:checkbox")) }
	p.Press("id:projects-filter-idle")
	p.WaitUntil(wait, func() bool { return count() == 3 })
	if p.Exists(p.project("/Users/me/Workspace/tool")) {
		t.Error("a recent project shows among the idle ones")
	}
	p.Press("id:projects-filter-dirty")
	p.WaitUntil(wait, func() bool { return count() == 1 })
	p.Find(p.project("/Users/me/Workspace/blog"))
	p.Press("id:projects-filter-all")
	p.WaitUntil(wait, func() bool { return count() == 4 })
}

// 清理 on 项目 asks with the lines of the plan and cleans only the checked build output, as 开发者's clean.
func TestProjectsClean(t *testing.T) {
	p := start(t, ready(), roots, func(n *nav.Navigator) { n.Go(nav.DevJunk) })
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	if got := p.Find("id:confirm-summary").Text; got != "将清理 2 项，共 7 KB。" {
		t.Errorf("summary %q", got)
	}
	if got := textOf(p.Find("id:confirm-line-0")); got != "•永久删除 2 项，无法恢复" {
		t.Errorf("line %q", got)
	}
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return len(p.cleans()) == 1 })
	want := storefake.CleanCall{Req: cleanup.CleanRequest{CategoryIDs: []string{"dev-junk:artifact-node-modules", "dev-junk:artifact-next"},
		ItemIDs: []string{id("/Users/me/Workspace/shop/node_modules"), id("/Users/me/Workspace/shop/.next")}}, Origin: app.CleanOrigin{Source: "dev-junk"}}
	if got := p.cleans()[0]; !reflect.DeepEqual(got, want) {
		t.Errorf("clean %+v", got)
	}
	p.WaitUntil(wait, func() bool { return p.stores.Toasts.Current() != nil })
	if got := p.stores.Toasts.Current().Text; got != "已释放 0 B" {
		t.Errorf("toast %q", got)
	}
}

// 工具缓存 lists the tools' caches; its 全选 and 全不选 leave the projects' checks alone, and a command's clean says it finishes first.
func TestToolsTab(t *testing.T) {
	p := start(t, ready(), roots, func(n *nav.Navigator) { n.Go(nav.DevJunk) })
	p.Press("id:tab-tools")
	p.WaitFor("id:list-tools", wait)
	p.Find("id:section-tools")
	p.Find("id:heading-tools")
	if p.Exists("id:projects") {
		t.Error("the projects show on 工具缓存")
	}
	if !p.checked("id:check-dev-junk:tool-npm") || p.checked("id:check-dev-junk:tool-xcode-archives") ||
		!p.Find("id:check-dev-junk:tool-xcode-device-support").Props.Indeterminate {
		t.Error("defaults")
	}
	if got := p.stat("已选择"); got != "2 项" {
		t.Errorf("已选择 %q", got)
	}
	p.Press("id:select-none")
	p.Press("id:tab-projects")
	p.WaitFor("id:projects", wait)
	if !p.checked(p.project("/Users/me/Workspace/shop") + "-check") {
		t.Error("全不选 on 工具缓存 unchecked a project")
	}
	p.Press("id:tab-tools")
	p.WaitFor("id:list-tools", wait)
	p.Press("id:check-dev-junk:tool-npm")
	p.ov.CleanRelease = make(chan struct{})
	p.Press("id:clean")
	p.WaitFor("id:confirm", wait)
	p.Find("text:运行 1 个清理命令：npm cache clean --force")
	p.Press("id:confirm-ok")
	p.WaitFor("id:clean-overlay >> text:共 1 项；停止后，正在运行的命令会先完成", wait)
	p.Press("id:overlay-cancel")
	p.WaitForGone("id:clean-overlay", wait)
	if got := p.cleans(); len(got) != 1 || !reflect.DeepEqual(got[0].Req.CategoryIDs, []string{"dev-junk:tool-npm"}) {
		t.Errorf("cleans %+v", got)
	}
}

// Links open the tab they ask for: GoDevTab, or a tool's category to reveal, expanded.
func TestLinksOpenTheirTab(t *testing.T) {
	p := start(t, ready(), roots, func(n *nav.Navigator) { n.GoDevTab(nav.DevTools) })
	p.WaitFor("id:list-tools", wait)

	q := start(t, ready(), roots, func(n *nav.Navigator) { n.GoDrill(nav.DevJunk, "dev-junk:tool-xcode-archives") })
	q.WaitFor("id:items-dev-junk:tool-xcode-archives", wait)

	r := start(t, ready(), roots, func(n *nav.Navigator) { n.GoDrill(nav.DevJunk, "dev-junk:artifact-node-modules") })
	r.WaitFor("id:projects", wait)
	r.Press("id:back")
	r.WaitUntil(wait, func() bool { return r.nav.Current() == nav.Overview })
}

// GoDrill on a tool's category below the fold opens 工具缓存 scrolled to it.
func TestOpenedOnAToolScrollsToIt(t *testing.T) {
	cats := make([]cleanup.Category, 16)
	for i := range cats {
		path := fmt.Sprintf("/Users/me/.cache/tool-%02d", i)
		cats[i] = cleanup.Category{ID: fmt.Sprintf("dev-junk:tool-t%02d", i), Source: "dev-junk", Title: fmt.Sprintf("工具 %02d", i), Icon: "build",
			Kind: cleanup.KindDevCache, Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{{ID: id(path), Path: path, Size: int64(100 + i), ModTime: now, Dir: true}}}
	}
	const last = "dev-junk:tool-t15"
	p := startSized(t, stateOf(cats...), roots, nsgotest.Size(1052, 600), func(n *nav.Navigator) { n.GoDrill(nav.DevJunk, last) })
	p.WaitFor("id:items-"+last, wait)
	p.WaitUntil(wait, func() bool { return p.Find("id:drill-dev-junk").Offset.Y > 0 })
	page := p.Find("id:drill-dev-junk")
	var y float32
	for v := p.Find("id:category-" + last); v != nil && v != page; v = v.Parent {
		y += float32(v.Frame.Origin.Y)
	}
	if top := y - float32(page.Offset.Y); top < 0 || top > float32(page.Frame.Size.Height) {
		t.Errorf("the category is at %v of a page %v tall, scrolled %v", top, page.Frame.Size.Height, page.Offset.Y)
	}
}

// A failed check of the source found nothing because it read nothing: neither tab says it is clean, and 重新检查 checks again.
func TestFailedSource(t *testing.T) {
	st := stateOf()
	st.Result.Scan.Sources = []cleanup.SourceState{{ID: "dev-junk", Status: cleanup.SourceFailed, Error: "读取超时"}}
	p := start(t, st, roots, func(n *nav.Navigator) { n.Go(nav.DevJunk) })
	if got := p.Find("id:source-failed-dev-junk").Text; got != "开发者没能检查完成：读取超时" {
		t.Errorf("failed %q", got)
	}
	p.Find("id:drill-failed >> text:暂时没有结果")
	if p.Exists("id:projects-empty") {
		t.Error("项目 says nothing was found")
	}
	p.Press("id:tab-tools")
	p.WaitFor("id:drill-failed", wait)
	if p.Exists("id:drill-empty") {
		t.Error("工具缓存 says the caches are clean")
	}
	p.Press("id:drill-retry")
	p.WaitUntil(wait, func() bool { c, _, _, _, _ := p.ov.Calls(); return c == 1 })
}

// Without projects: no project folder set leads to 设置; folders set but nothing found says so; no tool caches is the empty state.
func TestNoProjectsAndNoTools(t *testing.T) {
	p := start(t, stateOf(tools()...), nil, func(n *nav.Navigator) { n.Go(nav.DevJunk) })
	p.Find("id:projects-roots >> text:还没有设置项目文件夹，只检查了开发工具的缓存。")
	p.Press("role:link 去设置")
	p.WaitUntil(wait, func() bool { return p.nav.Current() == nav.Settings })

	q := start(t, stateOf(), roots, func(n *nav.Navigator) { n.Go(nav.DevJunk) })
	if got := q.Find("id:projects-empty").Text; got != "项目文件夹里没有找到构建产物。" {
		t.Errorf("empty %q", got)
	}
	q.Press("id:tab-tools")
	q.Find("id:drill-empty >> text:没有找到开发工具的缓存")
}

// The places the check could not read are told above the tabs.
func TestDenied(t *testing.T) {
	st := ready()
	st.Result.Scan.Sources[0].Denied = 2
	p := start(t, st, roots, func(n *nav.Navigator) { n.Go(nav.DevJunk) })
	if got := p.Find("id:denied-notice").Text; got != "有 2 个位置没有权限读取，结果可能不完整。" {
		t.Errorf("denied %q", got)
	}
}
