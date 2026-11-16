package kit

import (
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// ToastAction is one action of a Toast, drawn as a link button: 撤销, 查看.
type ToastAction struct {
	Text    string
	TestID  string
	OnPress func()
	// Busy, when set, disables the action and tells assistive technology it works (撤销 while it puts the items back).
	Busy func() bool
}

// Toast is the short message at the bottom of the main area after an action, such as 「已移到废纸篓 3.1 GB · 撤销」: a pill with the text, its actions and a close button. It is a pure view: whoever mounts it decides what it says and where it sits; it calls OnClose when Duration has run out, counting only while the pointer is not over it and the keyboard focus not in it, and when Escape is pressed while the focus is in it. Mount a new one (a new scope) for every message, so the time starts again. Screen readers hear the text as it appears (polite); NSGo has no status role, so it is a group with a live text.
type Toast struct {
	Text     func() string
	Actions  []ToastAction
	OnClose  func()
	Duration time.Duration
	// Leaving, once it says so, plays the toast's exit (it sinks as it fades), then calls OnGone, so whoever mounts it can take it away; the toast takes no presses meanwhile. Without it the toast goes when it is unmounted. It comes in rising from the bottom edge either way.
	Leaving func() bool
	OnGone  func()
}

// toastBase is the toast's opaque ground: unlike a dialog it has no scrim behind it, so the dialogs' translucent surface alone would let the page show through its text.
var toastBase = core.RGBA(0x2a1f33, 0.97)

// toastSheen is SurfaceDialog over the ground, the dialogs' tint.
func toastSheen() core.Gradient {
	return core.LinearGradient(180, SurfaceDialog, SurfaceDialog)
}

// Render implements core.Node.
func (t Toast) Render(cx *core.Scope) core.Node {
	enter := EnterSlide.spec(0)
	motion := core.NewMotion(cx, &enter)
	leaving := func() bool { return t.Leaving != nil && t.Leaving() }
	if t.Leaving != nil {
		gone := false
		cx.Effect(func() {
			if !leaving() {
				return
			}
			cx.Untrack(func() {
				motion.Play(leaveSpec(), func(bool) {
					if !gone && t.OnGone != nil {
						gone = true
						t.OnGone()
					}
				})
			})
		})
	}
	hovered := false
	focused := core.NewSignal(cx, false)
	left := t.Duration
	var deadline time.Time
	var stop func() bool
	start := func() {
		if stop != nil || left <= 0 || t.OnClose == nil {
			return
		}
		deadline = cx.Now().Add(left)
		stop = cx.AfterFunc(left, t.OnClose)
	}
	pause := func() {
		if stop == nil {
			return
		}
		stop()
		stop = nil
		left = max(0, deadline.Sub(cx.Now()))
	}
	sync := func() {
		if hovered || focused.GetUntracked() {
			pause()
		} else {
			start()
		}
	}
	start()
	// Escape is bound only while the focus is in the toast, so it never takes the key from a page or a dialog.
	escape := core.Show{When: focused.Get, Then: func(cx *core.Scope) core.Node {
		if t.OnClose != nil {
			cx.OnKey("escape", t.OnClose)
		}
		return core.Group{}
	}}

	children := []core.Node{core.Text{ContentFunc: t.Text, TestID: "toast-text", LiveRegion: core.LiveRegionPolite, Role: core.RoleText,
		Style: core.Style{FlexShrink: 1, FontFamily: FontSans, FontSize: FontBody, Color: ColorText, LineHeightMultiple: 1.3}}}
	for _, a := range t.Actions {
		children = append(children, Button{Variant: ButtonLink, Text: a.Text, TestID: a.TestID, OnPress: a.OnPress, Busy: a.Busy})
	}
	children = append(children, IconButton{Glyph: "close", Label: "关闭", TestID: "toast-close", OnPress: t.OnClose}, escape)
	// A leaving toast takes no more presses.
	children = append(children, core.Show{When: leaving, Then: func(*core.Scope) core.Node {
		return core.View{OnPress: func(core.PressEvent) {}, NoFocus: true,
			Style: core.Style{Position: core.Absolute, Top: core.Pt(0), Right: core.Pt(0), Bottom: core.Pt(0), Left: core.Pt(0)}}
	}})
	return core.View{Role: core.RoleGroup, Label: "提示", TestID: "toast",
		OnMouseEnter: func(core.MouseEvent) { hovered = true; sync() },
		OnMouseLeave: func(core.MouseEvent) { hovered = false; sync() },
		OnFocusIn:    func() { focused.Set(true); sync() },
		OnFocusOut:   func() { focused.Set(false); sync() },
		Style: core.Style{Position: core.Relative, Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, MinHeight: core.Pt(44), MaxWidth: core.Pt(520),
			Padding: core.Edges{Left: 18, Right: 8}, CornerRadius: RadiusPill, Background: toastBase, Gradients: []core.Gradient{toastSheen()},
			BorderWidth: 1, BorderColor: BorderDialog, Shadows: []core.BoxShadow{shadow(12, 32, ShadowDialog)}, Animated: motion.Style()},
		Children: children}
}
