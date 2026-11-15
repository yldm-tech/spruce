package systemjunk

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strconv"
	"sync"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/fsx"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/engine/trash"
)

// Env is what the cleaner reads from the machine. New fills in what is left zero for this platform; tests pass temporary folders and fakes.
type Env struct {
	// Home is the user's home folder.
	Home string
	// Getenv reads an environment variable (TEMP, TMP, LOCALAPPDATA on Windows); nil means os.Getenv.
	Getenv func(string) string
	// Downloads is the user's Downloads folder; "" means Home/Downloads, on Windows the Downloads known folder.
	Downloads string
	// TrashPath is the folder the Trash item shows in Finder or Explorer: ~/.Trash, or $Recycle.Bin on the system drive.
	TrashPath string
	// Bin measures and empties the Trash; nil means trash.System().
	Bin trash.Bin
	// Running lists the running apps; nil means the platform's (lsappinfo through Runner on macOS, the process list on Windows).
	Running func(ctx context.Context) (Running, error)
	// Runner runs lsappinfo on macOS when Running is nil.
	Runner execx.Runner
	// Settings returns the current settings, for the exclusions; nil means none.
	Settings func() settings.AppSettings
	// Deleter deletes the items; nil means cleanup.SystemDeleter().
	Deleter cleanup.Deleter
	// Now is the clock; nil means time.Now.
	Now func() time.Time
	// Log receives what the page is not told; nil discards it.
	Log *slog.Logger
	// MayRead reports whether a folder may be read (the background read gate), asked after the exclusions and before the folder is listed; a folder it refuses is skipped silently, neither read nor counted as denied. On macOS it keeps background checks out of the Downloads folder until a foreground read has entered it. Nil reads every folder.
	MayRead func(path string) bool

	// volumes is the folder the mounted volumes appear in, whose Trash folders Bin measures besides TrashPath: /Volumes on macOS, none on Windows. Tests point it at a temporary folder.
	volumes string
}

// Category IDs, stable across scans.
const (
	catAppCaches          = "app-caches"
	catAppCachesRunning   = "app-caches-running"
	catBrowserCaches      = "browser-caches"
	catBrowserRunning     = "browser-caches-running"
	catLogs               = "logs"
	catCrashReports       = "crash-reports"
	catTempFiles          = "temp-files"
	catErrorReports       = "error-reports"
	catIncompleteDownload = "incomplete-downloads"
	catTrash              = "trash"
)

// warnRunningUnknown is the warning of the caches a scan could not sort by running app.
const warnRunningUnknown = "无法确认哪些应用正在运行"

// maxItems caps the items of one category, keeping the largest: the page virtualizes long lists, but tens of thousands of crash reports help nobody. Items left out are not deleted.
const maxItems = 3000

// measureWorkers is how many items are measured at once; each fsx.DirSize walk has sizeWorkers of its own.
const (
	measureWorkers = 4
	sizeWorkers    = 4
)

// ErrBusy is returned by Scan and Clean while another scan or clean of the same Cleaner runs.
var ErrBusy = cleanup.ErrBusy

// Cleaner is the cleanup.Scanner of the 系统垃圾 page. It is safe for concurrent use; one scan or clean runs at a time.
type Cleaner struct {
	env   Env
	specs func(Env) []spec
	exec  *cleanup.Executor

	busy sync.Mutex

	mu sync.Mutex
	// trash is the Trash item of the last scan, nil when it had none or it has been emptied since.
	trash *cleanup.Item
	// trashListed is set when the last scan listed the Trash category, with or without an item.
	trashListed bool
}

var _ cleanup.Scanner = (*Cleaner)(nil)

// New returns the cleaner of this platform.
func New(env Env) *Cleaner {
	return newCleaner(env, platformSpecs)
}

func newCleaner(env Env, specs func(Env) []spec) *Cleaner {
	if env.Getenv == nil {
		env.Getenv = os.Getenv
	}
	if env.Now == nil {
		env.Now = time.Now
	}
	if env.Log == nil {
		env.Log = slog.New(slog.DiscardHandler)
	}
	if env.Bin == nil {
		env.Bin = trash.System()
	}
	fillPlatform(&env)
	if env.Running == nil {
		env.Running = func(context.Context) (Running, error) { return Running{}, nil }
	}
	c := &Cleaner{env: env, specs: specs}
	c.exec = cleanup.NewExecutorWith(env.Deleter, cleanup.ExecutorOptions{Exclusions: c.exclusions})
	return c
}

func (c *Cleaner) exclusions() []string {
	if c.env.Settings == nil {
		return nil
	}
	return c.env.Settings().Cleanup.Exclusions
}

