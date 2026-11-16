// Package devjunk is the 开发者 drill-down view of 概览, in two tabs over 概览's latest check: 项目, each project's build output with its last activity and Git status (a project with uncommitted changes can never be checked), and 工具缓存, the developer tools' caches. It never scans; its clean, its confirmation (commands, permanent deletions and the Trash counted apart), the cleaning overlay and the toast are the shared drill-down view's (cleanflow.Drill) over store.Overview.
package devjunk

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/overview"
)

// Page is the 开发者 view.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	d := cleanflow.NewDrill(cx, string(nav.DevJunk), overview.CleanToast)
	tab := core.NewSignal(cx, firstTab(nav.Use(cx), d.Reveal))
	rev := revealer(deps.Files)
	tabs := kit.Tabs[nav.DevTab]{Label: "开发者", Value: tab.Get, OnChange: tab.Set, Options: []kit.Option[nav.DevTab]{
		{Value: nav.DevProjects, Label: "项目", TestID: "tab-projects"},
		{Value: nav.DevTools, Label: "工具缓存", TestID: "tab-tools"},
	}}
	body := core.Show{When: func() bool { return d.Result() != nil }, Then: func(*core.Scope) core.Node {
		return core.Group{
			core.View{Style: core.Style{AlignItems: core.AlignStart, FlexShrink: 0}, Children: []core.Node{tabs}},
			core.Dynamic(func(*core.Scope) core.Node {
				if tab.Get() == nav.DevTools {
					return core.Func(func(cx *core.Scope) core.Node { return toolsTab(cx, d, rev) })
				}
				return core.Func(func(cx *core.Scope) core.Node { return projectsTab(cx, d, rev) })
			}),
		}
	}}
	return core.Group{
		kit.Page{TestID: "drill-dev-junk", Ref: d.Page, Children: []core.Node{
			kit.PageHeader{Route: nav.DevJunk}, cleanflow.DrillStatus(d), cleanflow.DrillNotices(d, DeniedNotice), body,
		}},
		cleanflow.DrillLayers(d),
	}
}

// firstTab is the tab the view opens on: the one a link asked for (nav.Navigator.GoDevTab), else 工具缓存 for a tool's category to reveal, else 项目.
func firstTab(n *nav.Navigator, reveal string) nav.DevTab {
	if n != nil {
		if t := n.TakeDevTab(); t != "" {
			return t
		}
	}
	if _, id, ok := strings.Cut(reveal, ":"); ok && !strings.HasPrefix(id, "artifact-") {
		return nav.DevTools
	}
	return nav.DevProjects
}

// toolsTab is 工具缓存: the tools' caches in one CategoryList, whose 全选 and 全不选 act on them only, with 清理.
func toolsTab(cx *core.Scope, d *cleanflow.Drill, rev reveal) core.Node {
	tools := core.NewMemoFunc(cx, func() []cleanup.Category { t, _ := SplitGroups(d.Categories()); return t }, nil)
	empty := core.NewMemo(cx, func() bool { return len(tools.Get()) == 0 })
	failed := core.NewMemo(cx, d.Failed)
	cleanRef := new(core.Ref)
	meta := nav.DevJunk.Meta()
	return core.Dynamic(func(*core.Scope) core.Node {
		if empty.Get() && failed.Get() {
			return core.Func(func(*core.Scope) core.Node { return cleanflow.DrillFailed(d) })
		}
		if empty.Get() {
			return core.Func(func(*core.Scope) core.Node {
				return kit.EmptyState{Title: "没有找到开发工具的缓存", Description: "开发工具的缓存都已经很干净了。", Tone: meta.Tone, Glyph: meta.Glyph, TestID: "drill-empty"}
			})
		}
		return core.Func(func(*core.Scope) core.Node {
			plan := func() Plan { return CleanPlan(tools.Get(), d.Selection.Get()) }
			return core.Group{
				core.View{TestID: "section-tools", Style: core.Style{Gap: 10, FlexShrink: 0}, Children: []core.Node{
					heading("工具缓存", "heading-tools"),
					cleanflow.CategoryList{Categories: tools.Get, Selection: d.Selection.Get, Label: "工具缓存", Reveal: rev, Disabled: d.Cleaning,
						OnChange: func(next cleanflow.Selection) {
							d.Change(cleanflow.WithinGroup(d.Selection.GetUntracked(), tools.GetUntracked(), next))
						},
						Expand: d.Reveal, Scroll: d.ScrollTo, TestID: "list-tools"},
				}},
				cleanflow.DrillBar(d,
					func() kit.Stat { return kit.Stat{Label: "已选择", Value: strconv.Itoa(plan().Count) + " 项"} },
					func() kit.Stat { return kit.Stat{Label: "已选大小", Value: format.Bytes(plan().Bytes)} },
					func() int { return plan().Count },
					func() {
						cats := tools.GetUntracked()
						d.RequestClean(cats, confirmation, OverlaySub(CleanPlan(cats, d.Selection.GetUntracked())), cleanRef)
					}, cleanRef),
			}
		})
	})
}

// confirmation is the question asked before cleaning the checked items of cats: the total, then one bulleted line per way things go.
func confirmation(cats []cleanup.Category, sel cleanflow.Selection) kit.ConfirmDialog {
	c := ConfirmationLines(CleanPlan(cats, sel))
	return kit.ConfirmDialog{Title: "确认清理", Message: confirmMessage(c), ConfirmLabel: "清理", Danger: c.Danger, TestID: "confirm"}
}

// confirmMessage is the confirmation's body: the total, then one bulleted line per way things go.
func confirmMessage(c Confirmation) core.Node {
	children := []core.Node{core.Text{Content: c.Summary, TestID: "confirm-summary", Style: core.Style{Margin: core.Edges{Bottom: 8}}}}
	if len(c.Lines) > 0 {
		lines := make([]core.Node, len(c.Lines))
		for i, l := range c.Lines {
			lines[i] = core.View{TestID: "confirm-line-" + strconv.Itoa(i), Style: core.Style{Direction: core.Row, Gap: 6}, Children: []core.Node{
				core.Text{Content: "•"},
				core.Text{Content: l, Style: core.Style{FlexShrink: 1, MinWidth: core.Pt(0)}},
			}}
		}
		children = append(children, core.View{TestID: "confirm-lines", Style: core.Style{Gap: 4, TextAlign: core.TextAlignLeft, AlignItems: core.AlignStart}, Children: lines})
	}
	return core.View{Style: core.Style{AlignItems: core.AlignCenter}, Children: children}
}

// heading is a section's title, with its TestID.
func heading(title, testID string) core.Text {
	t := kit.SectionTitle(title)
	t.TestID = testID
	return t
}
