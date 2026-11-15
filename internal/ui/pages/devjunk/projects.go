package devjunk

import (
	"context"
	"path/filepath"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	engdev "github.com/yldm-tech/spruce/internal/engine/devjunk"
	"github.com/yldm-tech/spruce/internal/ui/async"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// projectsTab is 项目: the filters, the projects largest first with 全选 and 全不选 (which leave held projects alone), and 清理; or why there are none.
func projectsTab(cx *core.Scope, d *cleanflow.Drill, rev reveal) core.Node {
	set := store.Use(cx).Settings
	projects := core.NewMemoFunc(cx, func() []engdev.Project {
		if r := d.Result(); r != nil {
			return r.Projects
		}
		return nil
	}, nil)
	artifacts := core.NewMemoFunc(cx, func() []cleanup.Category { _, a := SplitGroups(d.Categories()); return a }, nil)
	none := core.NewMemo(cx, func() bool { return len(projects.Get()) == 0 })
	return core.Dynamic(func(*core.Scope) core.Node {
		if none.Get() {
			return core.Func(func(cx *core.Scope) core.Node { return noProjects(cx, d, set) })
		}
		return core.Func(func(cx *core.Scope) core.Node { return projectList(cx, d, set, projects.Get, artifacts.Get, rev) })
	})
}

// noProjects is the 项目 tab without projects: no project folder set (with the way to 设置), the check of the source failed, or none found in them.
func noProjects(cx *core.Scope, d *cleanflow.Drill, set *store.Settings) core.Node {
	noRoots := func() bool { return set.Loaded() && len(set.Value().Cleanup.DevProjectRoots) == 0 }
	text := func(s func() string, testID string) core.Text {
		return core.Text{ContentFunc: s, TestID: testID, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5,
			Color: kit.ColorTextSecondary, TextAlign: core.TextAlignCenter}}
	}
	box := core.Style{AlignItems: core.AlignCenter, Gap: 8, Padding: core.Symmetric(40, 0), FlexShrink: 0}
	return core.Dynamic(func(*core.Scope) core.Node {
		if noRoots() {
			return core.Func(func(*core.Scope) core.Node {
				return core.View{TestID: "projects-roots", Style: box, Children: []core.Node{
					text(func() string { return "还没有设置项目文件夹，只检查了开发工具的缓存。" }, ""),
					kit.Link{Text: func() string { return "去设置" }, OnPress: func() {
						if n := nav.Use(cx); n != nil {
							n.GoSettings(nav.SettingsCleanup)
						}
					}},
				}}
			})
		}
		if d.Failed() {
			return core.Func(func(*core.Scope) core.Node { return cleanflow.DrillFailed(d) })
		}
		return core.Func(func(*core.Scope) core.Node {
			return core.View{Style: box, Children: []core.Node{
				text(func() string { return RootsNotice(set.Value().Cleanup.DevProjectRoots, set.Loaded()) }, "projects-empty"),
			}}
		})
	})
}

