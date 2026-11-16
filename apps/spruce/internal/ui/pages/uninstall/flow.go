package uninstall

import (
	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
)

// flowKey is what the dialog or overlay on screen is built from; a change of it builds another.
type flowKey struct {
	step Step
	// launched is set once a Windows uninstaller runs (the waiting overlay's title).
	launched bool
	// recheck is what 再次检查 found (a line of the still-installed card).
	recheck string
}

// flow is the dialog or overlay of the step the uninstall is at, over the main area. A dialog that goes with its step does not give the focus back itself, so the page gives it to 卸载 after 取消; after the done card the list lands near the app it was about (State.Close), and keeps the keys it had.
func flow(cx *core.Scope, s *State, actionRef *core.Ref) core.Node {
	windows := s.Windows()
	bin := trashName(windows)
	key := core.NewMemo(cx, func() flowKey {
		k := flowKey{step: s.Step.Get()}
		switch k.step {
		case StepWaiting:
			k.launched = s.Launched.Get()
		case StepNotRemoved:
			k.recheck = s.RecheckErr.Get()
		}
		return k
	})
	at := func(step Step) func() bool { return func() bool { return s.Step.Get() == step } }
	return core.Dynamic(func(cx *core.Scope) core.Node {
		k := key.Get()
		return core.Func(func(cx *core.Scope) core.Node {
			target := s.Target.GetUntracked()
			switch k.step {
			case StepConfirm:
				message := "将打开「" + target.Name + "」自带的卸载程序，请按它的提示完成卸载。它结束后，Spruce 会查找它留下的文件，由你决定是否移到" + bin + "。"
				confirm := "打开卸载程序"
				if !windows {
					var n int
					var size int64
					core.Untracked(cx, func() struct{} {
						n, size = s.Totals()
						size += s.BundleSize()
						return struct{}{}
					})
					message = ConfirmMessage(target.Name, n, size, false)
					confirm = "卸载"
				}
				return kit.ConfirmDialog{Open: at(StepConfirm), Title: "卸载「" + target.Name + "」", Lines: []string{message}, ConfirmLabel: confirm,
					OnConfirm: s.Confirm, OnCancel: func() { s.Cancel(); actionRef.Focus() }, TestID: "confirm"}
			case StepRemoving:
				return kit.Overlay{Open: at(StepRemoving), Title: "正在卸载「" + target.Name + "」…", TestID: "removing",
					Sub: func() string { return "移到" + bin + "，可以随时放回" },
					Ring: kit.ProgressRing{Value: func() (float64, bool) {
						p := s.Progress.Get()
						if p.Total <= 0 {
							return 0, false
						}
						return float64(p.Done) / float64(p.Total), true
					}},
					Lines:    func() []string { return removingLines(s.Progress.Get(), windows) },
					OnCancel: s.Stop}
			case StepWaiting:
				title := "正在打开卸载程序…"
				if k.launched {
					title = "正在等待卸载程序…"
				}
				return kit.Overlay{Open: at(StepWaiting), Title: title, TestID: "waiting", CancelLabel: "不再等待", OnCancel: s.Stop,
					Sub: func() string { return "请在「" + target.Name + "」的卸载窗口中完成操作" },
					Note: func() string {
						if s.Launched.Get() {
							return ""
						}
						return "如果系统请求管理员授权，请在弹出的窗口中确认"
					}}
			case StepNotRemoved:
				first := "已不再等待卸载程序，「" + target.Name + "」仍在已安装的程序中。"
				if s.Waited.GetUntracked() {
					first = "卸载程序已退出，但「" + target.Name + "」仍在已安装的程序中。"
				}
				lines := []string{first, "如果卸载窗口还开着，请在里面完成卸载，再点「再次检查」。"}
				if k.recheck != "" {
					lines = append(lines, k.recheck)
				}
				return kit.DoneDialog{Open: at(StepNotRemoved), Title: "卸载没有完成", Big: "未卸载", Lines: lines, OnClose: s.Close, TestID: "not-removed",
					Children: []core.Node{kit.Button{Variant: kit.ButtonGhost, Disabled: s.Checking.Get, OnPress: s.Recheck, TestID: "recheck",
						TextFunc: func() string {
							if s.Checking.Get() {
								return "正在检查…"
							}
							return "再次检查"
						}}}}
			case StepLeftovers:
				return leftoversDialog(s, at(StepLeftovers))
			case StepDone:
				sum := s.Summary.GetUntracked()
				var undo []core.Node
				if s.UndoRun() != "" {
					undo = []core.Node{kit.Button{Text: "撤销", Variant: kit.ButtonGhost, OnPress: s.Undo, TestID: "done-undo"}}
				}
				return kit.DoneDialog{Open: at(StepDone), Title: sum.Title, Big: sum.Big, Lines: sum.Lines, Succeeded: sum.Succeeded, OnClose: s.Close,
					TestID: "done", Children: undo}
			}
			return core.Group{}
		})
	})
}

// removingLines are the removal overlay's progress line and the item it is at.
func removingLines(p cleanup.Progress, windows bool) []string {
	lines := []string{ProgressLine(p, windows)}
	if p.Path != "" {
		lines = append(lines, format.TruncateMiddle(p.Path, 64))
	}
	return lines
}

// leftoversDialog offers what a Windows program left in the user's folders once its uninstaller removed it. Escape keeps them.
func leftoversDialog(s *State, open func() bool) core.Node {
	windows := s.Windows()
	bin := trashName(windows)
	name := s.Target.GetUntracked().Name
	totals := func() (int, int64) { return CheckedTotals(s.Found.Get().Leftovers, s.FoundChecked.Get()) }
	return kit.Dialog{Open: open, Title: "「" + name + "」留下的文件", OnClose: s.Keep, TestID: "leftovers-dialog", Children: []core.Node{
		core.Text{Content: "卸载程序已移除「" + name + "」。下面是它在你的用户文件夹里留下的文件，勾选的会移到" + bin + "，可以随时放回。",
			Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSubheading, LineHeightMultiple: 1.6, Color: kit.ColorTextNavFooter}},
		core.Scroll{Style: core.Style{FlexShrink: 1, MinHeight: core.Pt(0), AlignSelf: core.AlignStretch}, Children: []core.Node{
			LeftoverList{
				Leftovers: func() []app.UninstallLeftover { return s.Found.Get().Leftovers },
				Checked:   s.FoundChecked.Get,
				OnToggle:  s.ToggleFound,
				OnSet:     func(set map[string]bool) { s.FoundChecked.Set(set) },
				OnReveal:  s.Reveal,
				Windows:   windows,
			},
		}},
		core.View{Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 12, Margin: core.Edges{Top: 10}},
			Children: []core.Node{
				kit.Button{Text: "保留", Variant: kit.ButtonGhost, Large: true, OnPress: s.Keep, TestID: "keep"},
				kit.Button{Variant: kit.ButtonLight, Large: true, OnPress: s.RemoveFound, TestID: "remove-found",
					Disabled: func() bool { n, _ := totals(); return n == 0 },
					TextFunc: func() string {
						_, size := totals()
						return "移到" + bin + "（" + format.BytesIn(size, windows) + "）"
					}},
			}},
	}}
}
