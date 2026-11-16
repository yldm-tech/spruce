package settings

import (
	"math"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/store"
)

// rowPadding is a row's inset inside its group: rows, folders and the add row line up on it.
var rowPadding = core.Symmetric(12, 18)

// row is one setting in a group: the label and its description on the left, the control on the right, and the lines saying a save failed (Error, in the warning color, announced at once) or was adjusted (Note, announced politely).
type row struct {
	Label, Description string
	// DescriptionFunc replaces Description as it changes.
	DescriptionFunc func() string
	// Below are lines under the description, such as a rule's status.
	Below       []core.Node
	Error, Note func() string
	Control     []core.Node
	TestID      string
}

// Render implements core.Node.
func (r row) Render(*core.Scope) core.Node {
	return core.View{TestID: r.TestID,
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 24, MinHeight: core.Pt(56), Padding: rowPadding},
		Children: []core.Node{
			rowText(r.Label, r.Description, r.DescriptionFunc, r.Below, r.Error, r.Note, r.TestID),
			core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 10, FlexShrink: 0}, Children: r.Control},
		}}
}

// rowText is a row's label over its description and the lines below it, then its error and note lines; it takes the room the control leaves.
func rowText(label, description string, descriptionFunc func() string, below []core.Node, err, note func() string, testID string) core.Node {
	text := []core.Node{kit.Text(label, kit.FontSubheading, kit.Medium, kit.ColorText)}
	if description != "" || descriptionFunc != nil {
		text = append(text, core.Text{Content: description, ContentFunc: descriptionFunc, Style: lineStyle(kit.ColorTextMuted)})
	}
	text = append(text, below...)
	text = append(text, kit.ErrorLine{Text: err, TestID: testID + "-error"}, noteLine{Text: note, TestID: testID + "-note"})
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 3}, Children: text}
}

// lineStyle is a description or note line under a row's label.
func lineStyle(c core.Color) core.Style {
	return core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, LineHeightMultiple: 1.4, Color: c}
}

// noteLine is a row's line in the secondary color that is there only while Text returns something, announced politely as it appears (.setting-note, role=status).
type noteLine struct {
	Text   func() string
	TestID string
}

// Render implements core.Node.
func (n noteLine) Render(*core.Scope) core.Node {
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
	// DescriptionFunc replaces Description as it changes.
	DescriptionFunc func() string
	// Below are lines under the description.
	Below []core.Node
	// OnTurnOn runs as the user turns the switch on, in the same action: asking for a permission the setting needs belongs there.
	OnTurnOn func()
	TestID   string
}

// Render implements core.Node.
func (c switchSetting) Render(cx *core.Scope) core.Node {
	s := newSetting(cx, store.Use(cx).Settings, c.Field, 0, equalBool, func(v bool) string { return "已调整为 " + strconv.FormatBool(v) })
	change := func(v bool) {
		s.change(v)
		if v && c.OnTurnOn != nil {
			c.OnTurnOn()
		}
	}
	return row{Label: c.Label, Description: c.Description, DescriptionFunc: c.DescriptionFunc, Below: c.Below, Error: s.err.Get, Note: s.note.Get, TestID: c.TestID,
		Control: []core.Node{kit.Switch{Label: c.Label, TestID: c.TestID + "-switch", Checked: s.value, OnChange: change}}}
}

// statusLine is a grey line under a row's description that follows text.
func statusLine(text func() string, testID string) core.Node {
	return core.Text{ContentFunc: text, TestID: testID, Style: lineStyle(kit.ColorTextMuted)}
}

