// Package devjunk is the 开发者垃圾 page: before a scan the folders it will search for projects; while scanning the progress; then the tool caches and the projects' build output as two lists, 清理, its confirmation (commands, permanent deletions and the Trash counted apart), the cleaning overlay and the done card. The flow is the shared cleaner flow (package cleanflow).
package devjunk

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// rootsWidth is how many characters of a project folder the idle page shows, the rest elided in the middle.
const rootsWidth = 64

// Page is the 开发者垃圾 page.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	f := cleanflow.NewFlow(cx, deps.Cleanup, app.CleanerDevJunk)
	stores := store.Use(cx)
	roots := func() ([]string, bool) {
		if stores == nil || !stores.Settings.Loaded() {
			return nil, false
		}
		return stores.Settings.Value().Cleanup.DevProjectRoots, true
	}
	toSettings := func() {
		if n := nav.Use(cx); n != nil {
			n.Go(nav.Settings)
		}
	}
	plan := func() Plan { return CleanPlan(f.Categories(), f.Selection.Get()) }
	return cleanflow.View(cx, f, cleanflow.ViewConfig{
		Page:            nav.DevJunk,
		IdleTitle:       "查找开发者垃圾",
		IdleDescription: "扫描开发工具的缓存，以及项目文件夹里可以重新生成的构建产物。扫描只读取，不会删除任何东西。",
		IdleDetails: func(*core.Scope) core.Node {
			return projectRoots{Roots: roots, OnSettings: toSettings}
		},
		SizeLabel:        "已选大小",
		EmptyTitle:       "没有找到开发者垃圾",
		EmptyDescription: "开发工具的缓存和项目里的构建产物都已经很干净了。",
		Results: func(_ *core.Scope, f *cleanflow.Flow) core.Node {
			return results(f, roots, revealer(deps.Files))
		},
		Confirm: func(f *cleanflow.Flow) kit.ConfirmDialog {
			c := ConfirmationLines(CleanPlan(f.Result.GetUntracked().Categories, f.Selection.GetUntracked()))
			return kit.ConfirmDialog{Title: "确认清理", Message: confirmMessage(c), ConfirmLabel: "清理", Danger: c.Danger, TestID: "confirm"}
		},
		OverlaySub: func(*cleanflow.Flow) string { return OverlaySub(plan()) },
	})
}

// projectRoots lists the folders the scan searches for projects, with a link to 设置 to change them; nothing before the settings are loaded.
type projectRoots struct {
	Roots      func() ([]string, bool)
	OnSettings func()
}

// Render implements core.Node.
func (p projectRoots) Render(*core.Scope) core.Node {
	loaded := func() bool { _, ok := p.Roots(); return ok }
	list := func() []string { r, _ := p.Roots(); return r }
	none := func() bool { return len(list()) == 0 }
	return core.Show{When: loaded, Then: func(*core.Scope) core.Node {
		return core.View{TestID: "roots", Style: core.Style{AlignItems: core.AlignCenter, Gap: 8, MaxWidth: core.Pt(520)}, Children: []core.Node{
			core.Dynamic(func(*core.Scope) core.Node {
				if none() {
					return core.Func(func(*core.Scope) core.Node {
						return kit.Text("还没有设置项目文件夹，这次只扫描开发工具的缓存。", kit.FontBody, kit.Regular, kit.ColorTextSecondary)
					})
				}
				return core.Func(func(*core.Scope) core.Node {
					return core.Group{
						kit.Text("会在这些文件夹里查找项目：", kit.FontBody, kit.Regular, kit.ColorTextSecondary),
						rootsList(list),
					}
				})
			}),
			kit.Link{TestID: "roots-settings", Text: func() string {
				if none() {
					return "去设置"
				}
				return "更改文件夹"
			}, OnPress: p.OnSettings},
		}}
	}}
}

// rootsList is the folders in a quiet box (.dev-junk-roots-list), each on one line elided in the middle, with the whole path as its tooltip.
func rootsList(roots func() []string) core.Node {
	return core.View{Role: core.RoleGroup, Label: "项目文件夹", TestID: "roots-list",
		Style: core.Style{Gap: 4, MaxWidth: core.Pct(100), Padding: core.Symmetric(10, 16), CornerRadius: kit.RadiusCardIcon, Background: kit.SurfaceList,
			BorderWidth: 1, BorderColor: kit.BorderSubtle},
		Children: []core.Node{core.Each(roots, func(r string) string { return r }, func(_ *core.Scope, r func() string, i func() int) core.Node {
			path := r()
			t := kit.Tabular(kit.Text(format.TruncateMiddle(path, rootsWidth), kit.FontBody, kit.Regular, kit.ColorText))
			t.MaxLines, t.EllipsizeMode = 1, core.EllipsizeTail
			// The design draws the folders at the normal line height of Inter, tighter than the default of the font list.
			t.Style.LineHeightEm = 1.21
			return core.View{TestID: "root-" + strconv.Itoa(i()), Tooltip: kit.Tooltip(path), Children: []core.Node{t}}
		})}}
}

// results is the page after a scan that found something: the unreadable places, the tool caches and the projects' build output as two sections, or a line saying why there was no build output.
func results(f *cleanflow.Flow, roots func() ([]string, bool), rev reveal) core.Node {
	res := f.Result.GetUntracked()
	tools, projects := SplitGroups(res.Categories)
	notResults := func() bool { return f.Phase.Get() != cleanflow.PhaseResults }
	var children core.Group
	if res.Denied > 0 {
		denied := "有 " + strconv.FormatInt(res.Denied, 10) + " 个位置没有权限读取，结果可能不完整。"
		children = append(children, kit.Notice{Text: func() string { return denied }, Tone: kit.LevelWarning})
	}
	if len(tools) > 0 {
		children = append(children, section(f, "工具缓存", "tools", tools, rev, notResults))
	}
	if len(projects) > 0 {
		children = append(children, section(f, "项目构建产物", "projects", projects, rev, notResults))
	} else {
		children = append(children, core.Text{TestID: "roots-notice", ContentFunc: func() string { return RootsNotice(roots()) },
			Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: kit.ColorTextMuted}})
	}
	return children
}

// section is one group of results under its heading, with its own CategoryList whose 全选 and 全不选 act on the group only.
func section(f *cleanflow.Flow, title, key string, group []cleanup.Category, rev reveal, disabled func() bool) core.Node {
	return core.View{TestID: "section-" + key, Style: core.Style{Gap: 10}, Children: []core.Node{
		heading(title, "heading-"+key),
		cleanflow.CategoryList{Categories: func() []cleanup.Category { return group }, Selection: f.Selection.Get,
			OnChange: func(next cleanflow.Selection) {
				f.SetSelection(cleanflow.WithinGroup(f.Selection.GetUntracked(), group, next))
			},
			Label: title, Reveal: rev, Disabled: disabled, TestID: "list-" + key},
	}}
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
