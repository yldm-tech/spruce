// Package space is the 空间透视 page: pick a disk or a folder, watch the scan, then browse where the space went (a treemap and a list of the open folder, and the largest files) and move what is not needed to the Trash. Its controller lives with the app (Provide): a scan goes on while another page shows, and coming back shows the page as it was left, the running scan or the results at the folder and tab left open.
package space

import (
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/store"
)

// Page is the 空间透视 page. A link that asked it for a folder (空间变化's 在空间透视中查看, Navigator.GoSpace) starts scanning that folder as it opens; an undo of one of its moves scans its location again.
func Page(cx *core.Scope) core.Node {
	deps := DepsKey.Use(cx)
	s := stateKey.Use(cx)
	if s == nil {
		s = NewState(cx, deps.Space)
	}
	phase := core.NewMemo(cx, s.Phase)
	if n := nav.Use(cx); n != nil {
		if path := n.TakeSpacePath(); path != "" {
			s.ScanPath(path)
		}
	}
	if stores := store.Use(cx); stores != nil {
		// As the page opens and after every undo: an undo of one of its moves may have come while another page showed.
		cx.Effect(func() {
			stores.Overview.LastUndo()
			cx.Untrack(func() { s.Undone(stores.Overview.Undid) })
		})
	}
	// The menu's 重新检查 (Cmd+R) scans the location of the results again.
	nav.UseCommands(cx, nav.Commands{Refresh: s.Rescan, Busy: func() bool { return s.Current.Get() == nil || s.Scanning.Get() != nil }})
	// Back (Escape) stops a scan; the results take it first to go up a folder (resultsView).
	cx.OnBackPress(func() bool {
		if s.Scanning.GetUntracked() == nil {
			return false
		}
		s.Stop()
		return true
	})

	actions := core.Dynamic(func(*core.Scope) core.Node {
		switch phase.Get() {
		case PhaseResults:
			return kit.Button{Text: "更换位置", Variant: kit.ButtonGhost, TestID: "change-location", OnPress: s.ChangeLocation}
		case PhaseStart:
			if sum := s.Current.Get(); sum != nil {
				return kit.Button{Text: "返回「" + sum.Name + "」", Variant: kit.ButtonGhost, TestID: "return-results", OnPress: s.Return}
			}
		}
		return core.Group{}
	})

	body := core.Dynamic(func(*core.Scope) core.Node {
		switch phase.Get() {
		case PhaseStart:
			return core.Func(func(cx *core.Scope) core.Node { return startView(cx, s, deps.Files) })
		case PhaseScanning:
			return core.Func(func(*core.Scope) core.Node { return scanningView(s) })
		case PhaseResults:
			return core.Func(func(cx *core.Scope) core.Node { return resultsPhase(cx, s, deps.Files) })
		}
		return core.Group{}
	})

	// The action bar is pinned under the results, so the totals and 重新扫描 stay in view whatever shows above them.
	footer := core.Dynamic(func(*core.Scope) core.Node {
		if phase.Get() != PhaseResults {
			return core.Group{}
		}
		return core.Func(func(cx *core.Scope) core.Node { return actionBar(cx, s) })
	})

	return kit.Page{TestID: "space-page", Header: kit.PageHeader{Route: nav.Space, Actions: []core.Node{actions}}, Children: []core.Node{body}, Footer: footer}
}

