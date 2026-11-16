package kit

import (
	"strings"
	"time"

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

// layer is what every dialog and overlay shares, on base.Dialog: a modal layer over the window whose scrim (Scrim over a 16-point backdrop blur) covers only the main area, the sidebar staying clear but out of reach, the popup centered in the main area. Escape calls onClose, when set, and does nothing else: base.Dialog never closes the layer itself, since a layer it closed stays hidden while open still holds (its userClosed), so only open, the app's own state, takes it away; the scrim never closes it. As it opens it takes the keyboard from a text field of the page, which would otherwise keep it on macOS and with it Return, Escape and the typing; initial then takes the keyboard's focus, and returnFocus takes it as the layer goes. onReturn, when set, is what Return does on macOS while the layer is open (see returnKey); without it Return does nothing there, rather than reach a layer under this one. sidebarTip, when set, is what the sidebar says under the pointer while the layer keeps it out of reach (an overlay's work).
type layer struct {
	open        func() bool
	onClose     func()
	onReturn    func()
	sidebarTip  string
	label       string
	role        core.Role
	description string
	initial     *core.Ref
	returnFocus *core.Ref
	popup       core.Style
	children    []core.Node
	testID      string
}

// layerFade is how long a layer's scrim and popup take to come in; the popup also grows from 96%. They go at once: a base.Dialog EndingStyle holds the layer until OnTransitionEnd, which nsgotest's steps do not wait for (https://github.com/nsgo-dev/nsgo/issues/335), so every test that closes a dialog would need to move the clock.
const layerFade = 200 * time.Millisecond

var popupHidden = core.Style{Opacity: core.Alpha(0), Transform: core.Transform{Scale: 0.96}}

func (l layer) render(cx *core.Scope) core.Node {
	fill := core.Style{Position: core.Absolute, Top: core.Pt(0), Bottom: core.Pt(0)}
	scrim := fill
	scrim.Left, scrim.Right = core.Pt(SidebarWidth), core.Pt(0)
	scrim.Background, scrim.BackdropFilters = Scrim, []core.Filter{core.Blur(BlurOverlay)}
	// The scrim fades in after its first frame, as the popup does.
	scrimView := core.Func(func(cx *core.Scope) core.Node {
		entered := core.NewSignal(cx, cx.Env().ReduceMotion())
		if !entered.GetUntracked() {
			enter := func() { entered.Set(true) }
			cx.RequestAnimationFrame(func(time.Time) { enter() })
			cx.AfterFunc(20*time.Millisecond, enter)
		}
		return core.View{OnPress: func(core.PressEvent) {}, NoFocus: true, StyleFunc: func() core.Style {
			st := scrim
			if !cx.Env().ReduceMotion() {
				st.Transition = core.Transition{Duration: layerFade, Easing: core.Ease, Properties: core.TransitionOpacity}
			}
			if !entered.Get() {
				st.Opacity = core.Alpha(0)
			}
			return st
		}}
	})
	popup := l.popup
	popup.Transition = core.Transition{Duration: layerFade, Easing: core.Ease, Properties: core.TransitionOpacity | core.TransitionTransform}
	sidebar := fill
	sidebar.Left, sidebar.Width = core.Pt(0), core.Pt(SidebarWidth)
	absorb := func(core.PressEvent) {}
	var tip core.Node
	if l.sidebarTip != "" {
		// Beside the sidebar: a view's own tooltip goes above it, out of the window for one this tall.
		tip = base.Tooltip{Placement: core.PlaceRight, Children: []core.Node{Tooltip(l.sidebarTip)}}
	}
	hold := core.Func(func(cx *core.Scope) core.Node {
		if b := BlockerKey.Use(cx); b != nil {
			b.Hold(cx)
		}
		return core.Group{}
	})
	// The newest binding of a key wins, so the top layer takes Return even when it has nothing to do with it: a question opened over a dialog (退出 during a run over a page's confirmation) must not let Return confirm the dialog under it.
	onReturn := l.onReturn
	if onReturn == nil {
		onReturn = func() {}
	}
	children := []core.Node{hold, returnKey(onReturn)}
	return base.Dialog{
		Open:                   l.open,
		Animation:              core.ModalNone,
		NoCloseOnEscape:        true,
		NoCloseOnBackdropPress: true,
		// base.Dialog reports Escape here before it would close; NoCloseOnEscape keeps it open, so onClose decides.
		OnCancel: l.onClose,
		OnOpen: func() {
			// On macOS Ref.Focus cannot move the keyboard to a button (https://github.com/nsgo-dev/nsgo/issues/348: ItemFocus only focuses text fields on macOS), so a search field the user typed in would keep it under the layer.
			cx.DismissKeyboard()
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
			core.View{Style: sidebar, OnPress: absorb, NoFocus: true, Tooltip: tip, TestID: "sidebar-absorber"},
			scrimView,
		},
		PopupStyle:               popup,
		StartingStyle:            popupHidden,
		Label:                    l.label,
		Role:                     l.role,
		AccessibilityDescription: l.description,
		TestID:                   l.testID,
		Children:                 append(children, l.children...),
	}
}

// returnKey binds Return to fn while it is mounted, on macOS only. There Ref.Focus cannot put the keyboard's focus on a button (https://github.com/nsgo-dev/nsgo/issues/348: ItemFocus only focuses text fields on macOS), so the button a dialog focuses as it opens never has it and Return would do nothing; a focused button the user tabbed to still takes Return itself, since macOS leaves Return to a focused pressable before the keymap. Windows focuses the button and gives the keymap Return first, even over a focused button, so binding it there would press the default when the user tabbed to 取消.
func returnKey(fn func()) core.Node {
	return core.Func(func(cx *core.Scope) core.Node {
		if cx.Env().OS() == core.OSMacOS {
			cx.OnKey("enter", fn)
		}
		return core.Group{}
	})
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

// Dialog is the app's modal card (GlassDialog): Title, Children and a row of Actions over the blurred main area. OnClose, when set, is what Escape does; without it Escape does nothing. Return presses the action marked Initial unless it is disabled or busy, except in a Danger dialog, where Return does nothing so a destructive action never fires from it; Danger dialogs still focus the action marked Initial, as any other. ReturnFocus takes the focus back as it closes.
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
func (d Dialog) Render(cx *core.Scope) core.Node {
	var initial *core.Ref
	var onReturn func()
	children := []core.Node{title(d.Title)}
	children = append(children, d.Children...)
	if len(d.Actions) > 0 {
		row := make([]core.Node, len(d.Actions))
		for i, a := range d.Actions {
			ref := new(core.Ref)
			if a.Initial {
				initial = ref
				if !d.Danger && a.OnPress != nil {
					onReturn = func() {
						if !is(a.Disabled) && !is(a.Busy) {
							a.OnPress()
						}
					}
				}
			}
			row[i] = Button{Text: a.Text, Variant: a.Variant, OnPress: a.OnPress, Busy: a.Busy, Disabled: a.Disabled, Ref: ref, Large: true, TestID: a.TestID}
		}
		children = append(children, core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Margin: core.Edges{Top: 10}}, Children: row})
	}
	return layer{open: d.Open, onClose: d.OnClose, onReturn: onReturn, label: d.Title, initial: initial, returnFocus: d.ReturnFocus, testID: d.TestID,
		popup: cardStyle(core.Symmetric(32, 40)), children: children}.render(cx)
}

