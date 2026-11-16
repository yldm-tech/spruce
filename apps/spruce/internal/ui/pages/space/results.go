package space

import (
	"context"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/base"
	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/desktop"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/ui/async"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/store"
)

// Tab is the results' view.
type Tab uint8

// The tabs: 空间分布 (the open folder) and 大文件.
const (
	TabMap Tab = iota
	TabLarge
)

// NoticeKind is what a notice reports.
type NoticeKind uint8

// The notices: none, and something that did not work. A move that worked shows the toast instead (TrashToast).
const (
	NoticeNone NoticeKind = iota
	NoticeError
)

// Notice is the line under the tabs after an action that did not work.
type Notice struct {
	Kind NoticeKind
	Text string
}

// A move to the Trash that can be undone still asks first for what is easy to move by mistake: a folder from bigFolder up or right under the scanned location, and a file changed within recentFile, which may be in use.
const (
	bigFolder  = 1e9
	recentFile = time.Hour
)

// Results is the controller of a location's results: the open folder, the tab, the large files and a move to the Trash. Make it in the scope that shows them (NewResults); a newer scan of the same location moves it onto that scan (Rescanned), a scan of another location replaces it. Its methods run on the engine goroutine.
type Results struct {
	cx    *core.Scope
	ctx   context.Context
	page  *State
	svc   Service
	files Files
	moves Moves
	// scanID is the scan shown, root its location.
	scanID, root string

	Tab *core.Signal[Tab]
	// Listing is the open folder, kept while another loads; nil until the first arrives or after an error (ListingErr).
	Listing    *core.Signal[*app.SpaceListing]
	ListingErr *core.Signal[string]
	Fetching   *core.Signal[bool]
	// Large is the scan's large files, nil until the 大文件 tab first shows (or after an error, LargeErr).
	Large    *core.Signal[[]app.SpaceEntry]
	LargeErr *core.Signal[string]
	// Cursor is the row chosen in the 空间分布 list (by entryKey): the keyboard's, a pressed row's, the folder just left's or a treemap cell's.
	Cursor *base.Selection
	// Chosen are the rows chosen in the 大文件 list, to move to the Trash together.
	Chosen *base.Selection
	// List is the 空间分布 list's, for where it is scrolled to.
	List *core.Ref
	// Confirming are the entries the move to the Trash waits to be confirmed for, nil when none.
	Confirming *core.Signal[[]app.SpaceEntry]
	// Trashing is set while a move to the Trash runs, confirmed or not.
	Trashing  *core.Signal[bool]
	Notice    *core.Signal[Notice]
	RevealErr *core.Signal[string]

	listSeq, largeSeq int
	// settle is set after a move to the Trash: the listing and the large files it asks for again move into place (kit.SettleLayout).
	settle bool
	// largeStale is set when a move to the Trash changed the large files while the 大文件 tab was not showing.
	largeStale bool
	// positions are where the list was scrolled to in each folder left, by path, so going back up lands there again.
	positions map[string]core.ListPosition
}

// Moves is how a move to the Trash goes on around the page: whether it asks first and what it does after.
type Moves struct {
	// Undo reports whether a move can be undone from Spruce (macOS with 概览's controller, OverviewState.CanUndo): then it happens at once and the toast offers 撤销, unless it is easy to make by mistake (needsCare); otherwise it is confirmed first.
	Undo func() bool
	// Bump runs after every move (the Trash's size changed).
	Bump func()
	// Toast shows the toast after a move that moved something.
	Toast func(store.Toast)
}

