// Package systemjunk is the 系统垃圾 page: it scans with the system-junk cleaner and cleans what the user checks, through the shared cleaner flow (package cleanflow).
package systemjunk

import (
	"runtime"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/shared"
)

// ScanDescription is what the scan looks at, as the engine (internal/engine/systemjunk) lists its categories on each system.
func ScanDescription(windows bool) string {
	if windows {
		return "检查临时文件、应用缓存、浏览器缓存、错误报告、未完成的下载和回收站。只看你自己的用户文件夹，不需要管理员权限。"
	}
	return "检查应用缓存、浏览器缓存、日志、崩溃报告、未完成的下载和废纸篓。只看你自己的用户文件夹，不需要管理员权限。"
}

// Page is the 系统垃圾 page.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	f := cleanflow.NewFlow(cx, deps.Cleanup, app.CleanerSystemJunk)
	return cleanflow.View(cx, f, cleanflow.ViewConfig{
		Page:            nav.SystemJunk,
		IdleTitle:       "扫描系统垃圾",
		IdleDescription: ScanDescription(runtime.GOOS == "windows"),
		IdleDetails: func(cx *core.Scope) core.Node {
			last := shared.LastClean(cx, history.KindSystemJunk)
			return cleanflow.IdleDetails(func() []string {
				if run := last(); run != nil {
					return []string{shared.LastCleanText(*run, timeNow())}
				}
				return nil
			})
		},
		ListLabel:        "可以清理的系统垃圾",
		SizeLabel:        "可释放",
		EmptyTitle:       "没有找到系统垃圾",
		EmptyDescription: "缓存、日志和临时文件都很干净，过段时间再来看看。",
		Reveal:           revealer(deps.Files),
	})
}
