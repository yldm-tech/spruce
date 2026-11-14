package app

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/fsx"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/schedule"
	"github.com/yldm-tech/spruce/internal/host/instance"
)

// recordingDeleter deletes nothing: it records what it was asked to remove or trash.
type recordingDeleter struct {
	mu               sync.Mutex
	removed, trashed []string
}

func (d *recordingDeleter) Remove(_ context.Context, path string, _ []string) (fsx.Removal, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.removed = append(d.removed, path)
	return fsx.Removal{Freed: 1, Removed: 1}, nil
}

func (d *recordingDeleter) Trash(_ context.Context, path string, _ []string) (cleanup.Trashed, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.trashed = append(d.trashed, path)
	return cleanup.Trashed{Path: path + ".trashed"}, nil
}

// execScanner is a cleanup.Scanner over a real Executor and a recording Deleter: a cache category (DeletePermanent) of two files in a temporary folder.
type execScanner struct {
	exec  *cleanup.Executor
	del   *recordingDeleter
	scan  cleanup.ScanResult
	ids   []string
	calls int
	// during, when set, runs inside Clean before the Executor.
	during func(ctx context.Context)
}

func newExecScanner(t *testing.T) *execScanner {
	t.Helper()
	dir := t.TempDir()
	var items []cleanup.Item
	var ids []string
	for _, name := range []string{"a", "b"} {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("x"), 0o644); err != nil {
			t.Fatal(err)
		}
		info, err := os.Lstat(p)
		if err != nil {
			t.Fatal(err)
		}
		it := cleanup.NewItem(p, info, 100)
		items = append(items, it)
		ids = append(ids, it.ID)
	}
	scan := cleanup.NewResult([]cleanup.Category{{ID: "caches", Title: "缓存", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent, Roots: []string{dir}, Items: items}}, time.Now())
	del := &recordingDeleter{}
	s := &execScanner{exec: cleanup.NewExecutor(del), del: del, scan: scan, ids: ids}
	if err := s.exec.Remember(scan); err != nil {
		t.Fatal(err)
	}
	return s
}

func (s *execScanner) Scan(context.Context, func(cleanup.Progress)) (cleanup.ScanResult, error) {
	return s.scan, nil
}

func (s *execScanner) Clean(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	s.calls++
	if s.during != nil {
		s.during(ctx)
	}
	return s.exec.Clean(ctx, req, progress)
}

func newGatedCleanup(t *testing.T, a *schedule.Activity) (*Cleanup, *fakeStore) {
	t.Helper()
	store := &fakeStore{}
	c := NewCleanup(store, t.TempDir(), slog.New(slog.DiscardHandler), a)
	c.goos = "darwin"
	return c, store
}

