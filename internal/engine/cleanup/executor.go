package cleanup

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"slices"
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
)

// Deleter performs the deletions; SystemDeleter is the real one, tests pass a fake.
type Deleter interface {
	// Remove deletes path for good if it is strictly inside one of roots (fsx.SafeRemove).
	Remove(ctx context.Context, path string, roots []string) (fsx.Removal, error)
	// Trash moves path to the Trash if it is strictly inside one of roots.
	Trash(ctx context.Context, path string, roots []string) error
}

type systemDeleter struct{ bin trash.Bin }

// SystemDeleter deletes with fsx.SafeRemove and moves to the system Trash.
func SystemDeleter() Deleter { return systemDeleter{bin: trash.System()} }

func (systemDeleter) Remove(ctx context.Context, path string, roots []string) (fsx.Removal, error) {
	return fsx.SafeRemove(ctx, path, roots)
}

func (d systemDeleter) Trash(ctx context.Context, path string, roots []string) error {
	if err := fsx.CheckTrashable(path, roots); err != nil {
		return err
	}
	out, err := d.bin.MoveToTrash(ctx, []string{path})
	if err != nil {
		return err
	}
	return out[0].Err
}

// Executor remembers a cleaner's last scan and deletes from it. It is safe for concurrent use.
type Executor struct {
	del   Deleter
	lstat func(string) (fs.FileInfo, error)

	running sync.Mutex

	mu   sync.Mutex
	last *ScanResult
}

// NewExecutor returns an Executor deleting through del; nil means SystemDeleter.
func NewExecutor(del Deleter) *Executor {
	if del == nil {
		del = SystemDeleter()
	}
	return &Executor{del: del, lstat: os.Lstat}
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

// Clean deletes the items req names from the remembered scan, one at a time in scan order, reporting PhaseCleaning progress after each; see the package documentation for what it guarantees. It returns an error, and deletes nothing, only when the request cannot be planned (ErrBusy, ErrNoScan, ErrStale, ErrNothingSelected).
func (e *Executor) Clean(ctx context.Context, req CleanRequest, progress func(Progress)) (CleanResult, error) {
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
		if changed(j.item, info) {
			fail("扫描后发生了变化，已跳过", fmt.Errorf("%s: type is now %v", j.item.Path, info.Mode().Type()), false)
			continue
		}

		switch j.cat.Deletion {
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
			err := e.del.Trash(ctx, j.item.Path, j.cat.Roots)
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
	e.drop(gone)
	return res, nil
}

// changed reports whether the item at its path is no longer what the scan saw: a symlink where there was none, or a file where there was a folder or the other way round.
func changed(it Item, now fs.FileInfo) bool {
	typ := now.Mode().Type()
	if typ == fs.ModeSymlink {
		return !it.symlink
	}
	if it.symlink {
		return true
	}
	return (typ == fs.ModeDir) != it.Dir
}

// drop removes deleted items from the remembered scan and updates its sizes.
func (e *Executor) drop(ids []string) {
	if len(ids) == 0 {
		return
	}
	del := make(map[string]bool, len(ids))
	for _, id := range ids {
		del[id] = true
	}
	e.mu.Lock()
	defer e.mu.Unlock()
	if e.last == nil {
		return
	}
	e.last.Size, e.last.Items = 0, 0
	for i := range e.last.Categories {
		c := &e.last.Categories[i]
		c.Items = slices.DeleteFunc(c.Items, func(it Item) bool { return del[it.ID] })
		c.Size = 0
		for _, it := range c.Items {
			c.Size += it.Size
		}
		e.last.Size += c.Size
		e.last.Items += len(c.Items)
	}
}

// reasonFor turns a deletion error into the sentence the page shows.
func reasonFor(err error) string {
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