// NewResults makes the controller of the scan sum in cx and opens where the page left the results of its location, at its root the first time.
func NewResults(cx *core.Scope, page *State, sum app.SpaceSummary, files Files, moves Moves) *Results {
	at := place{root: sum.Root.Path}
	if page.place.root == sum.Root.Path {
		at = page.place
	}
	page.place = at
	r := &Results{cx: cx, ctx: cx.Context(), page: page, svc: page.svc, files: files, moves: moves, scanID: sum.ScanID, root: sum.Root.Path,
		Tab:        core.NewSignal(cx, at.tab),
		Listing:    core.NewSignalFunc[*app.SpaceListing](cx, nil, nil),
		ListingErr: core.NewSignal(cx, ""),
		Fetching:   core.NewSignal(cx, false),
		Large:      core.NewSignalFunc[[]app.SpaceEntry](cx, nil, nil),
		LargeErr:   core.NewSignal(cx, ""),
		Cursor:     base.NewSelection(cx, false),
		Chosen:     base.NewSelection(cx, true),
		List:       new(core.Ref),
		Confirming: core.NewSignalFunc[[]app.SpaceEntry](cx, nil, nil),
		Trashing:   core.NewSignal(cx, false),
		Notice:     core.NewSignal(cx, Notice{}),
		RevealErr:  core.NewSignal(cx, ""),
		positions:  map[string]core.ListPosition{},
	}
	if at.folder != "" {
		r.openPath(at.folder)
	} else {
		r.open(sum.Root.ID)
	}
	if at.tab == TabLarge {
		r.loadLarge()
	}
	cx.Effect(func() {
		next := place{root: r.root, folder: page.place.folder, tab: r.Tab.Get()}
		if l := r.Listing.Get(); l != nil {
			next.folder = l.Entry.Path
		}
		page.place = next
	})
	return r
}

// Rescanned moves the results onto sum, a newer scan of their location: the folder open stays open, or the nearest folder above it the scan holds, and the large files are asked for again.
func (r *Results) Rescanned(sum app.SpaceSummary) {
	r.scanID = sum.ScanID
	r.openPath(r.folder())
	if r.Tab.GetUntracked() == TabLarge {
		r.loadLarge()
	} else {
		r.largeStale = true
	}
}

// folder is the path of the folder open, or asked for while none shows.
func (r *Results) folder() string {
	if l := r.Listing.GetUntracked(); l != nil {
		return l.Entry.Path
	}
	if f := r.page.place.folder; f != "" {
		return f
	}
	return r.root
}

// reload asks for the folder shown again.
func (r *Results) reload() {
	if l := r.Listing.GetUntracked(); l != nil {
		r.open(l.Entry.ID)
		return
	}
	r.openPath(r.folder())
}

// open asks for the folder id of the scan shown.
func (r *Results) open(id int) {
	scanID := r.scanID
	r.loadListing(func() (app.SpaceListing, error) { return r.svc.Children(scanID, id) })
}

// openPath asks for the folder at path of the scan shown, or the nearest one above it the scan holds.
func (r *Results) openPath(path string) {
	scanID := r.scanID
	r.loadListing(func() (app.SpaceListing, error) { return r.svc.Folder(scanID, path) })
}

// loadListing asks for a folder with fetch; only the latest answer counts, and the folder shown stays until it comes. Before leaving a folder the list's position in it is kept, and the list then lands where it should in the folder that came (land).
func (r *Results) loadListing(fetch func() (app.SpaceListing, error)) {
	r.listSeq++
	seq := r.listSeq
	r.Fetching.Set(true)
	async.Do(r.cx, r.ctx, func(context.Context) (app.SpaceListing, error) { return fetch() },
		func(l app.SpaceListing, err error) {
			if seq != r.listSeq {
				return
			}
			r.Fetching.Set(false)
			if err != nil {
				r.Listing.Set(nil)
				r.ListingErr.Set(err.Error())
				return
			}
			r.ListingErr.Set("")
			prev := r.Listing.GetUntracked()
			if r.settle {
				r.settle = false
				if prev != nil && prev.Entry.Path == l.Entry.Path {
					kit.SettleLayout(r.cx)
				}
			}
			if prev != nil && prev.Entry.Path != l.Entry.Path {
				if pos, ok := r.List.ListPosition(); ok {
					r.positions[prev.Entry.Path] = pos
				}
			}
			r.Listing.Set(&l)
			if prev != nil {
				r.land(prev.Entry.Path, l)
			}
		})
}

