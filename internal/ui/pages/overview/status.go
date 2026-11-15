package overview

import (
	"runtime"
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/shared"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// historyLimit is how many runs the 维护 card reads: everything the store keeps (history.MaxRuns), as the history page does.
const historyLimit = history.MaxRuns

// statusCards are the informational cards of 概览's 其他检查: the sustained high CPU alert, Full Disk Access (macOS) and when 维护 last ran (free space is the disk row above). They clean nothing; each links to the page that acts on it.
type statusCards struct{}

// Render implements core.Node.
func (statusCards) Render(cx *core.Scope) core.Node {
	st := store.Use(cx)
	cards := []core.Node{cpuCard(cx, st)}
	if runtime.GOOS != "windows" {
		cards = append(cards, fullDiskAccessCard(cx, st))
	}
	cards = append(cards, maintenanceCard(cx, st))
	return core.View{Role: core.RoleRegion, Label: "其他检查", TestID: "status", Style: core.Style{Gap: 12}, Children: []core.Node{
		kit.SectionTitle("其他检查"),
		cardGrid("其他检查", "", cards...),
	}}
}

// statusCard is one card: its tile and title, Value (in the warning color while Warning), Note under it and Actions at its bottom; Actions shows while HasActions (always when nil).
type statusCard struct {
	Title      string
	Tone       kit.Tone
	Glyph      string
	Value      func() string
	Warning    func() bool
	Note       func() string
	Actions    []core.Node
	HasActions func() bool
	TestID     string
}

// Render implements core.Node.
func (c statusCard) Render(*core.Scope) core.Node {
	value := core.Text{ContentFunc: c.Value, TestID: c.TestID + "-value", StyleFunc: func() core.Style {
		color := kit.ColorText
		if c.Warning != nil && c.Warning() {
			color = kit.ColorWarning
		}
		return core.Style{FontFamily: kit.FontFace(kit.Semibold), FontWeight: kit.FaceWeight(kit.Semibold), FontSize: kit.FontCardTitle,
			LineHeightMultiple: 1.4, Color: color}
	}}
	children := []core.Node{cardHead(c.Tone, c.Glyph, 32, 17, c.Title), value}
	if c.Note != nil {
		children = append(children, core.Show{When: func() bool { return c.Note() != "" }, Then: func(*core.Scope) core.Node {
			return mutedText(c.Note, nil, c.TestID+"-note")
		}})
	}
	if len(c.Actions) > 0 {
		actions := func(*core.Scope) core.Node {
			return core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 8, AutoMargin: core.SideTop,
				Padding: core.Edges{Top: 4}}, Children: c.Actions}
		}
		if c.HasActions == nil {
			children = append(children, actions(nil))
		} else {
			children = append(children, core.Show{When: c.HasActions, Then: actions})
		}
	}
	return core.View{Role: core.RoleRegion, Label: c.Title, TestID: c.TestID, Style: cardStyle(), Children: children}
}

func cpuCard(cx *core.Scope, st *store.Stores) core.Node {
	enabled := func() bool { return !st.Settings.Loaded() || st.Settings.Value().Health.Enabled }
	meta := nav.Health.Meta()
	return statusCard{Title: "CPU 占用", Tone: meta.Tone, Glyph: meta.Glyph, TestID: "status-cpu",
		Warning: func() bool { return enabled() && len(st.Alerts.List()) > 0 },
		Value: func() string {
			alerts := st.Alerts.List()
			switch {
			case !enabled():
				return "高 CPU 提醒已关闭"
			case len(alerts) > 0:
				return shared.AlertTitle(alerts[0])
			}
			return "没有长时间高占用的进程"
		},
		Note: func() string {
			alerts := st.Alerts.List()
			switch {
			case !enabled():
				return "可以在「设置」里重新打开"
			case len(alerts) > 1:
				return "另有 " + strconv.Itoa(len(alerts)-1) + " 个进程也在高占用"
			case len(alerts) == 1:
				return "如果不是你正在用它，可以看看是什么在运行"
			}
			return "Spruce 只提醒，不会结束进程"
		},
		Actions: []core.Node{kit.Link{Start: true, TestID: "cpu-link",
			Text: func() string {
				if enabled() {
					return "健康监测"
				}
				return "去设置"
			},
			OnPress: func() {
				if enabled() {
					nav.Use(cx).Go(nav.Health)
					return
				}
				nav.Use(cx).GoSettings(nav.SettingsHealth)
			}}},
	}
}

