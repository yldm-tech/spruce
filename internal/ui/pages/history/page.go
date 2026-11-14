// Package history is the 操作历史 page: every recorded run, newest first and grouped by day, with filter chips by kind, the space the clean-ups freed, each run's details on demand, and 清空历史.
package history

import (
	"github.com/nsgo-dev/nsgo/core"

	enginehist "github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Page is the 操作历史 page.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	stores := store.Use(cx)
	s := NewState(cx, stores.History, deps.Catalog)
	clearing := core.NewSignal(cx, false)
	clearRef := new(core.Ref)
	hasRuns := func() bool { return len(s.Runs.Get()) > 0 }

	header := kit.PageHeader{Route: nav.History, Actions: []core.Node{core.Show{When: hasRuns, Then: func(*core.Scope) core.Node {
		return kit.Button{Text: "清空历史", Variant: kit.ButtonGhost, Ref: clearRef, TestID: "clear-history", OnPress: func() { clearing.Set(true) }}
	}}}}

	loadErr := core.Show{When: func() bool { return s.LoadErr.Get() != "" }, Then: func(*core.Scope) core.Node {
		return core.View{TestID: "history-error", Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 16}, Children: []core.Node{
			core.Text{ContentFunc: s.LoadErr.Get, Role: core.RoleAlert, LiveRegion: core.LiveRegionPolite,
				Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorWarning, FlexShrink: 1}},
			kit.Button{Text: "重试", Variant: kit.ButtonGhost, TestID: "history-retry", OnPress: s.Load},
		}}
	}}

	meta := nav.History.Meta()
	empty := core.Show{When: func() bool { return s.Loaded.Get() && s.LoadErr.Get() == "" && !hasRuns() }, Then: func(*core.Scope) core.Node {
		return kit.EmptyState{Title: "还没有操作记录", Description: "运行维护或清理后，每一次的结果都会记在这里。", Tone: meta.Tone, Glyph: meta.Glyph,
			TestID: "history-empty"}
	}}

	content := core.Show{When: hasRuns, Then: func(*core.Scope) core.Node {
		return core.Group{
			overview(s, stores.Settings),
			filters(s),
			core.Each(s.Groups, func(g DayGroup) string { return g.Key }, func(_ *core.Scope, g func() DayGroup, _ func() int) core.Node {
				return daySection(s, g)
			}),
		}
	}}

	return core.Group{
		kit.Page{Children: []core.Node{header, loadErr, empty, content}},
		ClearDialog{Open: clearing.Get, Count: func() int { return len(s.Runs.Get()) }, OnClose: func() { clearing.Set(false) }, ReturnFocus: clearRef},
	}
}

// overview is the line above the list: the space the clean-ups freed (once there is one) and the count of records with the days the history keeps.
func overview(s *State, settings *store.Settings) core.Node {
	total := core.Show{When: func() bool { _, n := TotalFreed(s.Runs.Get()); return n > 0 }, Then: func(*core.Scope) core.Node {
		return core.View{TestID: "history-total", Style: core.Style{Direction: core.Row, AlignItems: core.AlignBaseline, Gap: 4}, Children: []core.Node{
			kit.Text("累计释放 ", kit.FontCardTitle, kit.Regular, kit.ColorTextSecondary),
			kit.Tabular(core.Text{ContentFunc: func() string { b, _ := TotalFreed(s.Runs.Get()); return format.Bytes(b) },
				Style: core.Style{FontFamily: kit.FontFace(kit.Bold), FontWeight: kit.FaceWeight(kit.Bold), FontSize: kit.FontStat,
					LetterSpacingEm: kit.LetterSpacingStat, Color: kit.ColorText}}),
		}}
	}}
	count := kit.TextFunc(func() string {
		v := settings.Value()
		return CountText(len(s.Runs.Get()), v.History.RetentionDays, settings.Loaded())
	}, kit.FontBody, kit.Regular, kit.ColorTextMuted)
	count.TestID = "history-count"
	return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignBaseline, RowGap: 6, ColumnGap: 18},
		Children: []core.Node{total, count}}
}

// filters is the row of filter chips, there while the runs hold more than one kind: 全部, then each kind in the sidebar's order.
func filters(s *State) core.Node {
	return core.Dynamic(func(*core.Scope) core.Node {
		kinds := s.Kinds()
		if len(kinds) < 2 {
			return core.Group{}
		}
		options := []kit.Option[enginehist.Kind]{{Value: "", Label: "全部", TestID: "chip-all"}}
		for _, k := range kinds {
			options = append(options, kit.Option[enginehist.Kind]{Value: k, Label: KindMeta(k).Title, TestID: "chip-" + string(k)})
		}
		return kit.Chips[enginehist.Kind]{Options: options, Value: s.Active, OnChange: s.Filter.Set, Label: "按类型筛选"}
	})
}

// daySection is one day: its label over a card listing its runs.
func daySection(s *State, g func() DayGroup) core.Node {
	day := g()
	rows := core.Each(func() []enginehist.Run { return g().Runs }, func(r enginehist.Run) string { return r.ID },
		func(cx *core.Scope, r func() enginehist.Run, index func() int) core.Node {
			return runRow(cx, s, r(), index)
		})
	return core.View{Role: core.RoleRegion, LabelFunc: func() string { return g().Label }, TestID: "day-" + day.Key, Style: core.Style{Gap: 8},
		Children: []core.Node{
			core.Text{ContentFunc: func() string { return g().Label }, Role: core.RoleHeader, Style: core.Style{FontFamily: kit.FontFace(kit.Semibold),
				FontWeight: kit.FaceWeight(kit.Semibold), FontSize: kit.FontSection, LetterSpacingEm: kit.LetterSpacingSection, Color: kit.ColorTextSection}},
			kit.Card{Clip: true, Children: []core.Node{rows}},
		}}
}

