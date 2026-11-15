// Package nav names the app's pages and moves between them: the routes, what the sidebar and each page's header show for a page (the one port of src/components/common/pages.tsx), and the Navigator the shell provides.
//
// The sidebar has seven pages (Pages). 系统垃圾, 开发者 and 安装包 are drill-down views of 概览: routes with a Parent, outside the sidebar, which light their parent's entry and draw a link back.
package nav

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/kit/tone"
)

// Route names a page, as the React app's route path without the slash.
type Route string

// The pages.
const (
	Overview    Route = "overview"
	SystemJunk  Route = "system-junk"
	DevJunk     Route = "dev-junk"
	Installers  Route = "installers"
	Uninstall   Route = "uninstall"
	Space       Route = "space"
	Maintenance Route = "maintenance"
	Health      Route = "health"
	History     Route = "history"
	Settings    Route = "settings"
)

// Pages lists the sidebar's pages in its order; the drill-down views (SystemJunk, DevJunk, Installers) are not among them.
var Pages = []Route{Overview, Uninstall, Space, Maintenance, Health, History, Settings}

// Start is the page the app opens on.
const Start = Overview

// Meta is what the sidebar and a page's header show for a page.
type Meta struct {
	Title, Subtitle string
	Tone            tone.Tone
	// Glyph names the page's icon in package icons.
	Glyph string
	// Section is the sidebar group the page is under ("" for none); Footer puts it in the sidebar's footer, drawn as a bare glyph.
	Section string
	Footer  bool
	// Parent is the page a drill-down view belongs to: its header links back there and the sidebar lights it.
	Parent Route
}

var metas = map[Route]Meta{
	Overview:    {Title: "概览", Subtitle: "Spruce 会在后台定期检查，告诉你哪些空间可以放心释放", Tone: tone.Violet},
	SystemJunk:  {Title: "系统垃圾", Subtitle: "清理应用缓存、日志和临时文件，不会动你的文档和照片", Tone: tone.Green, Parent: Overview},
	DevJunk:     {Title: "开发者", Subtitle: "开发工具的缓存和项目里的构建产物，大多会在需要时重新下载或生成", Tone: tone.Orange, Parent: Overview},
	Installers:  {Title: "安装包", Subtitle: "找出装完就没再用的安装包和磁盘映像", Tone: tone.Pink, Parent: Overview},
	Uninstall:   {Title: "应用卸载", Subtitle: "连同应用留下的缓存和设置一起移除", Tone: tone.Blue, Section: "应用与空间"},
	Space:       {Title: "空间透视", Subtitle: "看看磁盘空间都被什么占用了", Tone: tone.Cyan, Section: "应用与空间"},
	Maintenance: {Title: "维护", Subtitle: "给系统做一次轻量保养，不会动你的文档和照片", Tone: tone.Amber, Section: "性能"},
	Health:      {Title: "健康监测", Subtitle: "实时查看 CPU、内存、磁盘、网络和电池，找出最占资源的进程", Tone: tone.Red, Section: "性能"},
	History:     {Title: "操作历史", Subtitle: "Spruce 做过的每一次维护、清理和卸载", Tone: tone.Slate, Footer: true},
	Settings:    {Title: "设置", Subtitle: "调整提醒、清理范围、排除的文件夹和历史记录", Tone: tone.Graphite, Footer: true},
}

// Meta returns what the sidebar and the header show for r.
func (r Route) Meta() Meta {
	m := metas[r]
	m.Glyph = "page/" + string(r)
	return m
}

// SettingsTab is a tab of the 设置 page.
type SettingsTab string

// The 设置 page's tabs, in their order.
const (
	SettingsGeneral SettingsTab = "general"
	SettingsAuto    SettingsTab = "auto"
	SettingsCleanup SettingsTab = "cleanup"
	SettingsHealth  SettingsTab = "health"
	SettingsHistory SettingsTab = "history"
	SettingsAbout   SettingsTab = "about"
)

// DevTab is a tab of the 开发者 view.
type DevTab string

// The 开发者 view's tabs.
const (
	DevProjects DevTab = "projects"
	DevTools    DevTab = "tools"
)

// Navigator holds the current page, where a drill-down view was entered from, and what a link asked the page it opens to show first (a 设置 tab, a category to reveal, a history filter, a 开发者 tab, an 概览 card, a 空间透视 folder). Each of those is read once by the page it is for (Take…), so the next visit opens plainly.
type Navigator struct {
	current     *core.Signal[Route]
	from        Route
	settingsTab SettingsTab
	reveal      string
	histKind    history.Kind
	histRun     string
	devTab      DevTab
	card        string
	spacePath   string

	// histAsked counts GoHistory's requests, so 操作历史 sees one made while it is already open.
	histAsked *core.Signal[int]
	// settingsAsked counts GoSettings's requests, so 设置 sees one made while it is already open.
	settingsAsked *core.Signal[int]
	// shown counts the times the main window came back after it was hidden, so 概览, which stayed mounted, checks again as it does when it opens.
	shown *core.Signal[int]
}

