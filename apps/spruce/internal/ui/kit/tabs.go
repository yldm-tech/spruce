package kit

import (
	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
)

// Option is one choice of Tabs or Chips.
type Option[T comparable] struct {
	Value  T
	Label  string
	TestID string
}

// Tabs is the 空间透视 page's segmented tab list: the choices in a dark pill, the selected one lit. The arrow keys move between them while the list has the keyboard's focus (macOS).
type Tabs[T comparable] struct {
	Options  []Option[T]
	Value    func() T
	OnChange func(T)
	Label    string
}

// Render implements core.Node.
func (t Tabs[T]) Render(*core.Scope) core.Node {
	tabs := make([]core.Node, len(t.Options))
	for i, o := range t.Options {
		selected := func() bool { return t.Value != nil && t.Value() == o.Value }
		tabs[i] = base.Tab{Label: o.Label, TestID: o.TestID, Selected: selected,
			OnPress: func(core.PressEvent) {
				if t.OnChange != nil && !selected() {
					t.OnChange(o.Value)
				}
			},
			StyleFunc: func() core.Style {
				st := core.Style{MinHeight: core.Pt(32), Padding: core.Symmetric(0, 16), CornerRadius: 16, JustifyContent: core.JustifyCenter,
					FontFamily: FontSans, FontSize: FontBody, Color: ColorTextSecondary, Cursor: core.CursorPointer, FocusVisible: focusRing(2, 2),
					Selected: &core.Style{Background: SurfaceNavActive, Color: ColorText, FontFamily: FontFace(Semibold), FontWeight: FaceWeight(Semibold)}}
				if !selected() {
					st.Hover = HoverEntry()
				}
				return Pressable(st, 0.96)
			},
			Children: []core.Node{core.Text{Content: o.Label}}}
	}
	return base.Tabs{Label: t.Label, Children: tabs,
		Style: core.Style{AlignSelf: core.AlignStart, Direction: core.Row, Gap: 4, Padding: core.All(4), CornerRadius: RadiusPill, Background: SurfaceBanner}}
}

// Chips is the 操作历史 page's row of filter chips: pills with a border, the selected one amber.
type Chips[T comparable] struct {
	Options  []Option[T]
	Value    func() T
	OnChange func(T)
	Label    string
}

// Render implements core.Node.
func (c Chips[T]) Render(*core.Scope) core.Node {
	chips := make([]core.Node, len(c.Options))
	for i, o := range c.Options {
		selected := func() bool { return c.Value != nil && c.Value() == o.Value }
		chips[i] = core.View{Role: core.RoleButton, Label: o.Label, TestID: o.TestID, Pressed: selected,
			OnPress: func(core.PressEvent) {
				if c.OnChange != nil {
					c.OnChange(o.Value)
				}
			},
			StyleFunc: func() core.Style {
				st := core.Style{MinHeight: core.Pt(32), Padding: core.Symmetric(0, 14), BorderWidth: 1, BorderColor: BorderCard,
					CornerRadius: RadiusPill, Background: SurfaceCardMuted, JustifyContent: core.JustifyCenter, FontFamily: FontSans,
					FontSize: FontBody, Color: ColorTextSecondary, Cursor: core.CursorPointer, FocusVisible: focusRing(2, 2),
					Hover: &core.Style{Color: ColorText}}
				if selected() {
					st.BorderColor, st.Background, st.Color, st.FontFamily, st.FontWeight = core.Transparent, 0, RunButtonText, FontFace(Semibold), FaceWeight(Semibold)
					st.Gradients = []core.Gradient{GradientAccent()}
					st.Hover = &core.Style{Color: RunButtonText}
				}
				return Pressable(st, 0.96)
			},
			Children: []core.Node{core.Text{Content: o.Label}}}
	}
	return core.View{Role: core.RoleToolbar, Label: c.Label, Style: core.Style{Direction: core.Row, Wrap: core.Wrap, Gap: 8}, Children: chips}
}
