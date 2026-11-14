package uninstall

import (
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
)

// text is a run of the app's font at size and weight in color.
func text(s string, size core.Points, weight core.FontWeight, color core.Color) core.Node {
	return kit.Text(s, size, weight, color)
}

// note is a paragraph of the detail card in the muted color, or the warning color.
func note(content func() string, warning bool) core.Node {
	color := kit.ColorTextMuted
	if warning {
		color = kit.ColorWarning
	}
	return core.Text{ContentFunc: content, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: color}}
}

// banner is the amber box of a running app or a failed lookup, with its button.
func banner(message func() string, button core.Node, testID string) core.Node {
	return core.View{Role: core.RoleAlert, TestID: testID,
		Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 12, Padding: core.Symmetric(12, 16), CornerRadius: 16,
			Background: kit.SurfaceBanner, BorderWidth: 1, BorderColor: kit.BorderBanner},
		Children: []core.Node{
			core.Text{ContentFunc: message, Style: core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(160), FontFamily: kit.FontSans, FontSize: kit.FontBody,
				LineHeightMultiple: 1.5, Color: kit.ColorWarning}},
			button,
		}}
}

// detail is the right column: the picked app's facts and, on macOS, the files it would take along, with their checkboxes; a prompt to pick one while none is.
func detail(cx *core.Scope, s *State) core.Node {
	picked := core.NewMemo(cx, func() string {
		if a, ok := s.App(); ok {
			return a.ID
		}
		return ""
	})
	return kit.Card{Clip: true, TestID: "detail", Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)}, Children: []core.Node{
		core.Dynamic(func(cx *core.Scope) core.Node {
			if picked.Get() == "" {
				return core.Func(func(*core.Scope) core.Node {
					return kit.EmptyState{Title: "选择一个应用", Description: "在左侧列表中选择要卸载的应用，这里会列出它和它留下的文件。", Tone: kit.ToneBlue,
						Glyph: "app", TestID: "detail-empty"}
				})
			}
			return core.Func(func(cx *core.Scope) core.Node { return appDetail(cx, s) })
		}),
	}}
}

// appDetail is the picked app's part of the detail card.
func appDetail(cx *core.Scope, s *State) core.Node {
	windows := s.Windows()
	current := func() app.UninstallApp { a, _ := s.App(); return a }
	name := current().Name
	sub := func() string {
		a := current()
		var parts []string
		if a.Version != "" {
			parts = append(parts, "版本 "+a.Version)
		}
		identity := a.BundleID
		if windows {
			identity = a.Publisher
		}
		if identity != "" {
			parts = append(parts, identity)
		}
		return strings.Join(parts, " · ")
	}
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 14}, Children: []core.Node{
		appIcon(cx, func() string { return s.DetailsOf(current().ID).Icon }, 56),
		core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinWidth: core.Pt(0), Gap: 2}, Children: []core.Node{
			core.Text{ContentFunc: func() string { return current().Name }, Role: core.RoleHeader,
				Style: core.Style{FontFamily: kit.FontFace(kit.Bold), FontSize: 18, FontWeight: kit.FaceWeight(kit.Bold), Color: kit.ColorText}},
			core.Text{ContentFunc: sub, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, Color: kit.ColorTextMuted}},
		}},
		core.Show{When: func() bool { return current().AppStore }, Then: func(*core.Scope) core.Node { return badge("App Store", false) }},
		core.Show{When: s.Running, Then: func(*core.Scope) core.Node { return badge("运行中", true) }},
	}}

	fact := func(label string, value func() string) core.Node {
		return core.View{Style: core.Style{Gap: 2}, Children: []core.Node{
			text(label, kit.FontSmall, kit.Regular, kit.ColorTextMuted),
			kit.Tabular(core.Text{ContentFunc: value, Style: core.Style{FontFamily: kit.FontFace(kit.Semibold), FontSize: kit.FontCardTitle,
				FontWeight: kit.FaceWeight(kit.Semibold), Color: kit.ColorText}}),
		}}
	}
	usedLabel := "最近使用"
	if windows {
		usedLabel = "安装时间"
	}
	facts := []core.Node{
		fact("大小", func() string { return SizeText(s.DetailsOf(current().ID), s.Measuring.Get(), windows) }),
		fact(usedLabel, func() string {
			a := current()
			if u := UsageText(a, s.DetailsOf(a.ID), windows, time.Now()); u != "" {
				return u
			}
			return "未知"
		}),
		core.Show{When: func() bool { return current().Path != "" }, Then: func(*core.Scope) core.Node {
			return core.View{Style: core.Style{GridColumn: core.GridLines{Start: core.GridLine(1), End: core.GridLine(-1)}, Gap: 2}, Children: []core.Node{
				text("位置", kit.FontSmall, kit.Regular, kit.ColorTextMuted),
				core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 6}, Children: []core.Node{
					core.Text{ContentFunc: func() string { return format.TruncateMiddle(current().Path, 56) }, MaxLines: 1,
						Style: core.Style{FlexShrink: 1, MinWidth: core.Pt(0), FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorTextSecondary}},
					kit.IconButton{Glyph: "reveal", Label: "在" + fileManagerName(windows) + "中显示 " + name, TestID: "reveal-app",
						OnPress: func() { s.Reveal(current().Path) }},
				}},
			}}
		}},
	}
	children := []core.Node{
		head,
		core.View{Style: core.Style{Display: core.DisplayGrid, GridTemplateColumns: "repeat(2, minmax(0, 1fr))", RowGap: 10, ColumnGap: 16},
			Children: facts},
		kit.ErrorLine{Text: s.RevealErr.Get, TestID: "reveal-error"},
	}
	if windows {
		children = append(children, core.View{Style: core.Style{Gap: 10}, Children: []core.Node{
			kit.SectionTitle("卸载方式"),
			note(func() string {
				return "卸载时会打开「" + current().Name + "」自带的卸载程序，请按它的提示完成。它结束后，Spruce 会查找它在你的用户文件夹里留下的文件，由你决定是否移到回收站。"
			}, false),
		}})
	} else {
		children = append(children, macLeftovers(cx, s, current))
	}
	return core.Scroll{TestID: "detail-scroll", Label: name + " 的详情",
		Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Gap: 16, Padding: core.All(22)},
		Children: children}
}

