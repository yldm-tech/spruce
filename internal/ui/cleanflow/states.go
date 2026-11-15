package cleanflow

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
)

// monoFont is the design's --font-mono, for the tools' clean commands.
const monoFont = "Menlo, Consolas, monospace"

// fractionOf is how far a progress event got, and whether its total is known.
func fractionOf(p cleanup.Progress) (float64, bool) {
	if p.Total <= 0 {
		return 0, false
	}
	return float64(p.Done) / float64(p.Total), true
}

// CleaningOverlay covers the main area while a clean runs, as the 维护 run overlay: the ring, Sub (e.g. 共 34 项), what is done and freed so far and the item being removed. OnCancel shows 停止, which ends the clean after the current item.
type CleaningOverlay struct {
	Open func() bool
	// Title is 正在清理… when "".
	Title    string
	Sub      func() string
	Progress func() cleanup.Progress
	OnCancel func()
	TestID   string
}

// Render implements core.Node.
func (o CleaningOverlay) Render(*core.Scope) core.Node {
	title := o.Title
	if title == "" {
		title = "正在清理…"
	}
	return kit.Overlay{Open: o.Open, Title: title, Sub: o.Sub, OnCancel: o.OnCancel, TestID: o.TestID,
		Ring: kit.ProgressRing{Value: func() (float64, bool) { return fractionOf(o.Progress()) }},
		Lines: func() []string {
			p := o.Progress()
			if p.Total <= 0 {
				return []string{"正在准备…"}
			}
			lines := []string{"已处理 " + strconv.Itoa(p.Done) + "/" + strconv.Itoa(p.Total) + " · 已释放 " + format.Bytes(p.Bytes)}
			if p.Path != "" {
				lines = append(lines, format.TruncateMiddle(p.Path, 64))
			}
			return lines
		}}
}

// CleanPlanDialog is the question asked before a clean (CleanConfirmation), listing what the tools' own commands clean, what is deleted for good and what goes to the Trash, category by category. It is the danger kind, focusing 取消 first, when anything is deleted for good. The caller sets Open, OnConfirm and OnCancel.
func CleanPlanDialog(categories []cleanup.Category, sel Selection) kit.ConfirmDialog {
	c := CleanConfirmation(categories, sel)
	plan := CleanPlan(categories, sel)
	message := core.View{Style: core.Style{AlignItems: core.AlignStretch}, Children: []core.Node{
		core.Text{Content: c.Message, TestID: "plan-message"},
		planGroup("运行清理命令", "plan-commands", plan.Commands),
		planGroup("永久删除", "plan-permanent", plan.Permanent),
		planGroup("移到"+format.TrashName(), "plan-trash", plan.Trash),
	}}
	return kit.ConfirmDialog{Title: "确认清理", Message: message, ConfirmLabel: "清理", Danger: c.Danger, TestID: "confirm"}
}

// planGroup is one group of the plan: its heading over a list of its categories, each with its count and size; nothing when lines is empty.
func planGroup(title, testID string, lines []PlanLine) core.Node {
	if len(lines) == 0 {
		return core.Group{}
	}
	rows := make([]core.Node, len(lines))
	for i, l := range lines {
		name := []core.Node{core.Text{Content: l.Title}}
		if l.Command != "" {
			name = append(name, core.Text{Content: l.Command, Style: core.Style{FontFamily: monoFont, FontSize: kit.FontSection, Color: kit.ColorTextSecondary}})
		}
		rows[i] = core.View{TestID: testID + "-" + strconv.Itoa(i), Style: core.Style{Direction: core.Row, JustifyContent: core.JustifySpaceBetween, Gap: 16, Padding: core.Symmetric(3, 0)},
			Children: []core.Node{
				core.View{Style: core.Style{Gap: 2, FlexShrink: 1, MinWidth: core.Pt(0)}, Children: name},
				core.Text{Content: strconv.Itoa(l.Count) + " 项 · " + format.Bytes(l.Bytes),
					Style: core.Style{FlexShrink: 0, Color: kit.ColorTextSecondary, FontVariant: core.FontTabularNums}},
			}}
	}
	return core.View{Role: core.RoleRegion, Label: title, TestID: testID, Style: core.Style{Gap: 6, Margin: core.Edges{Top: 14}, TextAlign: core.TextAlignLeft},
		Children: []core.Node{
			core.Text{Content: title, Style: core.Style{FontFamily: kit.FontFace(kit.Semibold), FontWeight: kit.FaceWeight(kit.Semibold), FontSize: kit.FontSection,
				LetterSpacingEm: kit.LetterSpacingSection, Color: kit.ColorTextSection}},
			core.View{Style: core.Style{Padding: core.Symmetric(8, 14), CornerRadius: kit.RadiusCardIcon, Background: kit.SurfaceList,
				BorderWidth: 1, BorderColor: kit.BorderSubtle}, Children: rows},
		}}
}