func TestCleanWithAutoOnlyMovesToTheTrash(t *testing.T) {
	a := schedule.NewActivity(nil)
	c, store := newGatedCleanup(t, a)
	s := newExecScanner(t)
	RegisterCleaner(c, CleanerSmartScanBackground, s)
	run, endRun, err := a.BeginBackground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer endRun()
	auto, endAuto, err := a.BeginAutoClean(run)
	if err != nil {
		t.Fatal(err)
	}
	defer endAuto()
	// The caller asks for the default mode: CleanWith forces trash-only anyway.
	got, err := c.CleanWith(auto, CleanerSmartScanBackground, cleanup.CleanRequest{CategoryIDs: []string{"caches"}, ItemIDs: s.ids},
		CleanOptions{Kind: history.KindAuto, Rule: "app-caches", Detail: "旧的应用缓存：2 项"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(s.del.removed) != 0 || len(s.del.trashed) != 2 {
		t.Fatalf("removed %v trashed %v", s.del.removed, s.del.trashed)
	}
	if run.Err() != nil {
		t.Error("the automatic clean cancelled its own background run")
	}
	if len(store.appended) != 1 {
		t.Fatalf("history %+v", store.appended)
	}
	r := store.appended[0]
	if r.ID != got.RunID || r.Kind != history.KindAuto || r.Rule != "app-caches" || len(r.Details) == 0 || r.Details[0] != "旧的应用缓存：2 项" {
		t.Errorf("run %+v, CleanRun %+v", r, got)
	}
	if len(got.Result.Moved) != 2 {
		t.Errorf("moved %+v", got.Result.Moved)
	}
}

func TestCleanWithRefusesAutomaticCleansOutsideTheRules(t *testing.T) {
	a := schedule.NewActivity(nil)
	run, endRun, err := a.BeginBackground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer endRun()
	auto, endAuto, err := a.BeginAutoClean(run)
	if err != nil {
		t.Fatal(err)
	}
	defer endAuto()
	autoOpts := CleanOptions{Kind: history.KindAuto, Rule: "app-caches"}
	cases := map[string]struct {
		ctx     context.Context
		cleaner CleanerID
		items   bool
		opts    CleanOptions
		goos    string
	}{
		"not from BeginAutoClean":    {ctx: t.Context(), cleaner: CleanerSmartScanBackground, items: true, opts: autoOpts, goos: "darwin"},
		"no items named":             {ctx: auto, cleaner: CleanerSmartScanBackground, items: false, opts: autoOpts, goos: "darwin"},
		"the foreground instance":    {ctx: auto, cleaner: CleanerSmartScan, items: true, opts: autoOpts, goos: "darwin"},
		"on Windows":                 {ctx: auto, cleaner: CleanerSmartScanBackground, items: true, opts: autoOpts, goos: "windows"},
		"automatic context, manual":  {ctx: auto, cleaner: CleanerSmartScanBackground, items: true, opts: CleanOptions{}, goos: "darwin"},
		"automatic context, restore": {ctx: auto, cleaner: CleanerSmartScanBackground, items: true, opts: CleanOptions{Kind: history.KindRestore}, goos: "darwin"},
	}
	for name, tc := range cases {
		c, store := newGatedCleanup(t, a)
		c.goos = tc.goos
		s := newExecScanner(t)
		RegisterCleaner(c, tc.cleaner, s)
		req := cleanup.CleanRequest{CategoryIDs: []string{"caches"}}
		if tc.items {
			req.ItemIDs = s.ids
		}
		_, err := c.CleanWith(tc.ctx, tc.cleaner, req, tc.opts, nil)
		if !errors.Is(err, schedule.ErrNotAuto) || strings.Contains(err.Error(), "schedule:") {
			t.Errorf("%s: err %v", name, err)
		}
		if s.calls != 0 || len(s.del.removed)+len(s.del.trashed) != 0 || len(store.appended) != 0 {
			t.Errorf("%s: the cleaner ran (%d calls)", name, s.calls)
		}
	}
}

func TestCleanWithRecordsKindDetailAndRunsInTheGate(t *testing.T) {
	a := schedule.NewActivity(nil)
	c, store := newGatedCleanup(t, a)
	s := newExecScanner(t)
	cleaning := false
	s.during = func(context.Context) { cleaning = a.Cleaning() }
	RegisterCleaner(c, CleanerSmartScan, s)
	got, err := c.CleanWith(t.Context(), CleanerSmartScan, cleanup.CleanRequest{CategoryIDs: []string{"caches"}}, CleanOptions{Detail: "命令行"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !cleaning || a.Cleaning() {
		t.Errorf("inside the gate %v, after %v", cleaning, a.Cleaning())
	}
	if len(s.del.removed) != 2 {
		t.Errorf("a manual clean deleted %v", s.del.removed)
	}
	r := store.appended[0]
	if r.Kind != history.KindSmartScan || r.Details[0] != "命令行" || r.ID != got.RunID || r.Rule != "" {
		t.Errorf("run %+v", r)
	}
}

// TestCleanWithRecordsAfterTheContextEnds: a clean stopped half way (quitting) still records what it did.
func TestCleanWithRecordsAfterTheContextEnds(t *testing.T) {
	a := schedule.NewActivity(nil)
	c, store := newGatedCleanup(t, a)
	s := newExecScanner(t)
	ctx, cancel := context.WithCancel(t.Context())
	s.exec = cleanup.NewExecutor(&hookDeleter{recordingDeleter: s.del, after: cancel})
	if err := s.exec.Remember(s.scan); err != nil {
		t.Fatal(err)
	}
	RegisterCleaner(c, CleanerSystemJunk, s)
	got, err := c.CleanWith(ctx, CleanerSystemJunk, cleanup.CleanRequest{CategoryIDs: []string{"caches"}}, CleanOptions{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !got.Result.Interrupted || got.Result.Removed != 1 || len(store.appended) != 1 || store.appended[0].ID != got.RunID {
		t.Errorf("result %+v, history %+v", got, store.appended)
	}
	if !slices.Contains(store.appended[0].Details, "清理在中途停止，其余项目没有改动") {
		t.Errorf("details %v", store.appended[0].Details)
	}
}

// hookDeleter calls after once the first removal is done.
type hookDeleter struct {
	*recordingDeleter
	after func()
}

func (d *hookDeleter) Remove(ctx context.Context, path string, roots []string) (fsx.Removal, error) {
	r, err := d.recordingDeleter.Remove(ctx, path, roots)
	d.after()
	return r, err
}

func TestCleanWithBusyErrorsKeepTheirSentinel(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clean.lock")
	release, held, err := instance.LockFile(path)
	if err != nil || !held {
		t.Fatal(held, err)
	}
	defer func() { _ = release() }()
	a := schedule.NewActivity(func() (func() error, bool, error) { return instance.LockFile(path) })
	c, store := newGatedCleanup(t, a)
	s := newExecScanner(t)
	RegisterCleaner(c, CleanerSmartScan, s)
	_, err = c.CleanWith(t.Context(), CleanerSmartScan, cleanup.CleanRequest{CategoryIDs: []string{"caches"}}, CleanOptions{}, nil)
	if !errors.Is(err, schedule.ErrBusyElsewhere) || err.Error() != "另一个 Spruce 正在清理，请稍后再试" {
		t.Errorf("err %v", err)
	}
	if s.calls != 0 || len(store.appended) != 0 {
		t.Error("the cleaner ran while another process cleaned")
	}

	broken := schedule.NewActivity(func() (func() error, bool, error) { return nil, false, errors.New("permission denied") })
	c, _ = newGatedCleanup(t, broken)
	RegisterCleaner(c, CleanerSmartScan, s)
	_, err = c.CleanWith(t.Context(), CleanerSmartScan, cleanup.CleanRequest{CategoryIDs: []string{"caches"}}, CleanOptions{}, nil)
	if !errors.Is(err, schedule.ErrLock) || err.Error() != "无法确认是否有另一个 Spruce 正在清理，请稍后再试" {
		t.Errorf("err %v", err)
	}

	// A cleaner's own busy refusal keeps cleanup.ErrBusy.
	c, _ = newGatedCleanup(t, schedule.NewActivity(nil))
	RegisterCleaner(c, CleanerSystemJunk, &fakeScanner{clnErr: cleanup.ErrBusy})
	_, err = c.CleanWith(t.Context(), CleanerSystemJunk, cleanup.CleanRequest{CategoryIDs: []string{"caches"}}, CleanOptions{}, nil)
	if !errors.Is(err, cleanup.ErrBusy) || err.Error() != "已有清理正在进行，请等它结束后再试" {
		t.Errorf("err %v", err)
	}
}

// TestCleanStopsTheBackgroundRun: a manual clean cancels the scheduled check and waits for it.
func TestCleanStopsTheBackgroundRun(t *testing.T) {
	a := schedule.NewActivity(nil)
	c, _ := newGatedCleanup(t, a)
	s := newExecScanner(t)
	RegisterCleaner(c, CleanerSmartScan, s)
	run, endRun, err := a.BeginBackground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		<-run.Done()
		endRun()
	}()
	if _, err := c.CleanWith(t.Context(), CleanerSmartScan, cleanup.CleanRequest{CategoryIDs: []string{"caches"}}, CleanOptions{}, nil); err != nil {
		t.Fatal(err)
	}
	if run.Err() == nil {
		t.Error("the background run kept going during a clean")
	}
}