// planMode is what the leftovers section shows.
type planMode uint8

const (
	planLoading planMode = iota
	planFailed
	planNone
	planList
)

// macLeftovers is the files the picked app would take along (macOS): the running app's prompt, the lookup's state, the list and the notes under it.
func macLeftovers(cx *core.Scope, s *State, current func() app.UninstallApp) core.Node {
	type key struct {
		mode planMode
		err  string
	}
	mode := core.NewMemo(cx, func() key {
		p := s.Plan.Get()
		switch {
		case s.PlanLoading.Get() && p == nil:
			return key{mode: planLoading}
		case s.PlanErr.Get() != "":
			return key{mode: planFailed, err: s.PlanErr.Get()}
		case p != nil && len(p.Leftovers) == 0:
			return key{mode: planNone}
		case p != nil:
			return key{mode: planList}
		}
		return key{mode: planLoading}
	})
	state := core.Dynamic(func(cx *core.Scope) core.Node {
		k := mode.Get()
		return core.Func(func(cx *core.Scope) core.Node {
			switch k.mode {
			case planLoading:
				return core.View{Busy: func() bool { return true }, Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12},
					Children: []core.Node{
						kit.ProgressRing{Size: 28, Label: "正在查找相关文件"},
						text("正在查找相关文件…", kit.FontBody, kit.Regular, kit.ColorTextSecondary),
					}}
			case planFailed:
				return banner(func() string { return k.err }, kit.Button{Text: "重试", Variant: kit.ButtonGhost, OnPress: s.LoadPlan, TestID: "plan-retry"}, "plan-error")
			case planNone:
				return note(func() string { return "没有找到它留下的文件，卸载时只会移除应用本身。" }, false)
			}
			return LeftoverList{
				Leftovers: func() []app.UninstallLeftover {
					if p := s.Plan.Get(); p != nil {
						return p.Leftovers
					}
					return nil
				},
				Checked:  s.Checked.Get,
				OnToggle: s.Toggle,
				OnSet:    func(set map[string]bool) { s.Checked.Set(set) },
				OnReveal: s.Reveal,
				Windows:  false,
			}
		})
	})
	plan := func() app.UninstallPlan {
		if p := s.Plan.Get(); p != nil {
			return *p
		}
		return app.UninstallPlan{}
	}
	return core.View{Style: core.Style{Gap: 10}, Children: []core.Node{
		core.Show{When: s.Running, Then: func(*core.Scope) core.Node {
			return banner(func() string { return "请先退出「" + current().Name + "」再卸载" },
				kit.Button{Text: "重新检查", Variant: kit.ButtonGhost, Disabled: s.PlanLoading.Get, OnPress: s.LoadPlan, TestID: "recheck-running"}, "running")
		}},
		kit.SectionTitle("相关文件"),
		state,
		core.Show{When: func() bool { return plan().SharedWith != "" }, Then: func(*core.Scope) core.Node {
			return note(func() string {
				return "另有一份「" + current().Name + "」（" + format.TruncateMiddle(plan().SharedWith, 48) + "）可能也在使用这些文件，所以默认都不勾选。"
			}, true)
		}},
		core.Show{When: func() bool { return plan().Denied > 0 }, Then: func(*core.Scope) core.Node {
			return note(func() string { return "部分位置没有权限读取，列出的文件和大小可能不完整。" }, false)
		}},
		core.Show{When: func() bool { return plan().Bundle != nil }, Then: func(*core.Scope) core.Node {
			return note(func() string {
				return "应用本身 " + format.BytesIn(s.BundleSize(), false) + "，会和勾选的文件一起移到废纸篓。"
			}, false)
		}},
	}}
}
