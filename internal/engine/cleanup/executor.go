package cleanup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/fsx"
	"github.com/yldm-tech/spruce/internal/engine/trash"
)

// Errors of Executor.Remember and Executor.Clean.
var (
	// ErrBusy is returned while another Clean of the same Executor runs.
	ErrBusy = errors.New("cleanup: a clean is already in progress")
	// ErrNoScan is returned by Clean before any scan was remembered.
	ErrNoScan = errors.New("cleanup: nothing has been scanned")
	// ErrStale is returned (wrapped, naming the ID) for a category or item ID the last scan does not have.
	ErrStale = errors.New("cleanup: the request does not match the last scan")
	// ErrNothingSelected is returned for a request that names no items.
	ErrNothingSelected = errors.New("cleanup: nothing selected")
	// ErrInvalidScan is returned (wrapped) by Remember for a scan with duplicate or empty IDs or an unknown deletion mode.
	ErrInvalidScan = errors.New("cleanup: invalid scan result")
	// ErrBadMode is returned (wrapped) by Clean for a request whose Mode is neither ModeDefault nor ModeTrashOnly.
	ErrBadMode = errors.New("cleanup: unknown clean mode")
)

// ReasonNoCommand is what a clean in ModeTrashOnly says about a category cleaned by a command, which it never runs.
const ReasonNoCommand = "自动清理不会运行命令"

// The reasons for an item an exclusion added after the scan covers and for one that changed since the scan.
const (
	reasonExcluded = "已在设置中排除，已跳过"
	reasonChanged  = "扫描后发生了变化，已跳过"
)

// ReasonNoEmptyTrash is what a clean in ModeTrashOnly says about the Trash category, which it never empties.
func ReasonNoEmptyTrash() string {
	if runtime.GOOS == "windows" {
		return "自动清理不会清空回收站"
	}
	return "自动清理不会清空废纸篓"
}

// Deleter performs the deletions; SystemDeleter is the real one, tests pass a fake.
type Deleter interface {
	// Remove deletes path for good if it is strictly inside one of roots (fsx.SafeRemove).
	Remove(ctx context.Context, path string, roots []string) (fsx.Removal, error)
	// Trash moves path to the Trash if it is strictly inside one of roots and says where it went.
	Trash(ctx context.Context, path string, roots []string) (Trashed, error)
}

type systemDeleter struct{ bin trash.Bin }

// SystemDeleter deletes with fsx.SafeRemove and moves to the system Trash.
func SystemDeleter() Deleter { return systemDeleter{bin: trash.System()} }

func (systemDeleter) Remove(ctx context.Context, path string, roots []string) (fsx.Removal, error) {
	return fsx.SafeRemove(ctx, path, roots)
}

func (d systemDeleter) Trash(ctx context.Context, path string, roots []string) (Trashed, error) {
	if err := fsx.CheckTrashable(path, roots); err != nil {
		return Trashed{}, err
	}
	out, err := d.bin.MoveToTrash(ctx, []string{path})
	if err != nil {
		return Trashed{}, err
	}
	o := out[0]
	if o.Err != nil {
		return Trashed{}, o.Err
	}
	return Trashed{Path: o.TrashedPath, Dev: o.Dev, Ino: o.Ino}, nil
}

// ExecutorOptions are the optional checks an Executor makes right before each item.
type ExecutorOptions struct {
	// Exclusions returns the user's current exclusions (settings Cleanup.Exclusions); an item they cover is refused with 「已在设置中排除，已跳过」. Nil checks nothing.
	Exclusions func() []string
}

// Executor remembers a cleaner's last scan and deletes from it. It is safe for concurrent use.
type Executor struct {
	del   Deleter
	opts  ExecutorOptions
	lstat func(string) (fs.FileInfo, error)

	running sync.Mutex

	mu   sync.Mutex
	last *ScanResult
}

