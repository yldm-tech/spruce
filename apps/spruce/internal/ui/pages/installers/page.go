// Package installers is the 安装包 drill-down view of 概览: the installers and disk images 概览's latest check found in the folders set in 设置, to look through and move to the Trash in part. It never scans; its list, its clean and its toast are the shared drill-down view's (cleanflow.DrillPage) over store.Overview.
package installers

import (
	"runtime"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/overview"
	"spruce/apps/spruce/internal/ui/store"
)

// Page is the 安装包 view.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	notice := Notice(runtime.GOOS == "windows")
	set := store.Use(cx).Settings
	meta := nav.Installers.Meta()
	return cleanflow.DrillPage{
		Route:            nav.Installers,
		Toast:            overview.CleanToast,
		Notice:           func() string { return notice },
		ListLabel:        "找到的安装包",
		SizeLabel:        "已选大小",
		EmptyTitle:       "没有找到安装包",
		EmptyDescription: "这些文件夹里没有旧的安装包或磁盘映像。",
		// With no folder to search there is nothing to find until one is added in 设置.
		Empty: func(cx *core.Scope) core.Node {
			if !set.Loaded() || len(set.Value().Cleanup.InstallerRoots) > 0 {
				return nil
			}
			return kit.EmptyState{Title: "没有找到安装包", Description: NoFolders, Tone: meta.Tone, Glyph: meta.Glyph, TestID: "drill-empty",
				Action: kit.Link{Text: func() string { return "去设置" }, TestID: "drill-settings-link", OnPress: func() {
					if n := nav.Use(cx); n != nil {
						n.GoSettings(nav.SettingsCleanup)
					}
				}}}
		},
		Reveal: revealer(deps.Files),
	}
}
