package kit

import (
	"github.com/nsgo-dev/nsgo/core"
)

// Overlay covers the main area while work goes on (the cleaning, running and waiting overlays): the ring, the title, Sub, the progress Lines and a Note in the warning color, centered on the blurred scrim; no card. With OnCancel a ghost button (CancelLabel, 停止 by default) stops the work; Escape never closes it.
type Overlay struct {
	Open        func() bool
	Title       string
	Sub         func() string
	Ring        ProgressRing
	Lines       func() []string
	Note        func() string
	CancelLabel string
	OnCancel    func()
	ReturnFocus *core.Ref
	TestID      string
}

// Render implements core.Node.
func (o Overlay) Render(*core.Scope) core.Node {
	ring := o.Ring
	if ring.Label == "" {
		ring.Label = o.Title
	}
	children := []core.Node{
		ring,
		Text(o.Title, FontTitle, Bold, ColorText),
	}
	if o.Sub != nil {
		children = append(children, TextFunc(o.Sub, FontSubheading, Regular, ColorTextOverlaySub))
	}
	if o.Lines != nil {
		children = append(children, core.Each(o.Lines, func(s string) string { return s }, func(_ *core.Scope, line func() string, _ func() int) core.Node {
			return Tabular(TextFunc(line, FontBody, Regular, ColorTextSecondary))
		}))
	}
	if o.Note != nil {
		children = append(children, core.Show{When: func() bool { return o.Note() != "" }, Then: func(*core.Scope) core.Node {
			return core.Text{ContentFunc: o.Note, Style: core.Style{MaxWidth: core.Pt(360), FontFamily: FontSans, FontSize: FontBody,
				LineHeightMultiple: 1.5, Color: ColorWarning, TextAlign: core.TextAlignCenter}}
		}})
	}
	if o.OnCancel != nil {
		label := o.CancelLabel
		if label == "" {
			label = "停止"
		}
		children = append(children, Button{Text: label, Variant: ButtonGhost, OnPress: o.OnCancel, TestID: "overlay-cancel"})
	}
	return layer{open: o.Open, label: o.Title, role: core.RoleGroup, returnFocus: o.ReturnFocus, testID: o.TestID,
		sidebarTip: "完成或停止当前操作后可切换页面",
		popup:      core.Style{AlignItems: core.AlignCenter, Gap: 18, MaxWidth: core.Pct(100)},
		children:   children}.render()
}