// runRow is one run: a button with the kind's tile, title and summary line, the start time and a chevron, which shows or hides the run's details under it.
func runRow(cx *core.Scope, s *State, run enginehist.Run, index func() int) core.Node {
	open := core.NewSignal(cx, false)
	meta := KindMeta(run.Kind)
	reduce := cx.Env().ReduceMotion
	head := core.View{Role: core.RoleButton, TestID: "run-" + run.ID + "-head", Expanded: open.Get,
		OnPress: func(core.PressEvent) { open.Update(func(v bool) bool { return !v }) },
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14, Padding: core.Symmetric(12, 18), Cursor: core.CursorPointer,
			Hover:        &core.Style{Background: kit.SurfaceRowHover},
			FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: -2, OutlineColor: kit.ColorText}},
		Children: []core.Node{
			kit.IconTile{Tone: meta.Tone, Size: 38, GlyphSize: 19, Glyph: meta.Glyph, Radius: kit.RadiusCardIcon},
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2}, Children: []core.Node{
				kit.Text(meta.Title, kit.FontCardTitle, kit.Semibold, kit.ColorText),
				core.Text{Content: RunSummary(run), MaxLines: 1, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorTextMuted}},
			}},
			kit.Tabular(kit.Text(Clock(run.StartedAt, false), kit.FontBody, kit.Regular, kit.ColorTextSecondary)),
			core.View{StyleFunc: func() core.Style {
				st := core.Style{FlexShrink: 0, Color: kit.ColorTextMuted}
				if !reduce() {
					st.Transition = core.Transition{Duration: 150_000_000, Easing: core.Ease, Properties: core.TransitionTransform}
				}
				if open.Get() {
					st.Transform.Rotate = 90
				}
				return st
			}, Children: []core.Node{kit.Glyph{Name: "chevron-right", Size: 16, Color: core.CurrentColor}}},
		}}
	details := core.Show{When: open.Get, Then: func(*core.Scope) core.Node { return runDetails(s, run) }}
	return core.View{TestID: "run-" + run.ID, StyleFunc: func() core.Style {
		if index() == 0 {
			return core.Style{}
		}
		return core.Style{Borders: core.Edges{Top: 1}, BorderColor: kit.BorderSubtle}
	}, Children: []core.Node{head, details}}
}

// runDetails is what a run's row shows when open: a maintenance run's tasks with how each ended, another run's detail lines, and the run's start, end and duration.
func runDetails(s *State, run enginehist.Run) core.Node {
	list := core.Style{Gap: 8, Padding: core.Edges{Top: 8}}
	var body core.Node = core.Group{}
	if run.Kind == enginehist.KindMaintenance {
		body = core.View{Style: list, TestID: "run-tasks", Children: []core.Node{
			core.Each(func() []TaskLine { return TaskLines(run.Results, s.Catalog.Get()) }, func(l TaskLine) string { return l.TaskID },
				func(_ *core.Scope, l func() TaskLine, _ func() int) core.Node { return taskLine(l) }),
		}}
	} else if len(run.Details) > 0 {
		lines := make([]core.Node, len(run.Details))
		for i, line := range run.Details {
			lines[i] = core.Text{Content: line, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5,
				Color: kit.ColorTextSecondary}}
		}
		body = core.View{Style: list, TestID: "run-lines", Children: lines}
	}
	times := kit.Tabular(kit.Text("开始 "+Clock(run.StartedAt, true)+" · 结束 "+Clock(run.EndedAt, true)+" · 用时 "+RunDuration(run),
		kit.FontSmall, kit.Regular, kit.ColorTextMuted))
	times.Style.Margin = core.Edges{Top: 10}
	times.TestID = "run-times"
	return core.View{TestID: "run-details", Style: core.Style{Padding: core.Edges{Top: 4, Right: 18, Bottom: 16, Left: 70}, Background: kit.SurfaceList,
		Borders: core.Edges{Top: 1}, BorderColor: kit.BorderSubtle}, Children: []core.Node{body, times}}
}

// taskLine is one task of a maintenance run: its title, how it ended (in the warning color when it needs attention) and its message.
func taskLine(l func() TaskLine) core.Node {
	text := func(fn func() string, size core.Points, weight core.FontWeight, color func() core.Color, st core.Style) core.Node {
		t := core.Text{ContentFunc: fn, StyleFunc: func() core.Style {
			s := st
			s.FontFamily, s.FontWeight, s.FontSize, s.LineHeightMultiple, s.Color = kit.FontFace(weight), kit.FaceWeight(weight), size, 1.5, color()
			return s
		}}
		return t
	}
	fixed := func(c core.Color) func() core.Color { return func() core.Color { return c } }
	return core.View{TestID: "task-" + l().TaskID, Style: core.Style{Direction: core.Row, AlignItems: core.AlignBaseline, Gap: 12}, Children: []core.Node{
		text(func() string { return l().Title }, kit.FontBody, kit.Semibold, fixed(kit.ColorText), core.Style{MinWidth: core.Pt(120), FlexShrink: 0}),
		text(func() string { return l().Status }, kit.FontSmall, kit.Regular, func() core.Color {
			if l().Attention {
				return kit.ColorWarning
			}
			return kit.ColorTextMuted
		}, core.Style{FlexShrink: 0}),
		text(func() string { return l().Message }, kit.FontBody, kit.Regular, fixed(kit.ColorTextSecondary), core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)}),
	}}
}
