package space

import (
	"context"
	"errors"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/advice"
	"github.com/yldm-tech/spruce/internal/engine/growth"
	"github.com/yldm-tech/spruce/internal/ui/async"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// growthMax is how many locations 最近变化 lists.
const growthMax = 20

// growthActionWidth fits a row's longer action, 去清理 (the other is 扫描).
const growthActionWidth = 48

// growthRead is what 最近变化 read for one tab.
type growthRead struct {
	window GrowthWindow
	list   []growth.Growth
	err    error
	loaded bool
}

// growthView is the start screen's 最近变化: the locations that grew most over the tab's window (24 小时, 7 天, 30 天), each with 去清理 when a card of 概览 covers it, else 扫描 (the folder, on this page), and 在访达中显示. It reads the snapshots again whenever the tab changes; before two snapshots exist (or without 概览's controller) it says they are coming, and when they cannot be read it says so.
func growthView(cx *core.Scope, s *State, files Files) core.Node {
	stores := store.Use(cx)
	navigator := nav.Use(cx)
	ctx := cx.Context()
	window := core.NewSignal(cx, GrowthWeek)
	read := core.NewSignalFunc(cx, growthRead{}, nil)
	revealErr := core.NewSignal(cx, "")
	seq := 0
	cx.Effect(func() {
		w := window.Get()
		seq++
		mine := seq
		cx.Untrack(func() {
			stores.Overview.Growth(cx, ctx, time.Duration(w), func(list []growth.Growth, err error) {
				if mine == seq {
					read.Set(growthRead{window: w, list: list, err: err, loaded: true})
				}
			})
		})
	})
	home := func() string {
		v, _ := stores.Locations.Get()
		for _, t := range v.Targets {
			if t.ID == "home" {
				return t.Path
			}
		}
		return ""
	}
	actions := growthActions{
		clean: func(path string) string {
			res := stores.Overview.State().Result
			if res == nil {
				return ""
			}
			return advice.Covering(res.Recommendations, res.Scan, path)
		},
		goCard: func(id string) {
			if navigator != nil {
				navigator.GoOverviewCard(id)
			}
		},
		scan: s.ScanPath,
		reveal: func(path string) {
			revealErr.Set("")
			async.Do(cx, ctx, func(ctx context.Context) (struct{}, error) { return struct{}{}, files.Reveal(ctx, path) },
				func(_ struct{}, err error) {
					if err != nil {
						revealErr.Set(err.Error())
					}
				})
		},
	}

	tabs := kit.Tabs[GrowthWindow]{Label: "时间范围", Value: window.Get, OnChange: window.Set, Options: []kit.Option[GrowthWindow]{
		{Value: GrowthDay, Label: GrowthDay.Label(), TestID: "growth-24h"},
		{Value: GrowthWeek, Label: GrowthWeek.Label(), TestID: "growth-7d"},
		{Value: GrowthMonth, Label: GrowthMonth.Label(), TestID: "growth-30d"},
	}}
	body := core.Dynamic(func(*core.Scope) core.Node {
		g := read.Get()
		switch {
		case !g.loaded:
			return core.Group{}
		case errors.Is(g.err, growth.ErrNoHistory), g.err != nil && !stores.Overview.Connected():
			return core.Func(func(*core.Scope) core.Node {
				return mutedLine("Spruce 会定期记下主要位置的大小，过一两天这里就会显示哪些位置在变大。", "growth-empty")
			})
		case g.err != nil:
			return core.Func(func(*core.Scope) core.Node {
				return kit.ErrorLine{Text: func() string { return "无法读取空间变化的记录" }, TestID: "growth-error"}
			})
		case len(g.list) == 0:
			return core.Func(func(*core.Scope) core.Node { return mutedLine(GrowthNone(g.window), "growth-empty") })
		}
		list := g.list[:min(growthMax, len(g.list))]
		// The names write the home folder as ~ once the start screen's folders arrive.
		h := home()
		return core.Func(func(cx *core.Scope) core.Node {
			// The rows name days in the user's time zone (GrowthWhen takes now's): the wall time a test clock gives (core.ManualClock.SetWallTime) comes in UTC whatever time zone it was set from.
			now := cx.WallTime().Local()
			rows := make([]core.Node, len(list))
			for i, item := range list {
				rows[i] = growthRow(i, item, GrowthName(item, h), now, actions)
			}
			return kit.Card{Clip: true, Padding: core.Symmetric(6, 0), Children: rows}
		})
	})
	return core.View{Role: core.RoleRegion, Label: "最近变化", TestID: "space-growth", Busy: func() bool { return !read.Get().loaded }, Style: core.Style{Gap: 10},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 12},
				Children: []core.Node{kit.SectionTitle("最近变化"), tabs}},
			body,
			kit.ErrorLine{Text: revealErr.Get, TestID: "growth-reveal-error"},
			mutedLine("每天记录一次，保留 90 天。", "growth-note"),
		}}
}

