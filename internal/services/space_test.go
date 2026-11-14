package services

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/engine/space"
	"github.com/yldm-tech/spruce/internal/engine/trash"
)

// spaceBin is a Trash that records what it was given and fails the paths in fail; it moves nothing.
type spaceBin struct {
	mu    sync.Mutex
	moved []string
	fail  map[string]error
}

func (b *spaceBin) MoveToTrash(_ context.Context, paths []string) ([]trash.Outcome, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]trash.Outcome, len(paths))
	for i, p := range paths {
		out[i].Path = p
		if err := b.fail[p]; err != nil {
			out[i].Err = err
			continue
		}
		b.moved = append(b.moved, p)
	}
	return out, nil
}
func (b *spaceBin) Size(context.Context) (trash.TrashUsage, error)  { return trash.TrashUsage{}, nil }
func (b *spaceBin) Empty(context.Context) (trash.TrashUsage, error) { return trash.TrashUsage{}, nil }

// spaceFixture is a home folder with a data folder to scan:
//
//	home/data/videos/film.mov  3 MB
//	home/data/videos/clip.mov  2 MB
//	home/data/notes.txt        small
//	home/data/keep/pinned.bin  2 MB   (excluded in the settings)
type spaceFixture struct {
	home, data string
	svc        *Space
	bin        *spaceBin
	store      *fakeStore
	sizes      map[string]int64
}

func newSpaceFixture(t *testing.T) *spaceFixture {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	f := &spaceFixture{home: filepath.Join(dir, "home"), bin: &spaceBin{}, store: &fakeStore{}, sizes: map[string]int64{}}
	f.data = filepath.Join(f.home, "data")
	for rel, n := range map[string]int{
		"videos/film.mov": 3 << 20,
		"videos/clip.mov": 2 << 20,
		"notes.txt":       100,
		"keep/pinned.bin": 2 << 20,
	} {
		p := filepath.Join(f.data, rel)
		if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
			t.Fatal(err)
		}
		f.sizes[rel] = int64(n)
	}
	cfg := settings.Defaults(f.home)
	cfg.Cleanup.Exclusions = []string{filepath.Join(f.data, "keep")}
	cfg.Cleanup.LargeFileMinMB = 2
	f.svc = NewSpace(f.home, func() settings.AppSettings { return cfg }, f.bin, f.store, slog.New(slog.DiscardHandler))
	f.svc.volumes = func(context.Context) ([]space.Volume, error) { return nil, errors.New("no disks in tests") }
	return f
}

func (f *spaceFixture) scan(t *testing.T) SpaceSummary {
	t.Helper()
	sum, err := f.svc.Scan(t.Context(), f.data, nil)
	if err != nil {
		t.Fatal(err)
	}
	return sum
}

func findEntry(t *testing.T, entries []SpaceEntry, name string) SpaceEntry {
	t.Helper()
	i := slices.IndexFunc(entries, func(e SpaceEntry) bool { return e.Name == name })
	if i < 0 {
		t.Fatalf("no %q in %+v", name, entries)
	}
	return entries[i]
}

