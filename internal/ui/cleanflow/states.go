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

// stateStyle is ScanIdle's and ScanProgress's: a column centered in what is left of the page.
var stateStyle = core.Style{FlexGrow: 1, FlexShrink: 0, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 12,
	Padding: core.Symmetric(24, 0), TextAlign: core.TextAlignCenter}

// subText is the line under a state's title (.scan-state-sub).
func subText(s string) core.Node {
	return core.Text{Content: s, Style: core.Style{MaxWidth: core.Pt(440), FontFamily: kit.FontSans, FontSize: kit.FontSubheading,
		LineHeightMultiple: 1.5, Color: kit.ColorTextOverlaySub, TextAlign: core.TextAlignCenter}}
}

// pathStyle is the one line of a path being read or removed (.scan-state-path).
var pathStyle = core.Style{MaxWidth: core.Pt(520), FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorTextMuted,
	FontVariant: core.FontTabularNums, WhiteSpace: core.WhiteSpaceNoWrap}

// ScanIdle is a cleaner page before a scan: the page's tile, what the scan looks at and the round 扫描 button, centred in the page. Details sit under the description (the last clean); Error is why the last scan failed, in the warning colour.
type ScanIdle struct {
	Tone        kit.Tone
	Glyph       string
	Title       string
	Description string
	// ActionLabel is the button's text, 扫描 when "".
	ActionLabel string
	OnStart     func()
	Disabled    func() bool
	Error       func() string
	Details     core.Node
	// Ref names the button, for the focus.
	Ref    *core.Ref
	TestID string
}

// Render implements core.Node.
func (s ScanIdle) Render(*core.Scope) core.Node {
	label := s.ActionLabel
	if label == "" {
		label = "扫描"
	}
	children := []core.Node{
		core.View{Style: core.Style{Margin: core.Edges{Bottom: 6}},
			Children: []core.Node{kit.IconTile{Tone: s.Tone, Size: 72, GlyphSize: 34, Glyph: s.Glyph, Glow: true, Radius: 22}}},
		kit.Text(s.Title, kit.FontTitle, kit.Bold, kit.ColorText),
		subText(s.Description),
	}
	if s.Details != nil {
		children = append(children, s.Details)
	}
	children = append(children,
		// Only there while there is an error, so the gaps around it stay the design's.
		core.Show{When: func() bool { return s.Error != nil && s.Error() != "" }, Then: func(*core.Scope) core.Node {
			return core.Text{ContentFunc: s.Error, LiveRegion: core.LiveRegionPolite, Role: core.RoleAlert, TestID: "scan-error",
				Style: core.Style{MaxWidth: core.Pt(440), FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5,
					Color: kit.ColorWarning, TextAlign: core.TextAlignCenter}}
		}},
		core.View{Style: core.Style{Margin: core.Edges{Top: 22}},
			Children: []core.Node{kit.RunButton{Text: label, OnPress: s.OnStart, Disabled: s.Disabled, Ref: s.Ref, TestID: "scan"}}},
	)
	return core.View{Role: core.RoleRegion, Label: s.Title, TestID: s.TestID, Style: stateStyle, Children: children}
}

// IdleDetails is the quiet text under ScanIdle's description (.cleaner-idle-details), one line per entry; nothing, not even a gap, without lines.
func IdleDetails(lines func() []string) core.Node {
	return core.Show{When: func() bool { return len(lines()) > 0 }, Then: func(*core.Scope) core.Node { return idleDetails(lines) }}
}

func idleDetails(lines func() []string) core.Node {
	return core.View{Style: core.Style{MaxWidth: core.Pt(520), AlignItems: core.AlignCenter, Gap: 4}, Children: []core.Node{
		core.Each(lines, func(s string) string { return s }, func(_ *core.Scope, line func() string, _ func() int) core.Node {
			return core.Text{ContentFunc: line, MaxLines: 1, EllipsizeMode: core.EllipsizeTail, Style: core.Style{FontFamily: kit.FontSans,
				FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: kit.ColorTextMuted, FontVariant: core.FontTabularNums}}
		}),
	}}
}

// ScanProgress is a cleaner page while it scans, in place of its content: the ring (filling once the scanner knows how many steps there are, with the percent inside), the space found so far, the folder being read and 停止.
type ScanProgress struct {
	// Title is 正在扫描… when "".
	Title    string
	Progress func() cleanup.Progress
	OnCancel func()
	// CancelLabel is 停止 when "".
	CancelLabel string
	TestID      string
}

// Render implements core.Node.
func (s ScanProgress) Render(*core.Scope) core.Node {
	title, cancel := s.Title, s.CancelLabel
	if title == "" {
		title = "正在扫描…"
	}
	if cancel == "" {
		cancel = "停止"
	}
	value := func() (float64, bool) { return fractionOf(s.Progress()) }
	percent := core.Text{ContentFunc: func() string {
		f, ok := value()
		if !ok {
			return ""
		}
		return strconv.Itoa(int(f*100+0.5)) + "%"
	}}
	return core.View{Role: core.RoleRegion, Label: title, TestID: s.TestID, Busy: func() bool { return true }, Style: stateStyle, Children: []core.Node{
		kit.ProgressRing{Value: value, Size: 120, Label: title, Children: []core.Node{percent}, TestID: "scan-ring"},
		kit.Text(title, kit.FontTitle, kit.Bold, kit.ColorText),
		core.Text{ContentFunc: func() string { return "已找到 " + format.Bytes(s.Progress().Bytes) }, Style: core.Style{FontFamily: kit.FontSans,
			FontSize: kit.FontSubheading, LineHeightMultiple: 1.5, Color: kit.ColorTextOverlaySub}},
		core.Text{ContentFunc: func() string {
			if p := s.Progress().Path; p != "" {
				return format.TruncateMiddle(p, 64)
			}
			return "正在准备…"
		}, MaxLines: 1, EllipsizeMode: core.EllipsizeTail, Style: pathStyle},
		kit.Button{Text: cancel, Variant: kit.ButtonGhost, OnPress: s.OnCancel, TestID: "scan-stop"},
	}}
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

// CleanDoneDialog is the done card of a clean: the space given back, one line per category, the items left in place and what the Trash means (CleanSummary). InUse, when above 0, adds the line about the items another program held open (SetInUseAside took them out of Outcome).
type CleanDoneDialog struct {
	Open    func() bool
	Outcome CleanOutcome
	InUse   int
	OnClose func()
	TestID  string
}

// Render implements core.Node.
func (d CleanDoneDialog) Render(*core.Scope) core.Node {
	s := CleanSummary(d.Outcome)
	var children []core.Node
	if d.InUse > 0 {
		children = append(children, core.Text{Content: strconv.Itoa(d.InUse) + " 项里有文件正被其他程序使用，已跳过这些文件，下次可以再试", TestID: "done-in-use",
			Style: core.Style{MaxWidth: core.Pt(380), FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5,
				Color: kit.ColorTextNavFooter, TextAlign: core.TextAlignCenter}})
	}
	return kit.DoneDialog{Open: d.Open, Title: s.Title, Big: s.Big, Lines: s.Lines, Succeeded: s.Succeeded, Children: children,
		OnClose: d.OnClose, TestID: d.TestID}
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
