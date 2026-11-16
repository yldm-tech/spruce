// Package history is the 操作历史 page: every recorded run, newest first and grouped by day, with filter chips by kind, the space the clean-ups freed, each run's details on demand, 放回原处 for recent moves to the Trash (macOS), and 清空历史.
package history

import (
	"context"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	enginehist "spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/ui/async"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/maintenance"
	"spruce/apps/spruce/internal/ui/shared"
	"spruce/apps/spruce/internal/ui/store"
)

// Page is the 操作历史 page.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	stores := store.Use(cx)
	s := NewState(cx, stores, deps.Catalog, nav.Use(cx))
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
		kit.Page{ScrollKey: nav.History, Header: header, Children: []core.Node{loadErr, empty, content}},
		shared.ClearHistoryDialog{Open: clearing.Get, Count: func() int { return len(s.Runs.Get()) }, OnClose: func() { clearing.Set(false) }, ReturnFocus: clearRef},
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

// The row's line heights, which match the browser's normal line height for the title (15 points) and the summary (13) with the Chinese fallback font, so a row is 65 points tall over its 1-point separator, as in the design (Apple text rounds each line up to a whole point).
const (
	titleLineHeight   = 1.25
	summaryLineHeight = 1.5
)

// rowTitle is a row's title, the kind's page title.
func rowTitle(s string) core.Text {
	t := kit.Text(s, kit.FontCardTitle, kit.Semibold, kit.ColorText)
	t.Style.LineHeightMultiple = titleLineHeight
	return t
}

// runRow is one run: a button with the kind's tile, title and summary line, the start time and a chevron, which shows or hides the run's details under it. A run 放回原处 can put back shows that button left of the time; it is a sibling of the row's button, not inside it, so the row's press area lies under the line and the line itself lets presses through to it.
// enterRows is how many rows of a day come in with a motion.
const enterRows = 12

func runRow(cx *core.Scope, s *State, run enginehist.Run, index func() int) core.Node {
	open := func() bool { return s.IsOpen(run.ID) }
	meta := KindMeta(run.Kind)
	title := RunTitle(run)
	summary := RunSummary(run)
	reduce := cx.Env().ReduceMotion
	head := core.View{Role: core.RoleButton, Label: title + "，" + summary + "，" + Clock(run.StartedAt, false), TestID: "run-" + run.ID + "-head",
		Expanded: open,
		OnPress:  func(core.PressEvent) { s.SetOpen(run.ID, !s.open.GetUntracked()[run.ID]) },
		// A run 放回原处 can put back offers it on a right click as well, while nothing is being cleaned or put back.
		OnContextMenu: kit.RowMenu(cx, func() []desktop.MenuItem {
			if !core.Untracked(cx, func() bool { return s.CanRestore(run.ID) && s.Restoring.Get() == "" && !s.stores.Overview.Busy() }) {
				return nil
			}
			return []desktop.MenuItem{{Label: "放回原处", OnSelect: func() { s.Restore(run.ID) }}}
		}),
		Style: core.Style{Position: core.Absolute, Left: core.Pt(0), Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Cursor: core.CursorPointer,
			Hover:        &core.Style{Background: kit.SurfaceRowHover},
			FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: -2, OutlineColor: kit.ColorText}}}
	inert := func(st core.Style, children ...core.Node) core.Node {
		return core.View{PointerEvents: core.PointerEventsNone, Style: st, Children: children}
	}
	restoredNote := func() string { return RestoredNote(run, s.Runs.Get()) }
	note := core.Show{When: func() bool { return restoredNote() != "" }, Then: func(*core.Scope) core.Node {
		return core.Text{ContentFunc: restoredNote, TestID: "run-" + run.ID + "-restored", MaxLines: 1,
			Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: summaryLineHeight, Color: kit.ColorTextMuted, FlexShrink: 0}}
	}}
	restore := core.Show{When: func() bool { return s.CanRestore(run.ID) }, Then: func(*core.Scope) core.Node {
		return kit.Button{Variant: kit.ButtonGhost, TestID: "run-" + run.ID + "-restore",
			TextFunc: func() string {
				if s.Restoring.Get() == run.ID {
					return "正在放回…"
				}
				return "放回原处"
			},
			Busy:     func() bool { return s.Restoring.Get() == run.ID },
			Disabled: func() bool { return s.Restoring.Get() != "" || s.stores.Overview.Busy() },
			OnPress:  func() { s.Restore(run.ID) }}
	}}
	line := core.View{PointerEvents: core.PointerEventsBoxNone,
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14, Padding: core.Symmetric(12, 18)},
		Children: []core.Node{
			inert(core.Style{FlexShrink: 0}, kit.IconTile{Tone: meta.Tone, Size: 38, GlyphSize: 19, Glyph: meta.Glyph, Radius: kit.RadiusCardIcon}),
			inert(core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2},
				rowTitle(title),
				core.View{Style: core.Style{Direction: core.Row, MinWidth: core.Pt(0)}, Children: []core.Node{
					core.Text{Content: summary, MaxLines: 1, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody,
						LineHeightMultiple: summaryLineHeight, Color: kit.ColorTextMuted, FlexShrink: 1, MinWidth: core.Pt(0)}},
					note,
				}},
			),
			restore,
			inert(core.Style{FlexShrink: 0}, kit.Tabular(kit.Text(Clock(run.StartedAt, false), kit.FontBody, kit.Regular, kit.ColorTextSecondary))),
			core.View{PointerEvents: core.PointerEventsNone, StyleFunc: func() core.Style {
				st := core.Style{FlexShrink: 0, Color: kit.ColorTextMuted}
				if !reduce() {
					st.Transition = core.Transition{Duration: 150_000_000, Easing: core.Ease, Properties: core.TransitionTransform}
				}
				if open() {
					st.Transform.Rotate = 90
				}
				return st
			}, Children: []core.Node{kit.Glyph{Name: "chevron-right", Size: 16, Color: core.CurrentColor}}},
		}}
	details := core.Show{When: open, Then: func(cx *core.Scope) core.Node {
		return kit.Appear{Entrance: kit.EnterFade, Children: []core.Node{runDetails(cx, s, run)}}
	}}
	// The first rows come in one after another; the ones further down, out of view, just show.
	var enter *core.AnimatedStyle
	if i := core.Untracked(cx, index); i < enterRows {
		enter = kit.Enter(cx, kit.EnterRise, kit.StaggerDelay(i))
	}
	return core.View{TestID: "run-" + run.ID, StyleFunc: func() core.Style {
		if index() == 0 {
			return core.Style{Animated: enter}
		}
		return core.Style{Borders: core.Edges{Top: 1}, BorderColor: kit.BorderSubtle, Animated: enter}
	}, Children: []core.Node{core.View{Children: []core.Node{head, line}}, details}}
}

