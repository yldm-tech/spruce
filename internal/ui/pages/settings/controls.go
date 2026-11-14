package settings

import (
	"math"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// row is one setting in a section card (.setting-row): the label and its description on the left, the control on the right (or below, Stacked), and the lines saying a save failed (Error, in the warning color, announced at once) or was adjusted (Note, announced politely).
type row struct {
	Label, Description string
	// DescriptionFunc replaces Description as it changes.
	DescriptionFunc func() string
	Error, Note     func() string
	Stacked         bool
	Control         []core.Node
	TestID          string
}

// Render implements core.Node.
func (r row) Render(*core.Scope) core.Node {
	text := []core.Node{kit.Text(r.Label, kit.FontSubheading, kit.Semibold, kit.ColorText)}
	if r.Description != "" || r.DescriptionFunc != nil {
		text = append(text, core.Text{Content: r.Description, ContentFunc: r.DescriptionFunc, Style: lineStyle(kit.ColorTextMuted)})
	}
	text = append(text, kit.ErrorLine{Text: r.Error, TestID: r.TestID + "-error"}, note{Text: r.Note, TestID: r.TestID + "-note"})
	st := core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween,
		RowGap: 10, ColumnGap: 24, Padding: core.Symmetric(14, 0)}
	textStyle := core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(280), MinWidth: core.Pt(0), Gap: 3}
	control := core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 10}
	if r.Stacked {
		st.Direction, st.AlignItems, st.Wrap = core.Column, core.AlignStretch, core.NoWrap
		textStyle.FlexBasis = core.Length{}
		control.Direction, control.AlignItems = core.Column, core.AlignStart
	}
	return core.View{TestID: r.TestID, Style: st, Children: []core.Node{
		core.View{Style: textStyle, Children: text},
		core.View{Style: control, Children: r.Control},
	}}
}

// lineStyle is a description or note line (.setting-description, .setting-note).
func lineStyle(c core.Color) core.Style {
	return core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: c}
}

// note is a row's line in the secondary color that is there only while Text returns something, announced politely as it appears (.setting-note, role=status).
type note struct {
	Text   func() string
	TestID string
}

// Render implements core.Node.
func (n note) Render(*core.Scope) core.Node {
	return core.Show{When: func() bool { return n.Text != nil && n.Text() != "" }, Then: func(*core.Scope) core.Node {
		return core.Text{ContentFunc: n.Text, LiveRegion: core.LiveRegionPolite, TestID: n.TestID, Style: lineStyle(kit.ColorTextSecondary)}
	}}
}

func equalInt(a, b int) bool   { return a == b }
func equalBool(a, b bool) bool { return a == b }

// switchSetting is an on/off setting, saved as soon as it is flipped.
type switchSetting struct {
	Field              Field[bool]
	Label, Description string
	TestID             string
}

// Render implements core.Node.
func (c switchSetting) Render(cx *core.Scope) core.Node {
	s := newSetting(cx, store.Use(cx).Settings, c.Field, 0, equalBool, func(v bool) string { return "已调整为 " + strconv.FormatBool(v) })
	return row{Label: c.Label, Description: c.Description, Error: s.err.Get, Note: s.note.Get, TestID: c.TestID,
		Control: []core.Node{kit.Switch{Label: c.Label, TestID: c.TestID + "-switch", Checked: s.value, OnChange: s.change}}}
}

// sliderSetting is a number on a slider over its allowed range, saved once the user stops moving it.
type sliderSetting struct {
	Field              Field[int]
	Label, Description string
	Range              app.SettingRange
	Step               int
	// Format is the value as shown and read out, e.g. "40%" or "10 分钟".
	Format func(int) string
	TestID string
}

// Render implements core.Node.
func (c sliderSetting) Render(cx *core.Scope) core.Node {
	s := newSetting(cx, store.Use(cx).Settings, c.Field, saveDebounce, equalInt, func(v int) string { return "已调整为 " + c.Format(v) })
	return row{Label: c.Label, Description: c.Description, Error: s.err.Get, Note: s.note.Get, TestID: c.TestID,
		Control: []core.Node{kit.Slider{Label: c.Label, TestID: c.TestID + "-slider",
			Min: float64(c.Range.Min), Max: float64(c.Range.Max), Step: float64(c.Step),
			Value:     func() float64 { return float64(s.value()) },
			OnChange:  func(v float64) { s.change(int(math.Round(v))) },
			ValueText: func(v float64) string { return c.Format(int(math.Round(v))) }}}}
}

// numberSetting is a whole number typed into a field. Text that is not a whole number in range shows the allowed range and is not saved; a valid number is saved once the user stops typing, or when the field loses the focus.
type numberSetting struct {
	Field              Field[int]
	Label, Description string
	Range              app.SettingRange
	// Unit is what follows the field, e.g. "天" or "MB".
	Unit   string
	TestID string
}

// Render implements core.Node.
func (c numberSetting) Render(cx *core.Scope) core.Node {
	s := newSetting(cx, store.Use(cx).Settings, c.Field, saveDebounce, equalInt, func(v int) string { return "已调整为 " + strconv.Itoa(v) + " " + c.Unit })
	// text is what is being edited; nil shows the setting's value.
	text := core.NewSignal[*string](cx, nil)
	invalid := func() bool {
		t := text.Get()
		if t == nil {
			return false
		}
		n, ok := ParseWhole(*t)
		return !ok || !InRange(n, c.Range)
	}
	return row{Label: c.Label, Description: c.Description, TestID: c.TestID,
		Error: func() string {
			if invalid() {
				return RangeText(c.Range)
			}
			return s.err.Get()
		},
		Note: func() string {
			if invalid() {
				return ""
			}
			return s.note.Get()
		},
		Control: []core.Node{kit.Field{Label: c.Label, Suffix: c.Unit, TestID: c.TestID + "-field", Invalid: invalid,
			Value: func() string {
				if t := text.Get(); t != nil {
					return *t
				}
				return strconv.Itoa(s.value())
			},
			OnChange: func(raw string) {
				text.Set(&raw)
				if n, ok := ParseWhole(raw); ok && InRange(n, c.Range) {
					s.change(n)
				}
			},
			OnBlur: func() {
				if core.Untracked(cx, invalid) {
					return
				}
				s.flush()
				text.Set(nil)
			}}}}
}

