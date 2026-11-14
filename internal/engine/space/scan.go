package space

import (
	"cmp"
	"container/heap"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"sync/atomic"
	"time"
)

// Defaults of Options.
const (
	DefaultMaxChildren   = 300
	DefaultMinFileNode   = 1 << 20
	DefaultMaxLargeFiles = 200
	DefaultMaxDenied     = 5
)

// ErrNotDir is returned by Scan for a root that is not a folder.
var ErrNotDir = errors.New("space: not a folder")

// Options tunes Scan. The zero value scans with the defaults and lists no large files.
type Options struct {
	// Workers bounds how many folders are read at once; 0 picks a default suited to the machine.
	Workers int
	// MaxChildren is how many children a folder keeps, the largest; the rest are folded into one KindOther node. 0 means DefaultMaxChildren.
	MaxChildren int
	// MinFileNode is the size from which a file gets a node of its own; smaller files are folded into one KindFiles node per folder. 0 means DefaultMinFileNode.
	MinFileNode int64
	// LargeFileMin is the size from which a file is listed in Tree.LargeFiles; 0 lists none.
	LargeFileMin int64
	// MaxLargeFiles is how many large files are kept, the largest. 0 means DefaultMaxLargeFiles.
	MaxLargeFiles int
	// MaxDeniedPaths is how many unreadable folders Stats.DeniedPaths names. 0 means DefaultMaxDenied.
	MaxDeniedPaths int
	// Skip, when set, is asked about every folder below the root; returning true leaves it out. It is called from several goroutines at once. Scan adds the platform's own (see planFor).
	Skip func(path string) bool
	// SameVolume lists folders on other volumes whose volume counts as the root's, so the walk enters them (the macOS data volume behind the startup disk). Scan adds the platform's own.
	SameVolume []string
	// Progress, when set, receives the running totals at most every ProgressEvery while the walk goes on, never concurrently.
	Progress func(Progress)
	// ProgressEvery is the minimum time between Progress calls; 0 means 100 ms.
	ProgressEvery time.Duration

	// devOf returns the volume of a folder; tests replace it to fake mount points.
	devOf func(fs.FileInfo) (uint64, bool)
}

// Progress is the running total of a scan.
type Progress struct {
	Files int64
	Dirs  int64
	Bytes int64
	// Path is the folder that was read last.
	Path string
}

// Scan walks root concurrently and builds its size tree. It never follows symlinks (or Windows junctions and other reparse points), stays on root's volume (plus Options.SameVolume), counts allocated bytes on macOS (st_blocks*512, hard-linked files once) and file sizes on Windows, as fsx.DirSize does, and counts unreadable folders in Stats.Denied instead of failing. Memory stays bounded: every folder keeps a node, but only its Options.MaxChildren largest children, and only files of at least Options.MinFileNode get nodes of their own. Only a root that cannot be read, or a cancelled ctx, returns an error.
func Scan(ctx context.Context, root string, opts Options) (*Tree, error) {
	if !filepath.IsAbs(root) {
		return nil, fmt.Errorf("space: root %q is not absolute", root)
	}
	root = filepath.Clean(root)
	info, err := os.Lstat(root)
	if err != nil {
		return nil, fmt.Errorf("space: %w", err)
	}
	if !info.IsDir() {
		return nil, fmt.Errorf("%w: %q", ErrNotDir, root)
	}
	f, err := os.Open(root)
	if err != nil {
		return nil, fmt.Errorf("space: %w", err)
	}
	f.Close()

	// The platform's plan for this root: folders to leave out (the walk never goes below a folder it leaves out, so exact paths suffice) and other volumes that belong to the root's.
	skipPaths, same := planFor(root)
	skip := func(p string) bool {
		return slices.ContainsFunc(skipPaths, func(s string) bool { return samePath(s, p) }) || (opts.Skip != nil && opts.Skip(p))
	}
	w := newWalker(ctx, opts, skip)
	if dev, ok := w.devOf(info); ok {
		w.checkDev = true
		w.devs[dev] = true
		for _, p := range append(same, opts.SameVolume...) {
			if fi, err := os.Lstat(p); err == nil {
				if d, ok := w.devOf(fi); ok {
					w.devs[d] = true
				}
			}
		}
	}

	started := time.Now()
	rootNode := &node{name: displayBase(root), kind: KindDir}
	rootNode.pending.Store(1)
	w.walkDir(rootNode, root)
	w.wg.Wait()
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	w.report(root, true)
	stats := Stats{
		Files:       w.files.Load(),
		Dirs:        w.dirs.Load(),
		Denied:      w.denied.Load(),
		DeniedPaths: slices.Clip(w.deniedPaths),
		Errors:      w.errs.Load(),
		Mounts:      w.mounts.Load(),
		StartedAt:   started,
		Duration:    time.Since(started),
	}
	return newTree(root, rootNode, w.large.sorted(), stats), nil
}

