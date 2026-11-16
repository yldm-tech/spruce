package space

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/async"
	"github.com/yldm-tech/spruce/internal/ui/kit"
	"github.com/yldm-tech/spruce/internal/ui/store"
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

// Results is the controller of one scan's results: the open folder, the tab, the large files and a move to the Trash. Make it in the scope that shows the scan scanID (NewResults), which a new scan replaces; its methods run on the engine goroutine.
type Results struct {
	cx     *core.Scope
	ctx    context.Context
	page   *State
	svc    Service
	files  Files
	moves  Moves
	scanID string

	// Folder is the id of the folder open on the 空间分布 tab.
	Folder *core.Signal[int]
	Tab    *core.Signal[Tab]
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

// NewResults makes the controller of the scan sum in cx and opens where the page left its results, its root the first time.
func NewResults(cx *core.Scope, page *State, sum app.SpaceSummary, files Files, moves Moves) *Results {
	folder, tab := sum.Root.ID, TabMap
	if p := page.place; p.scanID == sum.ScanID {
		folder, tab = p.folder, p.tab
	}
	r := &Results{cx: cx, ctx: cx.Context(), page: page, svc: page.svc, files: files, moves: moves, scanID: sum.ScanID,
		Folder:     core.NewSignal(cx, folder),
		Tab:        core.NewSignal(cx, tab),
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
	r.loadListing()
	if tab == TabLarge {
		r.loadLarge()
	}
	cx.Effect(func() { page.place = place{scanID: sum.ScanID, folder: r.Folder.Get(), tab: r.Tab.Get()} })
	return r
}

// loadListing asks for the open folder; only the latest answer counts.
func (r *Results) loadListing() {
	r.listSeq++
	seq, id := r.listSeq, r.Folder.GetUntracked()
	r.Fetching.Set(true)
	async.Do(r.cx, r.ctx, func(context.Context) (app.SpaceListing, error) { return r.svc.Children(r.scanID, id) },
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
			if r.settle && l.Entry.ID == core.Untracked(r.cx, r.Folder.Get) {
				r.settle = false
				kit.SettleLayout(r.cx)
			}
			r.Listing.Set(&l)
		})
}

// loadLarge asks for the large files.
func (r *Results) loadLarge() {
	r.largeSeq++
	seq := r.largeSeq
	r.largeStale = false
	async.Do(r.cx, r.ctx, func(context.Context) ([]app.SpaceEntry, error) { return r.svc.LargeFiles(r.scanID) },
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
	r.Folder.Set(e.ID)
	r.loadListing()
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
	async.Do(r.cx, r.ctx, func(ctx context.Context) (app.SpaceTrashResult, error) {
		return r.svc.Trash(ctx, r.scanID, []string{entry.Path})
	}, func(res app.SpaceTrashResult, err error) {
		r.Trashing.Set(false)
		r.Confirming.Set(nil)
		if err != nil {
			r.Notice.Set(Notice{Kind: NoticeError, Text: err.Error()})
			return
		}
		r.settle = true
		r.page.trashed(r.scanID, res.Root)
		r.page.Moved(res.RunID)
		r.loadListing()
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