// scanningView takes the page's place while a scan of a new location runs: the ring, which fills against the disk's used space for a disk and spins for a folder (a walk cannot know its length in advance), the files and space counted so far, the folder being read and 停止 (Escape too).
func scanningView(s *State) core.Node {
	req := s.Scanning.GetUntracked()
	title := "正在分析「" + req.Name + "」…"
	return core.View{Role: core.RoleGroup, Label: title, Busy: func() bool { return true }, TestID: "space-scanning",
		Style: core.Style{FlexGrow: 1, FlexShrink: 1, AlignItems: core.AlignCenter, JustifyContent: core.JustifyCenter, Gap: 12, Padding: core.Symmetric(24, 0)},
		Children: []core.Node{
			kit.ProgressRing{Size: 120, Label: title, Value: s.Fraction, TestID: "space-scan-ring", Children: []core.Node{
				kit.Tabular(core.Text{ContentFunc: s.Percent, TestID: "space-scan-percent",
					Style: core.Style{FontFamily: kit.FontFace(kit.Bold), FontSize: kit.FontCardTitle, FontWeight: kit.FaceWeight(kit.Bold), Color: kit.ColorText}}),
			}},
			kit.Text(title, kit.FontTitle, kit.Bold, kit.ColorText),
			core.Text{ContentFunc: func() string {
				p := s.Progress.Get()
				if p == nil {
					return "正在准备…"
				}
				return "已扫描 " + FileCount(p.Files) + " · " + format.Bytes(p.Bytes)
			}, Style: core.Style{MaxWidth: core.Pt(440), TextAlign: core.TextAlignCenter, LineHeightMultiple: 1.5, FontFamily: kit.FontSans,
				FontSize: kit.FontSubheading, FontVariant: core.FontTabularNums, Color: kit.ColorTextOverlaySub}},
			core.Text{ContentFunc: func() string {
				if p := s.Progress.Get(); p != nil && p.Path != "" {
					return format.TruncateMiddle(p.Path, 64)
				}
				return " "
			}, MaxLines: 1, EllipsizeMode: core.EllipsizeTail, TestID: "space-scan-path",
				Style: core.Style{MaxWidth: core.Pt(520), FontFamily: kit.FontSans, FontSize: kit.FontBody, FontVariant: core.FontTabularNums,
					Color: kit.ColorTextMuted}},
			kit.Button{Text: "停止", Variant: kit.ButtonGhost, TestID: "space-stop", OnPress: s.Stop},
		}}
}

// resultsPhase is the page after a scan: why a scan failed, if one did, and the scan's results. One controller shows the location's results while the page does; a newer scan of the location moves it onto that scan where it was (Results.Rescanned), and while that scan runs the results stay, dimmed.
func resultsPhase(cx *core.Scope, s *State, files Files) core.Node {
	stores := store.Use(cx)
	moves := Moves{Undo: func() bool { return stores.Overview.State().CanUndo }, Bump: stores.Trash.Bump, Toast: stores.Toasts.Show}
	scanID := core.NewMemo(cx, func() string {
		if sum := s.Current.Get(); sum != nil {
			return sum.ScanID
		}
		return ""
	})
	summary := s.summary
	has := core.NewMemo(cx, func() bool { return scanID.Get() != "" })
	return core.Group{
		kit.ErrorLine{Text: s.Err.Get, TestID: "space-error"},
		core.Dynamic(func(*core.Scope) core.Node {
			if !has.Get() {
				return core.Group{}
			}
			return core.Func(func(cx *core.Scope) core.Node {
				r := NewResults(cx, s, core.Untracked(cx, summary), files, moves)
				s.results.Set(r)
				cx.Cleanup(func() { s.results.Set(nil) })
				cx.Effect(func() {
					if id := scanID.Get(); id != r.scanID {
						cx.Untrack(func() { r.Rescanned(summary()) })
					}
				})
				return resultsView(cx, r, summary)
			})
		}),
	}
}

// barMode is what the action bar under the results shows.
type barMode uint8

// The bar shows the results' totals with 重新扫描, a scan of their location running with 停止, or the large files chosen with 移到废纸篓.
const (
	barResults barMode = iota
	barRescanning
	barChosen
)

// actionBar is the bar pinned under the results: resultsBar, rescanningBar while their location is scanned again, or chosenBar while rows of 大文件 are chosen.
func actionBar(cx *core.Scope, s *State) core.Node {
	summary := s.summary
	mode := core.NewMemo(cx, func() barMode {
		switch r := s.results.Get(); {
		case s.InPlace():
			return barRescanning
		case r != nil && r.Tab.Get() == TabLarge && len(r.ChosenEntries()) > 0:
			return barChosen
		}
		return barResults
	})
	return core.Dynamic(func(*core.Scope) core.Node {
		switch mode.Get() {
		case barRescanning:
			return core.Func(func(*core.Scope) core.Node { return rescanningBar(s, summary) })
		case barChosen:
			return core.Func(func(cx *core.Scope) core.Node { return chosenBar(core.Untracked(cx, s.results.Get)) })
		}
		return core.Func(func(*core.Scope) core.Node { return resultsBar(s, summary) })
	})
}