// projectList is the 项目 tab with projects.
func projectList(cx *core.Scope, d *cleanflow.Drill, set *store.Settings, projects func() []engdev.Project, artifacts func() []cleanup.Category, rev reveal) core.Node {
	filter := core.NewSignal(cx, FilterAll)
	days := func() int { return set.Value().Cleanup.DevArtifactMinAgeDays }
	now := timeNow()
	shown := core.NewMemoFunc(cx, func() []engdev.Project { return FilterProjects(projects(), filter.Get(), days(), now) }, nil)
	cleanRef := new(core.Ref)
	ctx := cx.Context()
	revealErr := core.NewSignal(cx, "")
	revealPath := func(path string) {
		if rev == nil {
			return
		}
		revealErr.Set("")
		async.Do(cx, ctx, func(ctx context.Context) (struct{}, error) { return struct{}{}, rev(ctx, path) }, func(_ struct{}, err error) {
			if err != nil {
				revealErr.Set(err.Error())
			}
		})
	}
	disabled := d.Cleaning
	chips := core.Dynamic(func(*core.Scope) core.Node {
		idle := IdleLabel(days())
		return core.Func(func(*core.Scope) core.Node {
			return kit.Chips[ProjectFilter]{Label: "筛选项目", Value: filter.Get, OnChange: filter.Set, Options: []kit.Option[ProjectFilter]{
				{Value: FilterAll, Label: "全部", TestID: "projects-filter-all"},
				{Value: FilterIdle, Label: idle, TestID: "projects-filter-idle"},
				{Value: FilterDirty, Label: "有未提交的改动", TestID: "projects-filter-dirty"},
			}}
		})
	})
	summary := func() string {
		var size int64
		for _, p := range shown.Get() {
			size += p.Size
		}
		return strconv.Itoa(len(shown.Get())) + " 个项目，共 " + format.Bytes(size)
	}
	toolbar := core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, RowGap: 8, ColumnGap: 12, FlexShrink: 0},
		Children: []core.Node{
			core.View{Style: core.Style{AutoMargin: core.SideRight}, Children: []core.Node{chips}},
			kit.Text("按大小排序", kit.FontBody, kit.Regular, kit.ColorTextMuted),
		}}
	tools := core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, RowGap: 8, ColumnGap: 12, FlexShrink: 0},
		Children: []core.Node{
			core.View{Style: core.Style{AutoMargin: core.SideRight}, Children: []core.Node{
				kit.Tabular(kit.TextFunc(summary, kit.FontBody, kit.Regular, kit.ColorTextSecondary))}},
			kit.Button{Text: "全选", Variant: kit.ButtonLink, Disabled: disabled, TestID: "select-all", OnPress: func() {
				d.Change(SetProjects(d.Selection.GetUntracked(), shown.GetUntracked(), core.Untracked(cx, artifacts), true))
			}},
			kit.Button{Text: "全不选", Variant: kit.ButtonLink, Disabled: disabled, TestID: "select-none", OnPress: func() {
				d.Change(SetProjects(d.Selection.GetUntracked(), shown.GetUntracked(), core.Untracked(cx, artifacts), false))
			}},
			core.View{Style: core.Style{FlexBasis: core.Pct(100)}, Children: []core.Node{kit.ErrorLine{Text: revealErr.Get, TestID: "reveal-error"}}},
		}}
	rows := core.Each(shown.Get, func(p engdev.Project) string { return p.ID }, func(cx *core.Scope, p func() engdev.Project, i func() int) core.Node {
		return projectRow(cx, d, p, i, artifacts, disabled, revealPath)
	})
	list := kit.Card{Clip: true, TestID: "projects", Style: core.Style{FlexShrink: 0}, Children: []core.Node{
		core.View{Role: core.RoleGroup, Label: "项目", Children: []core.Node{rows}},
	}}
	plan := func() Plan { return CleanPlan(artifacts(), d.Selection.Get()) }
	bar := cleanflow.DrillBar(d,
		func() kit.Stat {
			return kit.Stat{Label: "已选择", Value: strconv.Itoa(SelectedProjects(projects(), d.Selection.Get())) + " 个项目"}
		},
		func() kit.Stat { return kit.Stat{Label: "已选大小", Value: format.Bytes(plan().Bytes)} },
		func() int { return plan().Count },
		func() {
			cats := core.Untracked(cx, artifacts)
			d.RequestClean(cats, confirmation, OverlaySub(CleanPlan(cats, d.Selection.GetUntracked())), cleanRef)
		}, cleanRef)
	return core.Group{toolbar, tools, list, bar}
}

