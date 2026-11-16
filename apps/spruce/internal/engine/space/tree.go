package space

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"sync/atomic"
	"time"
)

// Kind is what a node of the tree stands for.
type Kind uint8

const (
	// KindDir is a folder; its children can be listed.
	KindDir Kind = iota
	// KindFile is a file of at least Options.MinFileNode bytes.
	KindFile
	// KindOther stands for the children of a folder beyond its Options.MaxChildren largest, folded into one node.
	KindOther
	// KindFiles stands for a folder's files smaller than Options.MinFileNode, folded into one node.
	KindFiles
)

// node is one folder, file or folded group of the tree. Sizes are allocated bytes, as fsx.DirSize counts them.
type node struct {
	name     string
	parent   *node
	children []*node
	size     int64
	// files counts the regular files at or below the node.
	files int64
	// count is how many entries a folded node stands for.
	count int64
	// modTime is a file's modification time in Unix nanoseconds.
	modTime int64
	id      int32
	kind    Kind
	// denied is set on a folder that could not be read for lack of permission.
	denied bool

	// Used by the walk only: the folder's own listing plus its sub-folders still being walked, and its files too small for a node of their own.
	pending    atomic.Int32
	small      int64
	smallCount int64
}

func bySizeDesc(a, b *node) int {
	if c := cmp.Compare(b.size, a.size); c != 0 {
		return c
	}
	if c := cmp.Compare(a.kind, b.kind); c != 0 {
		return c
	}
	return strings.Compare(a.name, b.name)
}

// Entry is one node of a Tree as the page lists it.
type Entry struct {
	// ID identifies the node within its Tree; -1 for a large file the tree folded into a group.
	ID   int
	Kind Kind
	// Name and Path are empty for folded groups (KindOther, KindFiles).
	Name string
	Path string
	// Size is in allocated bytes.
	Size int64
	// Files counts the regular files at or below the entry.
	Files int64
	// Count is how many entries a folded group stands for.
	Count int64
	// ModTime is set for files.
	ModTime time.Time
	// Denied is set on a folder that could not be read for lack of permission.
	Denied bool
}

// LargeFile is a file of at least Options.LargeFileMin bytes found anywhere below the root.
type LargeFile struct {
	Path    string
	Size    int64
	ModTime time.Time
}

// Stats is what a scan read.
type Stats struct {
	// Files counts regular files; Dirs counts folders read, the root included.
	Files int64
	Dirs  int64
	// Denied counts folders that could not be read for lack of permission; DeniedPaths holds the first few of them.
	Denied      int64
	DeniedPaths []string
	// Errors counts entries that failed otherwise (vanished mid-walk, I/O errors).
	Errors int64
	// Mounts counts folders left out because they are on another volume.
	Mounts    int64
	StartedAt time.Time
	Duration  time.Duration
}

// Tree is the result of Scan: the size tree under a root, the largest files and what the walk read. It is not safe for concurrent use; the caller serializes reads and Remove.
type Tree struct {
	rootPath string
	root     *node
	// nodes holds every node by ID; a removed node's slot is nil.
	nodes []*node
	large []LargeFile
	stats Stats
}

// newTree numbers the nodes under root, depth first, the root being 0.
func newTree(rootPath string, root *node, large []LargeFile, stats Stats) *Tree {
	t := &Tree{rootPath: rootPath, root: root, large: large, stats: stats}
	stack := []*node{root}
	for len(stack) > 0 {
		n := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		n.id = int32(len(t.nodes))
		t.nodes = append(t.nodes, n)
		stack = append(stack, n.children...)
	}
	return t
}

// RootPath is the folder that was scanned.
func (t *Tree) RootPath() string { return t.rootPath }

// Stats returns what the scan read.
func (t *Tree) Stats() Stats {
	s := t.stats
	s.DeniedPaths = slices.Clone(s.DeniedPaths)
	return s
}

// Root returns the root's entry, with the sizes as of the last Remove.
func (t *Tree) Root() Entry { return t.entry(t.root, t.rootPath) }

// Entry returns the node with id.
func (t *Tree) Entry(id int) (Entry, bool) {
	n := t.node(id)
	if n == nil {
		return Entry{}, false
	}
	return t.entry(n, t.path(n)), true
}

// Children returns the children of the folder with id, largest first; ok is false when id is not a folder of the tree.
func (t *Tree) Children(id int) (children []Entry, ok bool) {
	n := t.node(id)
	if n == nil || n.kind != KindDir {
		return nil, false
	}
	// Removals change sizes below the folder, so the order is checked on every read; the lists are short.
	slices.SortStableFunc(n.children, bySizeDesc)
	base := t.path(n)
	out := make([]Entry, 0, len(n.children))
	for _, c := range n.children {
		out = append(out, t.entry(c, filepath.Join(base, c.name)))
	}
	return out, true
}

// Ancestors returns the folders from the root down to the node with id, both included.
func (t *Tree) Ancestors(id int) []Entry {
	var chain []*node
	for n := t.node(id); n != nil; n = n.parent {
		chain = append(chain, n)
	}
	slices.Reverse(chain)
	out := make([]Entry, 0, len(chain))
	for _, n := range chain {
		out = append(out, t.entry(n, t.path(n)))
	}
	return out
}

// LargeFiles returns the largest files found, largest first.
func (t *Tree) LargeFiles() []LargeFile { return slices.Clone(t.large) }

// Find returns the folder or file of the tree at path, or a large file the tree folded into a group (with ID -1). Folded groups and the root are not found.
func (t *Tree) Find(path string) (Entry, bool) {
	n, rest, ok := t.lookup(path)
	if !ok {
		return Entry{}, false
	}
	if len(rest) == 0 {
		if n == t.root {
			return Entry{}, false
		}
		return t.entry(n, filepath.Clean(path)), true
	}
	if i := t.largeIndex(path); i >= 0 {
		return largeEntry(t.large[i]), true
	}
	return Entry{}, false
}

