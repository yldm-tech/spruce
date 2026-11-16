// Package systemjunk is the 系统垃圾 drill-down view of 概览: the system-junk categories of 概览's latest check, to look through and clean in part. It never scans; its list, its clean and its toast are the shared drill-down view's (cleanflow.DrillPage) over store.Overview.
package systemjunk

import (
	"runtime"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/overview"
)

// ScanDescription is what the check looks at, as the engine (internal/engine/systemjunk) lists its categories on each system.
func ScanDescription(windows bool) string {
	if windows {
		return "检查临时文件、应用缓存、浏览器缓存、错误报告、未完成的下载和回收站。只看你自己的用户文件夹，不需要管理员权限。"
	}
	return "检查应用缓存、浏览器缓存、日志、崩溃报告、未完成的下载和废纸篓。只看你自己的用户文件夹，不需要管理员权限。"
}

// Page is the 系统垃圾 view.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	description := ScanDescription(runtime.GOOS == "windows")
	return cleanflow.DrillPage{
		Route:            nav.SystemJunk,
		Toast:            overview.CleanToast,
		Notice:           func() string { return description },
		ListLabel:        "可以清理的系统垃圾",
		SizeLabel:        "已选大小",
		EmptyTitle:       "没有找到系统垃圾",
		EmptyDescription: "缓存、日志和临时文件都很干净，过段时间再来看看。",
		Reveal:           revealer(deps.Files),
	}
}