// land puts the list where it belongs in the folder l, opened from the folder at from: back up where it was in a folder above, the row of the folder just left chosen; at the top in a folder further down. The same folder again (a move, a rescan) stays where it is.
func (r *Results) land(from string, l app.SpaceListing) {
	to := l.Entry.Path
	switch {
	case from == to:
		return
	case within(from, to):
		for _, c := range l.Children {
			if c.Kind == app.SpaceDir && within(from, c.Path) {
				r.Cursor.Set(entryKey(c))
				break
			}
		}
		if pos, ok := r.positions[to]; ok {
			r.List.ScrollToIndexAt(pos.Index, 0, -pos.Offset, false)
		}
	default:
		r.Cursor.Clear()
		r.List.ScrollToIndex(0, false)
	}
}

// within reports whether path is dir or inside it.
func within(path, dir string) bool {
	return path == dir || strings.HasPrefix(path, strings.TrimSuffix(dir, string(filepath.Separator))+string(filepath.Separator))
}

// loadLarge asks for the large files.
func (r *Results) loadLarge() {
	r.largeSeq++
	seq, scanID := r.largeSeq, r.scanID
	r.largeStale = false
	async.Do(r.cx, r.ctx, func(context.Context) ([]app.SpaceEntry, error) { return r.svc.LargeFiles(scanID) },
		func(files []app.SpaceEntry, err error) {
			if seq != r.largeSeq {
				return
			}
			if err != nil {
				r.Large.Set(nil)
				r.LargeErr.Set(err.Error())
				return
			}
			r.LargeErr.Set("")
			r.Large.Set(files)
		})
}

// SetTab shows tab, asking for the large files whenever 大文件 shows without them (the first time, after an error) or a move changed them.
func (r *Results) SetTab(t Tab) {
	r.Tab.Set(t)
	if t == TabLarge && (r.largeStale || r.Large.GetUntracked() == nil) {
		r.loadLarge()
	}
}

// Open opens the folder e on the 空间分布 tab.
func (r *Results) Open(e app.SpaceEntry) {
	r.Notice.Set(Notice{})
	r.SetTab(TabMap)
	r.open(e.ID)
}

// OpenParent opens the folder holding e on the 空间分布 tab (a large file's location).
func (r *Results) OpenParent(e app.SpaceEntry) {
	r.Notice.Set(Notice{})
	r.SetTab(TabMap)
	r.openPath(filepath.Dir(e.Path))
}

// Up opens the folder above the open one on the 空间分布 tab, if there is one; it reports whether it did.
func (r *Results) Up() bool {
	l := r.Listing.GetUntracked()
	if r.Tab.GetUntracked() != TabMap || l == nil || len(l.Ancestors) < 2 {
		return false
	}
	r.Open(l.Ancestors[len(l.Ancestors)-2])
	return true
}

// Highlight chooses e's row in the 空间分布 list and scrolls it to the middle of the list (a treemap cell pressed).
func (r *Results) Highlight(e app.SpaceEntry) {
	l := r.Listing.GetUntracked()
	if l == nil {
		return
	}
	key := entryKey(e)
	if i := slices.IndexFunc(l.Children, func(c app.SpaceEntry) bool { return entryKey(c) == key }); i >= 0 {
		r.Cursor.Set(key)
		r.List.ScrollToIndexAt(i, 0.5, 0, true)
	}
}

// Reveal shows e in Finder or Explorer.
func (r *Results) Reveal(e app.SpaceEntry) {
	r.RevealErr.Set("")
	async.Do(r.cx, r.ctx, func(ctx context.Context) (struct{}, error) { return struct{}{}, r.files.Reveal(ctx, e.Path) },
		func(_ struct{}, err error) {
			if err != nil {
				r.RevealErr.Set(err.Error())
			}
		})
}

