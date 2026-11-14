// Package installers is the 安装包 page: it finds installers and disk images in the folders set in 设置 and moves the ones the user checks to the Trash, through the shared cleaner flow (package cleanflow).
package installers

import (
	"runtime"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/shared"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Page is the 安装包 page.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	windows := runtime.GOOS == "windows"
	set := store.Use(cx).Settings
	roots := func() []string { return set.Value().Cleanup.InstallerRoots }
	f := cleanflow.NewFlow(cx, deps.Cleanup, app.CleanerInstallers)
	return cleanflow.View(cx, f, cleanflow.ViewConfig{
		Page:            nav.Installers,
		IdleTitle:       "查找安装包",
		IdleDescription: ScanDescription(windows),
		IdleDetails: func(cx *core.Scope) core.Node {
			return idleDetails(cx, set, roots, TrashLine(windows))
		},
		// Until the settings are read the folders are unknown, and 扫描 stays as it was.
		ScanDisabled:     func() bool { return set.Loaded() && len(roots()) == 0 },
		ListLabel:        "找到的安装包",
		Notice:           Notice(windows),
		SizeLabel:        "已选大小",
		EmptyTitle:       "没有找到安装包",
		EmptyDescription: "这些文件夹里没有旧的安装包或磁盘映像。",
		Reveal:           revealer(deps.Files),
	})
}

// detailText is one quiet line under the idle description (.cleaner-idle-details).
func detailText(content func() string, testID string) core.Text {
	return core.Text{ContentFunc: content, MaxLines: 1, EllipsizeMode: core.EllipsizeTail, TestID: testID, Style: core.Style{FontFamily: kit.FontSans,
		FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: kit.ColorTextMuted, FontVariant: core.FontTabularNums}}
}

// idleDetails is what sits under the idle description: the folders the scan searches (or how to add one) once the settings are read, where cleaned installers go and the last clean. The folders are one list without gaps between them, 4 points from the other lines, as in the design.
func idleDetails(cx *core.Scope, set *store.Settings, roots func() []string, trashLine string) core.Node {
	last := shared.LastClean(cx, history.KindInstallers)
	return core.View{Style: core.Style{MaxWidth: core.Pt(520), AlignItems: core.AlignCenter, Gap: 4}, Children: []core.Node{
		core.Show{When: set.Loaded, Then: func(*core.Scope) core.Node {
			return core.Show{When: func() bool { return len(roots()) > 0 }, Then: func(*core.Scope) core.Node { return folderList(roots) },
				Else: func(*core.Scope) core.Node { return detailText(func() string { return NoFolders }, "no-folders") }}
		}},
		detailText(func() string { return trashLine }, "trash-line"),
		core.Show{When: func() bool { return last() != nil }, Then: func(*core.Scope) core.Node {
			return detailText(func() string {
				if run := last(); run != nil {
					return shared.LastCleanText(*run, timeNow())
				}
				return ""
			}, "last-clean")
		}},
	}}
}

// folderList lists the folders the scan searches, each shortened in the middle with the whole path in its tooltip.
func folderList(roots func() []string) core.Node {
	return core.View{Role: core.RoleGroup, Label: "查找的文件夹", TestID: "folders", Style: core.Style{MaxWidth: core.Pt(520), AlignItems: core.AlignCenter},
		Children: []core.Node{
			core.Each(roots, func(r string) string { return r }, func(_ *core.Scope, root func() string, _ func() int) core.Node {
				return core.View{Style: core.Style{MaxWidth: core.Pt(520)}, Tooltip: tooltip(root()), Children: []core.Node{
					detailText(func() string { return format.TruncateMiddle(root(), folderChars) }, ""),
				}}
			}),
		}}
}

// tooltip is the whole path over a shortened folder line, styled as kit's tooltips.
func tooltip(s string) core.Node {
	return core.View{Style: core.Style{Padding: core.Symmetric(4, 8), CornerRadius: 6, Background: core.RGBA(0x1a1030, 0.92)},
		Children: []core.Node{kit.Text(s, kit.FontSmall, kit.Regular, kit.ColorText)}}
}