// Scan finds the junk of every category and remembers it for Clean.
func (c *Cleaner) Scan(ctx context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	if !c.busy.TryLock() {
		return cleanup.ScanResult{}, ErrBusy
	}
	defer c.busy.Unlock()
	started := c.env.Now()
	rep := newReporter(progress)

	running, err := c.env.Running(ctx)
	unsure := err != nil
	if err != nil {
		if ctx.Err() != nil {
			return cleanup.ScanResult{}, ctx.Err()
		}
		// Without the list every cache looks idle and starts checked; the scan is still worth finishing, and SafeRemove skips what is held open. The caches the list would have sorted carry a warning, which keeps automatic rules away from them.
		c.env.Log.Warn("list running apps", "err", err)
		running = Running{}
	}
	exclusions := c.exclusions()
	sc := &scanCtx{env: c.env, running: running, now: started, report: rep.path, exclusions: exclusions}
	specs := c.specs(c.env)

	// The Trash is measured beside the other locations.
	var (
		trashCat  *cleanup.Category
		trashItem *cleanup.Item
		trashErr  error
		trashDone = make(chan struct{})
	)
	go func() {
		defer close(trashDone)
		trashCat, trashItem, trashErr = c.trashCategory(ctx)
	}()

	cands := make([][]candidate, len(specs))
	for i, s := range specs {
		if ctx.Err() != nil {
			break
		}
		cands[i] = s.collect(ctx, sc)
	}
	items, err := measure(ctx, sc, cands, exclusions, rep)
	<-trashDone
	if err != nil {
		return cleanup.ScanResult{}, err
	}
	if trashErr != nil {
		return cleanup.ScanResult{}, trashErr
	}

	var cats []cleanup.Category
	seen := map[string]bool{}
	for i, s := range specs {
		for _, cat := range assemble(s, items[i], c.env.Log) {
			// A folder two locations both reach (an odd %TEMP%, a Downloads folder inside a cache) is listed once, in the first category, as the Executor requires.
			cat.Items = slices.DeleteFunc(cat.Items, func(it cleanup.Item) bool {
				dup := seen[it.ID]
				seen[it.ID] = true
				return dup
			})
			if len(cat.Items) > 0 {
				if unsure && (cat.ID == catAppCaches || cat.ID == catBrowserCaches) {
					cat.Warning = warnRunningUnknown
				}
				cats = append(cats, cat)
			}
		}
	}
	if trashCat != nil {
		cats = append(cats, *trashCat)
	}
	res := cleanup.NewResult(cats, started)
	res.Denied = sc.denied.Load()

	remembered := res
	remembered.Categories = slices.DeleteFunc(slices.Clone(res.Categories), func(cat cleanup.Category) bool { return cat.ID == catTrash })
	if err := c.exec.Remember(remembered); err != nil {
		return cleanup.ScanResult{}, err
	}
	c.mu.Lock()
	c.trash = trashItem
	c.trashListed = trashCat != nil
	c.mu.Unlock()
	rep.done()
	return res, nil
}

// assemble turns a spec's measured candidates into its category and, for candidates whose owner is running, its busy sibling. A category without items is left out.
func assemble(s spec, found []candidate, log *slog.Logger) []cleanup.Category {
	main, busy := s.cat, cleanup.Category{}
	if s.busy != nil {
		busy = *s.busy
	}
	for _, cand := range found {
		cat := &main
		if cand.busy && s.busy != nil {
			cat = &busy
		}
		if cand.size == 0 && cat.Deletion == cleanup.DeletePermanent {
			continue // nothing to free
		}
		check := fsx.CheckRemovable
		if cat.Deletion == cleanup.DeleteTrash {
			check = fsx.CheckTrashable
		}
		if err := check(cand.path, cat.Roots); err != nil {
			log.Debug("left out a protected item", "path", cand.path, "err", err)
			continue
		}
		it := cleanup.NewItem(cand.path, cand.info, cand.size)
		it.Note = cand.note
		if it.Dir {
			it.Newest = cand.newest
		}
		cat.Items = append(cat.Items, it)
	}
	var out []cleanup.Category
	for _, cat := range []cleanup.Category{main, busy} {
		if len(cat.Items) == 0 {
			continue
		}
		if len(cat.Items) > maxItems {
			slices.SortStableFunc(cat.Items, func(a, b cleanup.Item) int { return cmp.Compare(b.Size, a.Size) })
			cat.Items = cat.Items[:maxItems]
		}
		out = append(out, cat)
	}
	return out
}

