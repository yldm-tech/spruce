// Package health is the 健康监测 page: the CPU alert (as on 维护), the alert settings, live overview cards with two-minute sparklines, and the processes using the most CPU and memory. It only reads: Spruce never ends processes. Sampling runs while the page is shown and stops when it goes.
package health

import (
	"runtime"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/shared"
	"spruce/apps/spruce/internal/ui/store"
)

// Page is the 健康监测 page.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	stores := store.Use(cx)
	s := NewState(cx, deps.Sysinfo)
	c := newCards(cx, s)
	// A desktop without a battery has no card; one whose battery could not be read says so.
	showBattery := core.NewMemo(cx, func() bool {
		l := s.Latest()
		return l != nil && (l.Battery == nil || l.Battery.Present)
	})

	grid := core.View{Style: core.Style{Display: core.DisplayGrid, GridTemplateColumns: "repeat(auto-fill, minmax(280px, 1fr))", Gap: 14},
		Children: []core.Node{
			c.CPU(), c.Memory(), c.Disk(), c.Network(),
			core.Show{When: showBattery.Get, Then: func(*core.Scope) core.Node { return c.Battery() }},
			c.System(),
		}}

	watchError := core.Show{When: s.Failed.Get, Then: func(*core.Scope) core.Node {
		return core.View{Role: core.RoleAlert, LiveRegion: core.LiveRegionPolite, TestID: "watch-error",
			Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 16},
			Children: []core.Node{
				kit.Text("无法读取系统状态", kit.FontBody, kit.Regular, kit.ColorWarning),
				kit.Button{Text: "重试", Variant: kit.ButtonGhost, TestID: "watch-retry", OnPress: s.Retry},
			}}
	}}

	return kit.Page{TestID: "health-page", Children: []core.Node{
		kit.PageHeader{Route: nav.Health},
		shared.AlertBanner{},
		alertSettings{settings: stores.Settings},
		watchError,
		grid,
		processSection{s: s, alerts: stores.Alerts},
	}}
}

// alertSettings states the current high-CPU alert settings, which 设置 changes, and links there.
type alertSettings struct {
	settings *store.Settings
}

// Render implements core.Node.
func (a alertSettings) Render(cx *core.Scope) core.Node {
	text := func() string {
		if !a.settings.Loaded() {
			return "正在读取提醒设置…"
		}
		h := a.settings.Value().Health
		if !h.Enabled {
			return "高 CPU 提醒已关闭"
		}
		return "高 CPU 提醒：单个进程持续 " + strconv.Itoa(h.SustainMinutes) + " 分钟占用超过 " + strconv.Itoa(h.CPUThreshold) + "% CPU 时提醒"
	}
	link := func() string {
		if a.settings.Loaded() && !a.settings.Value().Health.Enabled {
			return "去设置开启"
		}
		return "调整阈值"
	}
	return core.View{Role: core.RoleRegion, Label: "高 CPU 提醒设置", TestID: "alert-settings",
		Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween,
			RowGap: 8, ColumnGap: 16, Padding: core.Symmetric(12, 20), CornerRadius: kit.RadiusCard, Background: kit.SurfaceCardMuted,
			BorderWidth: 1, BorderColor: kit.BorderCard},
		Children: []core.Node{
			kit.TextFunc(text, kit.FontBody, kit.Regular, kit.ColorTextSecondary),
			core.View{Role: core.RoleLink, LabelFunc: link, TestID: "alert-settings-link", OnPress: func(core.PressEvent) { nav.Use(cx).GoSettings(nav.SettingsHealth) },
				Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorWarning, Cursor: core.CursorPointer,
					Hover:        &core.Style{Color: kit.ColorTextLinkHover, TextDecoration: core.TextDecorationUnderline},
					FocusVisible: &core.Style{OutlineWidth: 2, OutlineOffset: 2, OutlineColor: kit.ColorText}},
				Children: []core.Node{core.Text{ContentFunc: link}}},
		}}
}

// processSection is the page's process lists under their heading, with the button that opens the system's monitor and the note on what the figures count.
type processSection struct {
	s      *State
	alerts *store.Alerts
}

// Render implements core.Node.
func (p processSection) Render(cx *core.Scope) core.Node {
	monitor := shared.Monitor()
	openFailed := core.NewSignal(cx, false)
	head := core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 12},
		Children: []core.Node{
			core.Text{Content: "进程", Role: core.RoleHeader, Style: core.Style{FontFamily: kit.FontFace(kit.Semibold), FontWeight: kit.FaceWeight(kit.Semibold),
				FontSize: kit.FontCardTitle, Color: kit.ColorText}},
			kit.Button{Text: "在" + monitor.Name + "中打开", Variant: kit.ButtonGhost, TestID: "open-monitor", OnPress: func() {
				openFailed.Set(false)
				p.alerts.OpenSystemMonitor(func(err error) { openFailed.Set(err != nil) })
			}},
		}}
	grid := core.View{Style: core.Style{Display: core.DisplayGrid, GridTemplateColumns: "repeat(auto-fit, minmax(300px, 1fr))", Gap: 14},
		Children: []core.Node{
			processTable{Title: "CPU 占用最高", Sorted: byCPU, TestID: "top-cpu", Latest: p.s.Latest},
			processTable{Title: "内存占用最高", Sorted: byMemory, TestID: "top-memory", Latest: p.s.Latest},
		}}
	return core.View{Role: core.RoleRegion, Label: "进程", Style: core.Style{Gap: 12}, Children: []core.Node{
		head,
		kit.ErrorLine{TestID: "open-monitor-error", Text: func() string {
			if openFailed.Get() {
				return "无法打开" + monitor.Name
			}
			return ""
		}},
		grid,
		core.Text{Content: ProcessFootnote(runtime.GOOS), Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, LineHeightMultiple: 1.6,
			Color: kit.ColorTextMuted}},
	}}
}
