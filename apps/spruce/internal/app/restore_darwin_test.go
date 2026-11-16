package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"maps"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"syscall"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/fsx"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/schedule"
	"spruce/apps/spruce/internal/engine/trash"
	"spruce/apps/spruce/internal/engine/undo"
	"spruce/apps/spruce/internal/engine/uninstall"
)

// trashHome points HOME at a temporary home with its own .Trash, so trash.InTrash and trash.SystemRestorer see only that Trash; the real one is never touched. It returns the home, its Trash and a ledger in a temporary data folder.
func trashHome(t *testing.T) (home, bin string, l *undo.Ledger) {
	t.Helper()
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	home = filepath.Join(dir, "home")
	bin = filepath.Join(home, ".Trash")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", home)
	l, err = undo.Open(filepath.Join(dir, "data", "undo"))
	if err != nil {
		t.Fatal(err)
	}
	return home, bin, l
}

// moveIntoBin renames path into bin, as Foundation moves into ~/.Trash, and returns where it went with its identity.
func moveIntoBin(bin, path string) (to string, dev, ino uint64, err error) {
	to = filepath.Join(bin, filepath.Base(path))
	if err := os.Rename(path, to); err != nil {
		return "", 0, 0, err
	}
	info, err := os.Lstat(to)
	if err != nil {
		return "", 0, 0, err
	}
	st := info.Sys().(*syscall.Stat_t)
	return to, uint64(st.Dev), st.Ino, nil
}

// binDeleter is a cleanup.Deleter whose Trash moves into a fake Trash folder, except the paths in elsewhere, which it moves to a folder Spruce cannot put back from (as iCloud Drive's own trash). Remove deletes for real inside the test's folders.
type binDeleter struct {
	bin, elsewhere string
	away           map[string]bool
	// after, when set, runs after each move.
	after func(path string)
}

func (d *binDeleter) Remove(_ context.Context, path string, _ []string) (fsx.Removal, error) {
	if err := os.RemoveAll(path); err != nil {
		return fsx.Removal{}, err
	}
	return fsx.Removal{Freed: 1, Removed: 1}, nil
}

func (d *binDeleter) Trash(_ context.Context, path string, _ []string) (cleanup.Trashed, error) {
	bin := d.bin
	if d.away[path] {
		bin = d.elsewhere
	}
	to, dev, ino, err := moveIntoBin(bin, path)
	if err != nil {
		return cleanup.Trashed{}, err
	}
	if d.after != nil {
		d.after(path)
	}
	return cleanup.Trashed{Path: to, Dev: dev, Ino: ino}, nil
}

// execCleaner is a cleanup.Scanner over a real Executor: a DeleteTrash category of the files trash names and a DeletePermanent one of the files remove names, all under root.
type execCleaner struct {
	exec *cleanup.Executor
	ids  []string
}

func newExecCleaner(t *testing.T, root string, del cleanup.Deleter, trashNames, removeNames []string) *execCleaner {
	t.Helper()
	items := func(names []string) []cleanup.Item {
		var out []cleanup.Item
		for _, n := range names {
			p := filepath.Join(root, n)
			if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
				t.Fatal(err)
			}
			if err := os.WriteFile(p, []byte("12345"), 0o644); err != nil {
				t.Fatal(err)
			}
			info, err := os.Lstat(p)
			if err != nil {
				t.Fatal(err)
			}
			out = append(out, cleanup.NewItem(p, info, 5))
		}
		return out
	}
	cats := []cleanup.Category{
		{ID: "installers", Title: "安装包", Kind: cleanup.KindInstaller, Deletion: cleanup.DeleteTrash, Roots: []string{root}, Items: items(trashNames)},
		{ID: "caches", Title: "缓存", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent, Roots: []string{root}, Items: items(removeNames)},
	}
	res := cleanup.NewResult(cats, time.Now())
	s := &execCleaner{exec: cleanup.NewExecutor(del)}
	for _, c := range res.Categories {
		for _, it := range c.Items {
			s.ids = append(s.ids, it.ID)
		}
	}
	if err := s.exec.Remember(res); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *execCleaner) Scan(context.Context, func(cleanup.Progress)) (cleanup.ScanResult, error) {
	return cleanup.ScanResult{}, errors.New("not used")
}

func (s *execCleaner) Clean(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	return s.exec.Clean(ctx, req, progress)
}

// ledgerFirstStore is a history store that notes, at each Append, whether the ledger already held the run's entry.
type ledgerFirstStore struct {
	fakeStore
	ledger  *undo.Ledger
	entered []bool
}

