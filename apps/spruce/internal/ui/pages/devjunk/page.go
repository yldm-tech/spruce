// Package devjunk is the 开发者 drill-down view of 概览, in two tabs over 概览's latest check: 项目, each project's build output with its last activity and Git status (a project with uncommitted changes can never be checked), and 工具缓存, the developer tools' caches. It never scans; its clean, its confirmation (commands, permanent deletions and the Trash counted apart), the cleaning overlay and the toast are the shared drill-down view's (cleanflow.Drill) over store.Overview.
package devjunk

import (
	"strconv"
	"strings"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/engine/cleanup"
	engdev "spruce/apps/spruce/internal/engine/devjunk"
	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/overview"
	"spruce/apps/spruce/internal/ui/store"
)

// Page is the 开发者 view.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	d := cleanflow.NewDrill(cx, string(nav.DevJunk), overview.CleanToast)
	k := keptKey.Use(cx)
	// A link that asks for a tab or a category opens on it with every project shown; otherwise the view opens as it was left.
	if t, asked := firstTab(nav.Use(cx), d.Reveal); asked {
		k.tab.Set(t)
		k.filter.Set(FilterAll)
	}
	tab := k.tab
	rev := revealer(deps.Files)
	l := newLists(cx, d, store.Use(cx).Settings, k.filter)
	cleanRef := new(core.Ref)
	tabs := kit.Tabs[nav.DevTab]{Label: "开发者", Value: tab.Get, OnChange: tab.Set, Options: []kit.Option[nav.DevTab]{
		{Value: nav.DevProjects, Label: "项目", TestID: "tab-projects"},
		{Value: nav.DevTools, Label: "工具缓存", TestID: "tab-tools"},
	}}
	body := core.Show{When: func() bool { return d.Result() != nil }, Then: func(*core.Scope) core.Node {
		return core.Group{
			core.View{Style: core.Style{AlignItems: core.AlignStart, FlexShrink: 0}, Children: []core.Node{tabs}},
			core.Dynamic(func(*core.Scope) core.Node {
				if tab.Get() == nav.DevTools {
					return core.Func(func(cx *core.Scope) core.Node { return toolsTab(cx, d, l, rev) })
				}
				return core.Func(func(cx *core.Scope) core.Node { return projectsTab(cx, d, l, k.filter, rev) })
			}),
		}
	}}
	// The bar of the tab shown stays pinned under the page while it lists something.
	barOf := core.NewMemo(cx, func() nav.DevTab {
		switch t := tab.Get(); {
		case d.Result() == nil:
		case t == nav.DevTools && len(l.tools.Get()) > 0, t == nav.DevProjects && len(l.projects.Get()) > 0:
			return t
		}
		return ""
	})
	footer := core.Dynamic(func(*core.Scope) core.Node {
		switch barOf.Get() {
		case nav.DevTools:
			return core.Func(func(*core.Scope) core.Node { return toolsBar(d, l, cleanRef) })
		case nav.DevProjects:
			return core.Func(func(*core.Scope) core.Node { return projectsBar(d, l, cleanRef) })
		}
		return core.Group{}
	})
	return core.Group{
		kit.Page{TestID: "drill-dev-junk", Ref: d.Page, ScrollKey: nav.DevJunk, Header: kit.PageHeader{Route: nav.DevJunk}, Footer: footer, Children: []core.Node{
			cleanflow.DrillStatus(d), cleanflow.DrillNotices(d, DeniedNotice), body,
		}},
		cleanflow.DrillLayers(d),
	}
}

// lists are the view's lists over the latest check, which its tabs and the bar pinned under them read.
type lists struct {
	tools, artifacts *core.Memo[[]cleanup.Category]
	projects         *core.Memo[[]engdev.Project]
	// shown are the projects the 项目 filter shows, and shownArtifacts their build output: what the bar of 项目 counts and cleans.
	shown          *core.Memo[[]engdev.Project]
	shownArtifacts *core.Memo[[]cleanup.Category]
}

// newLists makes the view's lists in cx, with the projects shown under filter by the idle days of set.
func newLists(cx *core.Scope, d *cleanflow.Drill, set *store.Settings, filter *core.Signal[ProjectFilter]) *lists {
	l := &lists{
		tools:     core.NewMemoFunc(cx, func() []cleanup.Category { t, _ := SplitGroups(d.Categories()); return t }, nil),
		artifacts: core.NewMemoFunc(cx, func() []cleanup.Category { _, a := SplitGroups(d.Categories()); return a }, nil),
		projects: core.NewMemoFunc(cx, func() []engdev.Project {
			if r := d.Result(); r != nil {
				return r.Projects
			}
			return nil
		}, nil),
	}
	now := timeNow()
	l.shown = core.NewMemoFunc(cx, func() []engdev.Project {
		return FilterProjects(l.projects.Get(), filter.Get(), set.Value().Cleanup.DevArtifactMinAgeDays, now)
	}, nil)
	l.shownArtifacts = core.NewMemoFunc(cx, func() []cleanup.Category { return ArtifactsOf(l.shown.Get(), l.artifacts.Get()) }, nil)
	return l
}