// projectRow is one project: its checkbox (disabled, with the reason as its tooltip, for a held project), name, path and kinds of build output, Git status, last activity, size, and its build output once expanded.
func projectRow(cx *core.Scope, d *cleanflow.Drill, p func() engdev.Project, index func() int, artifacts func() []cleanup.Category, disabled func() bool, reveal func(string)) core.Node {
	id := core.Untracked(cx, p).ID
	open := core.NewSignal(cx, false)
	name := func() string { return filepath.Base(p().Path) }
	state := func() cleanflow.CheckState { return ProjectState(p(), artifacts(), d.Selection.Get()) }
	checkable := func() bool { return Checkable(p(), artifacts()) }
	check := core.Dynamic(func(*core.Scope) core.Node {
		tip := HoldTip(p())
		box := kit.Checkbox{Label: core.Untracked(cx, name), TestID: "project-" + id + "-check",
			Checked:       func() bool { return state() == cleanflow.CheckAll },
			Indeterminate: func() bool { return state() == cleanflow.CheckSome },
			Disabled:      func() bool { return disabled() || !checkable() },
			OnChange: func(on bool) {
				d.Change(SetProjects(d.Selection.GetUntracked(), []engdev.Project{core.Untracked(cx, p)}, core.Untracked(cx, artifacts), on))
			}}
		return core.Func(func(*core.Scope) core.Node {
			if tip == "" {
				return box
			}
			return core.View{Tooltip: kit.Tooltip(tip), Style: core.Style{FlexShrink: 0}, Children: []core.Node{box}}
		})
	})
	git := core.Dynamic(func(*core.Scope) core.Node {
		g := GitLabelOf(p())
		return core.Func(func(*core.Scope) core.Node {
			switch {
			case g.Text == "":
				return core.Group{}
			case g.Badge:
				// The badge aligns itself to the start of its parent; a box of its own centres it in the row.
				return core.View{Style: core.Style{FlexShrink: 0}, Children: []core.Node{
					kit.Badge{Text: g.Text, Glyph: "alert", Level: kit.LevelWarning, TestID: "project-" + id + "-git"},
				}}
			}
			t := kit.Text(g.Text, kit.FontSmall, kit.Regular, kit.ColorTextMuted)
			t.TestID, t.MaxLines = "project-"+id+"-git", 1
			return t
		})
	})
	ellipsis := func(t core.Text) core.Text {
		t.MaxLines, t.EllipsizeMode = 1, core.EllipsizeTail
		return t
	}
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14, Padding: core.Symmetric(14, 18)}, Children: []core.Node{
		check,
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2}, Children: []core.Node{
			ellipsis(kit.TextFunc(name, kit.FontCardTitle, kit.Semibold, kit.ColorText)),
			ellipsis(kit.TextFunc(func() string { return ProjectSub(p()) }, kit.FontSmall, kit.Regular, kit.ColorTextMuted)),
		}},
		git,
		kit.Tabular(kit.TextFunc(func() string { return format.RelativeTime(p().LastActive, timeNow()) }, kit.FontSmall, kit.Regular, kit.ColorTextSecondary)),
		core.View{Style: core.Style{MinWidth: core.Pt(72), AlignItems: core.AlignEnd, FlexShrink: 0}, Children: []core.Node{
			kit.Tabular(kit.TextFunc(func() string { return format.Bytes(p().Size) }, kit.FontCardTitle, kit.Bold, kit.ColorText)),
		}},
		expandButton("project-"+id+"-expand", name, open),
		kit.IconButton{Glyph: "reveal", Label: "在" + format.FileManagerName() + "中显示 " + core.Untracked(cx, name), TestID: "project-" + id + "-reveal",
			OnPress: func() { reveal(core.Untracked(cx, p).Path) }},
	}}
	items := core.Show{When: open.Get, Then: func(cx *core.Scope) core.Node {
		list := core.NewMemoFunc(cx, func() []ProjectArtifact { return ProjectItems(p(), artifacts()) }, nil)
		return core.View{Role: core.RoleGroup, Label: core.Untracked(cx, name) + "的构建产物", TestID: "project-" + id + "-items",
			Style: core.Style{Padding: core.Symmetric(4, 0), Background: kit.SurfaceList, Borders: core.Edges{Top: 1}, BorderColor: kit.BorderSubtle},
			Children: []core.Node{core.Each(list.Get, func(a ProjectArtifact) string { return a.Category.ID + "/" + a.Item.ID },
				func(cx *core.Scope, a func() ProjectArtifact, _ func() int) core.Node {
					art := core.Untracked(cx, a)
					return cleanflow.ItemRow{Item: art.Item, Disabled: disabled, Reveal: reveal,
						Checked: func() bool { return d.Selection.Get().Has(art.Category.ID, art.Item.ID) },
						OnChange: func(on bool) {
							d.Change(cleanflow.SetItem(d.Selection.GetUntracked(), art.Category, art.Item.ID, on))
						}}
				})}}
	}}
	return core.View{Role: core.RoleGroup, LabelFunc: name, TestID: "project-" + id,
		StyleFunc: func() core.Style {
			st := core.Style{FlexShrink: 0}
			if index() > 0 {
				st.Borders, st.BorderColor = core.Edges{Top: 1}, kit.BorderSubtle
			}
			return st
		},
		Children: []core.Node{head, items}}
}

// expandButton opens and closes a project's build output (.icon-button.category-expand).
func expandButton(testID string, title func() string, open *core.Signal[bool]) core.Node {
	return core.View{Role: core.RoleButton, TestID: testID, Expandable: func() bool { return true }, Expanded: open.Get,
		LabelFunc: func() string {
			if open.Get() {
				return "收起" + title()
			}
			return "展开" + title()
		},
		Style: core.Style{Width: core.Pt(30), Height: core.Pt(30), FlexShrink: 0, CornerRadius: 15, AlignItems: core.AlignCenter,
			JustifyContent: core.JustifyCenter, Color: kit.ColorTextSecondary, Cursor: core.CursorPointer,
			FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText},
			Hover:        &core.Style{Background: kit.SurfaceButtonGhost, Color: kit.ColorText}},
		OnPress: func(core.PressEvent) { open.Set(!open.GetUntracked()) },
		Children: []core.Node{core.Dynamic(func(*core.Scope) core.Node {
			glyph := "chevron-down"
			if open.Get() {
				glyph = "chevron-up"
			}
			return core.Func(func(*core.Scope) core.Node { return kit.Glyph{Name: glyph, Size: 16, Color: core.CurrentColor} })
		})},
	}
}
