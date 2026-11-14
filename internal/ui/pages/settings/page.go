// Package settings is the 设置 page: a tab per area (通用, 自动清理, 清理, 健康监测, 历史记录, 关于), each a column of grouped rows with the label and its description on the left and the control on the right. Every control saves on its own as it changes; there is no Save button.
package settings

import (
	"context"
	"runtime"
	"strconv"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/rules"
	"github.com/yldm-tech/spruce/internal/ui/async"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/shared"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// tabs are the page's tabs, in order.
var tabs = []kit.Option[nav.SettingsTab]{
	{Value: nav.SettingsGeneral, Label: "通用", TestID: "tab-general"},
	{Value: nav.SettingsAuto, Label: "自动清理", TestID: "tab-auto"},
	{Value: nav.SettingsCleanup, Label: "清理", TestID: "tab-cleanup"},
	{Value: nav.SettingsHealth, Label: "健康监测", TestID: "tab-health"},
	{Value: nav.SettingsHistory, Label: "历史记录", TestID: "tab-history"},
	{Value: nav.SettingsAbout, Label: "关于", TestID: "tab-about"},
}

// Page is the 设置 page. It opens on the tab a link asked for (nav.Navigator.GoSettings), else on 通用.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	stores := store.Use(cx)
	ctx := cx.Context()
	start := nav.SettingsGeneral
	if n := nav.Use(cx); n != nil {
		if t := n.TakeSettingsTab(); t != "" {
			start = t
		}
	}
	tab := core.NewSignal(cx, start)
	resetRef, clearRef := new(core.Ref), new(core.Ref)
	resetting, resetBusy, resetErr := core.NewSignal(cx, false), core.NewSignal(cx, false), core.NewSignal(cx, "")
	clearing := core.NewSignal(cx, false)
	notify := newNotifyPermission(cx, deps.NotificationPermission)
	windows := deps.windows()

	// 恢复默认设置 counts a generation, which draws the tab again so every control drops its draft and its notes.
	content := core.Show{When: stores.Settings.Loaded, Then: func(*core.Scope) core.Node {
		return core.Dynamic(func(*core.Scope) core.Node {
			stores.Settings.Generation()
			t := tab.Get()
			return core.Func(func(cx *core.Scope) core.Node {
				var groups []core.Node
				switch t {
				case nav.SettingsAuto:
					groups = autoTab(stores, notify, windows)
				case nav.SettingsCleanup:
					groups = cleanupTab(stores.Settings)
				case nav.SettingsHealth:
					groups = healthTab(stores.Settings)
				case nav.SettingsHistory:
					groups = historyTab(stores.Settings, clearRef, func() { clearing.Set(true) })
				case nav.SettingsAbout:
					groups = aboutTab(cx, ctx, stores.Settings, deps)
				default:
					groups = generalTab(stores, notify, windows, resetRef, func() { resetErr.Set(""); resetting.Set(true) })
				}
				return core.View{TestID: "settings-tab-" + string(t), Style: core.Style{Gap: 22}, Children: groups}
			})
		})
	}}

	reset := kit.ConfirmDialog{Open: resetting.Get, Title: "恢复默认设置？", ConfirmLabel: "恢复默认", Danger: true, Busy: resetBusy.Get,
		ReturnFocus: resetRef, TestID: "reset-dialog",
		Message: confirmMessage(resetText, resetErr.Get, "reset-error"),
		OnConfirm: func() {
			resetBusy.Set(true)
			resetErr.Set("")
			stores.Settings.Reset(func(err error) {
				resetBusy.Set(false)
				if err != nil {
					resetErr.Set(err.Error())
					return
				}
				resetting.Set(false)
			})
		},
		OnCancel: func() { resetting.Set(false) },
	}

	clear := shared.ClearHistoryDialog{Open: clearing.Get, OnClose: func() { clearing.Set(false) }, ReturnFocus: clearRef}

	return core.Group{
		kit.Page{TestID: "settings-page", Children: []core.Node{
			kit.PageHeader{Route: nav.Settings},
			kit.Tabs[nav.SettingsTab]{Label: "设置分类", Options: tabs, Value: tab.Get, OnChange: tab.Set},
			content,
		}},
		reset, clear,
	}
}