// NewNavigator returns a navigator on start.
func NewNavigator(cx *core.Scope, start Route) *Navigator {
	return &Navigator{current: core.NewSignal(cx, start), histAsked: core.NewSignal(cx, 0), settingsAsked: core.NewSignal(cx, 0), shown: core.NewSignal(cx, 0)}
}

// Current returns the page shown; reading it subscribes.
func (n *Navigator) Current() Route { return n.current.Get() }

// Go shows page r. Entering a drill-down view remembers the page it was entered from, for Back.
func (n *Navigator) Go(r Route) {
	if r.Meta().Parent != "" {
		if cur := n.current.GetUntracked(); cur != r {
			n.from = cur
		}
	}
	n.current.Set(r)
}

// Back leaves a drill-down view: to the sidebar page it was entered from, else to its parent.
func (n *Navigator) Back() {
	cur := n.current.GetUntracked()
	to := cur.Meta().Parent
	if to == "" {
		return
	}
	if n.from != "" && n.from.InSidebar() {
		to = n.from
	}
	n.from = ""
	n.current.Set(to)
}

// InSidebar reports whether r is one of the sidebar's pages.
func (r Route) InSidebar() bool {
	for _, p := range Pages {
		if p == r {
			return true
		}
	}
	return false
}

// GoDrill opens the drill-down view r with categoryID expanded and scrolled to ("" for none).
func (n *Navigator) GoDrill(r Route, categoryID string) {
	n.reveal = categoryID
	n.Go(r)
}

// TakeReveal returns the category GoDrill asked to reveal, or "", and forgets it.
func (n *Navigator) TakeReveal() string {
	c := n.reveal
	n.reveal = ""
	return c
}

// GoHistory opens 操作历史 with the filter of kind selected ("" for all) and the run runID expanded ("" for none).
func (n *Navigator) GoHistory(kind history.Kind, runID string) {
	n.histKind, n.histRun = kind, runID
	n.histAsked.Set(n.histAsked.GetUntracked() + 1)
	n.Go(History)
}

// HistoryAsked counts GoHistory's requests; reading it subscribes, so 操作历史 takes a request made while it is open (查看 in a toast shown over it).
func (n *Navigator) HistoryAsked() int { return n.histAsked.Get() }

// TakeHistoryFilter returns what GoHistory asked for, or zero values, and forgets it.
func (n *Navigator) TakeHistoryFilter() (history.Kind, string) {
	k, r := n.histKind, n.histRun
	n.histKind, n.histRun = "", ""
	return k, r
}

// GoDevTab opens the 开发者 view on tab.
func (n *Navigator) GoDevTab(tab DevTab) {
	n.devTab = tab
	n.Go(DevJunk)
}

// TakeDevTab returns the tab GoDevTab asked for, or "", and forgets it.
func (n *Navigator) TakeDevTab() DevTab {
	t := n.devTab
	n.devTab = ""
	return t
}

// GoOverviewCard opens 概览 scrolled to the card recID, expanded (空间变化's 去清理).
func (n *Navigator) GoOverviewCard(recID string) {
	n.card = recID
	n.Go(Overview)
}

// TakeOverviewCard returns the card GoOverviewCard asked for, or "", and forgets it.
func (n *Navigator) TakeOverviewCard() string {
	c := n.card
	n.card = ""
	return c
}

// GoSpace opens 空间透视 and starts its scan of path (空间变化's 在空间透视中查看).
func (n *Navigator) GoSpace(path string) {
	n.spacePath = path
	n.Go(Space)
}

// TakeSpacePath returns the folder GoSpace asked for, or "", and forgets it; 空间透视 reads it once as it opens.
func (n *Navigator) TakeSpacePath() string {
	p := n.spacePath
	n.spacePath = ""
	return p
}

// GoSettings shows 设置 on tab, as a link that names the setting it is about does (开发者垃圾's 更改文件夹 opens 清理).
func (n *Navigator) GoSettings(tab SettingsTab) {
	n.settingsTab = tab
	n.settingsAsked.Set(n.settingsAsked.GetUntracked() + 1)
	n.current.Set(Settings)
}

// SettingsAsked counts GoSettings's requests; reading it subscribes, so 设置 takes a request made while it is open (the menu bar's 自动清理 item over a window on 设置).
func (n *Navigator) SettingsAsked() int { return n.settingsAsked.Get() }

// WindowShown records that the main window came back after it was hidden (关闭窗口后继续在菜单栏运行), which keeps the page mounted.
func (n *Navigator) WindowShown() { n.shown.Set(n.shown.GetUntracked() + 1) }

// Shown counts WindowShown's calls; reading it subscribes, so 概览 treats the window coming back like opening it.
func (n *Navigator) Shown() int { return n.shown.Get() }

// TakeSettingsTab returns the tab GoSettings asked for, or "" when 设置 was opened without one, and forgets it, so the next visit opens on the first tab.
func (n *Navigator) TakeSettingsTab() SettingsTab {
	t := n.settingsTab
	n.settingsTab = ""
	return t
}

// Key provides the shell's Navigator to the pages.
var Key = core.NewKey[*Navigator]("spruce.nav", nil)

// Use returns the Navigator the shell provides.
func Use(cx *core.Scope) *Navigator { return Key.Use(cx) }
