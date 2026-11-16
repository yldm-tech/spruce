package overview

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// checkProgress is the conclusion's place while the first check runs (state A): the overall ring, what the check does, one row per source with its own bar and state, and 停止.
func checkProgress(progress func() cleanup.Progress, stop func()) core.Node {
	overall := func() (float64, bool) { return OverallFraction(progress()) }
	percent := core.Text{ContentFunc: func() string {
		v, ok := overall()
		if !ok {
			return ""
		}
		return strconv.Itoa(int(v*100+0.5)) + "%"
	}}
	rows := make([]core.Node, len(Sources))
	for i, id := range Sources {
		rows[i] = sourceRow{ID: id, Index: i, Row: func() SourceRow { return SourceRows(progress())[i] }}
	}
	return core.View{Role: core.RoleRegion, Label: "正在检查", TestID: "check-progress", Busy: func() bool { return true },
		Style: core.Style{FlexShrink: 0, AlignItems: core.AlignCenter, Gap: 14, Padding: core.Edges{Top: 16, Bottom: 8}, TextAlign: core.TextAlignCenter},
		Children: []core.Node{
			kit.ProgressRing{Value: overall, Size: 96, Label: "检查进度", Children: []core.Node{percent}, TestID: "check-ring"},
			kit.Text("正在检查哪些空间可以放心释放…", kit.FontTitle, kit.Bold, kit.ColorText),
			core.Text{Content: "只读取，不会删除任何东西，通常不到一分钟。", Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody,
				LineHeightMultiple: 1.5, Color: kit.ColorTextMuted}},
			core.View{Role: core.RoleGroup, Label: "检查进度", Style: core.Style{Width: core.Pct(100), MaxWidth: core.Pt(620), Gap: 10, Margin: core.Edges{Top: 4}},
				Children: rows},
			kit.Button{Text: "停止", Variant: kit.ButtonGhost, OnPress: stop, TestID: "check-stop"},
		}}
}

// sourceRow is one source while the check runs (.smart-scan-row): its tile, name and scope over its bar, and its state on the right.
type sourceRow struct {
	ID    app.CleanerID
	Index int
	Row   func() SourceRow
}

// Render implements core.Node.
func (r sourceRow) Render(cx *core.Scope) core.Node {
	meta := nav.Route(r.ID).Meta()
	id := string(r.ID)
	known := core.NewMemo(cx, func() bool { return r.Row().Known })
	// The bar fills once the source knows how much there is; until then the band of light sweeps across it.
	bar := core.Dynamic(func(*core.Scope) core.Node {
		if !known.Get() {
			return core.Func(func(*core.Scope) core.Node { return kit.SweepBar{Height: 6, TestID: "sweep-" + id} })
		}
		return core.Func(func(*core.Scope) core.Node {
			return kit.Meter{Value: func() float64 { return r.Row().Fraction }, Height: 6, Label: meta.Title + "检查进度", TestID: "bar-" + id,
				Active: func() bool { return r.Row().Status == RowStatus(cleanup.SourceScanning) }}
		})
	})
	status := core.Text{TestID: "row-status-" + id, ContentFunc: func() string { return RowStatusText(r.Row()) }, StyleFunc: func() core.Style {
		c := kit.ColorTextSecondary
		if r.Row().Status == RowStatus(cleanup.SourceFailed) {
			c = kit.ColorWarning
		}
		return core.Style{FlexGrow: 0, FlexShrink: 1, FlexBasis: core.Pt(220), TextAlign: core.TextAlignRight, FontFamily: kit.FontSans,
			FontSize: kit.FontBody, LineHeightMultiple: 1.4, Color: c, FontVariant: core.FontTabularNums}
	}}
	return core.View{TestID: "row-" + id,
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14, Padding: core.Symmetric(14, 18), CornerRadius: kit.RadiusCard,
			Background: kit.SurfaceCard, BorderWidth: 1, BorderColor: kit.BorderCard, TextAlign: core.TextAlignLeft,
			Animated: kit.Enter(cx, kit.EnterRise, kit.StaggerDelay(r.Index+1))},
		Children: []core.Node{
			kit.IconTile{Tone: meta.Tone, Size: 36, GlyphSize: 18, Glyph: meta.Glyph, Radius: kit.RadiusCardIcon},
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 4}, Children: []core.Node{
				kit.Text(meta.Title, kit.FontCardTitle, kit.Semibold, kit.ColorText),
				kit.Text(SourceScope[r.ID], kit.FontSection, kit.Regular, kit.ColorTextSection),
				core.View{Style: core.Style{Margin: core.Edges{Top: 4}}, Children: []core.Node{bar}},
			}},
			status,
		}}
}