func TestSpaceScanAndBrowse(t *testing.T) {
	f := newSpaceFixture(t)
	if f.svc.Current() != nil {
		t.Fatal("a scan before the first")
	}
	sum := f.scan(t)
	if sum.Name != "data" || sum.Root.Kind != SpaceDir || sum.Root.Files != 4 || sum.Root.Size < 7<<20 || sum.ScanID == "" || sum.DeniedPaths == nil {
		t.Errorf("summary %+v", sum)
	}
	if cur := f.svc.Current(); cur == nil || cur.ScanID != sum.ScanID {
		t.Errorf("Current = %+v", cur)
	}

	root, err := f.svc.Children(sum.ScanID, sum.Root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if root.Entry.Name != "data" || len(root.Ancestors) != 1 || root.Ancestors[0].Name != "data" {
		t.Errorf("root listing %+v", root)
	}
	videos := findEntry(t, root.Children, "videos")
	if videos.Kind != SpaceDir || videos.Locked != "" || videos.Path != filepath.Join(f.data, "videos") {
		t.Errorf("videos %+v", videos)
	}
	if keep := findEntry(t, root.Children, "keep"); keep.Locked != "这个位置在「设置」的排除列表中" {
		t.Errorf("excluded folder %+v", keep)
	}
	small := slices.IndexFunc(root.Children, func(e SpaceEntry) bool { return e.Kind == SpaceFiles })
	if small < 0 || root.Children[small].Count != 1 || root.Children[small].Locked != "" {
		t.Errorf("small files in %+v", root.Children)
	}

	inside, err := f.svc.Children(sum.ScanID, videos.ID)
	if err != nil {
		t.Fatal(err)
	}
	if len(inside.Ancestors) != 2 || inside.Ancestors[1].Name != "videos" || len(inside.Children) != 2 || inside.Children[0].Name != "film.mov" {
		t.Errorf("videos listing %+v", inside)
	}
	if _, err := f.svc.Children(sum.ScanID, inside.Children[0].ID); err == nil || err.Error() != "只能打开文件夹" {
		t.Errorf("children of a file: %v", err)
	}
	if _, err := f.svc.Children("old", videos.ID); err == nil || err.Error() != "扫描结果已过期，请重新扫描" {
		t.Errorf("stale scan: %v", err)
	}

	large, err := f.svc.LargeFiles(sum.ScanID)
	if err != nil {
		t.Fatal(err)
	}
	var names []string
	for _, e := range large {
		names = append(names, e.Name)
	}
	if !slices.Equal(names, []string{"film.mov", "clip.mov", "pinned.bin"}) && !slices.Equal(names, []string{"film.mov", "pinned.bin", "clip.mov"}) {
		t.Errorf("large files %v", names)
	}
}

func TestSpaceTrashUpdatesTheTreeAndHistory(t *testing.T) {
	f := newSpaceFixture(t)
	sum := f.scan(t)
	film := filepath.Join(f.data, "videos", "film.mov")
	filmEntry, _ := f.svc.LargeFiles(sum.ScanID)

	res, err := f.svc.Trash(t.Context(), sum.ScanID, []string{film})
	if err != nil {
		t.Fatal(err)
	}
	if res.Items != 1 || len(res.Failed) != 0 || res.Trashed != filmEntry[0].Size || !slices.Equal(f.bin.moved, []string{film}) {
		t.Fatalf("result %+v, moved %v", res, f.bin.moved)
	}
	if res.Root.Size != sum.Root.Size-res.Trashed || res.Root.Files != sum.Root.Files-1 || res.Root.Name != "data" {
		t.Errorf("root after trash %+v, before %+v", res.Root, sum.Root)
	}
	if cur := f.svc.Current(); cur.Root.Size != res.Root.Size {
		t.Errorf("Current not updated: %+v", cur.Root)
	}
	root, _ := f.svc.Children(sum.ScanID, sum.Root.ID)
	videos := findEntry(t, root.Children, "videos")
	inside, _ := f.svc.Children(sum.ScanID, videos.ID)
	if len(inside.Children) != 1 || inside.Children[0].Name != "clip.mov" || videos.Size != inside.Children[0].Size || videos.Files != 1 {
		t.Errorf("videos %+v holding %+v", videos, inside.Children)
	}
	if large, _ := f.svc.LargeFiles(sum.ScanID); slices.ContainsFunc(large, func(e SpaceEntry) bool { return e.Path == film }) {
		t.Error("the trashed file is still listed")
	}

	if len(f.store.appended) != 1 {
		t.Fatalf("history %+v", f.store.appended)
	}
	run := f.store.appended[0]
	want := "已移到" + binName() + "：" + filepath.Join("~", "data", "videos", "film.mov")
	if run.Kind != history.KindSpace || run.Summary == nil || run.Summary.Trashed != res.Trashed || run.Summary.Items != 1 || run.Summary.Freed != 0 || !slices.Equal(run.Details, []string{want}) {
		t.Errorf("history run %+v %+v", run, run.Summary)
	}
}

func TestSpaceTrashRefusals(t *testing.T) {
	f := newSpaceFixture(t)
	sum := f.scan(t)
	clip := filepath.Join(f.data, "videos", "clip.mov")
	pinned := filepath.Join(f.data, "keep", "pinned.bin")
	unknown := filepath.Join(f.data, "videos", "new.mov")
	swapped := filepath.Join(f.data, "videos", "film.mov")
	// film.mov becomes a symlink after the scan: nothing the user did not see may go.
	if err := os.Remove(swapped); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(clip, swapped); err != nil {
		if runtime.GOOS == "windows" {
			t.Skipf("symlinks need Developer Mode or administrator rights here: %v", err)
		}
		t.Fatal(err)
	}
	f.bin.fail = map[string]error{clip: fs.ErrPermission}

	res, err := f.svc.Trash(t.Context(), sum.ScanID, []string{pinned, unknown, swapped, clip, f.data, f.home})
	if err != nil {
		t.Fatal(err)
	}
	reasons := map[string]string{}
	for _, fl := range res.Failed {
		reasons[fl.Path] = fl.Reason
	}
	want := map[string]string{
		pinned:  "这个位置在「设置」的排除列表中",
		unknown: "不在这次扫描的结果里，请重新扫描",
		swapped: "扫描后被替换过，请重新扫描",
		clip:    "没有权限移动这个项目",
		f.data:  "不在这次扫描的结果里，请重新扫描",
		f.home:  "不在这次扫描的结果里，请重新扫描",
	}
	for p, w := range want {
		if reasons[p] != w {
			t.Errorf("reason for %q = %q, want %q", p, reasons[p], w)
		}
	}
	if res.Items != 0 || res.Trashed != 0 || res.Root.Size != sum.Root.Size || len(f.bin.moved) != 0 {
		t.Errorf("result %+v, moved %v", res, f.bin.moved)
	}
	if len(f.store.appended) != 1 || f.store.appended[0].Summary.Failed != 6 {
		t.Errorf("history %+v", f.store.appended)
	}

	if _, err := f.svc.Trash(t.Context(), "old", []string{clip}); err == nil || err.Error() != "扫描结果已过期，请重新扫描" {
		t.Errorf("stale scan: %v", err)
	}
	if _, err := f.svc.Trash(t.Context(), sum.ScanID, nil); err == nil || err.Error() != "没有选择任何项目" {
		t.Errorf("nothing selected: %v", err)
	}
}

func TestSpaceTopLevelOfHomeIsLocked(t *testing.T) {
	f := newSpaceFixture(t)
	sum, err := f.svc.Scan(t.Context(), f.home, nil)
	if err != nil {
		t.Fatal(err)
	}
	if sum.Name != "个人文件夹" {
		t.Errorf("name %q", sum.Name)
	}
	root, err := f.svc.Children(sum.ScanID, sum.Root.ID)
	if err != nil {
		t.Fatal(err)
	}
	if data := findEntry(t, root.Children, "data"); !strings.HasPrefix(data.Locked, "个人文件夹和磁盘最上一层的项目请在") {
		t.Errorf("data %+v", data)
	}
	res, err := f.svc.Trash(t.Context(), sum.ScanID, []string{f.data})
	if err != nil || res.Items != 0 || len(res.Failed) != 1 || len(f.bin.moved) != 0 {
		t.Errorf("trash of a top-level folder: %+v, %v", res, err)
	}
}

func TestSpaceScanErrors(t *testing.T) {
	f := newSpaceFixture(t)
	cases := map[string]string{
		"relative":                         "请选择一个文件夹",
		filepath.Join(f.home, "missing"):   "这个文件夹已经不存在了",
		filepath.Join(f.data, "notes.txt"): "只能分析文件夹",
	}
	for path, want := range cases {
		if _, err := f.svc.Scan(t.Context(), path, nil); err == nil || err.Error() != want {
			t.Errorf("Scan(%q) = %v, want %q", path, err, want)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := f.svc.Scan(ctx, f.data, nil); err == nil || err.Error() != "已取消" {
		t.Errorf("cancelled: %v", err)
	}
	if f.svc.Current() != nil {
		t.Error("a failed scan was kept")
	}

	f.svc.scanning.Store(true)
	if _, err := f.svc.Scan(t.Context(), f.data, nil); err == nil || err.Error() != "已有扫描正在进行，请等它结束后再试" {
		t.Errorf("busy: %v", err)
	}
	if _, err := f.svc.Trash(t.Context(), "x", []string{f.data}); err == nil || err.Error() != "正在扫描，请等扫描结束后再试" {
		t.Errorf("trash while scanning: %v", err)
	}
	f.svc.scanning.Store(false)
}

func TestSpaceLocationsAndChooser(t *testing.T) {
	f := newSpaceFixture(t)
	f.svc.volumes = func(context.Context) ([]space.Volume, error) {
		return []space.Volume{{Path: "/", Name: "Macintosh HD", Total: 10, Free: 4, Startup: true}}, nil
	}
	loc := f.svc.Locations(t.Context())
	if len(loc.Volumes) != 1 || loc.Volumes[0].Name != "Macintosh HD" || len(loc.Targets) == 0 || loc.Targets[0].ID != "home" || loc.Targets[0].Path != f.home {
		t.Errorf("locations %+v", loc)
	}
	f.svc.volumes = func(context.Context) ([]space.Volume, error) { return nil, errors.New("boom") }
	if loc := f.svc.Locations(t.Context()); loc.Volumes == nil || len(loc.Volumes) != 0 || len(loc.Targets) == 0 {
		t.Errorf("locations without disks %+v", loc)
	}

	f.svc.choose = func(context.Context, string) (string, error) { return f.data, nil }
	if p, err := f.svc.ChooseFolder(t.Context()); err != nil || p != f.data {
		t.Errorf("ChooseFolder = %q, %v", p, err)
	}
	f.svc.choose = func(context.Context, string) (string, error) { return "", errors.New("no window") }
	if _, err := f.svc.ChooseFolder(t.Context()); err == nil || err.Error() != "无法打开选择文件夹的窗口" {
		t.Errorf("chooser failure: %v", err)
	}
}
