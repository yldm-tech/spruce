package space_test

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/space/spacefake"
)

// listScroll is the scroll view of the list selector names: the first view under it with content.
func (p *page) listScroll(t *testing.T, selector string) *nsgotest.View {
	t.Helper()
	var find func(v *nsgotest.View) *nsgotest.View
	find = func(v *nsgotest.View) *nsgotest.View {
		if v.Content.Height > 0 {
			return v
		}
		for _, c := range v.Children {
			if f := find(c); f != nil {
				return f
			}
		}
		return nil
	}
	v := find(p.Find(selector))
	if v == nil {
		t.Fatalf("no scroll view in %s", selector)
	}
	return v
}

// top and bottom are where the view selector names starts and ends, from the top of the window.
func (p *page) top(selector string) float32 {
	return p.topOf(p.Find(selector))
}

func (p *page) bottom(selector string) float32 {
	return p.top(selector) + float32(p.Find(selector).Frame.Size.Height)
}

// firstRow is the label of the first row of the list selector names that shows whole at its top.
func (p *page) firstRow(t *testing.T, selector string) string {
	t.Helper()
	top := p.top(selector)
	best, at := "", float32(0)
	for _, o := range p.Query(selector + " >> role:option") {
		y := p.topOf(o)
		if y >= top-1 && (best == "" || y < at) {
			best, at = o.Props.Label, y
		}
	}
	return best
}

// topOf is where v starts, from the top of the window.
func (p *page) topOf(v *nsgotest.View) float32 {
	y := float32(v.Frame.Origin.Y)
	for q := v.Parent; q != nil; q = q.Parent {
		y += float32(q.Frame.Origin.Y - q.Offset.Y)
	}
	return y
}

// menuLabels are the labels of the context menu showing, "-" for a separator.
func (p *page) menuLabels() []string {
	var out []string
	for _, it := range p.PopupMenu {
		if it.Separator {
			out = append(out, "-")
			continue
		}
		out = append(out, it.Label)
	}
	return out
}

