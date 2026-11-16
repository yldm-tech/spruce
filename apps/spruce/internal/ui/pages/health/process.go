package health

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/engine/sysinfo"
	"spruce/apps/spruce/internal/ui/kit"
)

// sortColumn is the column a process list is sorted by, which its header emphasises.
type sortColumn uint8

const (
	byCPU sortColumn = iota
	byMemory
)

// listState is what a process list shows.
type listState uint8

const (
	listPending listState = iota
	listUnreadable
	listMeasuring
	listReady
)

// numWidth is the width of a process list's CPU and memory columns.
const numWidth core.Points = 84

// processTable is one of the two top-process lists: name with PID, CPU and memory. It only shows: Spruce never ends processes.
type processTable struct {
	Title  string
	Sorted sortColumn
	TestID string
	// Latest is the newest sample, nil before the first.
	Latest func() *sysinfo.Vitals
}

func (t processTable) list() []sysinfo.ProcessUsage {
	l := t.Latest()
	if l == nil || l.Processes == nil {
		return nil
	}
	if t.Sorted == byCPU {
		return l.Processes.TopCPU
	}
	return l.Processes.TopMemory
}

func (t processTable) state() listState {
	l := t.Latest()
	switch {
	case l == nil:
		return listPending
	case l.Processes == nil:
		return listUnreadable
	case len(t.list()) == 0:
		return listMeasuring
	}
	return listReady
}

// Render implements core.Node.
func (t processTable) Render(cx *core.Scope) core.Node {
	state := core.NewMemo(cx, t.state)
	note := func(s string) core.Node { return kit.Text(s, kit.FontBody, kit.Regular, kit.ColorTextMuted) }
	body := core.Dynamic(func(*core.Scope) core.Node {
		switch state.Get() {
		case listPending:
			return note("正在读取…")
		case listUnreadable:
			return note("无法读取")
		case listMeasuring:
			return note("正在测量…")
		}
		return core.Func(func(*core.Scope) core.Node { return t.table() })
	})
	return core.View{Role: core.RoleRegion, Label: t.Title, TestID: t.TestID, Style: core.Style{MinWidth: core.Pt(0)}, Children: []core.Node{
		kit.Card{Padding: core.Symmetric(18, 20), Gap: 8, Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0)}, Children: []core.Node{
			core.Text{Content: t.Title, Role: core.RoleHeader, Style: core.Style{FontFamily: kit.FontFace(kit.Semibold), FontWeight: kit.FaceWeight(kit.Semibold),
				FontSize: kit.FontBody, Color: kit.ColorTextSecondary}},
			body,
		}},
	}}
}

// headerCell is a column header, emphasised when the list is sorted by it.
func headerCell(text string, sorted bool, num bool) core.Node {
	st := core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, Color: kit.ColorTextMuted}
	if sorted {
		st.FontFamily, st.FontWeight, st.Color = kit.FontFace(kit.Semibold), kit.FaceWeight(kit.Semibold), kit.ColorText
	}
	if num {
		st.Width, st.TextAlign, st.FlexShrink = core.Pt(numWidth), core.TextAlignRight, 0
	} else {
		st.FlexGrow, st.FlexShrink, st.MinWidth = 1, 1, core.Pt(0)
	}
	return core.Text{Content: text, Role: core.RoleColumnHeader, Style: st}
}

// numCell is a right-aligned figure of a row.
func numCell(fn func() string) core.Node {
	return core.Text{ContentFunc: fn, Role: core.RoleCell, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorText,
		Width: core.Pt(numWidth), FlexShrink: 0, TextAlign: core.TextAlignRight, FontVariant: core.FontTabularNums}}
}

// nameTip is the hover label of a process name that does not fit (the title attribute of the design).
func nameTip(name string) core.Node {
	return core.View{Style: core.Style{Padding: core.Symmetric(4, 8), CornerRadius: 6, Background: core.RGBA(0x1a1030, 0.92)},
		Children: []core.Node{kit.Text(name, kit.FontSmall, kit.Regular, kit.ColorText)}}
}

func (t processTable) table() core.Node {
	row := func() core.Style {
		return core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Padding: core.Symmetric(6, 0)}
	}
	header := core.View{Role: core.RoleRow, Style: row(), Children: []core.Node{
		headerCell("进程", false, false), headerCell("CPU", t.Sorted == byCPU, true), headerCell("内存", t.Sorted == byMemory, true),
	}}
	key := func(p sysinfo.ProcessUsage) int32 { return p.PID }
	rows := core.Each(t.list, key, func(_ *core.Scope, p func() sysinfo.ProcessUsage, _ func() int) core.Node {
		pid := p().PID
		st := row()
		st.Borders, st.BorderColor = core.Edges{Top: 1}, kit.BorderSubtle
		name := core.View{Role: core.RoleCell, LabelFunc: func() string { return p().Name + "，PID " + strconv.Itoa(int(pid)) }, Tooltip: nameTip(p().Name),
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignBaseline, Gap: 8, FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0)},
			Children: []core.Node{
				core.Text{ContentFunc: func() string { return p().Name }, MaxLines: 1, EllipsizeMode: core.EllipsizeTail,
					Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorText, FlexShrink: 1, MinWidth: core.Pt(0)}},
				core.Text{Content: "PID " + strconv.Itoa(int(pid)), Style: core.Style{FontFamily: kit.FontSans, FontSize: 11, Color: kit.ColorTextSection, FlexShrink: 0}},
			}}
		return core.View{Role: core.RoleRow, TestID: t.TestID + "-" + strconv.Itoa(int(pid)), Style: st, Children: []core.Node{
			name,
			numCell(func() string { return ProcessCPU(p()) }),
			numCell(func() string { return ProcessMemory(p()) }),
		}}
	})
	return core.View{Role: core.RoleTable, Label: t.Title, Children: []core.Node{header, rows}}
}