// ConfirmDialog asks before an action: a title, the message (Lines, one per line, or Message) and 取消 and the confirmation. Return is the confirmation, except that Danger draws it red, focuses 取消 first and has Return do nothing, for what cannot be undone. Busy disables both buttons, Escape and Return while the action starts; Escape is 取消 otherwise.
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
	var onReturn func()
	if !c.Danger && c.OnConfirm != nil {
		onReturn = func() {
			if !busy.Get() {
				c.OnConfirm()
			}
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
	return layer{open: c.Open, onClose: onClose, onReturn: onReturn, label: c.Title, initial: initial, returnFocus: c.ReturnFocus, testID: c.TestID,
		popup: cardStyle(core.Symmetric(32, 40)), children: children}.render(cx)
}

// DoneDialog is the card at the end of a run or a clean, as the 维护 design draws it: the badge (a check mark when Succeeded, else an exclamation mark), which pops in as the card comes, the title, the big figure, the lines when there are any (scrolling when they outgrow the window), Children (a permission prompt) and 完成 (CloseLabel), which has the focus and which Return presses.
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
func (d DoneDialog) Render(cx *core.Scope) core.Node {
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
		Appear{Entrance: EnterPop, Delay: 80 * time.Millisecond, Style: core.Style{Width: core.Pt(88), Height: core.Pt(88), CornerRadius: 44, FlexShrink: 0,
			AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gradients: []core.Gradient{GradientAccent()}, Shadows: []core.BoxShadow{shadow(16, 40, ShadowAccent)}},
			Children: []core.Node{Glyph{Name: glyph, Size: 44, StrokeWidth: 2.6}}},
		core.Text{Content: d.Title, Role: core.RoleHeader, Style: core.Style{Margin: core.Edges{Top: 6}, FontFamily: FontFace(Bold), FontSize: FontTitle,
			FontWeight: FaceWeight(Bold), Color: ColorText}},
		core.Text{Content: d.Big, Style: core.Style{FontFamily: FontFace(Bold), FontSize: FontDone, FontWeight: FaceWeight(Bold), LetterSpacingEm: LetterSpacingStat,
			LineHeightMultiple: 1.1, Color: ColorText}},
	}
	// A card whose Children say it all (维护's grid of tasks) has no lines, and no empty line where they would be.
	if len(d.Lines) > 0 {
		children = append(children, core.Scroll{Style: core.Style{FlexShrink: 1, MinHeight: core.Pt(0), AlignSelf: core.AlignStretch},
			Children: []core.Node{core.Text{Content: strings.Join(d.Lines, "\n"), TestID: "done-lines", Style: core.Style{FontFamily: FontSans,
				FontSize: FontSubheading, LineHeightMultiple: 1.6, Color: ColorTextNavFooter, TextAlign: core.TextAlignCenter}}}})
	}
	children = append(children, d.Children...)
	children = append(children, core.View{Style: core.Style{Margin: core.Edges{Top: 10}, FlexShrink: 0},
		Children: []core.Node{Button{Text: label, Variant: ButtonLight, Ref: closeRef, OnPress: d.OnClose, TestID: "done-close"}}})
	return layer{open: d.Open, onReturn: d.OnClose, label: d.Title, description: d.Big + "\n" + strings.Join(d.Lines, "\n"), initial: closeRef,
		returnFocus: d.ReturnFocus, testID: d.TestID, popup: cardStyle(core.Symmetric(40, 52)), children: children}.render(cx)
}