// NewExecutor returns an Executor deleting through del; nil means SystemDeleter. It is NewExecutorWith(del, ExecutorOptions{}).
func NewExecutor(del Deleter) *Executor { return NewExecutorWith(del, ExecutorOptions{}) }

// NewExecutorWith is NewExecutor with opts.
func NewExecutorWith(del Deleter, opts ExecutorOptions) *Executor {
	if del == nil {
		del = SystemDeleter()
	}
	return &Executor{del: del, opts: opts, lstat: os.Lstat}
}

// Remember validates res and makes it the scan later Cleans pick from. The Executor keeps its own copy.
func (e *Executor) Remember(res ScanResult) error {
	cats := make(map[string]bool, len(res.Categories))
	items := make(map[string]bool, res.Items)
	for _, c := range res.Categories {
		switch {
		case c.ID == "":
			return fmt.Errorf("%w: a category has no ID", ErrInvalidScan)
		case cats[c.ID]:
			return fmt.Errorf("%w: category %q appears twice", ErrInvalidScan, c.ID)
		case c.Deletion != DeletePermanent && c.Deletion != DeleteTrash:
			return fmt.Errorf("%w: category %q has deletion mode %q", ErrInvalidScan, c.ID, c.Deletion)
		}
		cats[c.ID] = true
		for _, it := range c.Items {
			if it.ID == "" || items[it.ID] {
				return fmt.Errorf("%w: item %q of %q is empty or appears twice", ErrInvalidScan, it.ID, c.ID)
			}
			items[it.ID] = true
		}
	}
	cp := res
	cp.Categories = make([]Category, len(res.Categories))
	for i, c := range res.Categories {
		c.Items = slices.Clone(c.Items)
		c.Roots = slices.Clone(c.Roots)
		cp.Categories[i] = c
	}
	e.mu.Lock()
	e.last = &cp
	e.mu.Unlock()
	return nil
}

// Forget drops the remembered scan, so the next Clean fails with ErrNoScan.
func (e *Executor) Forget() {
	e.mu.Lock()
	e.last = nil
	e.mu.Unlock()
}

// job is one item to delete with its category.
type job struct {
	cat  *Category
	item Item
}

// plan resolves req against the remembered scan.
func (e *Executor) plan(req CleanRequest) ([]job, []Category, error) {
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last == nil {
		return nil, nil, ErrNoScan
	}
	byID := make(map[string]int, len(e.last.Categories))
	for i, c := range e.last.Categories {
		byID[c.ID] = i
	}
	picked := make(map[string]bool, len(req.CategoryIDs))
	for _, id := range req.CategoryIDs {
		if _, ok := byID[id]; !ok {
			return nil, nil, fmt.Errorf("%w: category %q", ErrStale, id)
		}
		picked[id] = true
	}
	missing := make(map[string]bool, len(req.ItemIDs))
	for _, id := range req.ItemIDs {
		missing[id] = true
	}
	filter := len(missing) > 0
	type pick struct {
		cat  int
		item Item
	}
	var picks []pick
	var cats []Category
	for _, c := range e.last.Categories {
		if !picked[c.ID] {
			continue
		}
		cat := c
		cat.Items = slices.Clone(c.Items)
		cat.Roots = slices.Clone(c.Roots)
		cats = append(cats, cat)
		for _, it := range c.Items {
			if filter && !missing[it.ID] {
				continue
			}
			delete(missing, it.ID)
			picks = append(picks, pick{cat: len(cats) - 1, item: it})
		}
	}
	for id := range missing {
		return nil, nil, fmt.Errorf("%w: item %q", ErrStale, id)
	}
	if len(picks) == 0 {
		return nil, nil, ErrNothingSelected
	}
	// The jobs point at the category copies, which outlive the lock.
	jobs := make([]job, len(picks))
	for i, p := range picks {
		jobs[i] = job{cat: &cats[p.cat], item: p.item}
	}
	return jobs, cats, nil
}