// toolsBar is 工具缓存's bar: what is checked among the tools' caches, and 清理.
func toolsBar(d *cleanflow.Drill, l *lists, cleanRef *core.Ref) core.Node {
	plan := func() Plan { return CleanPlan(l.tools.Get(), d.Selection.Get()) }
	return cleanflow.DrillBar(d,
		func() kit.Stat { return kit.Stat{Label: "已选择", Value: strconv.Itoa(plan().Count) + " 项"} },
		func() kit.Stat { return kit.Stat{Label: "已选大小", Value: format.Bytes(plan().Bytes)} },
		func() int { return plan().Count },
		func() {
			cats := l.tools.GetUntracked()
			d.RequestClean(cats, confirmation(nil), OverlaySub(CleanPlan(cats, d.Selection.GetUntracked())), cleanRef)
		}, cleanRef)
}

// projectsBar is 项目's bar: the projects checked among those the filter shows, and 清理, which cleans only those.
func projectsBar(d *cleanflow.Drill, l *lists, cleanRef *core.Ref) core.Node {
	plan := func() Plan { return CleanPlan(l.shownArtifacts.Get(), d.Selection.Get()) }
	names := func(sel cleanflow.Selection) string { return ProjectNames(l.shown.GetUntracked(), sel) }
	return cleanflow.DrillBar(d,
		func() kit.Stat {
			return kit.Stat{Label: "已选择", Value: strconv.Itoa(SelectedProjects(l.shown.Get(), d.Selection.Get())) + " 个项目"}
		},
		func() kit.Stat { return kit.Stat{Label: "已选大小", Value: format.Bytes(plan().Bytes)} },
		func() int { return plan().Count },
		func() {
			cats := l.shownArtifacts.GetUntracked()
			d.RequestClean(cats, confirmation(names), OverlaySub(CleanPlan(cats, d.Selection.GetUntracked())), cleanRef)
		}, cleanRef)
}

// firstTab is the tab a link asks the view to open on, and whether one asks: the tab it names (nav.Navigator.GoDevTab), else the tab of the category it reveals, 工具缓存 for a tool's and 项目 for build output.
func firstTab(n *nav.Navigator, reveal string) (nav.DevTab, bool) {
	if n != nil {
		if t := n.TakeDevTab(); t != "" {
			return t, true
		}
	}
	_, id, ok := strings.Cut(reveal, ":")
	switch {
	case !ok:
		return "", false
	case strings.HasPrefix(id, "artifact-"):
		return nav.DevProjects, true
	}
	return nav.DevTools, true
}

// toolsTab is 工具缓存: the tools' caches in one CategoryList, whose 全选 and 全不选 act on them only; its 清理 is in the pinned bar (toolsBar).
func toolsTab(cx *core.Scope, d *cleanflow.Drill, l *lists, rev reveal) core.Node {
	tools := l.tools
	empty := core.NewMemo(cx, func() bool { return len(tools.Get()) == 0 })
	failed := core.NewMemo(cx, d.Failed)
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
			return core.View{TestID: "section-tools", Style: core.Style{Gap: 10, FlexShrink: 0}, Children: []core.Node{
					heading("工具缓存", "heading-tools"),
					cleanflow.CategoryList{Categories: tools.Get, Selection: d.Selection.Get, Label: "工具缓存", Reveal: rev, Disabled: d.Cleaning, Open: d.Open, OnOpen: d.SetOpen,
						OnChange: func(next cleanflow.Selection) {
							d.Change(cleanflow.WithinGroup(d.Selection.GetUntracked(), tools.GetUntracked(), next))
						},
						Expand: d.Reveal, Scroll: d.ScrollTo, TestID: "list-tools"},
			}}
		})
	})
}

// confirmation is the question asked before cleaning the checked items of cats: the total, the projects names gives (nil for none), then one bulleted line per way things go.
func confirmation(names func(cleanflow.Selection) string) func(cats []cleanup.Category, sel cleanflow.Selection) kit.ConfirmDialog {
	return func(cats []cleanup.Category, sel cleanflow.Selection) kit.ConfirmDialog {
		c := ConfirmationLines(CleanPlan(cats, sel))
		projects := ""
		if names != nil {
			projects = names(sel)
		}
		return kit.ConfirmDialog{Title: "确认清理", Message: confirmMessage(c, projects), ConfirmLabel: "清理", Danger: c.Danger, TestID: "confirm"}
	}
}

// confirmMessage is the confirmation's body: the total, the projects it cleans ("" for none), then one bulleted line per way things go.
func confirmMessage(c Confirmation, projects string) core.Node {
	children := []core.Node{core.Text{Content: c.Summary, TestID: "confirm-summary", Style: core.Style{Margin: core.Edges{Bottom: 8}}}}
	if projects != "" {
		children = append(children, core.Text{Content: projects, TestID: "confirm-projects", Style: core.Style{Margin: core.Edges{Bottom: 8}}})
	}
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