// chosenBar is the action bar while rows of 大文件 are chosen: how many, 移到废纸篓 for all of them in one move, and their size.
func chosenBar(r *Results) core.Node {
	return kit.ActionBar{
		Start: func() kit.Stat {
			return kit.Stat{Label: "已选择", Value: strconv.Itoa(len(r.ChosenEntries())) + " 项"}
		},
		End: func() kit.Stat {
			var size int64
			for _, e := range r.ChosenEntries() {
				size += e.Size
			}
			return kit.Stat{Label: "共", Value: format.Bytes(size)}
		},
		Button: kit.ActionButton{Label: "移到" + format.TrashName(), TestID: "trash-chosen", Disabled: r.Trashing.Get,
			OnPress: func() { r.RequestTrash(r.ChosenEntries()...) }},
	}
}

// resultsBar is the action bar under the results: the scanned location's size, 重新扫描 and when it was scanned.
func resultsBar(s *State, summary func() app.SpaceSummary) core.Node {
	return kit.ActionBar{
		Start: func() kit.Stat {
			sum := summary()
			return kit.Stat{Label: sum.Name, Value: format.Bytes(sum.Root.Size)}
		},
		End: func() kit.Stat {
			when := format.RelativeTime(summary().ScannedAt, time.Now())
			if when == "" {
				when = "刚刚"
			}
			return kit.Stat{Label: "上次扫描", Value: when}
		},
		Button: kit.ActionButton{Label: "重新扫描", TestID: "rescan", OnPress: s.Rescan},
	}
}

// rescanningBar is the action bar while the location of the results is scanned again: what the scan counted, 停止 and how far it is against the last scan's size.
func rescanningBar(s *State, summary func() app.SpaceSummary) core.Node {
	return kit.ActionBar{
		Start: func() kit.Stat {
			counted := "正在准备…"
			if p := s.Progress.Get(); p != nil {
				counted = format.Bytes(p.Bytes)
			}
			return kit.Stat{Label: summary().Name, Value: counted}
		},
		End: func() kit.Stat {
			return kit.Stat{Label: "正在重新扫描", Value: s.Percent()}
		},
		Button: kit.ActionButton{Label: "停止", TestID: "space-stop", OnPress: s.Stop},
	}
}

// resultsView is one scan's results: the banner for folders it could not read, the 空间分布 and 大文件 tabs with what they show, the notices, and the confirmation before a move to the Trash that cannot be undone from Spruce (Windows) or is easy to make by mistake. Back (Escape, Cmd+[), Backspace and Cmd+↑ go up a folder; Cmd+Backspace moves what is chosen to the Trash.
func resultsView(cx *core.Scope, r *Results, summary func() app.SpaceSummary) core.Node {
	stores := store.Use(cx)
	navigator := nav.Use(cx)
	// The confirmation gives the focus back to the 移到废纸篓 button that opened it, through this Ref pointed at that button; a move from a menu or a key has none.
	opener := new(core.Ref)
	actions := EntryActions{
		OnOpen:   r.Open,
		OnReveal: r.Reveal,
		OnTrash: func(e app.SpaceEntry, ref *core.Ref) {
			*opener = *ref
			r.RequestTrash(e)
		},
		OnOpenParent: r.OpenParent,
		Menu: func(cx *core.Scope, e app.SpaceEntry) []desktop.MenuItem {
			*opener = core.Ref{}
			return r.Menu(cx, e)
		},
	}
	// The keys act on the results while no dialog holds the window (a binding stays live under a newer layer that does not bind the same key).
	free := func() bool {
		return (stores == nil || !stores.Block.Blocked()) && r.page.Scanning.GetUntracked() == nil
	}
	up := func() {
		if free() {
			r.Up()
		}
	}
	cx.OnBackPress(func() bool { return free() && r.Up() })
	cx.OnKeyWith("backspace", core.KeyOptions{When: core.KeyNotEditing}, up)
	cx.OnKey("mod+up", up)
	cx.OnKey("mod+backspace", func() {
		if free() {
			*opener = core.Ref{}
			r.RequestTrash(r.Selected()...)
		}
	})

	tabs := kit.Tabs[Tab]{Label: "查看方式", Value: r.Tab.Get, OnChange: r.SetTab,
		Options: []kit.Option[Tab]{{Value: TabMap, Label: "空间分布", TestID: "tab-map"}, {Value: TabLarge, Label: "大文件", TestID: "tab-large"}}}

	noticeErr := kit.ErrorLine{TestID: "space-notice-error", Text: func() string {
		if n := r.Notice.Get(); n.Kind == NoticeError {
			return n.Text
		}
		return ""
	}}
	revealErr := kit.ErrorLine{Text: r.RevealErr.Get, TestID: "space-reveal-error"}

	// A tab's panel fades in as it shows and takes the height the page leaves it.
	panel := core.Style{FlexGrow: 1, FlexShrink: 0}
	mapPanel := core.Show{When: func() bool { return r.Tab.Get() == TabMap }, Then: func(cx *core.Scope) core.Node {
		return kit.Appear{Entrance: kit.EnterFade, Style: panel, Children: []core.Node{mapView(cx, r, actions)}}
	}}
	largePanel := core.Show{When: func() bool { return r.Tab.Get() == TabLarge }, Then: func(*core.Scope) core.Node {
		settings := func() {
			if navigator != nil {
				navigator.GoSettings(nav.SettingsCleanup)
			}
		}
		return kit.Appear{Entrance: kit.EnterFade, Style: panel, Children: []core.Node{largeView(r, actions, summary, settings)}}
	}}

	trash := format.TrashName()
	confirm := kit.ConfirmDialog{Open: func() bool { return r.Confirming.Get() != nil }, Title: "移到" + trash, ConfirmLabel: "移到" + trash,
		Busy: r.Trashing.Get, OnConfirm: r.ConfirmTrash, OnCancel: r.CancelTrash, ReturnFocus: opener, TestID: "trash-confirm",
		Message: core.Text{ContentFunc: func() string {
			entries := r.Confirming.Get()
			if len(entries) == 0 {
				return ""
			}
			return r.ConfirmMessage(entries, time.Now())
		}}}

	// While the location is scanned again, the results that are about to change fade back.
	return core.View{TestID: "space-results", Busy: func() bool { return r.Trashing.Get() || r.page.InPlace() },
		StyleFunc: func() core.Style {
			st := core.Style{FlexGrow: 1, FlexShrink: 0, Gap: 14, Transition: core.Transition{Duration: 200 * time.Millisecond, Easing: core.Ease, Properties: core.TransitionOpacity}}
			if r.page.InPlace() {
				st.Opacity = core.Alpha(0.5)
			}
			return st
		},
		Children: []core.Node{
			core.Show{When: func() bool { return summary().Denied > 0 }, Then: func(cx *core.Scope) core.Node { return deniedBanner(cx, summary, r.page.Rescan) }},
			tabs, noticeErr, revealErr, mapPanel, largePanel, confirm,
		}}
}