func (s *ledgerFirstStore) Append(r history.Run) error {
	_, err := s.ledger.Load(r.ID)
	s.mu.Lock()
	s.entered = append(s.entered, err == nil)
	s.mu.Unlock()
	return s.fakeStore.Append(r)
}

func TestCleanWithRecordsTheLedgerBeforeTheHistory(t *testing.T) {
	home, bin, l := trashHome(t)
	root := filepath.Join(home, "Downloads")
	elsewhere := filepath.Join(home, "Library", "Mobile Documents", ".Trash")
	if err := os.MkdirAll(elsewhere, 0o755); err != nil {
		t.Fatal(err)
	}
	del := &binDeleter{bin: bin, elsewhere: elsewhere, away: map[string]bool{filepath.Join(root, "cloud.dmg"): true}}
	s := newExecCleaner(t, root, del, []string{"app.dmg", "cloud.dmg"}, []string{"cache.db"})
	store := &ledgerFirstStore{ledger: l}
	c := NewCleanup(store, home, slog.New(slog.DiscardHandler), schedule.NewActivity(nil))
	SetLedger(c, l)
	RegisterCleaner(c, CleanerInstallers, s)

	run, err := c.CleanWith(t.Context(), CleanerInstallers, cleanup.CleanRequest{CategoryIDs: []string{"installers", "caches"}}, CleanOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(run.Result.Moved) != 2 || run.Undo == nil || *run.Undo != (history.UndoInfo{Items: 1, Bytes: 5}) {
		t.Fatalf("moved %d, undo %+v", len(run.Result.Moved), run.Undo)
	}
	if len(store.appended) != 1 || !store.entered[0] {
		t.Fatalf("history %+v, ledger first %v", store.appended, store.entered)
	}
	got := store.appended[0]
	if got.ID != run.RunID || got.Undo == nil || *got.Undo != *run.Undo {
		t.Errorf("run %+v undo %+v", got, got.Undo)
	}
	if !slices.Contains(got.Details, "这次清理里直接删除的 1 项无法放回") {
		t.Errorf("details %q", got.Details)
	}
	if names, _ := filepath.Glob(filepath.Join(filepath.Dir(bin), "..", "data", "undo", "*.partial")); len(names) != 0 {
		t.Errorf("journals left: %v", names)
	}

	// 放回原处 puts the installer back; the item in the cloud's trash is not Spruce's to put back.
	r := NewRestore(l, trash.SystemRestorer(), store, schedule.NewActivity(nil), home, slog.New(slog.DiscardHandler))
	out, err := r.Run(t.Context(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Restored != 1 || out.Failed != 0 || out.Bytes != 5 || out.RunID == "" {
		t.Errorf("outcome %+v", out)
	}
	if _, err := os.Stat(filepath.Join(root, "app.dmg")); err != nil {
		t.Errorf("app.dmg did not come back: %v", err)
	}
	restore := store.appended[len(store.appended)-1]
	if restore.Kind != history.KindRestore || restore.RestoreOf != run.RunID || restore.ID != out.RunID || restore.Summary == nil ||
		*restore.Summary != (history.Totals{Items: 1, Trashed: 5}) {
		t.Errorf("restore run %+v %+v", restore, restore.Summary)
	}
	if _, err := r.Run(t.Context(), run.RunID); err == nil || err.Error() != "这条记录里的项目都已放回原处" {
		t.Errorf("second restore: %v", err)
	}
	// An automatic rule never takes the installer again.
	if _, ok := l.Restored(time.Now())[cleanup.ItemID(filepath.Join(root, "app.dmg"))]; !ok {
		t.Error("the put back item is not remembered")
	}
}

func TestCleanWithRecordsEvenWhenStopped(t *testing.T) {
	home, bin, l := trashHome(t)
	root := filepath.Join(home, "Downloads")
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	// The clean is stopped right after the first move.
	del := &binDeleter{bin: bin, after: func(string) { cancel() }}
	s := newExecCleaner(t, root, del, []string{"a.dmg", "b.dmg"}, nil)
	store := &fakeStore{}
	c := NewCleanup(store, home, slog.New(slog.DiscardHandler), schedule.NewActivity(nil))
	SetLedger(c, l)
	RegisterCleaner(c, CleanerInstallers, s)
	run, err := c.CleanWith(ctx, CleanerInstallers, cleanup.CleanRequest{CategoryIDs: []string{"installers"}}, CleanOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !run.Result.Interrupted || len(store.appended) != 1 || store.appended[0].Undo == nil || store.appended[0].Undo.Items != 1 {
		t.Fatalf("result %+v history %+v", run.Result, store.appended)
	}
	if _, _, ok := l.Restorable(run.RunID, time.Now()); !ok {
		t.Error("the stopped clean's move is not restorable")
	}
}

func TestCleanWithWithoutAnythingRestorable(t *testing.T) {
	home, _, l := trashHome(t)
	root := filepath.Join(home, "Downloads")
	// recordingDeleter says the item went somewhere outside every Trash folder.
	s := newExecCleaner(t, root, &recordingDeleter{}, []string{"a.dmg"}, nil)
	store := &fakeStore{}
	c := NewCleanup(store, home, slog.New(slog.DiscardHandler), schedule.NewActivity(nil))
	SetLedger(c, l)
	RegisterCleaner(c, CleanerInstallers, s)
	run, err := c.CleanWith(t.Context(), CleanerInstallers, cleanup.CleanRequest{CategoryIDs: []string{"installers"}}, CleanOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if run.Undo != nil || store.appended[0].Undo != nil {
		t.Errorf("undo %+v / %+v for a move that cannot be put back", run.Undo, store.appended[0].Undo)
	}
	if _, err := l.Load(run.RunID); !errors.Is(err, undo.ErrNoEntry) {
		t.Errorf("an entry was written: %v", err)
	}
}

// homeBin is a trash.Bin that moves into a fake Trash folder, one path at a time as Space asks.
type homeBin struct {
	bin   string
	calls [][]string
}

func (b *homeBin) MoveToTrash(_ context.Context, paths []string) ([]trash.Outcome, error) {
	b.calls = append(b.calls, paths)
	out := make([]trash.Outcome, len(paths))
	for i, p := range paths {
		to, dev, ino, err := moveIntoBin(b.bin, p)
		out[i] = trash.Outcome{Path: p, TrashedPath: to, Dev: dev, Ino: ino, Err: err}
	}
	return out, nil
}
func (b *homeBin) Size(context.Context) (trash.TrashUsage, error)  { return trash.TrashUsage{}, nil }
func (b *homeBin) Empty(context.Context) (trash.TrashUsage, error) { return trash.TrashUsage{}, nil }

func TestSpaceTrashComesBackIntoItsParentFolder(t *testing.T) {
	f := newSpaceFixture(t)
	bin := filepath.Join(f.home, ".Trash")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	t.Setenv("HOME", f.home)
	l, err := undo.Open(filepath.Join(t.TempDir(), "undo"))
	if err != nil {
		t.Fatal(err)
	}
	hb := &homeBin{bin: bin}
	f.svc.bin = hb
	SetSpaceLedger(f.svc, l)
	sum := f.scan(t)
	film := filepath.Join(f.data, "videos", "film.mov")
	clip := filepath.Join(f.data, "videos", "clip.mov")
	res, err := f.svc.Trash(t.Context(), sum.ScanID, []string{film, clip})
	if err != nil {
		t.Fatal(err)
	}
	if res.Items != 2 || !res.Undoable || res.NotUndoable != 0 || res.RunID == "" || len(hb.calls) != 2 {
		t.Fatalf("result %+v, calls %v", res, hb.calls)
	}
	run := f.store.appended[0]
	if run.ID != res.RunID || run.Undo == nil || run.Undo.Items != 2 || run.Undo.Bytes != res.Trashed {
		t.Errorf("run %+v undo %+v", run, run.Undo)
	}
	e, err := l.Load(res.RunID)
	if err != nil {
		t.Fatal(err)
	}
	for _, m := range e.Moves {
		if m.Root != filepath.Join(f.data, "videos") || m.CategoryID != undo.SpaceCategory {
			t.Errorf("move %+v", m)
		}
	}
	// Something new took film.mov's name: as the user's own file it comes back beside it.
	if err := os.WriteFile(film, []byte("new"), 0o644); err != nil {
		t.Fatal(err)
	}
	r := NewRestore(l, trash.SystemRestorer(), f.store, schedule.NewActivity(nil), f.home, slog.New(slog.DiscardHandler))
	out, err := r.Run(t.Context(), res.RunID)
	if err != nil || out.Restored != 2 {
		t.Fatalf("outcome %+v %v", out, err)
	}
	for _, p := range []string{clip, filepath.Join(f.data, "videos", "film 2.mov")} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s: %v", p, err)
		}
	}
}

func TestUninstallMovesComeBackIntoTheirCategoryRoot(t *testing.T) {
	home, bin, l := trashHome(t)
	apps := filepath.Join(home, "Applications")
	caches := filepath.Join(home, "Library", "Caches")
	bundle := filepath.Join(apps, "Foo.app")
	cache := filepath.Join(caches, "com.example.foo")
	for _, d := range []string{filepath.Join(bundle, "Contents"), cache} {
		if err := os.MkdirAll(d, 0o755); err != nil {
			t.Fatal(err)
		}
	}
	eng := &movingUninstallEngine{bin: bin, moves: map[string]string{bundle: apps, cache: caches}}
	eng.result = uninstall.Result{App: "Foo", BundleRemoved: true, Clean: cleanup.CleanResult{Removed: 2, Trashed: 20}}
	store := &fakeStore{}
	u := NewUninstall(eng, store, home, slog.New(slog.DiscardHandler), schedule.NewActivity(nil))
	SetUninstallLedger(u, l)
	removed, err := u.Remove(t.Context(), UninstallRequest{AppID: "a", LeftoverIDs: []string{"c"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(store.appended) != 1 || store.appended[0].Undo == nil || store.appended[0].Undo.Items != 2 {
		t.Fatalf("history %+v", store.appended)
	}
	if removed.RunID != store.appended[0].ID || !removed.Undoable {
		t.Errorf("outcome run %q undoable %v", removed.RunID, removed.Undoable)
	}
	runID := store.appended[0].ID
	r := NewRestore(l, trash.SystemRestorer(), store, schedule.NewActivity(nil), home, slog.New(slog.DiscardHandler))
	out, err := r.Run(t.Context(), runID)
	if err != nil || out.Restored != 2 {
		t.Fatalf("outcome %+v %v", out, err)
	}
	for _, p := range []string{filepath.Join(bundle, "Contents"), cache} {
		if _, err := os.Stat(p); err != nil {
			t.Errorf("%s did not come back: %v", p, err)
		}
	}
}

// movingUninstallEngine is a fakeUninstallEngine whose Remove moves each path of moves into bin and reports it through the context, as the Executor does, with the category root it maps to.
type movingUninstallEngine struct {
	fakeUninstallEngine
	bin   string
	moves map[string]string
}

func (f *movingUninstallEngine) Remove(ctx context.Context, req uninstall.Request, progress func(cleanup.Progress)) (uninstall.Result, error) {
	onMoved := cleanup.OnMoved(ctx)
	for _, p := range slices.Sorted(maps.Keys(f.moves)) {
		to, dev, ino, err := moveIntoBin(f.bin, p)
		if err != nil {
			return uninstall.Result{}, err
		}
		onMoved(cleanup.Moved{ItemID: cleanup.ItemID(p), CategoryID: "x", Kind: cleanup.KindLeftover, Path: p, TrashedPath: to, Root: f.moves[p], Size: 10, Dir: true, Dev: dev, Ino: ino})
	}
	return f.fakeUninstallEngine.Remove(ctx, req, progress)
}

func TestRestoreRefusals(t *testing.T) {
	home, _, l := trashHome(t)
	store := &fakeStore{}
	r := NewRestore(l, trash.SystemRestorer(), store, schedule.NewActivity(nil), home, slog.New(slog.DiscardHandler))
	if _, err := r.Run(t.Context(), "20261114T010412Z-0000"); err == nil || err.Error() != "这条记录已经不能放回原处了" {
		t.Errorf("unknown run: %v", err)
	}
	if _, err := r.Run(t.Context(), "../../etc"); err == nil || err.Error() != "这条记录已经不能放回原处了" {
		t.Errorf("bad run ID: %v", err)
	}
	r.canRestore = false
	if _, err := r.Run(t.Context(), "x"); err == nil || err.Error() != "这台电脑上不能放回原处" {
		t.Errorf("unsupported: %v", err)
	}
	busy := NewRestore(l, trash.SystemRestorer(), store, schedule.NewActivity(func() (func() error, bool, error) { return nil, false, nil }), home,
		slog.New(slog.DiscardHandler))
	if _, err := busy.Run(t.Context(), "x"); !errors.Is(err, schedule.ErrBusyElsewhere) || err.Error() != "另一个 Spruce 正在清理，请稍后再试" {
		t.Errorf("busy elsewhere: %v", err)
	}
	if len(store.appended) != 0 {
		t.Errorf("refusals were recorded: %+v", store.appended)
	}
}

func TestRestoreRecordsWhatCouldNotComeBack(t *testing.T) {
	home, bin, l := trashHome(t)
	root := filepath.Join(home, "Downloads")
	del := &binDeleter{bin: bin}
	s := newExecCleaner(t, root, del, []string{"gone.dmg", "kept.dmg"}, nil)
	store := &fakeStore{}
	c := NewCleanup(store, home, slog.New(slog.DiscardHandler), schedule.NewActivity(nil))
	SetLedger(c, l)
	RegisterCleaner(c, CleanerInstallers, s)
	run, err := c.CleanWith(t.Context(), CleanerInstallers, cleanup.CleanRequest{CategoryIDs: []string{"installers"}}, CleanOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(filepath.Join(bin, "gone.dmg")); err != nil {
		t.Fatal(err)
	}
	r := NewRestore(l, trash.SystemRestorer(), store, schedule.NewActivity(nil), home, slog.New(slog.DiscardHandler))
	out, err := r.Run(t.Context(), run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join("~", "Downloads", "gone.dmg") + "：已从废纸篓清除，无法放回"
	if out.Restored != 1 || out.Failed != 1 || !slices.Equal(out.Reasons, []string{want}) {
		t.Errorf("outcome %+v", out)
	}
	rec := store.appended[len(store.appended)-1]
	if !slices.Equal(rec.Details, []string{want}) || rec.Summary.Failed != 1 {
		t.Errorf("restore run %+v", rec)
	}
	// It tried every item, so the run no longer offers 放回原处 (2.11), though one item could not come back.
	h := NewHistory(store, l, slog.New(slog.DiscardHandler))
	if got := h.Restorable(store.appended, time.Now()); got[run.RunID] {
		t.Errorf("the run is still offered after a restore that tried everything: %v", got)
	}
}

// cancelling is a Restorer that puts back through trash.SystemRestorer and cancels after the first item, as quitting Spruce does mid-restore.
type cancelling struct{ cancel func() }

func (c cancelling) Restore(ctx context.Context, req trash.RestoreRequest) (string, error) {
	defer c.cancel()
	return trash.SystemRestorer().Restore(ctx, req)
}

func TestRestoreStoppedPartWayIsNotWhole(t *testing.T) {
	home, bin, l := trashHome(t)
	root := filepath.Join(home, "Downloads")
	s := newExecCleaner(t, root, &binDeleter{bin: bin}, []string{"a.dmg", "b.dmg", "c.dmg"}, nil)
	store := &fakeStore{}
	c := NewCleanup(store, home, slog.New(slog.DiscardHandler), schedule.NewActivity(nil))
	SetLedger(c, l)
	RegisterCleaner(c, CleanerInstallers, s)
	run, err := c.CleanWith(t.Context(), CleanerInstallers, cleanup.CleanRequest{CategoryIDs: []string{"installers"}}, CleanOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	defer cancel()
	r := NewRestore(l, cancelling{cancel: cancel}, store, schedule.NewActivity(nil), home, slog.New(slog.DiscardHandler))
	out, err := r.Run(ctx, run.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if out.Restored != 1 || out.Failed != 0 {
		t.Errorf("outcome %+v", out)
	}
	rec := store.appended[len(store.appended)-1]
	if rec.Kind != history.KindRestore || rec.Summary.Items != 1 || rec.Summary.Failed != 2 ||
		!slices.Contains(rec.Details, "放回在中途停止，其余项目还在废纸篓里") {
		t.Errorf("restore run %+v", rec)
	}
	// The two items it did not get to can still be put back.
	h := NewHistory(store, l, slog.New(slog.DiscardHandler))
	if got := h.Restorable(store.appended, time.Now()); !got[run.RunID] {
		t.Errorf("a restore that stopped part-way hid the run: %v", got)
	}
}

func TestRecoverInterruptedAddsTheMissingRun(t *testing.T) {
	home, bin, l := trashHome(t)
	root := filepath.Join(home, "Library", "Caches")
	p := filepath.Join(root, "com.example")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	started := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	j, err := l.Begin(undo.JournalMeta{RunID: "20261114T090000Z-abcd", Kind: history.KindAuto, Rule: "app-caches", Started: started})
	if err != nil {
		t.Fatal(err)
	}
	to, dev, ino, err := moveIntoBin(bin, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Add(cleanup.Moved{ItemID: cleanup.ItemID(p), Kind: cleanup.KindCache, Path: p, TrashedPath: to, Root: root, Size: 300, Dir: true, Dev: dev, Ino: ino}); err != nil {
		t.Fatal(err)
	}
	j.Close()
	// Spruce ended here. At the next launch, while another Spruce process holds the clean lock, recovery waits.
	next, _ := undo.Open(filepath.Join(filepath.Dir(home), "data", "undo"))
	store := &fakeStore{}
	held := schedule.NewActivity(func() (func() error, bool, error) { return nil, false, nil })
	RecoverInterrupted(t.Context(), next, store, held, slog.New(slog.DiscardHandler))
	if len(store.appended) != 0 {
		t.Fatalf("recovered while another process cleans: %+v", store.appended)
	}
	RecoverInterrupted(t.Context(), next, store, schedule.NewActivity(nil), slog.New(slog.DiscardHandler))
	if len(store.appended) != 1 {
		t.Fatalf("history %+v", store.appended)
	}
	run := store.appended[0]
	if run.ID != "20261114T090000Z-abcd" || run.Kind != history.KindAuto || run.Rule != "app-caches" || !run.StartedAt.Equal(started) ||
		run.Summary == nil || run.Summary.Items != 1 || run.Summary.Trashed != 300 || run.Undo == nil || run.Undo.Items != 1 {
		t.Errorf("run %+v", run)
	}
	if want := "Spruce 在清理时退出，这条记录是下次打开时补上的"; !slices.Contains(run.Details, want) || !slices.Contains(run.Details, "清理在中途停止，其余项目没有改动") {
		t.Errorf("details %q", run.Details)
	}
	RecoverInterrupted(t.Context(), next, store, schedule.NewActivity(nil), slog.New(slog.DiscardHandler))
	if len(store.appended) != 1 {
		t.Errorf("recovered twice: %+v", store.appended)
	}
}

// TestRecoverInterruptedCountsPermanentDeletions: a clean that only deleted for good before Spruce ended still gets its history run; a mixed one counts both kinds.
func TestRecoverInterruptedCountsPermanentDeletions(t *testing.T) {
	home, bin, l := trashHome(t)
	root := filepath.Join(home, "Library", "Caches")
	started := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	only, err := l.Begin(undo.JournalMeta{RunID: "20261114T090000Z-0001", Kind: history.KindSystemJunk, Started: started})
	if err != nil {
		t.Fatal(err)
	}
	for _, name := range []string{"a", "b"} {
		if err := only.AddRemoved(cleanup.Removed{ItemID: name, Path: filepath.Join(root, name), Freed: 100}); err != nil {
			t.Fatal(err)
		}
	}
	only.Close()
	mixed, err := l.Begin(undo.JournalMeta{RunID: "20261114T090000Z-0002", Kind: history.KindSmartScan, Started: started})
	if err != nil {
		t.Fatal(err)
	}
	p := filepath.Join(root, "com.example")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	to, dev, ino, err := moveIntoBin(bin, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := mixed.Add(cleanup.Moved{ItemID: cleanup.ItemID(p), Kind: cleanup.KindCache, Path: p, TrashedPath: to, Root: root, Size: 300, Dir: true, Dev: dev, Ino: ino}); err != nil {
		t.Fatal(err)
	}
	if err := mixed.AddRemoved(cleanup.Removed{ItemID: "c", Path: filepath.Join(root, "c"), Freed: 50}); err != nil {
		t.Fatal(err)
	}
	mixed.Close()
	next, _ := undo.Open(filepath.Join(filepath.Dir(home), "data", "undo"))
	store := &fakeStore{}
	RecoverInterrupted(t.Context(), next, store, schedule.NewActivity(nil), slog.New(slog.DiscardHandler))
	if len(store.appended) != 2 {
		t.Fatalf("history %+v", store.appended)
	}
	byID := map[string]history.Run{}
	for _, r := range store.appended {
		byID[r.ID] = r
	}
	r := byID["20261114T090000Z-0001"]
	if r.Kind != history.KindSystemJunk || r.Summary == nil || r.Summary.Items != 2 || r.Summary.Freed != 200 || r.Summary.Trashed != 0 || r.Undo != nil {
		t.Errorf("permanent-only run %+v %+v", r, r.Summary)
	}
	if !slices.Contains(r.Details, "已删除 2 项") || slices.ContainsFunc(r.Details, func(d string) bool { return strings.HasPrefix(d, "已移到") }) {
		t.Errorf("permanent-only details %q", r.Details)
	}
	r = byID["20261114T090000Z-0002"]
	if r.Summary == nil || r.Summary.Items != 2 || r.Summary.Freed != 50 || r.Summary.Trashed != 300 || r.Undo == nil || r.Undo.Items != 1 {
		t.Errorf("mixed run %+v %+v", r, r.Summary)
	}
	if !slices.Contains(r.Details, "已移到废纸篓 1 项") || !slices.Contains(r.Details, "已删除 1 项") {
		t.Errorf("mixed details %q", r.Details)
	}
}

// TestBeginUndoJournalsRemovals: the clean's context journals what the executor deletes for good, ahead of the callback it already carried.
func TestBeginUndoJournalsRemovals(t *testing.T) {
	home, _, l := trashHome(t)
	var passed []cleanup.Removed
	ctx := cleanup.WithOnRemoved(t.Context(), func(r cleanup.Removed) { passed = append(passed, r) })
	ctx, u := beginUndo(ctx, l, slog.New(slog.DiscardHandler), undo.JournalMeta{RunID: "20261114T090000Z-0003", Kind: history.KindSystemJunk, Started: time.Now()})
	r := cleanup.Removed{ItemID: "a", Path: filepath.Join(home, "Library", "Caches", "a"), Freed: 10}
	cleanup.OnRemoved(ctx)(r)
	if len(passed) != 1 || passed[0] != r {
		t.Errorf("passed on %+v", passed)
	}
	u.journal.Close()
	next, _ := undo.Open(filepath.Join(filepath.Dir(home), "data", "undo"))
	recs, err := next.Recover()
	if err != nil || len(recs) != 1 || len(recs[0].Removed) != 1 || recs[0].Removed[0] != r {
		t.Errorf("recovered %+v %v", recs, err)
	}
}

func TestHistoryRestorable(t *testing.T) {
	home, bin, l := trashHome(t)
	root := filepath.Join(home, "Downloads")
	now := time.Now()
	record := func(id string, at time.Time, name string) history.Run {
		t.Helper()
		p := filepath.Join(root, name)
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		to, dev, ino, err := moveIntoBin(bin, p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.Record(id, at, []cleanup.Moved{{Path: p, TrashedPath: to, Root: root, Size: 1, Dev: dev, Ino: ino}}); err != nil {
			t.Fatal(err)
		}
		return history.Run{ID: id, Kind: history.KindInstallers, StartedAt: at, EndedAt: at, Undo: &history.UndoInfo{Items: 1, Bytes: 1}}
	}
	fresh := record("fresh", now.Add(-time.Hour), "a")
	old := record("old", now.Add(-undo.Window-time.Hour), "b")
	done := record("done", now.Add(-time.Hour), "c")
	partly := record("partly", now.Add(-time.Hour), "d")
	noUndo := history.Run{ID: "plain", Kind: history.KindSystemJunk, EndedAt: now}
	gone := history.Run{ID: "gone", EndedAt: now, Undo: &history.UndoInfo{Items: 1}}
	runs := []history.Run{
		{ID: "r1", Kind: history.KindRestore, RestoreOf: "done", Summary: &history.Totals{Items: 1}},
		{ID: "r2", Kind: history.KindRestore, RestoreOf: "partly", Summary: &history.Totals{Failed: 1}},
		fresh, old, done, partly, noUndo, gone,
	}
	h := NewHistory(&fakeStore{}, l, slog.New(slog.DiscardHandler))
	got := h.Restorable(runs, now)
	want := map[string]bool{"fresh": true, "partly": true}
	if fmt.Sprint(got) != fmt.Sprint(want) {
		t.Errorf("Restorable = %v, want %v", got, want)
	}
	// Windows: nothing can be put back.
	h.canRestore = false
	if got := h.Restorable(runs, now); len(got) != 0 {
		t.Errorf("Restorable without restore = %v", got)
	}
	if got := NewHistory(&fakeStore{}, nil, slog.New(slog.DiscardHandler)).Restorable(runs, now); len(got) != 0 {
		t.Errorf("Restorable without a ledger = %v", got)
	}
}

func TestHistoryClearClearsTheLedgerAndWhatGoesWithIt(t *testing.T) {
	home, _, l := trashHome(t)
	dir := filepath.Join(filepath.Dir(home), "data", "undo")
	// A journal a Spruce that ended left behind, and one of a clean this process is running.
	earlier, err := undo.Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	if _, err := earlier.Begin(undo.JournalMeta{RunID: "j", Started: time.Now()}); err != nil {
		t.Fatal(err)
	}
	running, err := l.Begin(undo.JournalMeta{RunID: "k", Started: time.Now()})
	if err != nil {
		t.Fatal(err)
	}
	defer running.Close()
	store := &fakeStore{}
	h := NewHistory(store, l, slog.New(slog.DiscardHandler))
	var mu sync.Mutex
	var calls []string
	OnClear(h, func() error { mu.Lock(); calls = append(calls, "cache"); mu.Unlock(); return nil })
	OnClear(h, func() error { mu.Lock(); calls = append(calls, "growth"); mu.Unlock(); return nil })
	if err := h.Clear(); err != nil {
		t.Fatal(err)
	}
	if store.cleared != 1 || !slices.Equal(calls, []string{"cache", "growth"}) {
		t.Errorf("cleared %d, calls %v", store.cleared, calls)
	}
	if _, err := os.Stat(filepath.Join(dir, "j.partial")); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the left-over journal survived: %v", err)
	}
	if _, err := os.Stat(filepath.Join(dir, "k.partial")); err != nil {
		t.Errorf("the running clean's journal was deleted: %v", err)
	}
	OnClear(h, func() error { return errors.New("disk full") })
	if err := h.Clear(); err == nil || !strings.Contains(err.Error(), "操作历史已清空") {
		t.Errorf("err %v", err)
	}
}

func TestPruneLedgerKeepsTheRunsTheHistoryHolds(t *testing.T) {
	home, bin, l := trashHome(t)
	root := filepath.Join(home, "Downloads")
	now := time.Now()
	ids := []string{"20260101T000000Z-held", "20260101T000000Z-gone", "20260101T000000Z-new"}
	for i, id := range ids {
		p := filepath.Join(root, fmt.Sprint(i))
		if err := os.MkdirAll(root, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := os.WriteFile(p, nil, 0o644); err != nil {
			t.Fatal(err)
		}
		to, dev, ino, err := moveIntoBin(bin, p)
		if err != nil {
			t.Fatal(err)
		}
		if _, err := l.Record(id, now, []cleanup.Moved{{Path: p, TrashedPath: to, Root: root, Dev: dev, Ino: ino}}); err != nil {
			t.Fatal(err)
		}
	}
	// The first two were written long ago; the third a moment ago, its history run not appended yet.
	long := now.Add(-2 * time.Hour)
	for _, id := range ids[:2] {
		if err := os.Chtimes(filepath.Join(filepath.Dir(home), "data", "undo", id+".json"), long, long); err != nil {
			t.Fatal(err)
		}
	}
	store := &fakeStore{runs: []history.Run{{ID: ids[0]}}}
	pruneLedger(l, store, now, slog.New(slog.DiscardHandler))
	if _, err := l.Load(ids[0]); err != nil {
		t.Errorf("the held run was pruned: %v", err)
	}
	if _, err := l.Load(ids[1]); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("the orphaned run survived: %v", err)
	}
	// An entry written a moment ago may not have its history run yet.
	if _, err := l.Load(ids[2]); err != nil {
		t.Errorf("an entry just written was pruned: %v", err)
	}
}

// pruningStore is a history store whose Append first runs the daily prune of the ledger with the runs it held before, as FollowSettings' first prune can at launch, between RecoverInterrupted's ledger entry and its history run.
type pruningStore struct {
	fakeStore
	ledger *undo.Ledger
}

func (s *pruningStore) Append(r history.Run) error {
	stale := &fakeStore{runs: slices.Clone(s.runs)}
	pruneLedger(s.ledger, stale, time.Now(), slog.New(slog.DiscardHandler))
	s.mu.Lock()
	s.runs = append(s.runs, r)
	s.mu.Unlock()
	return s.fakeStore.Append(r)
}

func TestRecoveredEntrySurvivesAPruneAtLaunch(t *testing.T) {
	home, bin, l := trashHome(t)
	root := filepath.Join(home, "Library", "Caches")
	p := filepath.Join(root, "com.example")
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	// A clean that began three hours ago and was cut short.
	runID := newRunID(time.Now().Add(-3 * time.Hour))
	j, err := l.Begin(undo.JournalMeta{RunID: runID, Kind: history.KindSmartScan, Started: time.Now().Add(-3 * time.Hour)})
	if err != nil {
		t.Fatal(err)
	}
	to, dev, ino, err := moveIntoBin(bin, p)
	if err != nil {
		t.Fatal(err)
	}
	if err := j.Add(cleanup.Moved{ItemID: cleanup.ItemID(p), Kind: cleanup.KindCache, Path: p, TrashedPath: to, Root: root, Size: 3, Dir: true, Dev: dev, Ino: ino}); err != nil {
		t.Fatal(err)
	}
	j.Close()
	old := time.Now().Add(-3 * time.Hour)
	if err := os.Chtimes(filepath.Join(filepath.Dir(home), "data", "undo", runID+".partial"), old, old); err != nil {
		t.Fatal(err)
	}
	next, _ := undo.Open(filepath.Join(filepath.Dir(home), "data", "undo"))
	store := &pruningStore{ledger: next}
	RecoverInterrupted(t.Context(), next, store, schedule.NewActivity(nil), slog.New(slog.DiscardHandler))
	if len(store.appended) != 1 || store.appended[0].Undo == nil {
		t.Fatalf("history %+v", store.appended)
	}
	h := NewHistory(store, next, slog.New(slog.DiscardHandler))
	if got := h.Restorable(store.appended, time.Now()); !got[runID] {
		t.Errorf("the prune dropped the recovered entry: %v", got)
	}
}
