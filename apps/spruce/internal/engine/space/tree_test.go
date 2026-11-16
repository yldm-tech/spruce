package space

import (
	"path/filepath"
	"slices"
	"testing"
)

func TestRemoveFileShrinksEveryFolderAbove(t *testing.T) {
	root := filepath.Join(tempRoot(t), "root")
	keep := write(t, filepath.Join(root, "a", "keep.bin"), 64*kb)
	gone := write(t, filepath.Join(root, "a", "b", "gone.bin"), 256*kb)
	other := write(t, filepath.Join(root, "c.bin"), 96*kb)
	tree, err := Scan(t.Context(), root, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	a := childByName(t, tree, 0, "a")
	b := childByName(t, tree, a.ID, "b")
	g := childByName(t, tree, b.ID, "gone.bin")

	e, ok := tree.Remove(g.Path)
	if !ok || e.Size != gone || e.ID != g.ID {
		t.Fatalf("Remove = %+v, %v", e, ok)
	}
	if r := tree.Root(); r.Size != keep+other || r.Files != 2 {
		t.Errorf("root %+v, want %d bytes in 2 files", r, keep+other)
	}
	if a2, _ := tree.Entry(a.ID); a2.Size != keep || a2.Files != 1 {
		t.Errorf("a %+v", a2)
	}
	if b2, _ := tree.Entry(b.ID); b2.Size != 0 || b2.Files != 0 {
		t.Errorf("b %+v", b2)
	}
	if bc, _ := tree.Children(b.ID); len(bc) != 0 {
		t.Errorf("b still lists %+v", bc)
	}
	if _, ok := tree.Entry(g.ID); ok {
		t.Error("the removed node keeps its ID")
	}
	if slices.ContainsFunc(tree.LargeFiles(), func(lf LargeFile) bool { return lf.Path == g.Path }) {
		t.Error("the removed file is still a large file")
	}
	if _, ok := tree.Remove(g.Path); ok {
		t.Error("removed twice")
	}
	// The order follows the new sizes: c.bin is now larger than a.
	if rc, _ := tree.Children(0); rc[0].Name != "c.bin" {
		t.Errorf("root children %+v", rc)
	}
}

func TestRemoveFolderDropsItsLargeFiles(t *testing.T) {
	root := filepath.Join(tempRoot(t), "root")
	write(t, filepath.Join(root, "dir", "x", "big.bin"), 256*kb)
	write(t, filepath.Join(root, "dir", "big2.bin"), 192*kb)
	rest := write(t, filepath.Join(root, "rest.bin"), 160*kb)
	tree, err := Scan(t.Context(), root, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	dir := childByName(t, tree, 0, "dir")
	x := childByName(t, tree, dir.ID, "x")
	if _, ok := tree.Remove(dir.Path); !ok {
		t.Fatal("folder not removed")
	}
	if r := tree.Root(); r.Size != rest || r.Files != 1 {
		t.Errorf("root %+v", r)
	}
	if _, ok := tree.Entry(x.ID); ok {
		t.Error("a node under the removed folder keeps its ID")
	}
	if lf := tree.LargeFiles(); len(lf) != 1 || filepath.Base(lf[0].Path) != "rest.bin" {
		t.Errorf("large files %+v", lf)
	}
}

func TestRemoveFoldedLargeFile(t *testing.T) {
	root := filepath.Join(tempRoot(t), "root")
	write(t, filepath.Join(root, "d", "big.bin"), 512*kb)
	folded := write(t, filepath.Join(root, "d", "x", "inner.bin"), 192*kb)
	foldedSibling := write(t, filepath.Join(root, "d", "y", "inner.bin"), 128*kb)
	opts := testOptions()
	opts.MaxChildren = 1
	tree, err := Scan(t.Context(), root, opts)
	if err != nil {
		t.Fatal(err)
	}
	d := childByName(t, tree, 0, "d")
	before := d.Size
	path := filepath.Join(root, "d", "x", "inner.bin")
	found, ok := tree.Find(path)
	if !ok || found.ID != -1 || found.Size != folded {
		t.Fatalf("Find(folded large file) = %+v, %v", found, ok)
	}
	if _, ok := tree.Remove(path); !ok {
		t.Fatal("folded large file not removed")
	}
	d2, _ := tree.Entry(d.ID)
	if d2.Size != before-folded {
		t.Errorf("d %d, want %d", d2.Size, before-folded)
	}
	dc, _ := tree.Children(d.ID)
	g, ok := childOfKind(dc, KindOther)
	// The folded folder x is still there (now empty), so the group still stands for two entries.
	if !ok || g.Size != foldedSibling || g.Count != 2 {
		t.Errorf("其他 %+v, want %d bytes", g, foldedSibling)
	}
}

func TestFindOnlyKnowsTheScan(t *testing.T) {
	root := filepath.Join(tempRoot(t), "root")
	write(t, filepath.Join(root, "a.bin"), 64*kb)
	write(t, filepath.Join(root, "tiny.txt"), 10)
	tree, err := Scan(t.Context(), root, testOptions())
	if err != nil {
		t.Fatal(err)
	}
	for _, p := range []string{root, filepath.Join(root, "tiny.txt"), filepath.Join(root, "missing"), filepath.Dir(root), "relative"} {
		if e, ok := tree.Find(p); ok {
			t.Errorf("Find(%q) = %+v", p, e)
		}
		if _, ok := tree.Remove(p); ok {
			t.Errorf("Remove(%q) succeeded", p)
		}
	}
	if e, ok := tree.Find(filepath.Join(root, "a.bin")); !ok || e.Kind != KindFile {
		t.Errorf("Find(a.bin) = %+v, %v", e, ok)
	}
}
