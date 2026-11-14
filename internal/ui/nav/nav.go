// Package nav names the app's pages and moves between them: the routes, what the sidebar and each page's header show for a page (the one port of src/components/common/pages.tsx), and the Navigator the shell provides.
package nav

import (
	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/kit/tone"
)

// Route names a page, as the React app's route path without the slash.
type Route string

// The pages.
const (
	SmartScan   Route = "smart-scan"
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

// Pages lists every page in the sidebar's order.
var Pages = []Route{SmartScan, SystemJunk, DevJunk, Installers, Uninstall, Space, Maintenance, Health, History, Settings}

// Start is the page the app opens on.
const Start = SmartScan

// Meta is what the sidebar and a page's header show for a page.
type Meta struct {
	Title, Subtitle string
	Tone            tone.Tone
	// Glyph names the page's icon in package icons.
	Glyph string
	// Section is the sidebar group the page is under ("" for none); Footer puts it in the sidebar's footer, drawn as a bare glyph.
	Section string
	Footer  bool
}

var metas = map[Route]Meta{
	SmartScan:   {Title: "智能扫描", Subtitle: "一次扫描系统垃圾、开发者垃圾和安装包，按推荐勾选可以放心清理的", Tone: tone.Violet},
	SystemJunk:  {Title: "系统垃圾", Subtitle: "清理应用缓存、日志和临时文件，不会动你的文档和照片", Tone: tone.Green, Section: "清理"},
	DevJunk:     {Title: "开发者垃圾", Subtitle: "清理开发工具的缓存和项目里的构建产物，大多会在需要时重新下载或生成", Tone: tone.Orange, Section: "清理"},
	Installers:  {Title: "安装包", Subtitle: "找出装完就没再用的安装包和磁盘映像", Tone: tone.Pink, Section: "清理"},
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

// Navigator holds the current page.
type Navigator struct {
	current *core.Signal[Route]
}

// NewNavigator returns a navigator on start.
func NewNavigator(cx *core.Scope, start Route) *Navigator {
	return &Navigator{current: core.NewSignal(cx, start)}
}

// Current returns the page shown; reading it subscribes.
func (n *Navigator) Current() Route { return n.current.Get() }

// Go shows page r.
func (n *Navigator) Go(r Route) { n.current.Set(r) }

// Key provides the shell's Navigator to the pages.
var Key = core.NewKey[*Navigator]("spruce.nav", nil)

// Use returns the Navigator the shell provides.
func Use(cx *core.Scope) *Navigator { return Key.Use(cx) }