// Menu is the context menu of e, a cell or a row: 打开 for a folder, 在访达中显示, 拷贝路径 and 移到废纸篓, which goes through RequestTrash; for an entry that cannot be moved the item is disabled and the reason follows it.
func (r *Results) Menu(cx *core.Scope, e app.SpaceEntry) []desktop.MenuItem {
	if IsGroup(e) {
		return nil
	}
	var items []desktop.MenuItem
	if e.Kind == app.SpaceDir {
		items = append(items, desktop.MenuItem{Label: "打开", OnSelect: func() { r.Open(e) }})
	}
	items = append(items, kit.RevealItem(format.FileManagerName(), func() { r.Reveal(e) }), kit.CopyPathItem(cx, e.Path), desktop.MenuItem{Separator: true})
	trash := desktop.MenuItem{Label: "移到" + format.TrashName(), Disabled: e.Locked != "" || r.busy(), OnSelect: func() { r.RequestTrash(e) }}
	items = append(items, trash)
	if e.Locked != "" {
		items = append(items, desktop.MenuItem{Label: e.Locked, Disabled: true})
	}
	return items
}

// Selected are the entries the keyboard's move to the Trash (Mod+Backspace) takes: the 空间分布 list's chosen row, or the 大文件 rows chosen.
func (r *Results) Selected() []app.SpaceEntry {
	if r.Tab.GetUntracked() == TabLarge {
		return r.ChosenEntries()
	}
	l := r.Listing.GetUntracked()
	if l == nil || r.Cursor.Len() == 0 {
		return nil
	}
	return slices.DeleteFunc(slices.Clone(l.Children), func(e app.SpaceEntry) bool { return !r.Cursor.Has(entryKey(e)) || IsGroup(e) })
}

// ChosenEntries are the large files chosen that can be moved, largest first; reading it subscribes.
func (r *Results) ChosenEntries() []app.SpaceEntry {
	var out []app.SpaceEntry
	for _, e := range r.Large.Get() {
		if e.Locked == "" && r.Chosen.Has(entryKey(e)) {
			out = append(out, e)
		}
	}
	return out
}

// Busy reports whether a move to the Trash or a scan runs, while the rows' buttons are dimmed and nothing more can be moved (the service refuses a move during a scan); reading it subscribes.
func (r *Results) Busy() bool { return r.Trashing.Get() || r.page.Scanning.Get() != nil }

// busy is Busy without subscribing.
func (r *Results) busy() bool {
	return r.Trashing.GetUntracked() || r.page.Scanning.GetUntracked() != nil
}

// RequestTrash moves entries to the Trash at once when the move can be undone and nothing in it is easy to make by mistake (needsCare), else asks to confirm it; an entry the page may not move says why instead. Nothing happens while the page is busy (Busy), as the dimmed buttons show.
func (r *Results) RequestTrash(entries ...app.SpaceEntry) {
	if len(entries) == 0 || r.busy() {
		return
	}
	if e := entries[0]; len(entries) == 1 && e.Locked != "" {
		r.Notice.Set(Notice{Kind: NoticeError, Text: "「" + EntryLabel(e) + "」不能移走：" + e.Locked})
		return
	}
	entries = slices.DeleteFunc(slices.Clone(entries), func(e app.SpaceEntry) bool { return e.Locked != "" || IsGroup(e) })
	r.Notice.Set(Notice{})
	now := time.Now()
	if r.undoable() && !slices.ContainsFunc(entries, func(e app.SpaceEntry) bool { return r.needsCare(e, now) }) {
		r.trash(entries)
		return
	}
	r.Confirming.Set(entries)
}

// undoable reports whether a move can be undone from Spruce.
func (r *Results) undoable() bool { return r.moves.Undo != nil && r.moves.Undo() }