// mutedLine is a line of muted body text.
func mutedLine(text, testID string) core.Node {
	return core.Text{Content: text, TestID: testID, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: kit.ColorTextMuted}}
}

// growthActions are what a row of 最近变化 does.
type growthActions struct {
	// clean returns the card of 概览 that covers path, "" for none.
	clean  func(path string) string
	goCard func(id string)
	scan   func(path string)
	reveal func(path string)
}

// growthRow is one location that grew: its tile and name, since when and by how much, the figures, and what to do about it.
func growthRow(i int, g growth.Growth, name string, now time.Time, a growthActions) core.Node {
	id := "growth-row-" + strconv.Itoa(i)
	icon := g.Icon
	if icon == "" {
		icon = "folder"
	}
	action := core.Dynamic(func(*core.Scope) core.Node {
		card := a.clean(g.Path)
		return core.Func(func(*core.Scope) core.Node {
			if card != "" {
				return kit.Link{Text: func() string { return "去清理" }, TestID: id + "-clean", OnPress: func() { a.goCard(card) }}
			}
			return kit.Link{Text: func() string { return "扫描" }, TestID: id + "-space", OnPress: func() { a.scan(g.Path) }}
		})
	})
	return core.View{TestID: id, Tooltip: kit.Tooltip(g.Path),
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Padding: core.Symmetric(8, 16)},
		Children: []core.Node{
			kit.CategoryTile(icon),
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2}, Children: []core.Node{
				core.Text{Content: name, MaxLines: 1, EllipsizeMode: core.EllipsizeMiddle, Style: core.Style{FontFamily: kit.FontFace(kit.Semibold),
					FontWeight: kit.FaceWeight(kit.Semibold), FontSize: kit.FontBody, Color: kit.ColorText}},
				core.Text{Content: GrowthSentence(g, now), TestID: id + "-text", MaxLines: 1, EllipsizeMode: core.EllipsizeTail,
					Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorTextMuted}},
			}},
			core.View{Style: core.Style{AlignItems: core.AlignEnd, Gap: 2, FlexShrink: 0}, Children: []core.Node{
				kit.Tabular(kit.Text("+"+format.Bytes(g.After-g.Before), kit.FontCardTitle, kit.Bold, kit.ColorText)),
				kit.Tabular(kit.Text(format.Bytes(g.Before)+" → "+format.Bytes(g.After), kit.FontSmall, kit.Regular, kit.ColorTextMuted)),
			}},
			// The action column has one width, so the figures beside it line up from row to row.
			core.View{Style: core.Style{Width: core.Pt(growthActionWidth), FlexShrink: 0, Direction: core.Row, JustifyContent: core.JustifyEnd},
				Children: []core.Node{action}},
			kit.IconButton{Glyph: "reveal", Label: "在" + format.FileManagerName() + "中显示 " + name, TestID: id + "-reveal", OnPress: func() { a.reveal(g.Path) }},
		}}
}