// Clean deletes the items req names from the remembered scan, one at a time in scan order, reporting PhaseCleaning progress after each; see the package documentation for what it guarantees. It returns an error, and deletes nothing, only when the request cannot be planned (ErrBusy, ErrNoScan, ErrStale, ErrNothingSelected, ErrBadMode).
func (e *Executor) Clean(ctx context.Context, req CleanRequest, progress func(Progress)) (CleanResult, error) {
	if err := CheckMode(req.Mode); err != nil {
		return CleanResult{}, err
	}
	if !e.running.TryLock() {
		return CleanResult{}, ErrBusy
	}
	defer e.running.Unlock()
	jobs, cats, err := e.plan(req)
	if err != nil {
		return CleanResult{}, err
	}
	if progress == nil {
		progress = func(Progress) {}
	}
	trashOnly := req.Mode == ModeTrashOnly
	onMoved := OnMoved(ctx)
	var exclusions []string
	if e.opts.Exclusions != nil {
		exclusions = e.opts.Exclusions()
	}
	res := CleanResult{StartedAt: time.Now(), Failed: []Failure{}}
	outcomes := make(map[string]*CategoryOutcome, len(cats))
	res.Categories = make([]CategoryOutcome, len(cats))
	for i, c := range cats {
		res.Categories[i] = CategoryOutcome{CategoryID: c.ID, Title: c.Title}
		outcomes[c.ID] = &res.Categories[i]
	}
	var gone []string
	for i, j := range jobs {
		if ctx.Err() != nil {
			res.Interrupted = true
			break
		}
		progress(Progress{Phase: PhaseCleaning, CategoryID: j.cat.ID, Path: j.item.Path, Done: i, Total: len(jobs), Bytes: res.Freed + res.Trashed})
		out := outcomes[j.cat.ID]
		fail := func(reason string, detail error, inUse bool) {
			f := Failure{ItemID: j.item.ID, CategoryID: j.cat.ID, Path: j.item.Path, Reason: reason, InUse: inUse}
			if detail != nil {
				f.Detail = detail.Error()
			}
			res.Failed = append(res.Failed, f)
			out.Failed++
		}

		switch {
		case trashOnly && j.cat.Command != "":
			fail(ReasonNoCommand, nil, false)
			continue
		case trashOnly && j.cat.Kind == KindTrash:
			fail(ReasonNoEmptyTrash(), nil, false)
			continue
		case j.item.Hold != "":
			fail(j.item.Hold, nil, false)
			continue
		case len(exclusions) > 0 && fsx.Excluded(j.item.Path, exclusions):
			fail(reasonExcluded, nil, false)
			continue
		}

		info, err := e.lstat(j.item.Path)
		if errors.Is(err, fs.ErrNotExist) {
			res.Skipped++
			gone = append(gone, j.item.ID)
			continue
		}
		if err != nil {
			fail(reasonFor(err), err, false)
			continue
		}
		if why := changed(j.item, j.cat.Deletion, info); why != "" {
			fail(reasonChanged, fmt.Errorf("%s: %s", j.item.Path, why), false)
			continue
		}

		deletion := j.cat.Deletion
		if trashOnly {
			deletion = DeleteTrash
		}
		switch deletion {
		case DeletePermanent:
			rm, err := e.del.Remove(ctx, j.item.Path, j.cat.Roots)
			res.Freed += rm.Freed
			out.Freed += rm.Freed
			switch {
			case errors.Is(err, fs.ErrNotExist):
				res.Skipped++
				gone = append(gone, j.item.ID)
			case err != nil && ctx.Err() != nil:
				res.Interrupted = true
				fail("清理被中断，部分内容未删除", err, false)
			case err != nil:
				fail(reasonFor(err), err, false)
			case len(rm.Failures) > 0:
				inUse := slices.ContainsFunc(rm.Failures, func(f fsx.Failure) bool { return f.InUse })
				reason := fmt.Sprintf("有 %d 个文件未能删除", len(rm.Failures))
				if inUse {
					reason += "，可能正在使用"
				}
				fail(reason, rm.Failures[0], inUse)
			default:
				res.Removed++
				out.Removed++
				gone = append(gone, j.item.ID)
			}
		case DeleteTrash:
			to, err := e.del.Trash(ctx, j.item.Path, j.cat.Roots)
			switch {
			case errors.Is(err, fs.ErrNotExist):
				res.Skipped++
				gone = append(gone, j.item.ID)
			case err != nil:
				fail(reasonFor(err), err, false)
			default:
				res.Trashed += j.item.Size
				out.Trashed += j.item.Size
				res.Removed++
				out.Removed++
				gone = append(gone, j.item.ID)
				m := Moved{ItemID: j.item.ID, CategoryID: j.cat.ID, Kind: j.cat.Kind, Path: j.item.Path, TrashedPath: to.Path,
					Root: rootOf(j.item.Path, j.cat.Roots), Size: j.item.Size, Dir: j.item.Dir, Dev: to.Dev, Ino: to.Ino}
				res.Moved = append(res.Moved, m)
				onMoved(m)
			}
		}
	}
	done := len(jobs)
	if res.Interrupted {
		done = res.Removed + res.Skipped + len(res.Failed)
	}
	progress(Progress{Phase: PhaseCleaning, Done: done, Total: len(jobs), Bytes: res.Freed + res.Trashed})
	res.Partial = res.Interrupted || len(res.Failed) > 0
	res.Duration = time.Since(res.StartedAt)
	res.Handled = gone
	e.drop(gone)
	return res, nil
}