// confirmMessage is a confirmation's text with the error the action failed with under it.
func confirmMessage(text string, err func() string, testID string) core.Node {
	return core.View{Style: core.Style{AlignItems: core.AlignCenter, Gap: 8},
		Children: []core.Node{core.Text{Content: text}, kit.ErrorLine{Text: err, TestID: testID}}}
}

// group is a titled group of rows: its title over a glass card holding the rows, with a line between each two. A row that is itself several rows (a folder list) draws its own lines.
func group(title, testID string, rows ...core.Node) core.Node {
	var head []core.Node
	if title != "" {
		head = []core.Node{core.View{Style: core.Style{Padding: core.Edges{Left: 4}}, Children: []core.Node{kit.SectionTitle(title)}}}
	}
	return core.View{Role: core.RoleGroup, Label: title, TestID: testID, Style: core.Style{Gap: 8},
		Children: append(head, kit.Card{Clip: true, Children: withLines(rows)})}
}

// resetText is what 恢复默认设置 says it does, on its row and in its dialog.
const resetText = "所有设置回到初始值，「登录时启动」和自动清理规则会关闭。操作历史不受影响。"

// notifyDeniedText is the line under a setting that sends notifications while the system does not let Spruce send them.
const notifyDeniedText = "通知没有开启，可以在系统设置里允许 Spruce 发送通知"

func generalTab(stores *store.Stores, notify *notifyPermission, windows bool, resetRef *core.Ref, onReset func()) []core.Node {
	st := stores.Settings
	limits := st.Limits()
	general := []core.Node{
		switchSetting{Field: FieldLaunchAtLogin, Label: "登录时启动 Spruce", TestID: "launch-at-login",
			DescriptionFunc: func() string {
				if !windows && st.Value().General.KeepInTray {
					return "登录系统后在菜单栏安静运行，不打开窗口。"
				}
				return "登录系统后自动打开 Spruce。"
			}},
	}
	if !windows {
		general = append(general, switchSetting{Field: FieldKeepInTray, Label: "关闭窗口后继续在菜单栏运行", TestID: "close-to-tray",
			Description: "Spruce 会在后台定期检查，并在空间不足时提醒你。关闭后，关闭窗口就会退出 Spruce。"})
	}
	background := "每隔几小时检查一次可以释放的空间。只在接通电源、电脑不忙时进行，只读取，不会删除任何东西。"
	if windows {
		background = "Spruce 打开时" + background
	}
	return []core.Node{
		group("", "section-general", general...),
		group("后台检查", "section-background",
			switchSetting{Field: FieldBackgroundEnabled, Label: "在后台定期检查", Description: background, TestID: "background-check"},
		),
		group("提醒", "section-alerts",
			switchSetting{Field: FieldLowDiskAlert, Label: "空间不足时提醒我", Description: "系统盘可用空间低于下面的值时发一条通知。", TestID: "low-disk-alert",
				OnTurnOn: notify.ask,
				Below:    []core.Node{warningLine(notifyDeniedText, func() bool { return st.Value().General.LowDiskAlert && notify.denied() }, "notify-denied")}},
			numberSetting{Field: FieldLowDiskGB, Label: "提醒阈值", Range: limits.LowDiskGB, Unit: "GB", TestID: "low-disk-threshold"},
		),
		permissionsGroup(stores.Permission),
		group("", "section-reset",
			row{Label: "恢复默认设置", Description: resetText, TestID: "reset-row",
				Control: []core.Node{kit.Button{Text: "恢复默认设置…", Label: "恢复默认设置", Variant: kit.ButtonGhost, Ref: resetRef, TestID: "reset", OnPress: onReset}}},
		),
	}
}