// displayBase is the name of root's node: its last component, or the whole path for a volume root.
func displayBase(root string) string {
	if filepath.Dir(root) == root {
		return root
	}
	return filepath.Base(root)
}

type walker struct {
	ctx      context.Context
	opts     Options
	skip     func(string) bool
	devOf    func(fs.FileInfo) (uint64, bool)
	devs     map[uint64]bool
	checkDev bool
	sem      chan struct{}
	wg       sync.WaitGroup

	files, dirs, bytes, denied, errs, mounts atomic.Int64

	deniedMu    sync.Mutex
	deniedPaths []string

	seenMu sync.Mutex
	seen   map[fileID]struct{}

	large largeSet

	progMu   sync.Mutex
	lastProg time.Time
}

func newWalker(ctx context.Context, opts Options, skip func(string) bool) *walker {
	workers := opts.Workers
	if workers <= 0 {
		workers = min(max(4, 2*runtime.GOMAXPROCS(0)), 16)
	}
	if opts.MaxChildren <= 0 {
		opts.MaxChildren = DefaultMaxChildren
	}
	if opts.MinFileNode <= 0 {
		opts.MinFileNode = DefaultMinFileNode
	}
	if opts.MaxLargeFiles <= 0 {
		opts.MaxLargeFiles = DefaultMaxLargeFiles
	}
	if opts.MaxDeniedPaths <= 0 {
		opts.MaxDeniedPaths = DefaultMaxDenied
	}
	if opts.ProgressEvery <= 0 {
		opts.ProgressEvery = 100 * time.Millisecond
	}
	devOf := opts.devOf
	if devOf == nil {
		devOf = deviceOf
	}
	return &walker{
		ctx:      ctx,
		opts:     opts,
		skip:     skip,
		devOf:    devOf,
		devs:     make(map[uint64]bool),
		sem:      make(chan struct{}, workers),
		seen:     make(map[fileID]struct{}),
		large:    largeSet{max: opts.MaxLargeFiles},
		lastProg: time.Now(),
	}
}

// walkDir reads the folder n at path, adds its files and hands its sub-folders to spare workers or walks them inline. The folder is closed before any sub-folder is read, so open descriptors stay bounded by the number of workers. When n and everything under it is read, done folds it (finalize).
func (w *walker) walkDir(n *node, path string) {
	if w.ctx.Err() != nil {
		// A cancelled scan is thrown away, so nothing below is finalized.
		return
	}
	f, err := os.Open(path)
	if err != nil {
		w.failDir(n, path, err)
		w.done(n)
		return
	}
	w.dirs.Add(1)
	var subdirs []*node
	for {
		entries, rerr := f.ReadDir(512)
		for _, e := range entries {
			// Only plain folders are descended and only regular files counted: symlinks, Windows junctions and other reparse points (ModeIrregular), devices and sockets are neither.
			switch e.Type() {
			case fs.ModeDir:
				p := filepath.Join(path, e.Name())
				if w.skip(p) {
					continue
				}
				if w.checkDev {
					info, err := e.Info()
					if err != nil {
						w.errs.Add(1)
						continue
					}
					if dev, ok := w.devOf(info); ok && !w.devs[dev] {
						w.mounts.Add(1)
						continue
					}
				}
				c := &node{name: e.Name(), parent: n, kind: KindDir}
				c.pending.Store(1)
				n.children = append(n.children, c)
				subdirs = append(subdirs, c)
			case 0:
				info, err := e.Info()
				if err != nil {
					w.errs.Add(1)
					continue
				}
				w.addFile(n, path, info)
			}
		}
		if rerr != nil {
			if !errors.Is(rerr, io.EOF) {
				w.errs.Add(1)
			}
			break
		}
	}
	f.Close()
	w.report(path, false)
	n.pending.Add(int32(len(subdirs)))
	for _, c := range subdirs {
		if w.ctx.Err() != nil {
			return
		}
		w.spawn(c, filepath.Join(path, c.name))
	}
	w.done(n)
}

// spawn walks c on a spare worker, or inline when every worker is busy, so the number of goroutines stays bounded without a queue that could grow with the tree.
func (w *walker) spawn(c *node, path string) {
	select {
	case w.sem <- struct{}{}:
		w.wg.Go(func() {
			defer func() { <-w.sem }()
			w.walkDir(c, path)
		})
	default:
		w.walkDir(c, path)
	}
}

