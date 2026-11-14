package kit

import (
	"strings"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// Blocker is the shell's: while a dialog or an overlay is open the sidebar shows disabled. The shell provides it with BlockerKey; store.Block implements it.
type Blocker interface {
	// Hold blocks the shell until cx is disposed.
	Hold(cx *core.Scope)
}

// BlockerKey provides the shell's Blocker to dialogs and overlays.
var BlockerKey = core.NewKey[Blocker]("spruce.blocker", nil)

// layer is what every dialog and overlay shares, on base.Dialog: a modal layer over the window whose scrim (Scrim over a 16-point backdrop blur) covers only the main area, the sidebar staying clear but out of reach, the popup centered in the main area. Escape closes it only when onClose is set; the scrim never does. initial takes the keyboard's focus as it opens and returnFocus as it goes.
type layer struct {
	open        func() bool
	onClose     func()
	label       string
	role        core.Role
	description string
	initial     *core.Ref
	returnFocus *core.Ref
	popup       core.Style
	children    []core.Node
	testID      string
}

func (l layer) render() core.Node {
	fill := core.Style{Position: core.Absolute, Top: core.Pt(0), Bottom: core.Pt(0)}
	scrim := fill
	scrim.Left, scrim.Right = core.Pt(SidebarWidth), core.Pt(0)
	scrim.Background, scrim.BackdropFilters = Scrim, []core.Filter{core.Blur(BlurOverlay)}
	sidebar := fill
	sidebar.Left, sidebar.Width = core.Pt(0), core.Pt(SidebarWidth)
	absorb := func(core.PressEvent) {}
	hold := core.Func(func(cx *core.Scope) core.Node {
		if b := BlockerKey.Use(cx); b != nil {
			b.Hold(cx)
		}
		return core.Group{}
	})
	return base.Dialog{
		Open:                   l.open,
		Animation:              core.ModalNone,
		NoCloseOnEscape:        l.onClose == nil,
		NoCloseOnBackdropPress: true,
		OnOpenChange: func(open bool, _ base.Reason) {
			if !open && l.onClose != nil {
				l.onClose()
			}
		},
		OnOpen: func() {
			if l.initial != nil {
				l.initial.Focus()
			}
		},
		OnClose: func() {
			if l.returnFocus != nil {
				l.returnFocus.Focus()
			}
		},
		LayerStyle: core.Style{Padding: core.Edges{Top: 24, Right: 24, Bottom: 24, Left: SidebarWidth + 24}, FontFamily: FontSans, Color: ColorText,
			TextAlign: core.TextAlignCenter},
		Before: []core.Node{
			core.View{Style: sidebar, OnPress: absorb, NoFocus: true},
			core.View{Style: scrim, OnPress: absorb, NoFocus: true},
		},
		PopupStyle:               l.popup,
		Label:                    l.label,
		Role:                     l.role,
		AccessibilityDescription: l.description,
		TestID:                   l.testID,
		Children:                 append([]core.Node{hold}, l.children...),
	}
}

// cardStyle is the glass card of the dialogs (.glass-card).
func cardStyle(padding core.Edges) core.Style {
	return core.Style{AlignItems: core.AlignCenter, Gap: 12, MaxWidth: core.Pt(480), MaxHeight: core.Pct(100), Padding: padding,
		CornerRadius: RadiusDialog, Background: SurfaceDialog, BorderWidth: 1, BorderColor: BorderDialog,
		Shadows: []core.BoxShadow{shadow(30, 80, ShadowDialog)}}
}

// title is a dialog's heading.
func title(s string) core.Node {
	return core.Text{Content: s, Role: core.RoleHeader, Style: textStyle(FontTitle, Bold, ColorText)}
}

// DialogAction is one of a Dialog's buttons.
type DialogAction struct {
	Text           string
	Variant        ButtonVariant
	OnPress        func()
	Busy, Disabled func() bool
	// Initial takes the keyboard's focus as the dialog opens.
	Initial bool
	TestID  string
}

// Dialog is the app's modal card (GlassDialog): Title, Children and a row of Actions over the blurred main area. OnClose, when set, is what Escape does; without it Escape does nothing. Danger dialogs focus the action marked Initial, as any other. ReturnFocus takes the focus back as it closes.
type Dialog struct {
	Open        func() bool
	Title       string
	Danger      bool
	Children    []core.Node
	Actions     []DialogAction
	OnClose     func()
	ReturnFocus *core.Ref
	TestID      string
}

// Render implements core.Node.
func (d Dialog) Render(*core.Scope) core.Node {
	var initial *core.Ref
	children := []core.Node{title(d.Title)}
	children = append(children, d.Children...)
	if len(d.Actions) > 0 {
		row := make([]core.Node, len(d.Actions))
		for i, a := range d.Actions {
			ref := new(core.Ref)
			if a.Initial {
				initial = ref
			}
			row[i] = Button{Text: a.Text, Variant: a.Variant, OnPress: a.OnPress, Busy: a.Busy, Disabled: a.Disabled, Ref: ref, Large: true, TestID: a.TestID}
		}
		children = append(children, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Margin: core.Edges{Top: 10}}, Children: row})
	}
	return layer{open: d.Open, onClose: d.OnClose, label: d.Title, initial: initial, returnFocus: d.ReturnFocus, testID: d.TestID,
		popup: cardStyle(core.Symmetric(32, 40)), children: children}.render()
}