// changed says why the item at its path is no longer what the scan saw, or "" when it still is: a symlink where there was none, a file where there was a folder or the other way round, another device or inode (deleted and made again, or downloaded again under the same name), and for a file of a DeleteTrash category (an installer, an unfinished download: the user's own files) another modification time or size. Caches, logs and build output change all the time and are rebuilt on demand, so only their identity counts.
func changed(it Item, deletion DeletionMode, now fs.FileInfo) string {
	typ := now.Mode().Type()
	switch {
	case typ == fs.ModeSymlink && !it.symlink:
		return "it is now a symlink"
	case typ != fs.ModeSymlink && it.symlink:
		return "it is no longer a symlink"
	case typ != fs.ModeSymlink && (typ == fs.ModeDir) != it.Dir:
		return fmt.Sprintf("type is now %v", typ)
	case !it.stat:
		return ""
	}
	if it.ino != 0 {
		if dev, ino := identity(now); dev != it.dev || ino != it.ino {
			return "it is another file now (device or inode differs)"
		}
	}
	if deletion == DeleteTrash && !it.Dir && !it.symlink && (!now.ModTime().Equal(it.lmod) || now.Size() != it.lsize) {
		return "its modification time or size changed"
	}
	return ""
}

// rootOf is the first of roots that contains path (or is it), "" when none does.
func rootOf(path string, roots []string) string {
	for _, r := range roots {
		rel, err := filepath.Rel(r, path)
		if err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			return r
		}
	}
	return ""
}

// drop removes deleted items from the remembered scan and updates its sizes.
func (e *Executor) drop(ids []string) {
	if len(ids) == 0 {
		return
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last == nil {
		return
	}
	next := Drop(*e.last, ids)
	e.last = &next
}

// reasonFor turns a deletion error into the sentence the page shows.
func reasonFor(err error) string {
	var ue *UserError
	if errors.As(err, &ue) {
		return ue.Message
	}
	switch {
	case errors.Is(err, fsx.ErrProtected), errors.Is(err, fsx.ErrOutsideRoots), errors.Is(err, fsx.ErrNotAbsolute):
		return "位于受保护的位置，已跳过"
	case errors.Is(err, fs.ErrPermission):
		return "没有权限删除"
	case errors.Is(err, context.Canceled), errors.Is(err, context.DeadlineExceeded):
		return "清理被中断"
	}
	return "删除失败"
}