// mayMeasureTrash asks the read gate (Env.MayRead) about every Trash folder Bin.Size reads: TrashPath and, on macOS, the user's .Trashes folder on each mounted volume, which lies in a prompting folder (a removable or network volume). Only the volumes folder itself is listed, which reads nothing on the volumes. Without a gate everything may be read.
func (c *Cleaner) mayMeasureTrash() bool {
	if c.env.MayRead == nil {
		return true
	}
	if !c.env.MayRead(c.env.TrashPath) {
		return false
	}
	if c.env.volumes == "" {
		return true
	}
	vols, err := os.ReadDir(c.env.volumes)
	if err != nil {
		return true // no volumes to read: Bin.Size finds none either
	}
	uid := strconv.Itoa(os.Getuid())
	for _, v := range vols {
		// The startup volume appears there as a symlink to /; its Trash is TrashPath. Bin.Size skips it and anything that is not a folder in the same way.
		if v.Type()&fs.ModeSymlink != 0 || !v.IsDir() {
			continue
		}
		if !c.env.MayRead(filepath.Join(c.env.volumes, v.Name(), ".Trashes", uid)) {
			return false
		}
	}
	return true
}

// binName is what the system calls its Trash.
func binName() string {
	if runtime.GOOS == "windows" {
		return "回收站"
	}
	return "废纸篓"
}

// trashCategory measures the Trash. It returns no category for an empty Trash, and a category without items, explaining why, when the Trash cannot be read. Only a cancelled ctx is an error.
func (c *Cleaner) trashCategory(ctx context.Context) (*cleanup.Category, *cleanup.Item, error) {
	bin := binName()
	cat := cleanup.Category{
		ID:          catTrash,
		Title:       bin,
		Description: bin + "里的全部项目",
		Icon:        "trash",
		Kind:        cleanup.KindTrash,
		Deletion:    cleanup.DeletePermanent,
		Warning:     "清空后无法恢复，默认不选",
		Items:       []cleanup.Item{},
	}
	if !c.mayMeasureTrash() {
		// Measuring would read a Trash folder the read gate refuses; the Trash is left out of this check rather than measured in part.
		return nil, nil, nil
	}
	u, err := c.env.Bin.Size(ctx)
	switch {
	case err != nil && ctx.Err() != nil:
		return nil, nil, ctx.Err()
	case errors.Is(err, fs.ErrPermission):
		cat.Warning = "需要为 Spruce 开启「完全磁盘访问权限」才能读取" + bin + "，开启后重新检查"
		return &cat, nil, nil
	case err != nil:
		c.env.Log.Warn("measure trash", "err", err)
		cat.Warning = "无法读取" + bin + "的大小"
		return &cat, nil, nil
	case u.Items == 0 && u.Bytes == 0:
		return nil, nil, nil
	}
	it := cleanup.Item{ID: cleanup.ItemID(c.env.TrashPath), Path: c.env.TrashPath, Size: u.Bytes, Dir: true, Note: fmt.Sprintf("共 %d 项", u.Items)}
	if info, err := os.Lstat(c.env.TrashPath); err == nil {
		it.ModTime = info.ModTime()
	}
	cat.Items = []cleanup.Item{it}
	return &cat, &it, nil
}

// Clean deletes what req names from the last scan. The Trash, when named, is emptied first, then the other items are handled by the cleanup.Executor; a request that does not match the last scan is rejected before anything is deleted.
func (c *Cleaner) Clean(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	// Checked first: an unknown mode must not empty the Trash, which the Executor's own check does not cover when only the Trash is named.
	if err := cleanup.CheckMode(req.Mode); err != nil {
		return cleanup.CleanResult{}, err
	}
	if !c.busy.TryLock() {
		return cleanup.CleanResult{}, ErrBusy
	}
	defer c.busy.Unlock()
	if progress == nil {
		progress = func(cleanup.Progress) {}
	}
	c.mu.Lock()
	trashItem, trashListed := c.trash, c.trashListed
	c.mu.Unlock()

	emptyTrash, rest, err := splitTrash(req, trashItem, trashListed)
	if err != nil {
		return cleanup.CleanResult{}, err
	}
	if !emptyTrash && len(rest.CategoryIDs) == 0 {
		return cleanup.CleanResult{}, cleanup.ErrNothingSelected
	}
	rest.Mode = req.Mode
	// An automatic clean never empties the Trash: the Trash is refused as one failure and the rest goes on.
	refuseTrash := emptyTrash && req.Mode == cleanup.ModeTrashOnly
	if refuseTrash {
		emptyTrash = false
	}
	if len(rest.CategoryIDs) > 0 {
		// The Executor plans the whole request before it deletes anything and checks ctx before every item, so a cancelled context validates the request without touching a file. Doing that first keeps a stale request from emptying the Trash.
		probe, cancel := context.WithCancel(ctx)
		cancel()
		if _, err := c.exec.Clean(probe, rest, nil); err != nil {
			return cleanup.CleanResult{}, err
		}
	}

	res := cleanup.CleanResult{StartedAt: c.env.Now(), Failed: []cleanup.Failure{}, Categories: []cleanup.CategoryOutcome{}}
	var trashOut *cleanup.CategoryOutcome
	steps := 0
	if refuseTrash {
		res.Failed = append(res.Failed, cleanup.Failure{ItemID: trashItem.ID, CategoryID: catTrash, Path: trashItem.Path, Reason: cleanup.ReasonNoEmptyTrash()})
		trashOut = &cleanup.CategoryOutcome{CategoryID: catTrash, Title: binName(), Failed: 1}
	}
	if emptyTrash {
		steps = 1
		progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, CategoryID: catTrash, Path: trashItem.Path})
		out := c.empty(ctx, *trashItem, &res)
		trashOut = &out
	}
	if len(rest.CategoryIDs) > 0 {
		base := res.Freed
		r, err := c.exec.Clean(ctx, rest, func(p cleanup.Progress) {
			p.Done += steps
			p.Total += steps
			p.Bytes += base
			progress(p)
		})
		if err != nil {
			// Validated above under the same lock; only a scan dropped in between could cause this.
			return cleanup.CleanResult{}, err
		}
		res.Freed += r.Freed
		res.Trashed += r.Trashed
		res.Removed += r.Removed
		res.Skipped += r.Skipped
		res.Failed = append(res.Failed, r.Failed...)
		res.Moved = append(res.Moved, r.Moved...)
		res.Handled = append(res.Handled, r.Handled...)
		res.Categories = append(res.Categories, r.Categories...)
		res.Interrupted = r.Interrupted
	} else {
		progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Done: steps, Total: steps, Bytes: res.Freed})
	}
	// The Trash is the last category of a scan.
	if trashOut != nil {
		res.Categories = append(res.Categories, *trashOut)
	}
	res.Partial = res.Interrupted || len(res.Failed) > 0
	res.Duration = c.env.Now().Sub(res.StartedAt)
	return res, nil
}