// mapView is the 空间分布 tab: the path to the open folder, then the folder's treemap beside the list of its children.
func mapView(cx *core.Scope, r *Results, actions EntryActions) core.Node {
	listing := func() app.SpaceListing {
		if l := r.Listing.Get(); l != nil {
			return *l
		}
		return app.SpaceListing{}
	}
	has := func() bool { return r.Listing.Get() != nil }
	children := func() []app.SpaceEntry { return listing().Children }
	total := func() int64 { return listing().Entry.Size }
	folderName := func() string { return listing().Entry.Name }
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 0, Gap: 12}, Children: []core.Node{
		core.Show{When: has, Then: func(*core.Scope) core.Node { return breadcrumb(r, listing) }},
		core.Show{When: func() bool { return r.ListingErr.Get() != "" }, Then: func(*core.Scope) core.Node {
			return core.View{Role: core.RoleAlert, Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 12},
				Children: []core.Node{
					core.Text{ContentFunc: r.ListingErr.Get, TestID: "space-listing-error",
						Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, LineHeightMultiple: 1.5, Color: kit.ColorWarning}},
					kit.Button{Text: "重新扫描", Variant: kit.ButtonGhost, OnPress: r.page.Rescan},
				}}
		}},
		core.Show{When: func() bool { return has() && len(children()) == 0 }, Then: func(*core.Scope) core.Node {
			return core.Dynamic(func(*core.Scope) core.Node {
				if listing().Entry.Denied {
					return kit.EmptyState{Title: "无权读取这个文件夹", Description: "Spruce 没有读取它的权限，它的大小没有算进结果", Tone: kit.ToneCyan, TestID: "space-empty"}
				}
				return kit.EmptyState{Title: "这个文件夹是空的", Tone: kit.ToneCyan, TestID: "space-empty"}
			})
		}},
		core.Show{When: func() bool { return has() && len(children()) > 0 }, Then: func(cx *core.Scope) core.Node {
			// Opening another folder fades the map and the list in again.
			fade := kit.FadeOnChange(cx, func() int { return listing().Entry.ID })
			return core.View{Busy: r.Fetching.Get, TestID: "space-browser",
				StyleFunc: func() core.Style {
					// The map and the list share one row that takes the height the page leaves them, never less than 340 points; the list scrolls inside it. A height of 0 to grow from keeps the list's rows out of the height the page measures.
					return core.Style{Display: core.DisplayGrid, GridTemplateColumns: "minmax(0, 3fr) minmax(300px, 2fr)", GridTemplateRows: "minmax(0, 1fr)", Gap: 14,
						FlexGrow: 1, FlexShrink: 0, FlexBasis: core.Pt(0), Height: core.Pt(0), MinHeight: core.Pt(340), Animated: fade}
				},
				Children: []core.Node{
					Treemap{Entries: children, Total: total, Label: folderName, OnOpen: actions.OnOpen, OnPick: r.Highlight, Menu: actions.Menu},
					core.View{TestID: "space-list-box",
						Style: core.Style{MinWidth: core.Pt(0), MinHeight: core.Pt(0), CornerRadius: kit.RadiusCard, Background: kit.SurfaceCard, BorderWidth: 1, BorderColor: kit.BorderCard,
							Overflow: core.Hidden},
						Children: []core.Node{EntryList{Label: func() string { return folderName() + " 的内容" }, Entries: children, Detail: DetailShare,
							Total: total, Actions: actions, Selection: r.Cursor, Busy: r.Trashing.Get, Ref: r.List, TestID: "space-list"}}},
				}}
		}},
	}}
}