// Remove takes the entry at path (as Find finds it) out of the tree, as after moving it to the Trash: it drops the node and everything under it, subtracts its size and files from every folder above it, and drops the large files at or under path. It returns the entry as it was.
func (t *Tree) Remove(path string) (Entry, bool) {
	n, rest, ok := t.lookup(path)
	if !ok {
		return Entry{}, false
	}
	clean := filepath.Clean(path)
	if len(rest) == 0 {
		if n == t.root {
			return Entry{}, false
		}
		e := t.entry(n, clean)
		p := n.parent
		p.children = slices.DeleteFunc(p.children, func(c *node) bool { return c == n })
		t.forget(n)
		shrink(p, n.size, n.files)
		t.dropLarge(clean)
		return e, true
	}
	i := t.largeIndex(clean)
	if i < 0 {
		return Entry{}, false
	}
	e := largeEntry(t.large[i])
	// The file is inside a group of n: the beyond-the-largest group when n has one (the file or the folder holding it was folded there), otherwise the small files.
	var group *node
	for _, c := range n.children {
		if c.kind == KindOther || (c.kind == KindFiles && group == nil) {
			group = c
		}
	}
	if group != nil {
		group.size = max(0, group.size-e.Size)
		group.files = max(0, group.files-1)
		if len(rest) == 1 {
			group.count = max(0, group.count-1)
		}
		if group.count == 0 {
			n.children = slices.DeleteFunc(n.children, func(c *node) bool { return c == group })
			t.forget(group)
		}
	}
	shrink(n, e.Size, 1)
	t.dropLarge(clean)
	return e, true
}

func largeEntry(lf LargeFile) Entry {
	return Entry{ID: -1, Kind: KindFile, Name: filepath.Base(lf.Path), Path: lf.Path, Size: lf.Size, Files: 1, ModTime: lf.ModTime}
}

// shrink subtracts size and files from n and every folder above it.
func shrink(n *node, size, files int64) {
	for ; n != nil; n = n.parent {
		n.size = max(0, n.size-size)
		n.files = max(0, n.files-files)
	}
}

// forget frees the IDs of n and everything under it.
func (t *Tree) forget(n *node) {
	stack := []*node{n}
	for len(stack) > 0 {
		m := stack[len(stack)-1]
		stack = stack[:len(stack)-1]
		if int(m.id) < len(t.nodes) && t.nodes[m.id] == m {
			t.nodes[m.id] = nil
		}
		stack = append(stack, m.children...)
	}
}

func (t *Tree) dropLarge(path string) {
	t.large = slices.DeleteFunc(t.large, func(lf LargeFile) bool { return within(lf.Path, path) })
}

func (t *Tree) largeIndex(path string) int {
	clean := filepath.Clean(path)
	return slices.IndexFunc(t.large, func(lf LargeFile) bool { return samePath(lf.Path, clean) })
}

func (t *Tree) node(id int) *node {
	if id < 0 || id >= len(t.nodes) {
		return nil
	}
	return t.nodes[id]
}

// lookup follows path down the tree from the root as far as nodes go and returns the deepest folder or file reached with the components left over; ok is false when path is not at or under the root.
func (t *Tree) lookup(path string) (n *node, rest []string, ok bool) {
	if !filepath.IsAbs(path) {
		return nil, nil, false
	}
	clean := filepath.Clean(path)
	if !within(clean, t.rootPath) {
		return nil, nil, false
	}
	rel := strings.TrimPrefix(clean[len(t.rootPath):], string(filepath.Separator))
	var parts []string
	if rel != "" {
		parts = strings.Split(rel, string(filepath.Separator))
	}
	n = t.root
	for i, part := range parts {
		next := n.child(part)
		if next == nil {
			return n, parts[i:], true
		}
		n = next
	}
	return n, nil, true
}

// child returns the folder or file named name directly under n.
func (n *node) child(name string) *node {
	for _, c := range n.children {
		if (c.kind == KindDir || c.kind == KindFile) && samePath(c.name, name) {
			return c
		}
	}
	return nil
}

func (t *Tree) path(n *node) string {
	var names []string
	for ; n != nil && n != t.root; n = n.parent {
		names = append(names, n.name)
	}
	slices.Reverse(names)
	return filepath.Join(append([]string{t.rootPath}, names...)...)
}

func (t *Tree) entry(n *node, path string) Entry {
	e := Entry{ID: int(n.id), Kind: n.kind, Size: n.size, Files: n.files, Count: n.count, Denied: n.denied}
	if n.kind == KindDir || n.kind == KindFile {
		e.Name, e.Path = n.name, path
	}
	if n.kind == KindFile {
		e.ModTime = time.Unix(0, n.modTime)
	}
	return e
}

// within reports whether p is base or inside it, by whole path components.
func within(p, base string) bool {
	if samePath(p, base) {
		return true
	}
	if !strings.HasSuffix(base, string(filepath.Separator)) {
		base += string(filepath.Separator)
	}
	return len(p) > len(base) && samePath(p[:len(base)], base)
}

// strictlyWithin reports whether p is inside base and not base itself.
func strictlyWithin(p, base string) bool {
	return base != "" && within(p, base) && !samePath(p, base)
}

// samePath compares two cleaned paths, ignoring case where the platform does (foldPaths).
func samePath(a, b string) bool {
	if foldPaths {
		return strings.EqualFold(a, b)
	}
	return a == b
}
