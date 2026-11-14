package fsx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"sync/atomic"
	"time"
)

// SizeOptions tunes DirSize.
type SizeOptions struct {
	// CrossDevices lets the walk descend into folders on another volume than root (mount points under root). macOS only: on Windows other volumes are mounted through reparse points, which are never followed.
	CrossDevices bool
	// Workers bounds how many folders are read at once; 0 picks a default suited to the machine.
	Workers int
	// Skip, when set, is asked about every entry below root before it is measured; returning true leaves it (and everything under it) out. It is called from several goroutines at once.
	Skip func(path string) bool
	// Progress, when set, receives the running totals at most every ProgressEvery while the walk goes on, never concurrently, and once more at the end.
	Progress func(SizeProgress)
	// ProgressEvery is the minimum time between Progress calls; 0 means 100 ms.
	ProgressEvery time.Duration
}

// SizeProgress is the running total of a DirSize walk.
type SizeProgress struct {
	Bytes int64
	Files int64
	// Path is the folder that was read last.
	Path string
}

// SizeResult is what DirSize measured.
type SizeResult struct {
	// Bytes is the space the files take on disk: allocated blocks on macOS (hard-linked files counted once), file sizes on Windows.
	Bytes int64
	// Logical is the sum of the files' sizes.
	Logical int64
	// Files counts regular files; Dirs counts folders read, root included.
	Files int64
	Dirs  int64
	// Denied counts entries that could not be read for lack of permission; Errors counts those that failed otherwise (vanished mid-walk, I/O errors).
	Denied int64
	Errors int64
	// Mounts counts folders left out because they are on another volume.
	Mounts int64
	// Newest is the latest modification time of root and every file and folder measured under it; zero when nothing was read.
	Newest time.Time
}

// DirSize walks root concurrently and returns its size; see the package documentation for what it counts.
func DirSize(ctx context.Context, root string, opts SizeOptions) (SizeResult, error) {
	if root == "" {
		return SizeResult{}, errors.New("fsx: empty root")
	}
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil {
		return SizeResult{}, fmt.Errorf("fsx: %w", err)
	}
	w := newWalker(ctx, opts)
	switch info.Mode().Type() {
	case 0:
		w.addFile(info)
	case fs.ModeDir:
		w.seenTime(info.ModTime())
		if dev, ok := deviceOf(info); ok && !opts.CrossDevices {
			w.rootDev, w.checkDev = dev, true
		}
		if err := w.walkDir(root); err != nil {
			return w.result(), fmt.Errorf("fsx: %w", err)
		}
		w.wg.Wait()
	}
	w.progress(root, true)
	if err := ctx.Err(); err != nil {
		return w.result(), err
	}
	return w.result(), nil
}

type walker struct {
	ctx      context.Context
	opts     SizeOptions
	rootDev  uint64
	checkDev bool
	sem      chan struct{}
	wg       sync.WaitGroup

	bytes, logical, files, dirs, denied, errs, mounts atomic.Int64
	// newest is the latest modification time seen, in Unix nanoseconds; 0 until one is seen.
	newest atomic.Int64

	seenMu sync.Mutex
	seen   map[fileID]struct{}

	progMu   sync.Mutex
	lastProg time.Time
}

func newWalker(ctx context.Context, opts SizeOptions) *walker {
	workers := opts.Workers
	if workers <= 0 {
		workers = min(max(4, 2*runtime.GOMAXPROCS(0)), 16)
	}
	if opts.ProgressEvery <= 0 {
		opts.ProgressEvery = 100 * time.Millisecond
	}
	return &walker{ctx: ctx, opts: opts, sem: make(chan struct{}, workers), seen: make(map[fileID]struct{}), lastProg: time.Now()}
}