// breadcrumb is the path from the scanned location to the open folder, each folder above it a link, and the open folder's size.
func breadcrumb(r *Results, listing func() app.SpaceListing) core.Node {
	sep := func() core.Node {
		return core.Text{Content: "›", Style: core.Style{Margin: core.Symmetric(0, 4), FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorTextSection}}
	}
	crumbs := core.Dynamic(func(*core.Scope) core.Node {
		anc := listing().Ancestors
		var out core.Group
		for i, a := range anc {
			if i > 0 {
				out = append(out, sep())
			}
			if i == len(anc)-1 {
				out = append(out, core.Text{Content: a.Name, TestID: "space-path-current",
					Style: core.Style{Padding: core.Symmetric(2, 4), FontFamily: kit.FontFace(kit.Semibold), FontSize: kit.FontBody,
						FontWeight: kit.FaceWeight(kit.Semibold), Color: kit.ColorText}})
				continue
			}
			out = append(out, kit.Button{Text: a.Name, Variant: kit.ButtonLink, TestID: "crumb-" + strconv.Itoa(a.ID), OnPress: func() { r.Open(a) }})
		}
		return out
	})
	return core.View{Role: core.RoleNavigation, Label: "位置", TestID: "space-path",
		Style: core.Style{Direction: core.Row, AlignItems: core.AlignCenter, Gap: 12, MinHeight: core.Pt(28)},
		Children: []core.Node{
			core.View{Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, Gap: 2, FlexShrink: 1, MinWidth: core.Pt(0)},
				Children: []core.Node{crumbs}},
			kit.Tabular(core.Text{ContentFunc: func() string { return format.Bytes(listing().Entry.Size) },
				Style: core.Style{AutoMargin: core.SideLeft, FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorTextSecondary}}),
		}}
}

// largeView is the 大文件 tab: what it lists, with a link to 设置 where its size is set (settings), and the files, largest first, any number of them chosen to move together.
func largeView(r *Results, actions EntryActions, summary func() app.SpaceSummary, settings func()) core.Node {
	large := func() []app.SpaceEntry { return r.Large.Get() }
	note := core.Style{FontFamily: kit.FontSans, FontSize: kit.FontBody, Color: kit.ColorTextMuted}
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 0, Gap: 12}, Children: []core.Node{
		core.View{TestID: "space-large-note", Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter}, Children: []core.Node{
			core.Text{ContentFunc: func() string {
				sum := summary()
				return "「" + sum.Name + "」中 " + format.Bytes(sum.LargeFileMin) + " 以上的文件，最多列出 200 个。可以在"
			}, Style: note},
			kit.Link{Text: func() string { return "设置" }, OnPress: settings, TestID: "space-large-settings"},
			core.Text{Content: "中调整大小。", Style: note},
		}},
		kit.ErrorLine{Text: r.LargeErr.Get, TestID: "space-large-error"},
		core.Show{When: func() bool { l := large(); return l != nil && len(l) == 0 }, Then: func(*core.Scope) core.Node {
			return kit.EmptyState{Title: "没有找到大文件", Description: "这里没有达到这个大小的文件", Tone: kit.ToneCyan, TestID: "space-large-empty"}
		}},
		core.Show{When: func() bool { return len(large()) > 0 }, Then: func(*core.Scope) core.Node {
			// The list's card is as tall as its rows, up to the height the page leaves it (a height of 0 to grow from, as the map's row), where it scrolls.
			return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 0, FlexBasis: core.Pt(0), Height: core.Pt(0), MinHeight: core.Pt(340)},
				Children: []core.Node{core.View{Style: core.Style{MaxHeight: core.Pct(100), CornerRadius: kit.RadiusCard,
					Background: kit.SurfaceCard, BorderWidth: 1, BorderColor: kit.BorderCard, Overflow: core.Hidden},
					Children: []core.Node{EntryList{Label: func() string { return "大文件" }, Entries: large, Detail: DetailLocation,
						Actions: actions, Selection: r.Chosen, Checks: true, Busy: r.Trashing.Get, TestID: "space-large"}}}}}
		}},
	}}
}