// fullDiskAccessCard is the Full Disk Access card, absent where the system does not ask for it. It reads the state again as it shows, since it changes in System Settings; until the store first answers it says 正在检查… and offers no guide.
func fullDiskAccessCard(cx *core.Scope, st *store.Stores) core.Node {
	perm := st.Permission
	perm.Recheck()
	failed := core.NewSignal(cx, false)
	granted := func() bool { return perm.FullDiskAccess() == maintenance.PermissionGranted }
	card := statusCard{Title: "完全磁盘访问权限", Tone: kit.ToneAmber, Glyph: "lock", TestID: "status-fda",
		Warning: func() bool { return perm.FullDiskAccess() == maintenance.PermissionDenied },
		Value: func() string {
			if !perm.Checked() {
				return "正在检查…"
			}
			switch perm.FullDiskAccess() {
			case maintenance.PermissionGranted:
				return "已开启"
			case maintenance.PermissionDenied:
				return "未开启"
			}
			return "无法确定"
		},
		Note: func() string {
			if granted() {
				return "废纸篓、Safari 缓存和受保护的文件夹都能扫描"
			}
			return FullDiskAccessNote()
		},
		Actions: []core.Node{
			core.Show{When: func() bool { return perm.Checked() && !granted() }, Then: func(*core.Scope) core.Node {
				return kit.Button{Variant: kit.ButtonGhost, TestID: "grant", Disabled: perm.Guiding,
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
			}},
			kit.ErrorLine{TestID: "grant-error", Text: func() string {
				if failed.Get() {
					return "无法打开系统设置"
				}
				return ""
			}},
		}}
	return core.Show{When: func() bool { return perm.FullDiskAccess() != maintenance.PermissionNotRequired }, Then: func(*core.Scope) core.Node { return card }}
}

// maintenanceCard says when 维护 last ran, read from the history again whenever it records a run, and offers 去维护 when it is due.
func maintenanceCard(cx *core.Scope, st *store.Stores) core.Node {
	type state struct {
		run            *history.Run
		loaded, failed bool
	}
	s := core.NewSignalFunc(cx, state{}, nil)
	ctx := cx.Context()
	seq := 0
	cx.Effect(func() {
		st.History.Generation()
		seq++
		mine := seq
		st.History.Load(cx, ctx, historyLimit, func(runs []history.Run, err error) {
			if mine != seq {
				return
			}
			if err != nil {
				s.Set(state{failed: true})
				return
			}
			s.Set(state{run: LastMaintenance(runs), loaded: true})
		})
	})
	due := func() bool { v := s.Get(); return v.loaded && MaintenanceDue(v.run, timeNow()) }
	meta := nav.Maintenance.Meta()
	return statusCard{Title: "维护", Tone: meta.Tone, Glyph: meta.Glyph, TestID: "status-maintenance", Warning: due,
		Value: func() string {
			switch v := s.Get(); {
			case v.loaded:
				return MaintenanceValue(v.run, timeNow())
			case v.failed:
				return "无法读取操作历史"
			}
			return "正在读取…"
		},
		Note: func() string {
			v := s.Get()
			if !v.loaded {
				return ""
			}
			return MaintenanceNote(v.run, due())
		},
		Actions:    []core.Node{kit.Link{Start: true, Text: fixed("去维护"), To: to(nav.Maintenance), TestID: "maintenance-link"}},
		HasActions: due,
	}
}