func (w *walker) addFile(n *node, dir string, info fs.FileInfo) {
	alloc, id, linked := usage(info)
	w.files.Add(1)
	n.files++
	if linked {
		w.seenMu.Lock()
		_, dup := w.seen[id]
		w.seen[id] = struct{}{}
		w.seenMu.Unlock()
		if dup {
			// Another link to a file already counted takes no more space.
			return
		}
	}
	w.bytes.Add(alloc)
	if alloc >= w.opts.MinFileNode {
		n.children = append(n.children, &node{name: info.Name(), parent: n, kind: KindFile, size: alloc, files: 1, modTime: info.ModTime().UnixNano()})
	} else {
		n.small += alloc
		n.smallCount++
	}
	if w.opts.LargeFileMin > 0 && alloc >= w.opts.LargeFileMin {
		w.large.offer(LargeFile{Path: filepath.Join(dir, info.Name()), Size: alloc, ModTime: info.ModTime()})
	}
}

func (w *walker) failDir(n *node, path string, err error) {
	if !errors.Is(err, fs.ErrPermission) {
		w.errs.Add(1)
		return
	}
	n.denied = true
	w.denied.Add(1)
	w.deniedMu.Lock()
	if len(w.deniedPaths) < w.opts.MaxDeniedPaths {
		w.deniedPaths = append(w.deniedPaths, path)
	}
	w.deniedMu.Unlock()
}

// done marks one part of n finished (its own listing or one sub-folder) and, when it was the last, folds n and goes on to its parent. The atomic counter orders every write to a child before its parent's finalize reads it.
func (w *walker) done(n *node) {
	for n != nil && n.pending.Add(-1) == 0 {
		w.finalize(n)
		n = n.parent
	}
}

// finalize sums a finished folder and bounds what it keeps: its MaxChildren largest children, one KindOther node for the rest (whose subtrees are dropped), and one KindFiles node for its small files.
func (w *walker) finalize(n *node) {
	size := n.small
	for _, c := range n.children {
		size += c.size
		if c.kind == KindDir {
			n.files += c.files
		}
	}
	n.size = size
	slices.SortFunc(n.children, bySizeDesc)
	if keep := w.opts.MaxChildren; len(n.children) > keep {
		rest := n.children[keep:]
		other := &node{parent: n, kind: KindOther, count: int64(len(rest))}
		for _, c := range rest {
			other.size += c.size
			other.files += c.files
		}
		// A fresh slice, so the folded nodes and their subtrees become garbage.
		kept := make([]*node, keep, keep+2)
		copy(kept, n.children[:keep])
		n.children = append(kept, other)
	}
	if n.smallCount > 0 {
		n.children = append(n.children, &node{parent: n, kind: KindFiles, size: n.small, files: n.smallCount, count: n.smallCount})
	}
	n.small, n.smallCount = 0, 0
	slices.SortStableFunc(n.children, bySizeDesc)
	n.children = slices.Clip(n.children)
}

// report sends the totals when ProgressEvery has passed (or always when final). A walker that finds another one reporting skips its turn rather than wait.
func (w *walker) report(path string, final bool) {
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
	w.opts.Progress(Progress{Files: w.files.Load(), Dirs: w.dirs.Load(), Bytes: w.bytes.Load(), Path: path})
}

// largeSet keeps the largest files offered to it, up to max.
type largeSet struct {
	mu  sync.Mutex
	max int
	h   largeHeap
}

func (s *largeSet) offer(lf LargeFile) {
	s.mu.Lock()
	defer s.mu.Unlock()
	if len(s.h) < s.max {
		heap.Push(&s.h, lf)
		return
	}
	if lf.Size > s.h[0].Size {
		s.h[0] = lf
		heap.Fix(&s.h, 0)
	}
}

func (s *largeSet) sorted() []LargeFile {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := slices.Clone([]LargeFile(s.h))
	slices.SortFunc(out, func(a, b LargeFile) int {
		if c := cmp.Compare(b.Size, a.Size); c != 0 {
			return c
		}
		return strings.Compare(a.Path, b.Path)
	})
	if out == nil {
		out = []LargeFile{}
	}
	return out
}

// largeHeap is a min-heap by size, so the smallest kept file is the one to replace.
type largeHeap []LargeFile

func (h largeHeap) Len() int           { return len(h) }
func (h largeHeap) Less(i, j int) bool { return h[i].Size < h[j].Size }
func (h largeHeap) Swap(i, j int)      { h[i], h[j] = h[j], h[i] }
func (h *largeHeap) Push(x any)        { *h = append(*h, x.(LargeFile)) }
func (h *largeHeap) Pop() any {
	old := *h
	x := old[len(old)-1]
	*h = old[:len(old)-1]
	return x
}
