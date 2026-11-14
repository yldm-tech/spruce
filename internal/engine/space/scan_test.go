package space

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"sync/atomic"
	"testing"
)

// write creates the file at path (and its folders) with n bytes and returns the space it takes as the walk counts it.
func write(t testing.TB, path string, n int) int64 {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	info, err := os.Lstat(path)
	if err != nil {
		t.Fatal(err)
	}
	alloc, _, _ := usage(info)
	return alloc
}

func symlink(t *testing.T, target, link string) {
	t.Helper()
	if err := os.Symlink(target, link); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks need Developer Mode or administrator rights here: %v", err)
		}
		t.Fatal(err)
	}
}

// tempRoot returns a fresh folder with symlinks resolved, as the page's paths are.
func tempRoot(t testing.TB) string {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	return dir
}

const kb = 1024

// testOptions keeps files of 64 KB and up as nodes, so the trees stay small.
func testOptions() Options {
	return Options{Workers: 3, MinFileNode: 64 * kb, MaxChildren: 3, LargeFileMin: 128 * kb, MaxLargeFiles: 3}
}

func childByName(t *testing.T, tree *Tree, id int, name string) Entry {
	t.Helper()
	children, ok := tree.Children(id)
	if !ok {
		t.Fatalf("no children of %d", id)
	}
	for _, c := range children {
		if c.Name == name {
			return c
		}
	}
	t.Fatalf("no child %q of %d in %+v", name, id, children)
	return Entry{}
}

func childOfKind(children []Entry, kind Kind) (Entry, bool) {
	i := slices.IndexFunc(children, func(e Entry) bool { return e.Kind == kind })
	if i < 0 {
		return Entry{}, false
	}
	return children[i], true
}

