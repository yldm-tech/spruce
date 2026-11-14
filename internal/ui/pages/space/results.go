package space

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/ui/async"
	"github.com/yldm-tech/spruce/internal/ui/format"
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

// The notices: none, a move to the Trash that worked, and something that did not.
const (
	NoticeNone NoticeKind = iota
	NoticeDone
	NoticeError
)

// Notice is the line under the tabs after an action.
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
	bump   func()
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
	Trashing   *core.Signal[bool]
	Notice     *core.Signal[Notice]
	RevealErr  *core.Signal[string]

	listSeq, largeSeq int
	// largeStale is set when a move to the Trash changed the large files while the 大文件 tab was not showing.
	largeStale bool
}

// NewResults makes the controller of the scan sum in cx and opens its root. bump runs after every move to the Trash (the Trash's size changed).
func NewResults(cx *core.Scope, page *State, sum app.SpaceSummary, files Files, bump func()) *Results {
	r := &Results{cx: cx, ctx: cx.Context(), page: page, svc: page.svc, files: files, bump: bump, scanID: sum.ScanID,
		Folder:     core.NewSignal(cx, sum.Root.ID),
		Tab:        core.NewSignal(cx, TabMap),
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

// RequestTrash asks to confirm moving e to the Trash, or says why it cannot move.
func (r *Results) RequestTrash(e app.SpaceEntry) {
	if e.Locked != "" {
		r.Notice.Set(Notice{Kind: NoticeError, Text: "「" + EntryLabel(e) + "」不能移走：" + e.Locked})
		return
	}
	r.Notice.Set(Notice{})
	r.Confirming.Set(&e)
}

// CancelTrash closes the confirmation.
func (r *Results) CancelTrash() {
	if !r.Trashing.GetUntracked() {
		r.Confirming.Set(nil)
	}
}

// ConfirmTrash moves the entry waiting for confirmation to the Trash, then corrects the sizes the page shows and says what happened.
func (r *Results) ConfirmTrash() {
	e := r.Confirming.GetUntracked()
	if e == nil || r.Trashing.GetUntracked() {
		return
	}
	entry := *e
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
		r.page.trashed(r.scanID, res.Root)
		r.loadListing()
		if r.Tab.GetUntracked() == TabLarge {
			r.loadLarge()
		} else {
			r.largeStale = true
		}
		if r.bump != nil {
			r.bump()
		}
		name := EntryLabel(entry)
		if res.Items > 0 {
			r.Notice.Set(Notice{Kind: NoticeDone, Text: "已把「" + name + "」移到" + format.TrashName() + "，清倒" + format.TrashName() + "后可释放 " + format.Bytes(res.Trashed)})
			return
		}
		reason := "原因不明"
		if len(res.Failed) > 0 {
			reason = res.Failed[0].Reason
		}
		r.Notice.Set(Notice{Kind: NoticeError, Text: "「" + name + "」没有移走：" + reason})
	})
}