// needsCare reports whether moving e asks first even where it can be undone: a folder from bigFolder up or right under the scanned location, or a file changed within recentFile of now.
func (r *Results) needsCare(e app.SpaceEntry, now time.Time) bool {
	switch e.Kind {
	case app.SpaceDir:
		return e.Size >= bigFolder || filepath.Dir(e.Path) == filepath.Clean(r.root)
	case app.SpaceFile:
		return !e.ModTime.IsZero() && now.Sub(e.ModTime) < recentFile
	}
	return false
}

// ConfirmMessage is what the confirmation says about entries: what moves and how much, a file changed lately, and how to get it back.
func (r *Results) ConfirmMessage(entries []app.SpaceEntry, now time.Time) string {
	trash := format.TrashName()
	var size int64
	recent := 0
	var changed time.Time
	for _, e := range entries {
		size += e.Size
		if e.Kind == app.SpaceFile && r.needsCare(e, now) {
			recent++
			changed = e.ModTime
		}
	}
	var b strings.Builder
	if len(entries) == 1 {
		b.WriteString("将把「" + EntryLabel(entries[0]) + "」（" + format.Bytes(size) + "）移到" + trash + "。")
	} else {
		b.WriteString("将把 " + strconv.Itoa(len(entries)) + " 项（" + format.Bytes(size) + "）移到" + trash + "。")
	}
	switch {
	case recent == 1 && len(entries) == 1:
		b.WriteString("它" + format.RelativeTime(changed, now) + "还在修改，可能正在使用。")
	case recent > 0:
		b.WriteString("其中 " + strconv.Itoa(recent) + " 个文件一小时内还在修改，可能正在使用。")
	}
	if r.undoable() {
		b.WriteString("移走后可以撤销。")
	} else {
		b.WriteString("需要时可以从" + trash + "放回原处。")
	}
	return b.String()
}

// CancelTrash closes the confirmation.
func (r *Results) CancelTrash() {
	if !r.Trashing.GetUntracked() {
		r.Confirming.Set(nil)
	}
}

// ConfirmTrash moves the entries waiting for confirmation to the Trash.
func (r *Results) ConfirmTrash() {
	if entries := r.Confirming.GetUntracked(); len(entries) > 0 {
		r.trash(entries)
	}
}

// trash moves entries to the Trash in one call, one move at a time, then corrects the sizes the page shows and says what happened: the toast when something moved, the notice when nothing did.
func (r *Results) trash(entries []app.SpaceEntry) {
	if r.Trashing.GetUntracked() || len(entries) == 0 {
		return
	}
	undo := r.undoable()
	r.Trashing.Set(true)
	scanID := r.scanID
	paths := make([]string, len(entries))
	for i, e := range entries {
		paths[i] = e.Path
	}
	async.Do(r.cx, r.ctx, func(ctx context.Context) (app.SpaceTrashResult, error) {
		return r.svc.Trash(ctx, scanID, paths)
	}, func(res app.SpaceTrashResult, err error) {
		r.Trashing.Set(false)
		r.Confirming.Set(nil)
		if err != nil {
			r.Notice.Set(Notice{Kind: NoticeError, Text: err.Error()})
			return
		}
		r.Chosen.Clear()
		r.settle = true
		r.page.trashed(scanID, res.Root)
		r.page.Moved(res.RunID)
		r.reload()
		if r.Tab.GetUntracked() == TabLarge {
			r.loadLarge()
		} else {
			r.largeStale = true
		}
		if r.moves.Bump != nil {
			r.moves.Bump()
		}
		if res.Items > 0 {
			if r.moves.Toast != nil {
				r.moves.Toast(TrashToast(res, undo))
			}
			return
		}
		reason := "原因不明"
		if len(res.Failed) > 0 {
			reason = res.Failed[0].Reason
		}
		what := "「" + EntryLabel(entries[0]) + "」"
		if len(entries) > 1 {
			what = strconv.Itoa(len(entries)) + " 项都"
		}
		r.Notice.Set(Notice{Kind: NoticeError, Text: what + "没有移走：" + reason})
	})
}
