package space

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/ui/async"
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
	// Confirming is the entry the move to the Trash waits to be confirmed for, nil when none.
	Confirming *core.Signal[*app.SpaceEntry]
	// Trashing is set while a move to the Trash runs, confirmed or not.
	Trashing  *core.Signal[bool]
	Notice    *core.Signal[Notice]
	RevealErr *core.Signal[string]

	listSeq, largeSeq int
	// settle is set after a move to the Trash: the listing and the large files it asks for again move into place (kit.SettleLayout).
	settle bool
	// largeStale is set when a move to the Trash changed the large files while the 大文件 tab was not showing.
	largeStale bool
}

// Moves is how a move to the Trash goes on around the page: whether it asks first and what it does after.
type Moves struct {
	// Undo reports whether a move can be undone from Spruce (macOS with 概览's controller, OverviewState.CanUndo): then it happens at once and the toast offers 撤销; otherwise it is confirmed first.
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
		Confirming: core.NewSignal[*app.SpaceEntry](cx, nil),
		Trashing:   core.NewSignal(cx, false),
		Notice:     core.NewSignal(cx, Notice{}),
		RevealErr:  core.NewSignal(cx, ""),
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

// loadListing asks for a folder with fetch; only the latest answer counts, and the folder shown stays until it comes.
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
			if r.settle {
				r.settle = false
				if cur := r.Listing.GetUntracked(); cur != nil && cur.Entry.Path == l.Entry.Path {
					kit.SettleLayout(r.cx)
				}
			}
			r.Listing.Set(&l)
		})
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

// RequestTrash moves e to the Trash at once when the move can be undone, else asks to confirm it; an entry the page may not move says why instead.
func (r *Results) RequestTrash(e app.SpaceEntry) {
	if e.Locked != "" {
		r.Notice.Set(Notice{Kind: NoticeError, Text: "「" + EntryLabel(e) + "」不能移走：" + e.Locked})
		return
	}
	r.Notice.Set(Notice{})
	if r.moves.Undo != nil && r.moves.Undo() {
		r.trash(e)
		return
	}
	r.Confirming.Set(&e)
}

// CancelTrash closes the confirmation.
func (r *Results) CancelTrash() {
	if !r.Trashing.GetUntracked() {
		r.Confirming.Set(nil)
	}
}

// ConfirmTrash moves the entry waiting for confirmation to the Trash.
func (r *Results) ConfirmTrash() {
	if e := r.Confirming.GetUntracked(); e != nil {
		r.trash(*e)
	}
}

// trash moves entry to the Trash, one move at a time, then corrects the sizes the page shows and says what happened: the toast when something moved, the notice when nothing did.
func (r *Results) trash(entry app.SpaceEntry) {
	if r.Trashing.GetUntracked() {
		return
	}
	undo := r.moves.Undo != nil && r.moves.Undo()
	r.Trashing.Set(true)
	scanID := r.scanID
	async.Do(r.cx, r.ctx, func(ctx context.Context) (app.SpaceTrashResult, error) {
		return r.svc.Trash(ctx, scanID, []string{entry.Path})
	}, func(res app.SpaceTrashResult, err error) {
		r.Trashing.Set(false)
		r.Confirming.Set(nil)
		if err != nil {
			r.Notice.Set(Notice{Kind: NoticeError, Text: err.Error()})
			return
		}
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
		r.Notice.Set(Notice{Kind: NoticeError, Text: "「" + EntryLabel(entry) + "」没有移走：" + reason})
	})
}