func (w *walker) result() SizeResult {
	r := SizeResult{
		Bytes:   w.bytes.Load(),
		Logical: w.logical.Load(),
		Files:   w.files.Load(),
		Dirs:    w.dirs.Load(),
		Denied:  w.denied.Load(),
		Errors:  w.errs.Load(),
		Mounts:  w.mounts.Load(),
	}
	if n := w.newest.Load(); n != 0 {
		r.Newest = time.Unix(0, n)
	}
	return r
}

// seenTime keeps t when it is later than every time seen so far.
func (w *walker) seenTime(t time.Time) {
	n := t.UnixNano()
	for {
		cur := w.newest.Load()
		if n <= cur || w.newest.CompareAndSwap(cur, n) {
			return
		}
	}
}

func (w *walker) fail(err error) {
	if errors.Is(err, fs.ErrPermission) {
		w.denied.Add(1)
	} else {
		w.errs.Add(1)
	}
}

func (w *walker) addFile(info fs.FileInfo) {
	w.seenTime(info.ModTime())
	alloc, id, linked := usage(info)
	w.files.Add(1)
	w.logical.Add(info.Size())
	if linked {
		w.seenMu.Lock()
		_, dup := w.seen[id]
		w.seen[id] = struct{}{}
		w.seenMu.Unlock()
		if dup {
			return
		}
	}
	w.bytes.Add(alloc)
}

// walkDir reads dir, measures its files and hands its sub-folders to spare workers or walks them inline. The folder is closed before any sub-folder is read, so open descriptors stay bounded by the number of workers.
func (w *walker) walkDir(dir string) error {
	if err := w.ctx.Err(); err != nil {
		return err
	}
	f, err := os.Open(dir)
	if err != nil {
		return err
	}
	w.dirs.Add(1)
	var subdirs []string
	for {
		entries, rerr := f.ReadDir(512)
		for _, e := range entries {
			p := filepath.Join(dir, e.Name())
			if w.opts.Skip != nil && w.opts.Skip(p) {
				continue
			}
			// Only plain folders are descended and only regular files counted: symlinks, Windows junctions and other reparse points (ModeIrregular), devices and sockets are neither.
			switch e.Type() {
			case fs.ModeDir:
				info, err := e.Info()
				if err != nil {
					if w.checkDev {
						w.fail(err)
						continue
					}
				} else {
					if dev, ok := deviceOf(info); ok && w.checkDev && dev != w.rootDev {
						w.mounts.Add(1)
						continue
					}
					w.seenTime(info.ModTime())
				}
				subdirs = append(subdirs, p)
			case 0:
				info, err := e.Info()
				if err != nil {
					w.fail(err)
					continue
				}
				w.addFile(info)
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				w.fail(rerr)
			}
			break
		}
	}
	f.Close()
	w.progress(dir, false)
	for _, sub := range subdirs {
		if w.ctx.Err() != nil {
			break
		}
		w.spawn(sub)
	}
	return nil
}

// spawn walks sub on a spare worker, or inline when every worker is busy, so the number of goroutines stays bounded without a queue that could grow with the tree.
func (w *walker) spawn(sub string) {
	select {
	case w.sem <- struct{}{}:
		w.wg.Go(func() {
			defer func() { <-w.sem }()
			if err := w.walkDir(sub); err != nil && w.ctx.Err() == nil {
				w.fail(err)
			}
		})
	default:
		if err := w.walkDir(sub); err != nil && w.ctx.Err() == nil {
			w.fail(err)
		}
	}
}

// progress reports the totals when ProgressEvery has passed (or always when final). A walker that finds another one reporting skips its turn rather than wait.
func (w *walker) progress(path string, final bool) {
	if w.opts.Progress == nil {
		return
	}
	if final {
		w.progMu.Lock()
	} else if !w.progMu.TryLock() {
		return
	}
	defer w.progMu.Unlock()
	now := time.Now()
	if !final && now.Sub(w.lastProg) < w.opts.ProgressEvery {
		return
	}
	w.lastProg = now
	w.opts.Progress(SizeProgress{Bytes: w.bytes.Load(), Files: w.files.Load(), Path: path})
}
