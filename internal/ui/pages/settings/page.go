// Package settings is the 设置 page: a tab per area (通用, 清理, 健康监测, 历史记录, 关于), each a column of grouped rows with the label and its description on the left and the control on the right. Every control saves on its own as it changes; there is no Save button.
package settings

import (
	"context"
	"runtime"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/maintenance"
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

	// 恢复默认设置 counts a generation, which draws the tab again so every control drops its draft and its notes.
	content := core.Show{When: stores.Settings.Loaded, Then: func(*core.Scope) core.Node {
		return core.Dynamic(func(*core.Scope) core.Node {
			stores.Settings.Generation()
			t := tab.Get()
			return core.Func(func(cx *core.Scope) core.Node {
				var groups []core.Node
				switch t {
				case nav.SettingsCleanup:
					groups = cleanupTab(stores.Settings)
				case nav.SettingsHealth:
					groups = healthTab(stores.Settings)
				case nav.SettingsHistory:
					groups = historyTab(stores.Settings, clearRef, func() { clearing.Set(true) })
				case nav.SettingsAbout:
					groups = aboutTab(cx, ctx, stores.Settings, deps)
				default:
					groups = generalTab(stores.Permission, resetRef, func() { resetErr.Set(""); resetting.Set(true) })
				}
				return core.View{TestID: "settings-tab-" + string(t), Style: core.Style{Gap: 22}, Children: groups}
			})
		})
	}}

	reset := kit.ConfirmDialog{Open: resetting.Get, Title: "恢复默认设置？", ConfirmLabel: "恢复默认", Danger: true, Busy: resetBusy.Get,
		ReturnFocus: resetRef, TestID: "reset-dialog",
		Message: confirmMessage("所有设置都会回到初始值，「登录时启动」会关闭。操作历史不受影响。", resetErr.Get, "reset-error"),
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

func generalTab(perm *store.Permission, resetRef *core.Ref, onReset func()) []core.Node {
	return []core.Node{
		group("", "section-general",
			switchSetting{Field: FieldLaunchAtLogin, Label: "登录时启动 Spruce", Description: "登录系统后自动打开 Spruce。", TestID: "launch-at-login"},
			switchSetting{Field: FieldConfirmBeforeClean, Label: "清理前确认", Description: "每次清理前先说明要清理多少、能否恢复，确认后才开始。",
				TestID: "confirm-before-clean"},
		),
		permissionsGroup(perm),
		group("", "section-reset",
			row{Label: "恢复默认设置", Description: "所有设置回到初始值，「登录时启动」会关闭。操作历史不受影响。", TestID: "reset-row",
				Control: []core.Node{kit.Button{Text: "恢复默认设置…", Label: "恢复默认设置", Variant: kit.ButtonGhost, Ref: resetRef, TestID: "reset", OnPress: onReset}}},
		),
	}
}

func cleanupTab(st *store.Settings) []core.Node {
	limits := st.Limits()
	return []core.Node{
		group("所有清理", "section-cleanup",
			folderListSetting{Field: FieldExclusions, Label: "排除的文件夹", Description: "任何清理功能都不会删除这些位置里的内容。",
				MaxPaths: limits.MaxPaths, TestID: "exclusions"},
		),
		group("开发者垃圾", "section-dev-junk",
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