// autoTab is 设置 → 自动清理: on macOS what the rules do, a switch per rule with what it would take now or what it did last, and the weekly summary; on Windows only why there are none.
func autoTab(stores *store.Stores, notify *notifyPermission, windows bool) []core.Node {
	if windows {
		return []core.Node{kit.Card{Variant: kit.CardMuted, TestID: "auto-unsupported", Padding: core.Symmetric(16, 18), Children: []core.Node{
			core.Text{Content: "Windows 版暂不提供自动清理。Windows 在回收站放不下某个项目时会询问是否直接删除，无人值守时无法保证只移到回收站。",
				Style: lineStyle(kit.ColorTextSecondary)},
		}}}
	}
	st := stores.Settings
	trash := format.TrashName()
	intro := core.Text{Content: "自动清理只会把文件移到" + trash + "，不会运行命令，也不会直接删除。每次运行都会记在操作历史里，在清空" + trash +
		"之前可以放回原处。只在接通电源、电脑不忙时运行。你放回原处的项目不会再被自动清理。",
		Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: kit.ColorTextMuted, Padding: core.Edges{Left: 4, Right: 4}}}
	var ruleRows []core.Node
	for _, spec := range rules.Catalog() {
		field := RuleField(spec.ID)
		testID := "rule-" + string(spec.ID)
		ruleRows = append(ruleRows, switchSetting{Field: field, Label: spec.Title, Description: RuleDescription(spec.ID), TestID: testID,
			OnTurnOn: notify.askOnce,
			Below: []core.Node{statusLine(func() string {
				ov := stores.Overview.State()
				return RuleStatusText(field.Get(st.Value()), RuleStatusOf(ov, spec.ID), ov.Result != nil, time.Now())
			}, testID+"-status")}})
	}
	return []core.Node{
		intro,
		group("规则", "section-auto-rules", ruleRows...),
		group("通知", "section-auto-notify",
			switchSetting{Field: FieldWeeklySummary, Label: "每周汇总", Description: "每周一告诉你自动清理了多少。", TestID: "auto-summary-notify",
				OnTurnOn: notify.ask,
				Below:    []core.Node{warningLine(notifyDeniedText, func() bool { return st.Value().Automation.WeeklySummary && notify.denied() }, "notify-denied")}},
		),
	}
}

// notifyPermission is whether Spruce may send notifications, as the page last heard: read without asking as the page opens, and asked for only as the user turns on something that notifies.
type notifyPermission struct {
	cx     *core.Scope
	answer func(cx *core.Scope, ask bool, done func(core.NotificationPermission))
	state  *core.Signal[core.NotificationPermission]
}

// newNotifyPermission reads the permission through answer (the scope's own when nil) without asking.
func newNotifyPermission(cx *core.Scope, answer func(cx *core.Scope, ask bool, done func(core.NotificationPermission))) *notifyPermission {
	if answer == nil {
		answer = func(cx *core.Scope, ask bool, done func(core.NotificationPermission)) {
			if ask {
				cx.RequestNotificationPermission(done)
			} else {
				cx.NotificationPermission(done)
			}
		}
	}
	n := &notifyPermission{cx: cx, answer: answer, state: core.NewSignal(cx, core.NotificationsNotAsked)}
	answer(cx, false, n.state.Set)
	return n
}

// ask asks the user to let Spruce send notifications; the system prompts only the first time.
func (n *notifyPermission) ask() { n.answer(n.cx, true, n.state.Set) }

// askOnce asks only when the user was never asked: turning on a rule wants the weekly summary to be able to show, but does not need it.
func (n *notifyPermission) askOnce() {
	if n.state.GetUntracked() == core.NotificationsNotAsked {
		n.ask()
	}
}

// denied reports whether the user turned notifications off for Spruce; reading it subscribes.
func (n *notifyPermission) denied() bool { return n.state.Get() == core.NotificationsDenied }