// TestFolderListLandsWhereItShould: a folder opens at the top of its list, and going back up (Backspace) lands the list where it was, the folder just left chosen.
func TestFolderListLandsWhereItShould(t *testing.T) {
	svc := spacefake.Crowded(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Scroll(homeList+` >> role:option "Library"`, 2400)
	p.Settle()
	if y := p.listScroll(t, homeList).Offset.Y; y < 1000 {
		t.Fatalf("the list scrolled to %v", y)
	}
	before := p.firstRow(t, homeList)
	if before == "" || before == "Library" {
		t.Fatalf("the list shows %q at its top", before)
	}
	// Open a folder row that shows now.
	var target app.SpaceEntry
	for _, e := range svc.Listings[0].Children {
		if e.Kind == app.SpaceDir && p.Exists(homeList+` >> role:button "`+e.Name+`"`) {
			target = e
			break
		}
	}
	if target.Path == "" {
		t.Fatal("no folder row shows")
	}
	inside := app.SpaceListing{Entry: target, Ancestors: []app.SpaceEntry{svc.Listings[0].Entry, target},
		Children: []app.SpaceEntry{spacefake.Entry(900, app.SpaceFile, "inner.bin", target.Size)}}
	inside.Children[0].Path = filepath.Join(target.Path, "inner.bin")
	svc.SetListing(target.ID, inside)
	p.Press(homeList + ` >> role:button "` + target.Name + `"`)
	list := `role:group "` + target.Name + ` 的内容"`
	p.WaitFor(list, wait)
	p.Settle()
	if y := p.listScroll(t, list).Offset.Y; y != 0 {
		t.Errorf("the folder opened scrolled to %v", y)
	}

	p.Key("backspace")
	p.WaitFor(homeList, wait)
	p.Settle()
	if got := p.firstRow(t, homeList); got != before {
		t.Errorf("back up at %q, left at %q", got, before)
	}
	if !p.Find(homeList + ` >> role:option "` + target.Name + `"`).Props.Selected {
		t.Error("the folder just left is not chosen")
	}
}

// TestKeysGoUpAFolder: Backspace, Cmd+↑ and Escape go up a folder; at the scanned location Escape leaves the page alone.
func TestKeysGoUpAFolder(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	projects := `role:group "Projects 的内容"`
	for _, key := range []string{"backspace", "mod+up", "Escape"} {
		p.Press(homeList + ` >> role:button "Projects"`)
		p.WaitFor(projects, wait)
		p.Key(key)
		p.WaitFor(homeList, wait)
	}
	p.Key("Escape")
	p.Settle()
	p.Find(homeList)
}

// TestListTakesTheKeyboard: the folder's rows are one list: after a press on a row the arrows move through it and Return opens a folder; Cmd+Backspace moves the chosen row to the Trash.
func TestListTakesTheKeyboard(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:option "big.iso"`)
	if !p.Find(homeList + ` >> role:option "big.iso"`).Props.Selected {
		t.Fatal("the row pressed is not chosen")
	}
	p.Key("mod+backspace")
	p.WaitFor("id:trash-confirm", wait)
	p.Key("Escape")
	p.WaitForGone("id:trash-confirm", wait)
	p.Press(homeList + ` >> role:option "big.iso"`)
	p.Key("up")
	if !p.Find(homeList + ` >> role:option "Projects"`).Props.Selected {
		t.Fatal("↑ did not choose Projects")
	}
	p.Key("enter")
	p.WaitFor(`role:group "Projects 的内容"`, wait)
	// The row's buttons are no Tab stops of their own.
	for _, b := range p.Query(`role:group "Projects 的内容" >> role:button`) {
		if !b.Props.NoFocus {
			t.Errorf("%q takes the focus", b.Props.Label)
		}
	}
}

// TestRowAndCellMenus: a right click on a row or a cell shows what can be done with it; an entry that cannot be moved says why there.
func TestRowAndCellMenus(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	p.WaitFor(homeList, wait)
	p.RightClick(homeList + ` >> role:option "Projects" >> text:"` + spacefake.Size(6e9) + `"`)
	if got := p.menuLabels(); !slices.Equal(got, []string{"打开", revealItem, "拷贝路径", "-", trashItem}) {
		t.Errorf("folder menu %q", got)
	}
	p.ChoosePopup("拷贝路径")
	if p.Clipboard != spacefake.Projects.Path {
		t.Errorf("clipboard %q", p.Clipboard)
	}
	p.RightClick(homeList + ` >> role:option "Movies" >> text:"` + spacefake.Size(500e6) + `"`)
	got := p.menuLabels()
	if len(got) != 6 || got[4] != trashItem || !p.PopupMenu[4].Disabled || got[5] != spacefake.Movies.Locked {
		t.Errorf("locked menu %q", got)
	}
	p.ChoosePopup(revealItem)
	p.WaitUntil(wait, func() bool { return len(p.files.Reveals()) == 1 })

	p.WaitUntil(wait, func() bool { return len(p.cellLabels()) == 2 })
	p.RightClick(`id:treemap >> role:button "打开 Projects，` + spacefake.Size(6e9) + `"`)
	p.ChoosePopup(trashItem)
	p.WaitFor("id:trash-confirm", wait)
	p.Find("id:trash-confirm >> text:" + confirmText("「Projects」", 6e9) + confirmRestore)
}

// TestFileCellPicksItsRow: a file's cell is no button, but pressing it chooses its row in the list.
func TestFileCellPicksItsRow(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.WaitFor(`id:treemap >> text:"big.iso"`, wait)
	p.Press(`id:treemap >> text:"big.iso"`)
	if !p.Find(homeList + ` >> role:option "big.iso"`).Props.Selected {
		t.Error("the file's row is not chosen")
	}
}

// TestCarelessMovesAskFirst: where a move can be undone, a big folder and a file changed within the hour are still confirmed, naming the size and the change; an old file moves at once.
func TestCarelessMovesAskFirst(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	fresh := spacefake.Data
	fresh.ModTime = time.Now().Add(-5 * time.Minute)
	l := svc.Listings[1]
	l.Children = []app.SpaceEntry{spacefake.App, fresh}
	svc.Listings[1] = l
	p := start(t, svc, undoable(t))
	p.WaitFor(homeList, wait)
	p.Press(homeList + ` >> role:button "Projects"`)
	projects := `role:group "Projects 的内容"`
	p.WaitFor(projects, wait)

	p.Press(moveButton(projects, "app"))
	p.WaitFor("id:trash-confirm", wait)
	p.Find("id:trash-confirm >> text:" + confirmText("「app」", 4e9) + confirmUndo)
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:trash-confirm", wait)

	p.Press(moveButton(projects, "data.bin"))
	p.WaitFor("id:trash-confirm", wait)
	p.Find("id:trash-confirm >> text:" + confirmText("「data.bin」", 2e9) + "它5分钟前还在修改，可能正在使用。" + confirmUndo)
	p.Press("id:confirm-cancel")
	p.WaitForGone("id:trash-confirm", wait)
	if len(svc.Trashes()) != 0 {
		t.Fatalf("moved without asking: %v", svc.Trashes())
	}

	// The two buttons of a row stand 10 points apart.
	reveal := p.Find(revealButton(projects, "app"))
	trash := p.Find(moveButton(projects, "app"))
	if gap := trash.Frame.Origin.X - reveal.Frame.Origin.X - reveal.Frame.Size.Width; gap < 8 {
		t.Errorf("the buttons are %v apart", gap)
	}
}

// TestChosenLargeFilesMoveTogether: rows of 大文件 are chosen with their checkboxes (or Cmd+A), the action bar says how many and how much, and its 移到废纸篓 moves them in one go with one toast, confirmed first since together they come to more than 1 GB.
func TestChosenLargeFilesMoveTogether(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	p := start(t, svc, undoable(t))
	p.WaitFor(homeList, wait)
	p.Press("id:tab-large")
	p.WaitFor(`role:group "大文件"`, wait)
	if p.Exists("id:trash-chosen") {
		t.Fatal("the bar offers a move before anything is chosen")
	}
	p.Press(`role:checkbox "选择 big.iso"`)
	p.Press(`role:checkbox "选择 data.bin"`)
	p.WaitFor("id:trash-chosen", wait)
	p.Find("text:已选择")
	p.Find(`id:page-footer >> text:"2 项"`)
	p.Find(`id:page-footer >> text:"` + spacefake.Size(5e9) + `"`)
	p.Press("id:trash-chosen")
	p.WaitFor("id:trash-confirm", wait)
	p.Find("id:trash-confirm >> text:" + confirmText(" 2 项", 5e9) + confirmUndo)
	if len(svc.Trashes()) != 0 {
		t.Fatalf("moved before the confirmation: %v", svc.Trashes())
	}
	p.Press("id:confirm-ok")
	p.WaitUntil(wait, func() bool { return len(svc.Trashes()) == 1 })
	if got := svc.Trashes()[0]; !slices.Equal(got, []string{spacefake.ISO.Path, spacefake.Data.Path}) {
		t.Errorf("trash %v", got)
	}
	if got := p.waitToast(); got.UndoRun != "run-1" {
		t.Errorf("toast %+v", got)
	}
	p.WaitForGone("id:trash-chosen", wait)

	// Cmd+A after a press on a row chooses them all.
	p.Press(`role:group "大文件" >> role:option "big.iso"`)
	p.Key("mod+a")
	p.WaitFor(`id:page-footer >> text:"2 项"`, wait)
}

// TestOneMoveKeepsTheOthersChosen: moving one large file with its own button leaves the rows chosen before it chosen.
func TestOneMoveKeepsTheOthersChosen(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	clip := spacefake.Entry(8, app.SpaceFile, "clip.mov", 0.6e9)
	svc.Large = append(svc.Large, clip)
	p := start(t, svc, undoable(t))
	p.WaitFor(homeList, wait)
	p.Press("id:tab-large")
	p.WaitFor(`role:group "大文件"`, wait)
	p.Press(`role:checkbox "选择 big.iso"`)
	p.Press(`role:checkbox "选择 data.bin"`)
	p.WaitFor(`id:page-footer >> text:"2 项"`, wait)
	p.Press(moveButton(`role:group "大文件"`, "clip.mov"))
	p.WaitUntil(wait, func() bool { return len(svc.Trashes()) == 1 })
	p.waitToast()
	if got := svc.Trashes()[0]; !slices.Equal(got, []string{clip.Path}) {
		t.Errorf("trash %v", got)
	}
	p.Find(`id:page-footer >> text:"2 项"`)
}

// TestNothingMovesDuringARescan: while the location of the results is scanned again, which refuses a move, the rows' 移到废纸篓 does nothing and the menu's is disabled; Backspace and Cmd+↑ still go up a folder.
func TestNothingMovesDuringARescan(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t), nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	p.WaitFor(homeList, wait)
	projects := `role:group "Projects 的内容"`
	p.Press(homeList + ` >> role:button "Projects"`)
	p.WaitFor(projects, wait)
	p.Press("id:rescan")
	p.WaitFor("id:space-stop", wait)
	p.WaitUntil(wait, func() bool { return len(svc.Scans()) == 1 })

	p.Press(moveButton(projects, "data.bin"))
	p.RightClick(projects + ` >> role:option "data.bin" >> text:"` + spacefake.Size(2e9) + `"`)
	if got := p.menuLabels(); len(got) == 0 || got[len(got)-1] != trashItem || !p.PopupMenu[len(got)-1].Disabled {
		t.Errorf("menu %q", got)
	}
	p.ClosePopup()
	p.Press(projects + ` >> role:option "data.bin"`)
	p.Key("mod+backspace")
	p.Settle()
	if p.Exists("id:trash-confirm") || p.Exists("id:space-notice-error") || len(svc.Trashes()) != 0 {
		t.Fatalf("a move during the scan: confirm %v, trashes %v, %v", p.Exists("id:trash-confirm"), svc.Trashes(), p.Texts())
	}

	for _, key := range []string{"backspace", "mod+up"} {
		p.Key(key)
		p.WaitFor(homeList, wait)
		p.Press(homeList + ` >> role:button "Projects"`)
		p.WaitFor(projects, wait)
	}
	p.Find("id:space-stop")
}

// TestChosenMoveConfirmsWhereItCannotBeUndone: on Windows the move of the files chosen is confirmed, naming how many and how much.
func TestChosenMoveConfirmsWhereItCannotBeUndone(t *testing.T) {
	svc := spacefake.New(time.Now())
	trashed(svc)
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Press("id:tab-large")
	p.WaitFor(`role:group "大文件"`, wait)
	p.Press(`role:checkbox "选择 big.iso"`)
	p.Press(`role:checkbox "选择 data.bin"`)
	p.Press("id:trash-chosen")
	p.WaitFor("id:trash-confirm", wait)
	p.Find("id:trash-confirm >> text:" + confirmText(" 2 项", 5e9) + confirmRestore)
}

// TestLargeFileLinks: a large file's folder opens on the 空间分布 tab, and 设置 opens where the size is set.
func TestLargeFileLinks(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Press("id:tab-large")
	p.WaitFor(`role:group "大文件"`, wait)
	projects := filepath.Join(spacefake.Home, "Projects")
	p.Press(`role:link "在空间分布中打开 ` + projects + `"`)
	p.WaitFor(`role:group "Projects 的内容"`, wait)
	if got := svc.FolderCalls(); got[len(got)-1] != projects {
		t.Errorf("folders %v", got)
	}
	p.Press("id:tab-large")
	p.WaitFor("id:space-large-settings", wait)
	p.Press("id:space-large-settings")
	p.Settle()
	if p.nav.Current() != nav.Settings || p.nav.TakeSettingsTab() != nav.SettingsCleanup {
		t.Errorf("设置 went to %q", p.nav.Current())
	}
}

// TestDiskScanSaysHowFarItIs: a disk's scan fills its ring against the disk's used space, never full before it ends; Escape stops it.
func TestDiskScanSaysHowFarItIs(t *testing.T) {
	svc := &spacefake.Service{Listings: spacefake.Listings(),
		OnScan: func(ctx context.Context, path string, progress func(app.SpaceProgress)) (app.SpaceSummary, error) {
			progress(app.SpaceProgress{Files: 1000, Bytes: 190e9, Path: path})
			<-ctx.Done()
			return app.SpaceSummary{}, ctx.Err()
		}}
	p := start(t, svc, newFakes(t))
	p.WaitFor("id:volume-/", wait)
	// Macintosh HD uses 380 GB.
	p.Press("id:volume-/")
	p.waitTicking("text:已扫描 1,000 个文件 · " + spacefake.Size(190e9))
	if v := p.Find("id:space-scan-ring").Props.Value; v.NoNow || v.Now != 50 {
		t.Errorf("ring %+v", v)
	}
	p.Key("Escape")
	// The stopped scan ends once the service returns; a location pressed before that is ignored, as a scan still runs.
	p.WaitUntil(wait, func() bool {
		if !p.Exists("id:space-scanning") {
			return true
		}
		p.Advance(20 * time.Millisecond)
		return false
	})
	p.WaitFor("id:volume-/", wait)
	if p.Exists("role:alert") {
		t.Errorf("an alert shows: %v", p.Texts())
	}
	// The start screen fades in; a press lands once it has.
	p.Advance(time.Second)
	p.Settle()

	// A folder's size is not known in advance: the ring spins without a figure.
	p.Press("id:target-home")
	p.WaitFor("id:space-scanning", wait)
	if v := p.Find("id:space-scan-ring").Props.Value; !v.NoNow {
		t.Errorf("folder ring %+v", v)
	}
}

// TestActionBarStaysInView: the folders a scan could not read take room from the map, not the action bar, which stays pinned at the bottom of the window.
func TestActionBarStaysInView(t *testing.T) {
	svc := spacefake.New(time.Now())
	svc.Kept.Denied, svc.Kept.DeniedPaths = 3, []string{spacefake.Home + "/Library/Mail"}
	fakes := newFakes(t)
	fakes.Permission = maintenance.PermissionDenied
	p := start(t, svc, fakes, nsgotest.Size(1052, 680))
	p.WaitFor("id:denied-banner", wait)
	p.WaitFor(homeList, wait)
	p.Settle()
	if bottom := p.bottom("id:rescan"); bottom > 680 {
		t.Errorf("重新扫描 ends at %v, below the window", bottom)
	}
	if h := p.Find("id:space-browser").Frame.Size.Height; h < 340 {
		t.Errorf("the browser is %v tall", h)
	}
}

// TestBrowserFillsTheWindow: without a banner the map and the list take the height left under the tabs.
func TestBrowserFillsTheWindow(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Settle()
	if gap := p.top("id:page-footer") - p.bottom("id:space-browser"); gap < 0 || gap > 40 {
		t.Errorf("%v between the browser and the action bar", gap)
	}
	if page := p.Find("id:space-page"); page.Content.Height > page.Frame.Size.Height+1 {
		t.Errorf("the page scrolls: content %v in %v", page.Content.Height, page.Frame.Size.Height)
	}
}

// TestLargeListFits: the 大文件 card is as tall as its few rows, and a long list scrolls inside the height the page leaves it.
func TestLargeListFits(t *testing.T) {
	svc := spacefake.New(time.Now())
	p := start(t, svc, newFakes(t))
	p.WaitFor(homeList, wait)
	p.Press("id:tab-large")
	p.WaitFor(`role:group "大文件"`, wait)
	p.Settle()
	if h := p.bottom("id:space-large") - p.top("id:space-large"); h > 200 {
		t.Errorf("two rows take %v", h)
	}

	many := spacefake.New(time.Now())
	for i := range 120 {
		e := spacefake.Entry(100+i, app.SpaceFile, "file-"+strconv.Itoa(i)+".bin", int64(200-i)*1e7)
		many.Large = append(many.Large, e)
	}
	q := start(t, many, newFakes(t))
	q.WaitFor(homeList, wait)
	q.Press("id:tab-large")
	q.WaitFor(`role:group "大文件"`, wait)
	q.Settle()
	if page := q.Find("id:space-page"); page.Content.Height > page.Frame.Size.Height+1 {
		t.Errorf("the page scrolls: content %v in %v", page.Content.Height, page.Frame.Size.Height)
	}
	if bottom, footer := q.bottom("id:space-large"), q.top("id:page-footer"); bottom > footer {
		t.Errorf("the list ends at %v, under the action bar at %v", bottom, footer)
	}
}