// folderListSetting is a list of folders with a remove button each and 添加…, which opens the system's folder picker. Each change is saved at once.
type folderListSetting struct {
	Field              Field[[]string]
	Label, Description string
	// MaxPaths is how many entries the list keeps.
	MaxPaths int
	TestID   string
}

// Render implements core.Node.
func (c folderListSetting) Render(cx *core.Scope) core.Node {
	s := newSetting(cx, store.Use(cx).Settings, c.Field, 0, SameList, func([]string) string { return "有重复或不完整的路径，已从列表中去掉" })
	problem := core.NewSignal(cx, "")
	choosing := core.NewSignal(cx, false)
	full := func() bool { return len(s.value()) >= c.MaxPaths }
	choose := func() {
		problem.Set("")
		choosing.Set(true)
		cx.PickDirectories(core.PickDirectoriesOptions{}, func(dirs []core.Directory, err error) {
			choosing.Set(false)
			switch {
			case err != nil:
				problem.Set("无法打开选择文件夹的窗口")
			case len(dirs) == 0 || dirs[0].Path == "":
				// Cancelled.
			default:
				list := core.Untracked(cx, s.value)
				if ContainsFold(list, dirs[0].Path) {
					problem.Set("这个文件夹已经在列表中了")
					return
				}
				s.change(With(list, dirs[0].Path))
			}
		})
	}
	list := core.View{Role: core.RoleGroup, Label: c.Label, TestID: c.TestID + "-list",
		Style: core.Style{AlignSelf: core.AlignStretch, Width: core.Pct(100), Padding: core.Symmetric(4, 0), CornerRadius: 12, Background: kit.SurfaceList,
			BorderWidth: 1, BorderColor: kit.BorderSubtle},
		Children: []core.Node{
			core.Show{When: func() bool { return len(s.value()) == 0 }, Then: func(*core.Scope) core.Node {
				return core.Text{Content: "还没有添加文件夹", Style: core.Style{Padding: core.Symmetric(8, 14), FontFamily: kit.FontSans,
					FontSize: kit.FontBody, Color: kit.ColorTextMuted}}
			}},
			core.Each(s.value, func(p string) string { return p }, func(_ *core.Scope, path func() string, _ func() int) core.Node {
				p := path()
				return folderRow(p, func() {
					problem.Set("")
					s.change(Without(core.Untracked(cx, s.value), p))
				})
			}),
		}}
	return row{Label: c.Label, Description: c.Description, Stacked: true, TestID: c.TestID,
		Error: func() string {
			if p := problem.Get(); p != "" {
				return p
			}
			return s.err.Get()
		},
		Note: func() string {
			if full() {
				return "最多 " + strconv.Itoa(c.MaxPaths) + " 项"
			}
			return s.note.Get()
		},
		Control: []core.Node{list, smallButton{TestID: c.TestID + "-add", OnPress: choose,
			Text: func() string {
				if choosing.Get() {
					return "正在选择…"
				}
				return "添加…"
			},
			Disabled: func() bool { return full() || choosing.Get() }}}}
}

// folderRow is one folder of a list (.folder-row): its path on one line, cut at the end, and the button that removes it.
func folderRow(path string, remove func()) core.Node {
	return core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, MinHeight: core.Pt(38), Padding: core.Edges{Right: 6, Left: 14},
			Hover: &core.Style{Background: kit.SurfaceRowHover}},
		Children: []core.Node{
			core.Text{Content: path, Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), NumberOfLines: 1,
				TextOverflow: core.EllipsizeTail, FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorText}},
			kit.IconButton{Glyph: "close", Label: "移除 " + path, OnPress: remove},
		}}
}

// smallButton is the 32-point translucent pill under a folder list (.ghost-button.folder-add), a smaller kit.ButtonGhost.
type smallButton struct {
	Text     func() string
	Disabled func() bool
	OnPress  func()
	TestID   string
}

// Render implements core.Node.
func (b smallButton) Render(*core.Scope) core.Node {
	disabled := func() bool { return b.Disabled != nil && b.Disabled() }
	return core.View{Role: core.RoleButton, TestID: b.TestID, Disabled: disabled,
		Style: core.Style{MinHeight: core.Pt(32), Padding: core.Symmetric(0, 14), CornerRadius: 16, Direction: core.Row,
			AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Background: kit.SurfaceButtonGhost, Color: kit.ColorText,
			FontFamily: kit.FontSans, FontSize: kit.FontBody, Cursor: core.CursorPointer, FlexShrink: 0,
			Hover:        &core.Style{Color: kit.ColorTextLinkHover},
			Disabled:     &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault, Color: kit.ColorText},
			FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText}},
		OnPress: func(core.PressEvent) {
			if b.OnPress != nil && !disabled() {
				b.OnPress()
			}
		},
		Children: []core.Node{core.Text{ContentFunc: b.Text}}}
}