func cleanupTab(st *store.Settings) []core.Node {
	limits := st.Limits()
	return []core.Node{
		group("所有清理", "section-cleanup",
			folderListSetting{Field: FieldExclusions, Label: "排除的文件夹", Description: "任何清理功能都不会删除这些位置里的内容。",
				MaxPaths: limits.MaxPaths, TestID: "exclusions"},
		),
		group("开发者", "section-dev-junk",
			folderListSetting{Field: FieldDevProjectRoots, Label: "项目所在文件夹", Description: "在这些文件夹里查找项目的构建产物。",
				MaxPaths: limits.MaxPaths, TestID: "dev-project-roots"},
			numberSetting{Field: FieldDevArtifactMinAgeDays, Label: "最少闲置天数", Description: "项目超过这么多天没有改动，才会列出它的构建产物。",
				Range: limits.DevArtifactMinAgeDays, Unit: "天", TestID: "dev-artifact-min-age"},
		),
		group("安装包", "section-installers",
			folderListSetting{Field: FieldInstallerRoots, Label: "查找位置", Description: "在这些文件夹和它们的下一层子文件夹里查找安装包和磁盘映像。",
				MaxPaths: limits.MaxPaths, TestID: "installer-roots"},
		),
		group("空间透视", "section-space",
			numberSetting{Field: FieldLargeFileMinMB, Label: "大文件阈值", Description: "不小于这个大小的文件算作大文件。",
				Range: limits.LargeFileMinMB, Unit: "MB", TestID: "large-file-min"},
		),
	}
}

func healthTab(st *store.Settings) []core.Node {
	limits := st.Limits()
	return []core.Node{
		group("高 CPU 提醒", "section-health",
			switchSetting{Field: FieldHealthEnabled, Label: "启用高 CPU 提醒", Description: "有进程长时间占用大量 CPU 时提醒你。只提醒，不会结束进程。",
				TestID: "health-enabled"},
			sliderSetting{Field: FieldCPUThreshold, Label: "提醒阈值", Description: "按单个处理器核心计算：100% 表示占满一个核心，200% 表示占满两个核心。",
				Range: limits.CPUThreshold, Step: 5, Format: func(v int) string { return strconv.Itoa(v) + "%" }, TestID: "cpu-threshold"},
			sliderSetting{Field: FieldSustainMinutes, Label: "持续时间", Description: "进程连续超过阈值这么久才提醒。",
				Range: limits.SustainMinutes, Step: 1, Format: func(v int) string { return strconv.Itoa(v) + " 分钟" }, TestID: "sustain-minutes"},
		),
	}
}

func historyTab(st *store.Settings, clearRef *core.Ref, onClear func()) []core.Node {
	limits := st.Limits()
	return []core.Node{
		group("", "section-history",
			numberSetting{Field: FieldRetentionDays, Label: "保留天数", Description: "更早的操作记录会自动删除。", Range: limits.RetentionDays, Unit: "天",
				TestID: "retention-days"},
			row{Label: "清空历史", Description: "删除全部操作记录，无法恢复。", TestID: "clear-history",
				Control: []core.Node{kit.Button{Text: "清空历史…", Variant: kit.ButtonGhost, Ref: clearRef, OnPress: onClear, TestID: "clear-history-button"}}},
		),
	}
}

// permissionsGroup shows Full Disk Access on macOS, with the guide the 维护 page uses; it reads the state again as it shows, and says 正在检查… until the store first answers. Windows has no such permission, so it shows nothing there.
func permissionsGroup(perm *store.Permission) core.Node {
	perm.Recheck()
	return core.Show{When: func() bool {
		return runtime.GOOS != "windows" && perm.FullDiskAccess() != maintenance.PermissionNotRequired
	},
		Then: func(cx *core.Scope) core.Node {
			failed := core.NewSignal(cx, false)
			granted := func() bool { return perm.FullDiskAccess() == maintenance.PermissionGranted }
			state := core.Text{ContentFunc: func() string {
				if !perm.Checked() {
					return "正在检查…"
				}
				return PermissionText(perm.FullDiskAccess())
			}, TestID: "permission-state",
				StyleFunc: func() core.Style {
					c := kit.ColorWarning
					if granted() {
						c = kit.ColorTextSecondary
					}
					return core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: c}
				}}
			guide := core.Show{When: func() bool { return perm.Checked() && !granted() }, Then: func(*core.Scope) core.Node {
				return kit.Button{Variant: kit.ButtonGhost, TestID: "grant-full-disk-access", Disabled: perm.Guiding,
					TextFunc: func() string {
						if perm.Guiding() {
							return "等待授权…"
						}
						return "去授权"
					},
					OnPress: func() {
						failed.Set(false)
						perm.Guide(func(_ maintenance.PermissionState, err error) { failed.Set(err != nil) })
					}}
			}}
			return group("权限", "section-permissions",
				row{Label: "完全磁盘访问权限", TestID: "full-disk-access",
					Description: "优化 Safari 和邮件的数据库、读取" + format.TrashName() + "和查找部分应用的残留文件时需要这项权限。",
					Error: func() string {
						if failed.Get() {
							return "无法打开系统设置"
						}
						return ""
					},
					Control: []core.Node{state, guide}})
		}}
}