// empty empties the Trash into res and returns its category outcome.
func (c *Cleaner) empty(ctx context.Context, it cleanup.Item, res *cleanup.CleanResult) cleanup.CategoryOutcome {
	bin := binName()
	out := cleanup.CategoryOutcome{CategoryID: catTrash, Title: bin}
	u, err := c.env.Bin.Empty(ctx)
	res.Freed += u.Bytes
	out.Freed = u.Bytes
	c.mu.Lock()
	c.trash = nil
	c.mu.Unlock()
	// The Trash item is dropped from the scan whether or not emptying worked, so the caller drops it too.
	res.Handled = append(res.Handled, it.ID)
	if err == nil {
		res.Removed++
		out.Removed = 1
		return out
	}
	reason := "清空" + bin + "没有完成"
	switch {
	case errors.Is(err, fs.ErrPermission):
		reason = "需要为 Spruce 开启「完全磁盘访问权限」才能清空" + bin
	case ctx.Err() != nil:
		reason = "清理被中断，" + bin + "没有清空"
		res.Interrupted = true
	}
	res.Failed = append(res.Failed, cleanup.Failure{ItemID: it.ID, CategoryID: catTrash, Path: it.Path, Reason: reason, Detail: err.Error()})
	out.Failed = 1
	return out
}

// splitTrash takes the Trash out of req: whether to empty it, and the request for the Executor. It keeps the CleanRequest rule that a non-empty ItemIDs deletes only the named items, so taking the Trash's item out never widens the rest into "every item".
func splitTrash(req cleanup.CleanRequest, item *cleanup.Item, listed bool) (bool, cleanup.CleanRequest, error) {
	named := slices.Contains(req.CategoryIDs, catTrash)
	filter := len(req.ItemIDs) > 0
	itemID := ""
	if item != nil {
		itemID = item.ID
	}
	itemNamed := itemID != "" && slices.Contains(req.ItemIDs, itemID)
	switch {
	case named && !listed:
		return false, cleanup.CleanRequest{}, fmt.Errorf("%w: category %q", cleanup.ErrStale, catTrash)
	case itemNamed && !named:
		return false, cleanup.CleanRequest{}, fmt.Errorf("%w: item %q", cleanup.ErrStale, itemID)
	}
	empty := named && item != nil && (!filter || itemNamed)
	rest := cleanup.CleanRequest{CategoryIDs: slices.DeleteFunc(slices.Clone(req.CategoryIDs), func(id string) bool { return id == catTrash })}
	if filter {
		rest.ItemIDs = slices.DeleteFunc(slices.Clone(req.ItemIDs), func(id string) bool { return id == itemID })
		if len(rest.ItemIDs) == 0 {
			rest.CategoryIDs = nil
		}
	}
	if len(rest.CategoryIDs) == 0 {
		if len(rest.ItemIDs) > 0 {
			// Item IDs left over with no category to belong to: the Executor would call them stale too.
			return false, cleanup.CleanRequest{}, fmt.Errorf("%w: item %q", cleanup.ErrStale, rest.ItemIDs[0])
		}
		rest = cleanup.CleanRequest{}
	}
	return empty, rest, nil
}