// warningLine is a line in the warning color under a row's description, there only while when reports true.
func warningLine(text string, when func() bool, testID string) core.Node {
	return core.Show{When: when, Then: func(*core.Scope) core.Node {
		return core.Text{Content: text, TestID: testID, Style: lineStyle(kit.ColorWarning)}
	}}
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

// numberSetting is a whole number typed into a field. Text that is not a whole number in range shows the allowed range and is not saved, and drops the save a number typed before it was waiting for; a valid number is saved once the user stops typing, or when the field loses the focus.
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
				} else {
					s.drop()
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

// folderListSetting is a list of folders as rows of its group: its label and description, a row per folder with a remove button, and 添加文件夹…, which opens the system's folder picker. Each change is saved at once.
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
	header := core.View{TestID: c.TestID, Style: core.Style{Padding: rowPadding, MinHeight: core.Pt(56), JustifyContent: core.JustifyCenter},
		Children: []core.Node{rowText(c.Label, c.Description, nil, nil,
			func() string {
				if p := problem.Get(); p != "" {
					return p
				}
				return s.err.Get()
			},
			func() string {
				if full() {
					return "最多 " + strconv.Itoa(c.MaxPaths) + " 项"
				}
				return s.note.Get()
			}, c.TestID)}}
	list := core.View{Role: core.RoleGroup, Label: c.Label, TestID: c.TestID + "-list",
		Children: []core.Node{
			core.Show{When: func() bool { return len(s.value()) == 0 }, Then: func(*core.Scope) core.Node {
				return core.Group{line(), core.View{Style: core.Style{Padding: rowPadding},
					Children: []core.Node{kit.Text("还没有添加文件夹", kit.FontBody, kit.Regular, kit.ColorTextMuted)}}}
			}},
			core.Each(s.value, func(p string) string { return p }, func(_ *core.Scope, path func() string, _ func() int) core.Node {
				p := path()
				return folderRow(p, func() {
					problem.Set("")
					s.change(Without(core.Untracked(cx, s.value), p))
				})
			}),
		}}
	add := addRow{TestID: c.TestID + "-add", OnPress: choose,
		Text: func() string {
			if choosing.Get() {
				return "正在选择…"
			}
			return "添加文件夹…"
		},
		Disabled: func() bool { return full() || choosing.Get() }}
	return core.Group{header, list, add}
}

// folderRow is one folder of a list: a folder glyph, the folder's name over its whole path (cut at the end), and the button that removes it.
func folderRow(path string, remove func()) core.Node {
	name := format.BaseName(path)
	if name == "" {
		name = path
	}
	oneLine := func(s string, size core.Points, weight core.FontWeight, c core.Color) core.Text {
		t := kit.Text(s, size, weight, c)
		t.Style.NumberOfLines, t.Style.TextOverflow = 1, core.EllipsizeTail
		return t
	}
	return core.Group{line(), core.View{
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, MinHeight: core.Pt(52), Padding: core.Edges{Top: 8, Right: 12, Bottom: 8, Left: 18},
			Hover: &core.Style{Background: kit.SurfaceRowHover}},
		Children: []core.Node{
			kit.Glyph{Name: "folder", Size: 18, Color: kit.ColorTextSecondary},
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2}, Children: []core.Node{
				oneLine(name, kit.FontBody, kit.Medium, kit.ColorText),
				oneLine(path, kit.FontSmall, kit.Regular, kit.ColorTextMuted),
			}},
			kit.IconButton{Glyph: "close", Label: "移除 " + path, OnPress: remove},
		}}}
}

// addRow is the last row of a folder list: a plus and 添加文件夹…, which opens the system's folder picker.
type addRow struct {
	Text     func() string
	Disabled func() bool
	OnPress  func()
	TestID   string
}

// Render implements core.Node.
func (b addRow) Render(*core.Scope) core.Node {
	disabled := func() bool { return b.Disabled != nil && b.Disabled() }
	return core.Group{line(), core.View{Role: core.RoleButton, TestID: b.TestID, Disabled: disabled, LabelFunc: b.Text,
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, MinHeight: core.Pt(46), Padding: rowPadding,
			Color: kit.ColorAccent, FontFamily: kit.FontFace(kit.Medium), FontSize: kit.FontBody,
			FontWeight: kit.FaceWeight(kit.Medium), Cursor: core.CursorPointer,
			Hover:        &core.Style{Background: kit.SurfaceRowHover, Color: kit.ColorAccentBright},
			Disabled:     &core.Style{Opacity: core.Alpha(0.45), Cursor: core.CursorDefault, Color: kit.ColorAccent},
			FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: -2, OutlineColor: kit.ColorText}},
		OnPress: func(core.PressEvent) {
			if b.OnPress != nil && !disabled() {
				b.OnPress()
			}
		},
		Children: []core.Node{kit.Glyph{Name: "plus", Size: 18, Color: core.CurrentColor}, core.Text{ContentFunc: b.Text}}}}
}