func aboutTab(cx *core.Scope, ctx context.Context, st *store.Settings, deps Deps) []core.Node {
	about := st.About()
	revealErr := core.NewSignal(cx, "")
	reveal := func() {
		revealErr.Set("")
		async.Do(cx, ctx, func(ctx context.Context) (struct{}, error) { return struct{}{}, deps.Files.Reveal(ctx, about.DataDir) },
			func(_ struct{}, err error) {
				if err != nil {
					revealErr.Set(err.Error())
				}
			})
	}

	var icon core.Node = kit.IconTile{Tone: nav.Settings.Meta().Tone, Size: 72, GlyphSize: 36, Glyph: nav.Settings.Meta().Glyph, Radius: 18}
	if len(deps.Icon) > 0 {
		icon = core.Image{Source: core.ImageSource{Data: deps.Icon}, ElementsHidden: true, Style: core.Style{Width: core.Pt(88), Height: core.Pt(88)}}
	}
	hero := core.View{TestID: "version", Style: core.Style{AlignItems: core.AlignCenter, Gap: 6, Padding: core.Edges{Top: 8, Bottom: 6}},
		Children: []core.Node{
			icon,
			core.View{Style: core.Style{Height: core.Pt(6)}},
			core.Text{Content: "Spruce", Role: core.RoleHeader, Style: core.Style{FontFamily: kit.FontFace(kit.Bold), FontSize: kit.FontTitle,
				FontWeight: kit.FaceWeight(kit.Bold), Color: kit.ColorText}},
			kit.Tabular(kit.Text(Version(about.Version), kit.FontBody, kit.Regular, kit.ColorTextSecondary)),
			kit.Text("清理磁盘、保养系统的桌面工具。", kit.FontBody, kit.Regular, kit.ColorTextMuted),
		}}

	licenses := make([]core.Node, len(Licenses))
	for i, l := range Licenses {
		licenses[i] = core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifySpaceBetween, Gap: 12,
			MinHeight: core.Pt(40), Padding: core.Symmetric(8, 18)},
			Children: []core.Node{
				kit.Text(l.Name, kit.FontBody, kit.Regular, kit.ColorText),
				kit.Text(l.License, kit.FontSmall, kit.Regular, kit.ColorTextMuted),
			}}
	}

	return []core.Node{
		core.View{TestID: "section-about", Style: core.Style{Gap: 22}, Children: []core.Node{
			hero,
			group("", "section-data",
				row{Label: "数据文件夹", Description: about.DataDir, Error: revealErr.Get, TestID: "data-dir",
					Control: []core.Node{kit.Button{Text: "在" + format.FileManagerName() + "中显示", Variant: kit.ButtonGhost, TestID: "reveal-data-dir",
						Disabled: func() bool { return about.DataDir == "" }, OnPress: reveal}}},
			),
			group("开源许可", "license-list", licenses...),
		}},
	}
}

// withLines puts a line between each two of rows.
func withLines(rows []core.Node) []core.Node {
	out := make([]core.Node, 0, 2*len(rows))
	for i, r := range rows {
		if i > 0 {
			out = append(out, line())
		}
		out = append(out, r)
	}
	return out
}

// line is the line between two rows of a group, inset from the left as the rows' text is.
func line() core.Node {
	return core.View{Style: core.Style{Height: core.Pt(1), FlexShrink: 0, Margin: core.Edges{Left: 18}, Background: kit.BorderSubtle}}
}