// deniedBanner says how many folders the scan could not read and, on macOS without Full Disk Access, offers 去授权 (the same guide as the 维护 page); once granted it offers 重新扫描.
func deniedBanner(cx *core.Scope, summary func() app.SpaceSummary, rescan func()) core.Node {
	perm := store.Use(cx).Permission
	// The state is read again whenever the banner shows, as the React page's query did.
	perm.Recheck()
	guided := core.NewSignal(cx, false)
	failed := core.NewSignal(cx, false)
	access := perm.FullDiskAccess
	granted := func() bool { return guided.Get() && access() == maintenance.PermissionGranted }
	sub := func() string {
		switch {
		case granted():
			return "已获得完全磁盘访问权限，重新扫描即可算进这些文件夹"
		case access() == maintenance.PermissionDenied:
			return "开启「完全磁盘访问权限」后重新扫描，就能算进这些文件夹"
		}
		return "这些文件夹的大小没有算进结果"
	}
	text := []core.Node{
		kit.TextFunc(func() string { return format.Thousands(summary().Denied) + " 个文件夹无权读取" }, kit.FontCardTitle, kit.Semibold, kit.ColorText),
		kit.TextFunc(sub, kit.FontBody, kit.Regular, kit.ColorTextSecondary),
		core.Show{When: func() bool { return len(summary().DeniedPaths) > 0 }, Then: func(*core.Scope) core.Node {
			return core.View{Tooltip: core.Dynamic(func(*core.Scope) core.Node { return kit.Tooltip(strings.Join(summary().DeniedPaths, "\n")) }), Children: []core.Node{
				core.Text{ContentFunc: func() string { return "例如 " + format.TruncateMiddle(summary().DeniedPaths[0], 60) }, MaxLines: 1,
					EllipsizeMode: core.EllipsizeTail, Style: core.Style{FontFamily: kit.FontSans, FontSize: kit.FontSmall, Color: kit.ColorTextMuted}},
			}}
		}},
	}
	return core.View{TestID: "denied-banner",
		Style: core.Style{Direction: core.Row, Wrap: core.Wrap, AlignItems: core.AlignCenter, RowGap: 12, ColumnGap: 16, Padding: core.Symmetric(14, 18),
			CornerRadius: kit.RadiusCard, Background: kit.SurfaceBanner, BorderWidth: 1, BorderColor: kit.BorderBanner},
		Children: []core.Node{
			core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, FlexBasis: core.Pt(280), MinWidth: core.Pt(0), Gap: 2}, Children: text},
			core.Show{When: func() bool { return access() == maintenance.PermissionDenied && !granted() }, Then: func(*core.Scope) core.Node {
				return kit.Button{Variant: kit.ButtonAccent, TestID: "grant-full-disk-access", Disabled: perm.Guiding,
					TextFunc: func() string {
						if perm.Guiding() {
							return "等待授权…"
						}
						return "去授权"
					},
					OnPress: func() {
						failed.Set(false)
						perm.Guide(func(_ maintenance.PermissionState, err error) {
							failed.Set(err != nil)
							guided.Set(err == nil)
						})
					}}
			}},
			core.Show{When: granted, Then: func(*core.Scope) core.Node {
				return kit.Button{Text: "重新扫描", Variant: kit.ButtonGhost, TestID: "denied-rescan", OnPress: rescan}
			}},
			kit.ErrorLine{Text: func() string {
				if failed.Get() {
					return "无法打开系统设置"
				}
				return ""
			}},
		}}
}
