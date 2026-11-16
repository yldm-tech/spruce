// Package maintenance is the 维护 page: the platform's maintenance cards, the CPU alert banner, and the run button with its running and done overlays. It is the reference page every other page follows.
package maintenance

import (
	"strconv"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/shared"
	"spruce/apps/spruce/internal/ui/store"
)

// The Full Disk Access prompts of the page.
const (
	permissionText = "需要「完全磁盘访问权限」才能优化 Safari 和邮件的数据库"
	grantedText    = "已获得完全磁盘访问权限，再运行一次即可优化数据库"
)

// Page is the 维护 page.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	perm := store.Use(cx).Permission
	s := NewState(cx, deps.Maintenance, perm.Recheck)
	// Full Disk Access changes in System Settings: it is read again as the page opens, and again after a run or a guide.
	perm.Recheck()
	runRef := new(core.Ref)
	// 完成 takes the card away with the phase, so the focus goes back to 运行 here rather than as the card closes.
	finish := func() {
		s.Finish()
		runRef.Focus()
	}
	// Nothing is asked for before the first check answers.
	missing := func() bool { return perm.Checked() && shared.NeedsFullDiskAccess(perm.FullDiskAccess()) }
	onGuided := func(st maintenance.PermissionState) {
		if st == maintenance.PermissionGranted {
			s.Guided.Set(true)
		}
	}

	grid := core.View{Role: core.RoleGroup, Label: "维护任务", TestID: "task-grid",
		Busy:  func() bool { return s.Catalog.Get() == nil },
		Style: core.Style{Display: core.DisplayGrid, GridTemplateColumns: "repeat(auto-fit, minmax(250px, 1fr))", Gap: 14},
		Children: []core.Node{core.Each(s.Catalog.Get, func(t app.TaskView) string { return t.ID }, func(cx *core.Scope, task func() app.TaskView, index func() int) core.Node {
			// The cards come in one after another.
			return kit.Appear{Entrance: kit.EnterRise, Delay: kit.StaggerDelay(core.Untracked(cx, index)), Style: core.Style{MinWidth: core.Pt(0)},
				Children: []core.Node{taskCard(task(), s, missing, onGuided)}}
		})}}

	bar := kit.ActionBar{
		Start:  func() kit.Stat { return kit.Stat{Label: "已选择", Value: strconv.Itoa(len(s.Selected())) + " 项"} },
		End:    func() kit.Stat { return kit.Stat{Label: "预计用时", Value: FormatEstimate(s.Estimate())} },
		Button: kit.ActionButton{Label: "运行", Ref: runRef, TestID: "run", OnPress: s.Run, Disabled: func() bool { return len(s.Selected()) == 0 }},
	}

	running := func() bool { return s.Phase.Get() == PhaseRunning }
	overlay := kit.Overlay{Open: running, Title: "正在维护系统…", TestID: "run-overlay",
		Ring: kit.ProgressRing{Label: "正在维护系统"},
		Sub:  func() string { return RunSub(s.RunTasks.Get(), s.Progress.Get()) },
		Lines: func() []string {
			return []string{RunStatusText(s.RunTasks.Get(), s.Progress.Get())}
		},
		Note: func() string {
			if AwaitingAdmin(s.RunTasks.Get(), s.Progress.Get()) {
				return "部分任务需要管理员权限，请在系统弹出的授权窗口中确认"
			}
			return ""
		},
	}

	done := core.Dynamic(func(cx *core.Scope) core.Node {
		if s.Phase.Get() != PhaseDone {
			return core.Group{}
		}
		return core.Func(func(cx *core.Scope) core.Node { return doneCard(s, missing, perm, onGuided, finish) })
	})

	live := kit.LiveStatus{Text: func() string {
		if s.Phase.Get() != PhaseRunning {
			return ""
		}
		return LiveText(s.RunTasks.Get(), s.Progress.Get())
	}}

	return core.Group{
		kit.Page{ScrollKey: nav.Maintenance, Header: kit.PageHeader{Route: nav.Maintenance}, Children: []core.Node{shared.AlertBanner{}, grid, bar}},
		live, overlay, done,
	}
}

// taskCard is one maintenance card: the tile, the switch, the title and either the description or, for a task with a prerequisite, the hint in the warning color on a dashed card, and the Full Disk Access prompt while the task needs it and is on.
func taskCard(t app.TaskView, s *State, missing func() bool, onGuided func(maintenance.PermissionState)) core.Node {
	variant, text, color := kit.CardDefault, t.Description, kit.ColorTextMuted
	if t.Hint != "" {
		variant, text, color = kit.CardHint, t.Hint, kit.ColorWarning
	}
	on := func() bool { return s.On(t) }
	children := []core.Node{
		kit.Text(t.Title, kit.FontCardTitle, kit.Semibold, kit.ColorText),
		core.Text{Content: text, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: color}},
	}
	if NeedsFullDiskAccess(t.ID) {
		children = append(children, core.Show{When: func() bool { return on() && missing() }, Then: func(*core.Scope) core.Node {
			return shared.PermissionAction{Text: permissionText, Start: true, OnGuided: onGuided}
		}})
	}
	return kit.Card{Variant: variant, Padding: core.Symmetric(24, 22), Gap: 18, TestID: "task-" + t.ID, Style: core.Style{FlexGrow: 1}, Children: []core.Node{
		core.View{Style: core.Style{Direction: core.Row, JustifyContent: core.JustifySpaceBetween, AlignItems: core.AlignStart}, Children: []core.Node{
			kit.TaskTile(string(t.Icon)),
			kit.Switch{Label: t.Title, TestID: "switch-" + t.ID, Checked: on, OnChange: func(v bool) { s.Toggle(t.ID, v) }},
		}},
		core.View{Style: core.Style{Gap: 4}, Children: children},
	}}
}

// doneCard is the card at the end of a run: its big number counts the tasks that completed, its lines give every task's own message, those needing attention first. A run that could not start says why. 完成 returns the page to idle and the focus to 运行.
func doneCard(s *State, missing func() bool, perm *store.Permission, onGuided func(maintenance.PermissionState), finish func()) core.Node {
	open := func() bool { return s.Phase.Get() == PhaseDone }
	sum := s.Summary.GetUntracked()
	if sum == nil {
		return kit.DoneDialog{Open: open, Title: "维护没有运行", Big: "0 项", Lines: []string{s.RunErr.GetUntracked()}, OnClose: finish,
			TestID: "done"}
	}
	var children []core.Node
	if FullDiskAccessBlocked(sum.Results) {
		children = append(children, core.Dynamic(func(cx *core.Scope) core.Node {
			switch {
			case missing():
				return core.Func(func(*core.Scope) core.Node {
					return shared.PermissionAction{Text: permissionText, OnGuided: onGuided}
				})
			case perm.FullDiskAccess() == maintenance.PermissionGranted && s.Guided.Get():
				return core.Text{Content: grantedText, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5,
					Color: kit.ColorTextMuted}}
			}
			return core.Group{}
		}))
	}
	return kit.DoneDialog{Open: open, Title: DoneTitle(*sum), Big: strconv.Itoa(OKCount(*sum)) + " 项",
		Lines: DoneLines(*sum, s.Catalog.GetUntracked()), Succeeded: OKCount(*sum) > 0, Children: children, OnClose: finish,
		TestID: "done"}
}