// ConfirmDialog asks before an action: a title, the message (Lines, one per line, or Message) and 取消 and the confirmation. Danger draws the confirmation red and focuses 取消 first, for what cannot be undone. Busy disables both buttons and Escape while the action starts; Escape is 取消 otherwise.
type ConfirmDialog struct {
	Open                      func() bool
	Title                     string
	Lines                     []string
	Message                   core.Node
	ConfirmLabel, CancelLabel string
	Danger                    bool
	Busy                      func() bool
	OnConfirm, OnCancel       func()
	ReturnFocus               *core.Ref
	TestID                    string
}

// Render implements core.Node.
func (c ConfirmDialog) Render(cx *core.Scope) core.Node {
	confirm, cancel := c.ConfirmLabel, c.CancelLabel
	if confirm == "" {
		confirm = "确定"
	}
	if cancel == "" {
		cancel = "取消"
	}
	busy := core.NewMemo(cx, func() bool { return is(c.Busy) })
	message := c.Message
	if message == nil {
		message = core.Text{Content: strings.Join(c.Lines, "\n")}
	}
	variant := ButtonLight
	if c.Danger {
		variant = ButtonDanger
	}
	cancelRef, confirmRef := new(core.Ref), new(core.Ref)
	initial := confirmRef
	if c.Danger {
		initial = cancelRef
	}
	// Escape is 取消 unless the action is starting.
	onClose := func() {
		if !busy.Get() && c.OnCancel != nil {
			c.OnCancel()
		}
	}
	children := []core.Node{
		title(c.Title),
		core.View{Style: core.Style{MaxWidth: core.Pt(380), FontSize: FontSubheading, LineHeightMultiple: 1.6, Color: ColorTextNavFooter}, Children: []core.Node{message}},
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Margin: core.Edges{Top: 10}}, Children: []core.Node{
			Button{Text: cancel, Variant: ButtonGhost, Large: true, Ref: cancelRef, Disabled: busy.Get, OnPress: c.OnCancel, TestID: "confirm-cancel"},
			Button{Text: confirm, Variant: variant, Large: true, Ref: confirmRef, Disabled: busy.Get, OnPress: c.OnConfirm, TestID: "confirm-ok"},
		}},
	}
	return layer{open: c.Open, onClose: onClose, label: c.Title, initial: initial, returnFocus: c.ReturnFocus, testID: c.TestID,
		popup: cardStyle(core.Symmetric(32, 40)), children: children}.render()
}

// DoneDialog is the card at the end of a run or a clean, as the 维护 design draws it: the badge (a check mark when Succeeded, else an exclamation mark), the title, the big figure, the lines (scrolling when they outgrow the window), Children (a permission prompt) and 完成 (CloseLabel), which has the focus.
type DoneDialog struct {
	Open        func() bool
	Title, Big  string
	Lines       []string
	Succeeded   bool
	Children    []core.Node
	CloseLabel  string
	OnClose     func()
	ReturnFocus *core.Ref
	TestID      string
}

// Render implements core.Node.
func (d DoneDialog) Render(*core.Scope) core.Node {
	label := d.CloseLabel
	if label == "" {
		label = "完成"
	}
	glyph := "exclaim"
	if d.Succeeded {
		glyph = "check"
	}
	closeRef := new(core.Ref)
	children := []core.Node{
		core.View{Style: core.Style{Width: core.Pt(88), Height: core.Pt(88), CornerRadius: 44, FlexShrink: 0, AlignItems: core.AlignCenter,
			JustifyContent: core.JustifyCenter, Gradients: []core.Gradient{GradientAccent()}, Shadows: []core.BoxShadow{shadow(16, 40, ShadowAccent)}},
			Children: []core.Node{Glyph{Name: glyph, Size: 44, StrokeWidth: 2.6}}},
		core.Text{Content: d.Title, Role: core.RoleHeader, Style: core.Style{Margin: core.Edges{Top: 6}, FontFamily: FontSans, FontSize: FontTitle,
			FontWeight: Bold, Color: ColorText}},
		core.Text{Content: d.Big, Style: core.Style{FontFamily: FontSans, FontSize: FontDone, FontWeight: Bold, LetterSpacingEm: LetterSpacingStat,
			LineHeightMultiple: 1.1, Color: ColorText}},
		core.Scroll{Style: core.Style{FlexShrink: 1, MinHeight: core.Pt(0), AlignSelf: core.AlignStretch},
			Children: []core.Node{core.Text{Content: strings.Join(d.Lines, "\n"), TestID: "done-lines", Style: core.Style{FontFamily: FontSans,
				FontSize: FontSubheading, LineHeightMultiple: 1.6, Color: ColorTextNavFooter, TextAlign: core.TextAlignCenter}}}},
	}
	children = append(children, d.Children...)
	children = append(children, core.View{Style: core.Style{Margin: core.Edges{Top: 10}, FlexShrink: 0},
		Children: []core.Node{Button{Text: label, Variant: ButtonLight, Ref: closeRef, OnPress: d.OnClose, TestID: "done-close"}}})
	return layer{open: d.Open, label: d.Title, description: d.Big + "\n" + strings.Join(d.Lines, "\n"), initial: closeRef,
		returnFocus: d.ReturnFocus, testID: d.TestID, popup: cardStyle(core.Symmetric(40, 52)), children: children}.render()
}
