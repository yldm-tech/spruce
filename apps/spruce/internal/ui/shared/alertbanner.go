package shared

import (
	"fmt"
	"math"
	"runtime"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/engine/health"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/store"
)

// SystemMonitor is the system's process monitor, as 查看详情 opens it.
type SystemMonitor struct {
	Name string
	// Unit notes the CPU unit where the monitor counts differently from the alert, "" where it matches.
	Unit string
}

// Monitor names the process monitor of this platform. The alert's CPU percent is the share of one logical CPU, as Activity Monitor shows it; Task Manager shows the share of the whole machine, so on Windows the banner says which unit it uses.
func Monitor() SystemMonitor { return monitorFor(runtime.GOOS) }

func monitorFor(goos string) SystemMonitor {
	if goos == "windows" {
		return SystemMonitor{Name: "任务管理器", Unit: "占用按单个处理器核心计算，任务管理器按全部核心显示，数值会更低。"}
	}
	return SystemMonitor{Name: "活动监视器"}
}

// AlertTitle is the banner's title, e.g. "Xcode 已经持续占用 48% CPU 有 12 分钟": the monitor alerts only after its sustain period, so at least 1 minute shows.
func AlertTitle(a health.Alert) string {
	minutes := max(1, int64(a.Duration/time.Minute))
	return fmt.Sprintf("%s 已经持续占用 %d%% CPU 有 %d 分钟", a.Name, int(math.Round(a.CPUPercent)), minutes)
}

// AlertSub is the banner's second line for an alert with others more alerts beside it.
func AlertSub(others int, m SystemMonitor) string {
	s := ""
	if others > 0 {
		s = fmt.Sprintf("另有 %d 个进程也在高占用。", others)
	}
	return s + m.Unit + "如果不是你正在用它，可以在" + m.Name + "里看看。Spruce 不会结束进程，只提醒你。"
}

// AlertBanner warns about a process that has kept the CPU busy for a long time. It only informs: Spruce never ends processes. Its close button hides that process until the app quits (Alerts.Ignore), so the next alert shows, or none. It is absent while there is no alert; screen readers hear it appear.
type AlertBanner struct{}

// Render implements core.Node.
func (AlertBanner) Render(cx *core.Scope) core.Node {
	alerts := store.Use(cx).Alerts
	failed := core.NewSignal(cx, false)
	m := Monitor()
	return core.Show{When: func() bool { return len(alerts.List()) > 0 }, Then: func(*core.Scope) core.Node {
		// The texts update once more as the last alert goes, before the Show takes the banner away (https://github.com/nsgo-dev/nsgo/issues/400: a Show's branch reruns its text functions after When turned false).
		top := func() health.Alert {
			if list := alerts.List(); len(list) > 0 {
				return list[0]
			}
			return health.Alert{}
		}
		return core.View{Role: core.RoleRegion, Label: "CPU 占用提醒", LiveRegion: core.LiveRegionPolite, TestID: "alert-banner",
			Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 16, Padding: core.Symmetric(16, 20),
				CornerRadius: kit.RadiusCard, Background: kit.SurfaceBanner, BorderWidth: 1, BorderColor: kit.BorderBanner},
			Children: []core.Node{
				core.View{Style: core.Style{Width: core.Pt(44), Height: core.Pt(44), CornerRadius: 22, FlexShrink: 0, AlignItems: core.AlignCenter,
					JustifyContent: core.JustifyCenter, Background: kit.SurfaceBannerIcon},
					Children: []core.Node{kit.Glyph{Name: "alert", Size: 22, Color: kit.ColorWarning}}},
				core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(280), Gap: 2}, Children: []core.Node{
					kit.TextFunc(func() string { return AlertTitle(top()) }, kit.FontCardTitle, kit.Semibold, kit.ColorText),
					kit.TextFunc(func() string { return AlertSub(len(alerts.List())-1, m) }, kit.FontBody, kit.Regular, kit.ColorTextSecondary),
					kit.ErrorLine{Text: func() string {
						if failed.Get() {
							return "无法打开" + m.Name
						}
						return ""
					}},
				}},
				kit.Button{Text: "查看详情", Variant: kit.ButtonGhost, TestID: "alert-open-monitor", OnPress: func() {
					failed.Set(false)
					alerts.OpenSystemMonitor(func(err error) { failed.Set(err != nil) })
				}},
				kit.IconButton{Glyph: "close", Label: "忽略此进程", TestID: "alert-ignore", OnPress: func() {
					failed.Set(false)
					alerts.Ignore(top().Name)
				}},
			}}
	}}
}
