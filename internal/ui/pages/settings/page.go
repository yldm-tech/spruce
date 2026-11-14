// Package settings is the 设置 page: one glass card per area (通用, 健康监测, 清理, 历史记录, 权限 on macOS, 关于) and 恢复默认设置. Every control saves on its own as it changes; there is no Save button.
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
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Page is the 设置 page.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	stores := store.Use(cx)
	ctx := cx.Context()
	resetRef, clearRef := new(core.Ref), new(core.Ref)
	resetting, resetBusy, resetErr := core.NewSignal(cx, false), core.NewSignal(cx, false), core.NewSignal(cx, "")
	clearing, clearBusy, clearErr := core.NewSignal(cx, false), core.NewSignal(cx, false), core.NewSignal(cx, "")

	// 恢复默认设置 counts a generation, which draws the sections again so every control drops its draft and its notes.
	sections := core.Show{When: stores.Settings.Loaded, Then: func(*core.Scope) core.Node {
		return core.Dynamic(func(*core.Scope) core.Node {
			stores.Settings.Generation()
			return core.Func(func(cx *core.Scope) core.Node {
				return core.View{Style: core.Style{Gap: 14}, Children: []core.Node{
					generalSection(),
					healthSection(stores.Settings),
					cleanupSection(stores.Settings),
					historySection(stores.Settings, clearRef, func() { clearErr.Set(""); clearing.Set(true) }),
					permissionsSection(stores.Permission),
					aboutSection(cx, ctx, stores.Settings, deps.Files),
					core.View{Style: core.Style{Direction: core.Row, JustifyContent: core.JustifyCenter, Padding: core.Edges{Top: 6, Bottom: 4}},
						Children: []core.Node{kit.Button{Text: "恢复默认设置", Variant: kit.ButtonGhost, Ref: resetRef, TestID: "reset",
							OnPress: func() { resetErr.Set(""); resetting.Set(true) }}}},
				}}
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

	clear := kit.ConfirmDialog{Open: clearing.Get, Title: "清空操作历史？", ConfirmLabel: "清空", Danger: true, Busy: clearBusy.Get,
		ReturnFocus: clearRef, TestID: "clear-dialog",
		Message: confirmMessage("将删除全部操作记录，无法恢复。只删除记录，不会撤销或改动已经做过的清理和维护。", clearErr.Get, "clear-error"),
		OnConfirm: func() {
			clearBusy.Set(true)
			clearErr.Set("")
			stores.History.Clear(func(err error) {
				clearBusy.Set(false)
				if err != nil {
					clearErr.Set(err.Error())
					return
				}
				clearing.Set(false)
			})
		},
		OnCancel: func() { clearing.Set(false) },
	}

	return core.Group{
		kit.Page{TestID: "settings-page", Children: []core.Node{kit.PageHeader{Route: nav.Settings}, sections}},
		reset, clear,
	}
}

// confirmMessage is a confirmation's text with the error the action failed with under it.
func confirmMessage(text string, err func() string, testID string) core.Node {
	return core.View{Style: core.Style{AlignItems: core.AlignCenter, Gap: 8},
		Children: []core.Node{core.Text{Content: text}, kit.ErrorLine{Text: err, TestID: testID}}}
}

// section is one area's card (.settings-card): its tile and title, then its rows with a line between each two.
func section(title string, tone kit.Tone, glyph, testID string, rows ...core.Node) core.Node {
	children := []core.Node{core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, Padding: core.Edges{Bottom: 6}},
		Children: []core.Node{
			kit.IconTile{Tone: tone, Size: 32, GlyphSize: 17, Glyph: glyph},
			core.Text{Content: title, Role: core.RoleHeader, Style: core.Style{FontFamily: kit.FontFace(kit.Bold), FontSize: kit.FontCardTitle,
				FontWeight: kit.FaceWeight(kit.Bold), Color: kit.ColorText}},
		}}}
	for i, r := range rows {
		if i > 0 {
			children = append(children, core.View{Style: core.Style{Height: core.Pt(1), FlexShrink: 0, Background: kit.BorderSubtle}})
		}
		children = append(children, r)
	}
	return core.View{Role: core.RoleGroup, Label: title, TestID: testID, Children: []core.Node{
		kit.Card{Padding: core.Edges{Top: 18, Right: 22, Bottom: 6, Left: 22}, Children: children},
	}}
}

func generalSection() core.Node {
	m := nav.Settings.Meta()
	return section("通用", m.Tone, m.Glyph, "section-general",
		switchSetting{Field: FieldLaunchAtLogin, Label: "登录时启动 Spruce", Description: "登录系统后自动打开 Spruce。", TestID: "launch-at-login"},
		switchSetting{Field: FieldConfirmBeforeClean, Label: "清理前确认", Description: "每次清理前先说明要清理多少、能否恢复，确认后才开始。",
			TestID: "confirm-before-clean"},
	)
}

func healthSection(st *store.Settings) core.Node {
	m, limits := nav.Health.Meta(), st.Limits()
	return section("健康监测", m.Tone, m.Glyph, "section-health",
		switchSetting{Field: FieldHealthEnabled, Label: "启用高 CPU 提醒", Description: "有进程长时间占用大量 CPU 时提醒你。只提醒，不会结束进程。",
			TestID: "health-enabled"},
		sliderSetting{Field: FieldCPUThreshold, Label: "提醒阈值", Description: "按单个处理器核心计算：100% 表示占满一个核心，200% 表示占满两个核心。",
			Range: limits.CPUThreshold, Step: 5, Format: func(v int) string { return strconv.Itoa(v) + "%" }, TestID: "cpu-threshold"},
		sliderSetting{Field: FieldSustainMinutes, Label: "持续时间", Description: "进程连续超过阈值这么久才提醒。",
			Range: limits.SustainMinutes, Step: 1, Format: func(v int) string { return strconv.Itoa(v) + " 分钟" }, TestID: "sustain-minutes"},
	)
}

func cleanupSection(st *store.Settings) core.Node {
	m, limits := nav.SystemJunk.Meta(), st.Limits()
	return section("清理", m.Tone, m.Glyph, "section-cleanup",
		folderListSetting{Field: FieldExclusions, Label: "排除的文件夹", Description: "任何清理功能都不会删除这些位置里的内容。",
			MaxPaths: limits.MaxPaths, TestID: "exclusions"},
		folderListSetting{Field: FieldDevProjectRoots, Label: "开发项目所在文件夹", Description: "开发者垃圾在这些文件夹里查找项目的构建产物。",
			MaxPaths: limits.MaxPaths, TestID: "dev-project-roots"},
		numberSetting{Field: FieldDevArtifactMinAgeDays, Label: "项目构建产物最少闲置天数", Description: "项目超过这么多天没有改动，才会列出它的构建产物。",
			Range: limits.DevArtifactMinAgeDays, Unit: "天", TestID: "dev-artifact-min-age"},
		folderListSetting{Field: FieldInstallerRoots, Label: "安装包查找位置", Description: "安装包在这些文件夹里查找安装包和磁盘映像。",
			MaxPaths: limits.MaxPaths, TestID: "installer-roots"},
		numberSetting{Field: FieldLargeFileMinMB, Label: "大文件阈值", Description: "空间透视把不小于这个大小的文件算作大文件。",
			Range: limits.LargeFileMinMB, Unit: "MB", TestID: "large-file-min"},
	)
}

func historySection(st *store.Settings, clearRef *core.Ref, onClear func()) core.Node {
	m, limits := nav.History.Meta(), st.Limits()
	return section("历史记录", m.Tone, m.Glyph, "section-history",
		numberSetting{Field: FieldRetentionDays, Label: "保留天数", Description: "更早的操作记录会自动删除。", Range: limits.RetentionDays, Unit: "天",
			TestID: "retention-days"},
		row{Label: "清空历史", Description: "删除全部操作记录，无法恢复。", TestID: "clear-history",
			Control: []core.Node{kit.Button{Text: "清空历史…", Variant: kit.ButtonGhost, Ref: clearRef, OnPress: onClear, TestID: "clear-history-button"}}},
	)
}

// permissionsSection shows Full Disk Access on macOS, with the guide the 维护 page uses. Windows has no such permission, so it shows nothing there.
func permissionsSection(perm *store.Permission) core.Node {
	return core.Show{When: func() bool {
		return runtime.GOOS != "windows" && perm.FullDiskAccess() != maintenance.PermissionNotRequired
	},
		Then: func(cx *core.Scope) core.Node {
			failed := core.NewSignal(cx, false)
			granted := func() bool { return perm.FullDiskAccess() == maintenance.PermissionGranted }
			state := core.Text{ContentFunc: func() string { return PermissionText(perm.FullDiskAccess()) }, TestID: "permission-state",
				StyleFunc: func() core.Style {
					c := kit.ColorWarning
					if granted() {
						c = kit.ColorTextSecondary
					}
					return core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: c}
				}}
			guide := core.Show{When: func() bool { return !granted() }, Then: func(*core.Scope) core.Node {
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
			return section("权限", kit.ToneAmber, "lock", "section-permissions",
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

func aboutSection(cx *core.Scope, ctx context.Context, st *store.Settings, files Files) core.Node {
	about := st.About()
	revealErr := core.NewSignal(cx, "")
	reveal := func() {
		revealErr.Set("")
		async.Do(cx, ctx, func(ctx context.Context) (struct{}, error) { return struct{}{}, files.Reveal(ctx, about.DataDir) },
			func(_ struct{}, err error) {
				if err != nil {
					revealErr.Set(err.Error())
				}
			})
	}
	licenses := make([]core.Node, len(Licenses))
	for i, l := range Licenses {
		licenses[i] = core.View{Style: core.Style{Direction: core.Row, JustifyContent: core.JustifySpaceBetween, Gap: 12},
			Children: []core.Node{
				kit.Text(l.Name, kit.FontBody, kit.Regular, kit.ColorText),
				kit.Text(l.License, kit.FontBody, kit.Regular, kit.ColorTextMuted),
			}}
	}
	return section("关于", kit.ToneViolet, "info", "section-about",
		row{Label: "Spruce", Description: "清理磁盘、保养系统的桌面工具。", TestID: "version",
			Control: []core.Node{kit.Tabular(kit.Text(Version(about.Version), kit.FontBody, kit.Regular, kit.ColorTextSecondary))}},
		row{Label: "数据文件夹", Description: about.DataDir, Error: revealErr.Get, TestID: "data-dir",
			Control: []core.Node{kit.Button{Text: "在" + format.FileManagerName() + "中显示", Variant: kit.ButtonGhost, TestID: "reveal-data-dir",
				Disabled: func() bool { return about.DataDir == "" }, OnPress: reveal}}},
		row{Label: "开源许可", Description: "Spruce 使用了以下开源软件：", Stacked: true, TestID: "licenses",
			Control: []core.Node{core.View{Role: core.RoleGroup, Label: "开源许可", TestID: "license-list",
				Style: core.Style{AlignSelf: core.AlignStretch, Width: core.Pct(100), Display: core.DisplayGrid, GridTemplateColumns: "repeat(auto-fill, minmax(220px, 1fr))",
					RowGap: 6, ColumnGap: 24},
				Children: licenses}}},
	)
}