func TestScanSizesAndFolding(t *testing.T) {
	root := filepath.Join(tempRoot(t), "root")
	var total int64
	total += write(t, filepath.Join(root, "big.bin"), 200*kb)
	small := write(t, filepath.Join(root, "a.txt"), 100) + write(t, filepath.Join(root, "b.txt"), 3000)
	total += small
	// many has seven folders of growing size; it keeps the three largest and folds four into 其他.
	var folded int64
	for i := range 7 {
		n := write(t, filepath.Join(root, "many", "d"+string(rune('0'+i)), "f.bin"), (i+1)*8*kb)
		total += n
		if i < 4 {
			folded += n
		}
	}
	deep := write(t, filepath.Join(root, "sub", "deeper", "c.bin"), 96*kb)
	total += deep

	tree, err := Scan(t.Context(), root, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	r := tree.Root()
	if r.Size != total || r.Files != 11 || r.Kind != KindDir || r.Path != root {
		t.Fatalf("root %+v, want size %d and 11 files", r, total)
	}
	st := tree.Stats()
	if st.Files != 11 || st.Dirs != 11 || st.Denied != 0 || st.Errors != 0 {
		t.Errorf("stats %+v", st)
	}

	children, _ := tree.Children(r.ID)
	if len(children) != 4 {
		t.Fatalf("root children %+v", children)
	}
	if !slices.IsSortedFunc(children, func(a, b Entry) int { return int(b.Size - a.Size) }) {
		t.Errorf("children not sorted largest first: %+v", children)
	}
	files, ok := childOfKind(children, KindFiles)
	if !ok || files.Count != 2 || files.Size != small || files.Files != 2 || files.Name != "" || files.Path != "" {
		t.Errorf("small files %+v, want 2 files of %d", files, small)
	}
	big := childByName(t, tree, r.ID, "big.bin")
	if big.Kind != KindFile || big.Files != 1 || big.ModTime.IsZero() || big.Path != filepath.Join(root, "big.bin") {
		t.Errorf("big.bin %+v", big)
	}

	many := childByName(t, tree, r.ID, "many")
	mc, _ := tree.Children(many.ID)
	if len(mc) != 4 {
		t.Fatalf("many keeps %d children: %+v", len(mc), mc)
	}
	other, ok := childOfKind(mc, KindOther)
	if !ok || other.Count != 4 || other.Size != folded || other.Files != 4 {
		t.Errorf("其他 %+v, want 4 folders of %d", other, folded)
	}
	for _, c := range mc {
		if c.Kind == KindDir && c.Name < "d4" {
			t.Errorf("kept a small folder %q", c.Name)
		}
	}
	if _, ok := tree.Children(other.ID); ok {
		t.Error("a folded group lists children")
	}

	sub := childByName(t, tree, r.ID, "sub")
	deeper := childByName(t, tree, sub.ID, "deeper")
	if deeper.Size != deep || sub.Size != deep {
		t.Errorf("sub %d, deeper %d, want %d", sub.Size, deeper.Size, deep)
	}
	chain := tree.Ancestors(deeper.ID)
	if len(chain) != 3 || chain[0].ID != r.ID || chain[1].Name != "sub" || chain[2].ID != deeper.ID {
		t.Errorf("ancestors %+v", chain)
	}
}

func TestScanLargeFilesComeFromTheWholeTree(t *testing.T) {
	root := filepath.Join(tempRoot(t), "root")
	write(t, filepath.Join(root, "x", "a.bin"), 128*kb)
	write(t, filepath.Join(root, "x", "b.bin"), 160*kb)
	write(t, filepath.Join(root, "y", "z", "c.bin"), 192*kb)
	write(t, filepath.Join(root, "y", "d.bin"), 224*kb)
	write(t, filepath.Join(root, "small.bin"), 64*kb)
	opts := testOptions()
	// Every folder keeps only its largest child, so x (and a.bin in it) is folded away but a.bin is still listed.
	opts.MaxChildren = 1
	opts.MaxLargeFiles = 4
	tree, err := Scan(t.Context(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, lf := range tree.LargeFiles() {
		names = append(names, filepath.Base(lf.Path))
		if lf.ModTime.IsZero() || lf.Size < 128*kb {
			t.Errorf("large file %+v", lf)
		}
	}
	if !slices.Equal(names, []string{"d.bin", "c.bin", "b.bin", "a.bin"}) {
		t.Errorf("large files %v", names)
	}
}

func TestScanNeverFollowsSymlinks(t *testing.T) {
	dir := tempRoot(t)
	root := filepath.Join(dir, "root")
	own := write(t, filepath.Join(root, "own.bin"), 64*kb)
	outside := filepath.Join(dir, "outside")
	write(t, filepath.Join(outside, "huge.bin"), 512*kb)
	symlink(t, outside, filepath.Join(root, "dir-link"))
	symlink(t, filepath.Join(outside, "huge.bin"), filepath.Join(root, "file-link"))

	tree, err := Scan(t.Context(), root, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r := tree.Root(); r.Size != own || r.Files != 1 {
		t.Errorf("root %+v, want only own.bin (%d)", r, own)
	}
	if len(tree.LargeFiles()) != 0 {
		t.Errorf("large files through a link: %+v", tree.LargeFiles())
	}
	if _, err := Scan(t.Context(), filepath.Join(root, "dir-link"), testOptions()); !errors.Is(err, ErrNotDir) {
		t.Errorf("scan of a link to a folder: %v", err)
	}
}

func TestScanCountsUnreadableFolders(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("folder permissions are ACLs on Windows")
	}
	if os.Getuid() == 0 {
		t.Skip("root reads every folder")
	}
	root := filepath.Join(tempRoot(t), "root")
	readable := write(t, filepath.Join(root, "ok", "a.bin"), 64*kb)
	write(t, filepath.Join(root, "locked", "secret.bin"), 64*kb)
	locked := filepath.Join(root, "locked")
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })

	tree, err := Scan(t.Context(), root, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	st := tree.Stats()
	if st.Denied != 1 || !slices.Equal(st.DeniedPaths, []string{locked}) {
		t.Errorf("denied %d %v", st.Denied, st.DeniedPaths)
	}
	if r := tree.Root(); r.Size != readable {
		t.Errorf("root size %d, want %d", r.Size, readable)
	}
	if e := childByName(t, tree, 0, "locked"); !e.Denied || e.Size != 0 {
		t.Errorf("locked %+v", e)
	}

	if err := os.Chmod(root, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(root, 0o755) })
	if _, err := Scan(t.Context(), root, testOptions()); !errors.Is(err, fs.ErrPermission) {
		t.Errorf("unreadable root: %v", err)
	}
}

// fakeDevices puts every folder named "mnt" on volume 2 and everything else on volume 1.
func fakeDevices(fi fs.FileInfo) (uint64, bool) {
	if fi.Name() == "mnt" {
		return 2, true
	}
	return 1, true
}

func TestScanStaysOnTheRootsVolume(t *testing.T) {
	root := filepath.Join(tempRoot(t), "root")
	own := write(t, filepath.Join(root, "a.bin"), 64*kb)
	mounted := write(t, filepath.Join(root, "mnt", "b.bin"), 128*kb)

	opts := testOptions()
	opts.devOf = fakeDevices
	tree, err := Scan(t.Context(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r, st := tree.Root(), tree.Stats(); r.Size != own || st.Mounts != 1 {
		t.Errorf("root %d with %d mounts left out, want %d and 1", r.Size, st.Mounts, own)
	}

	// A volume named in SameVolume counts as the root's, as the macOS data volume does for "/".
	opts.SameVolume = []string{filepath.Join(root, "mnt")}
	tree, err = Scan(t.Context(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if r, st := tree.Root(), tree.Stats(); r.Size != own+mounted || st.Mounts != 0 {
		t.Errorf("root %d with %d mounts left out, want %d and 0", r.Size, st.Mounts, own+mounted)
	}
}

func TestPlanForTheStartupDisk(t *testing.T) {
	skip, same := planFor(filepath.Join(tempRoot(t), "x"))
	if skip != nil || same != nil {
		t.Errorf("plan for a folder: %v %v", skip, same)
	}
	if runtime.GOOS != "darwin" {
		return
	}
	skip, same = planFor("/")
	if !slices.Contains(skip, "/System/Volumes") || !slices.Contains(skip, "/private/var/vm") || !slices.Equal(same, []string{"/System/Volumes/Data"}) {
		t.Errorf("plan for /: skip %v, same %v", skip, same)
	}
}

func TestScanSkipAndProgress(t *testing.T) {
	root := filepath.Join(tempRoot(t), "root")
	own := write(t, filepath.Join(root, "keep", "a.bin"), 64*kb)
	write(t, filepath.Join(root, "skip", "b.bin"), 64*kb)
	var calls atomic.Int64
	var last Progress
	opts := testOptions()
	opts.Skip = func(p string) bool { return filepath.Base(p) == "skip" }
	opts.Progress = func(p Progress) {
		calls.Add(1)
		last = p
	}
	tree, err := Scan(t.Context(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	if tree.Root().Size != own {
		t.Errorf("size %d, want %d", tree.Root().Size, own)
	}
	if calls.Load() == 0 || last.Files != 1 || last.Bytes != own || last.Dirs != 2 {
		t.Errorf("%d progress calls, last %+v", calls.Load(), last)
	}
}

func TestScanCountsHardLinksOnce(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("hard links are not detected on Windows")
	}
	root := filepath.Join(tempRoot(t), "root")
	n := write(t, filepath.Join(root, "a.bin"), 128*kb)
	if err := os.Link(filepath.Join(root, "a.bin"), filepath.Join(root, "b.bin")); err != nil {
		t.Fatal(err)
	}
	tree, err := Scan(t.Context(), root, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	if r := tree.Root(); r.Size != n || r.Files != 2 {
		t.Errorf("root %+v, want %d bytes in 2 files", r, n)
	}
}

func TestScanCancelled(t *testing.T) {
	root := filepath.Join(tempRoot(t), "root")
	for i := range 20 {
		write(t, filepath.Join(root, "d", string(rune('a'+i)), "f"), 10)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := Scan(ctx, root, testOptions()); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled scan: %v", err)
	}
	if _, err := Scan(t.Context(), "relative", testOptions()); err == nil {
		t.Error("relative root accepted")
	}
	if _, err := Scan(t.Context(), filepath.Join(root, "missing"), testOptions()); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("missing root: %v", err)
	}
}
