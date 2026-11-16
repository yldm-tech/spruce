package kit

import "github.com/nsgo-dev/nsgo/core"

// Field is a text field: with Suffix, the 设置 page's 96-point number field with its unit after it; otherwise the 34-point round search field (with Glyph before its text) that takes the width it gets. Invalid draws its border in the warning color and tells assistive technology.
type Field struct {
	Value       func() string
	OnChange    func(string)
	OnBlur      func()
	OnSubmit    func(string)
	Placeholder string
	Suffix      string
	Label       string
	Invalid     func() bool
	Glyph       string
	Ref         *core.Ref
	TestID      string
}

// Render implements core.Node.
func (f Field) Render(*core.Scope) core.Node {
	if f.Suffix != "" {
		input := core.Input{Value: f.Value, OnChange: f.OnChange, OnBlur: f.OnBlur, OnSubmit: f.OnSubmit, Placeholder: f.Placeholder,
			Label: f.Label, Invalid: f.Invalid, Ref: f.Ref, TestID: f.TestID, KeyboardType: core.KeyboardNumber,
			StyleFunc: func() core.Style {
				border := BorderCard
				if is(f.Invalid) {
					border = ColorWarning
				}
				return core.Style{Width: core.Pt(96), MinHeight: core.Pt(34), Padding: core.Symmetric(0, 12), BorderWidth: 1, BorderColor: border,
					CornerRadius: 10, Background: SurfaceList, Color: ColorText, FontFamily: FontSans, FontSize: FontSubheading,
					FontVariant: core.FontTabularNums, TextAlign: core.TextAlignEnd, PlaceholderColor: InputPlaceholder,
					FocusVisible: focusRing(2, 1)}
			}}
		return core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8},
			Children: []core.Node{input, core.Text{Content: f.Suffix, Style: core.Style{MinWidth: core.Pt(24), FontFamily: FontSans,
				FontSize: FontBody, Color: ColorTextSecondary}}}}
	}
	input := core.Input{Value: f.Value, OnChange: f.OnChange, OnBlur: f.OnBlur, OnSubmit: f.OnSubmit, Placeholder: f.Placeholder,
		Label: f.Label, Invalid: f.Invalid, Ref: f.Ref, TestID: f.TestID,
		Style: core.Style{FlexGrow: 1, MinWidth: core.Pt(0), Height: core.Pt(34), Color: ColorText, FontFamily: FontSans, FontSize: FontBody,
			PlaceholderColor: InputPlaceholder}}
	children := []core.Node{input}
	if f.Glyph != "" {
		children = append([]core.Node{Glyph{Name: f.Glyph, Size: 14, Color: InputPlaceholder}}, children...)
	}
	return core.View{
		StyleFunc: func() core.Style {
			border := InputBorder
			if is(f.Invalid) {
				border = ColorWarning
			}
			return core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 8, FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(140),
				MinWidth: core.Pt(0), Height: core.Pt(34), Padding: core.Symmetric(0, 14), CornerRadius: 17, BorderWidth: 1, BorderColor: border,
				Background: InputBackground, FocusWithin: focusRing(2, 1)}
		},
		Children: children}
}