// runDetails is what a run's row shows when open: a maintenance run's tasks with how each ended, another run's detail lines, and the run's start, end and duration, all selectable to copy. A line that names a path offers 在访达中显示 and 拷贝路径 on a right click.
func runDetails(cx *core.Scope, s *State, run enginehist.Run) core.Node {
	list := core.Style{Gap: 8, Padding: core.Edges{Top: 8}}
	var body core.Node = core.Group{}
	if run.Kind == enginehist.KindMaintenance {
		// The grid is made again when the catalog (the titles) arrives.
		body = core.Dynamic(func(*core.Scope) core.Node {
			return maintenance.TaskGrid(maintenance.TaskLines(run.Results, s.Catalog.Get()), core.Style{Padding: core.Edges{Top: 8}}, "run-tasks")
		})
	} else if len(run.Details) > 0 {
		deps := DepsKey.Use(cx)
		lines := make([]core.Node, len(run.Details))
		for i, line := range run.Details {
			lines[i] = detailLine(cx, s, deps, run.ID, line, "run-line-"+strconv.Itoa(i))
		}
		body = core.View{Style: list, TestID: "run-lines", Children: lines}
	}
	times := kit.Tabular(kit.Text("开始 "+Clock(run.StartedAt, true)+" · 结束 "+Clock(run.EndedAt, true)+" · 用时 "+RunDuration(run),
		kit.FontSmall, kit.Regular, kit.ColorTextMuted))
	times.Style.Margin = core.Edges{Top: 10}
	times.TestID = "run-times"
	return core.View{TestID: "run-details", Selectable: true, Style: core.Style{Padding: core.Edges{Top: 4, Right: 18, Bottom: 16, Left: 70}, Background: kit.SurfaceList,
		Borders: core.Edges{Top: 1}, BorderColor: kit.BorderSubtle}, Children: []core.Node{body, times}}
}

// detailLine is one line of the details of the run with ID id. One that names a path (DetailPath) reveals it or copies it from its right-click menu; a reveal that fails says so in the toast. A path something was moved from is not revealed while the run can put it back, since the item is then in the Trash.
func detailLine(cx *core.Scope, s *State, deps Deps, id, line, testID string) core.Node {
	text := core.Text{Content: line, TestID: testID, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5,
		Color: kit.ColorTextSecondary}}
	path, moved := DetailPath(line, deps.Home)
	if path == "" {
		return text
	}
	toasts := s.stores.Toasts
	reveal := func() {
		async.Do(cx, cx.Context(), func(ctx context.Context) (struct{}, error) { return struct{}{}, deps.Files.Reveal(ctx, path) },
			func(_ struct{}, err error) {
				if err != nil {
					toasts.Show(store.Toast{Text: err.Error(), Duration: toastShort})
				}
			})
	}
	return core.View{OnContextMenu: kit.RowMenu(cx, func() []desktop.MenuItem {
		if moved && core.Untracked(cx, func() bool { return s.CanRestore(id) }) {
			return []desktop.MenuItem{kit.CopyPathItem(cx, path)}
		}
		return []desktop.MenuItem{kit.RevealItem(format.FileManagerName(), reveal), kit.CopyPathItem(cx, path)}
	}), Children: []core.Node{text}}
}
