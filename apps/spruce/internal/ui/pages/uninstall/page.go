// Package uninstall is the 应用卸载 page: the installed apps with their sizes, icons and last use, the picked app's details and leftovers, and the uninstall itself. On macOS the app and the checked leftovers go to the Trash; on Windows the program's own uninstaller runs, then the page offers what it left in the user's folders.
package uninstall

import (
	"fmt"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
)

// bodyMode is what the page's body shows.
type bodyMode uint8

const (
	bodyLoading bodyMode = iota
	bodyFailed
	bodyEmpty
	bodyApps
)

// Page is the 应用卸载 page, over the State Provide made: it reads the apps on its first showing and shows them as they were on the next ones.
func Page(cx *core.Scope) core.Node {
	s := stateKey.Use(cx)
	s.Visit()
	actionRef, searchRef := new(core.Ref), new(core.Ref)
	windows := s.Windows()
	bin := trashName(windows)
	// ⌘R reads the list again, ⌘F goes to the search field.
	nav.UseCommands(cx, nav.Commands{Refresh: s.Refresh, Find: searchRef.Focus,
		Busy: func() bool { return s.Fetching.Get() || s.Step.Get() != StepIdle }})
	// ⌘⌫ uninstalls the picked app, asking first as 卸载 does.
	cx.OnKey("mod+backspace", s.Start)
	// Coming back to Spruce after quitting the picked app clears its 运行中 at once.
	lifecycle := core.Untracked(cx, cx.Env().Lifecycle)
	cx.Effect(func() {
		now := cx.Env().Lifecycle()
		if now == core.LifecycleActive && lifecycle != core.LifecycleActive {
			cx.Untrack(s.Active)
		}
		lifecycle = now
	})

	refresh := kit.Button{Variant: kit.ButtonGhost, TestID: "refresh", Disabled: s.Fetching.Get, OnPress: s.Refresh,
		TextFunc: func() string {
			if s.Fetching.Get() && s.Apps.Get() != nil {
				return "正在刷新…"
			}
			return "刷新"
		}}

	type bodyKey struct {
		mode bodyMode
		err  string
	}
	key := core.NewMemo(cx, func() bodyKey {
		switch {
		case !s.Loaded.Get():
			return bodyKey{mode: bodyLoading}
		case s.AppsErr.Get() != "":
			return bodyKey{mode: bodyFailed, err: s.AppsErr.Get()}
		case len(s.Apps.Get()) == 0:
			return bodyKey{mode: bodyEmpty}
		}
		return bodyKey{mode: bodyApps}
	})
	body := core.Dynamic(func(cx *core.Scope) core.Node {
		k := key.Get()
		return core.Func(func(cx *core.Scope) core.Node {
			switch k.mode {
			case bodyLoading:
				return core.View{Role: core.RoleGroup, Label: "已安装的应用", Busy: func() bool { return true }, TestID: "apps-loading",
					Style: core.Style{FlexGrow: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 12, Padding: core.Symmetric(24, 0)},
					Children: []core.Node{
						kit.ProgressRing{Size: 64, Label: "正在读取已安装的应用"},
						core.Text{Content: "正在读取已安装的应用…", Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSubheading, LineHeightMultiple: 1.5,
							Color: kit.ColorTextOverlaySub}},
					}}
			case bodyFailed:
				return kit.EmptyState{Title: "无法读取已安装的应用", Description: k.err, Tone: kit.ToneRed, Glyph: "error", TestID: "apps-error",
					Action: kit.Button{Text: "重试", Variant: kit.ButtonGhost, OnPress: s.LoadApps, TestID: "retry"}}
			case bodyEmpty:
				desc := "应用程序文件夹里没有系统自带应用以外的应用。"
				if windows {
					desc = "已安装的程序列表里没有可以卸载的桌面程序。"
				}
				return kit.EmptyState{Title: "没有找到可以卸载的应用", Description: desc, Tone: kit.ToneBlue, Glyph: nav.Uninstall.Meta().Glyph, TestID: "apps-empty"}
			}
			return columns(cx, s, searchRef)
		})
	})

	footnote := "系统自带的应用不会列出。卸载会把应用和勾选的相关文件移到" + bin + "，可以随时放回。"
	if windows {
		footnote = "这里列出通过安装程序安装的桌面程序；Microsoft Store 应用请在 Windows「设置 → 应用」中卸载。卸载时会运行程序自带的卸载程序，Spruce 只把它留下的文件移到回收站。"
	}

	bar := kit.ActionBar{
		Start: func() kit.Stat {
			_, ok := s.App()
			switch {
			case !ok:
				return kit.Stat{Label: "已选择", Value: "0 项"}
			case windows:
				return kit.Stat{Label: "已选择", Value: "1 个程序"}
			}
			n, _ := s.Totals()
			return kit.Stat{Label: "已选择", Value: fmt.Sprintf("%d 项", 1+n)}
		},
		End: func() kit.Stat {
			if windows {
				return kit.Stat{Label: "卸载方式", Value: "自带卸载程序"}
			}
			_, size := s.Totals()
			return kit.Stat{Label: "将移到" + bin, Value: format.BytesIn(s.BundleSize()+size, windows)}
		},
		Button: kit.ActionButton{Label: "卸载", Ref: actionRef, TestID: "uninstall", OnPress: s.Start, Disabled: func() bool { return !s.CanUninstall() }},
	}

	return core.Group{
		column{Children: []core.Node{
			kit.PageHeader{Route: nav.Uninstall, Actions: []core.Node{refresh}},
			body,
			core.Text{Content: footnote, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, LineHeightMultiple: 1.5, Color: kit.ColorTextMuted}},
			bar,
		}},
		kit.LiveStatus{Text: s.LiveText},
		flow(cx, s, actionRef),
	}
}

// column is the page's column as kit.Page lays it out, without its scroll view: the columns take the height between the header and the action bar and scroll inside, as the design's grid row minmax(0, 1fr) does, which a scroll view's unbounded content height cannot. It fills the shell's main area absolutely, so its content never sizes the main area. The window's minimum height leaves the columns more than their 300-point minimum, so the page itself never needs to scroll.
type column struct {
	Children []core.Node
}

// Render implements core.Node.
func (c column) Render(*core.Scope) core.Node {
	return core.View{TestID: "uninstall-page", Style: core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0),
		Left: core.Pt(0), Gap: 18, Padding: core.Edges{Left: 32, Right: 32, Bottom: 24}}, Children: c.Children}
}

// narrowBelow is the width of the columns under which they share it evenly and the rows drop the App Store badge: the design's 1080-point window less the sidebar and the page's margins.
const narrowBelow = 1080 - kit.SidebarWidth - 64

// columns is the page's body once the apps are listed: the app list and the picked app side by side, filling the height between the header and the action bar.
func columns(cx *core.Scope, s *State, searchRef *core.Ref) core.Node {
	narrow := core.NewSignal(cx, false)
	share := func(grow float32, minWide, minNarrow core.Points) func() core.Style {
		return func() core.Style {
			if narrow.Get() {
				return core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(minNarrow), MinHeight: core.Pt(0)}
			}
			return core.Style{FlexGrow: grow, FlexShrink: 1, FlexBasis: core.Pt(0), MinWidth: core.Pt(minWide), MinHeight: core.Pt(0)}
		}
	}
	return core.View{
		OnLayout: func(b core.Bounds) { narrow.Set(b.Size.Width <= narrowBelow) },
		Style:    core.Style{Direction: core.Row, Gap: 16, FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(300)},
		Children: []core.Node{
			core.View{StyleFunc: share(1, 280, 260), Children: []core.Node{appList(cx, s, searchRef, narrow.Get)}},
			core.View{StyleFunc: share(1.15, 300, 280), Children: []core.Node{detail(cx, s)}},
		},
	}
}
