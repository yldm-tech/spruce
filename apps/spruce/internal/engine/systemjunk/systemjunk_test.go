package systemjunk

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/fsx"
	"spruce/apps/spruce/internal/engine/settings"
	"spruce/apps/spruce/internal/engine/trash"
)

// recorder logs, in order, what the fake Trash and the fake deleter were asked to do.
type recorder struct {
	mu  sync.Mutex
	log []string
}

func (r *recorder) add(s string) {
	r.mu.Lock()
	r.log = append(r.log, s)
	r.mu.Unlock()
}

func (r *recorder) entries() []string {
	r.mu.Lock()
	defer r.mu.Unlock()
	return slices.Clone(r.log)
}

// fakeBin is a Trash that never touches the real one.
type fakeBin struct {
	rec      *recorder
	usage    trash.TrashUsage
	sizeErr  error
	emptyErr error
}

func (b *fakeBin) MoveToTrash(context.Context, []string) ([]trash.Outcome, error) {
	return nil, errors.New("fakeBin: MoveToTrash is not used")
}

func (b *fakeBin) Size(context.Context) (trash.TrashUsage, error) { return b.usage, b.sizeErr }

func (b *fakeBin) Empty(context.Context) (trash.TrashUsage, error) {
	b.rec.add("empty")
	if b.emptyErr != nil {
		return trash.TrashUsage{Bytes: 100}, b.emptyErr
	}
	return b.usage, nil
}

// fakeDeleter deletes permanently with the real fsx.SafeRemove (inside the test's folder) and stands in for the Trash with os.RemoveAll after the same root check.
type fakeDeleter struct{ rec *recorder }

func (d fakeDeleter) Remove(ctx context.Context, path string, roots []string) (fsx.Removal, error) {
	d.rec.add("remove:" + filepath.Base(path))
	return fsx.SafeRemove(ctx, path, roots)
}

func (d fakeDeleter) Trash(_ context.Context, path string, roots []string) (cleanup.Trashed, error) {
	if err := fsx.CheckTrashable(path, roots); err != nil {
		return cleanup.Trashed{}, err
	}
	d.rec.add("trash:" + filepath.Base(path))
	return cleanup.Trashed{}, os.RemoveAll(path)
}

// now is the clock of every test; files are dated relative to it.
var now = time.Now()

// write creates the file rel under base with n bytes, modified age before now.
func write(t *testing.T, base, rel string, n int, age time.Duration) string {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	setAge(t, p, age)
	return p
}

func setAge(t *testing.T, p string, age time.Duration) {
	t.Helper()
	ts := now.Add(-age)
	if err := os.Chtimes(p, ts, ts); err != nil {
		t.Fatal(err)
	}
}

func mkdir(t *testing.T, base, rel string) string {
	t.Helper()
	p := filepath.Join(base, filepath.FromSlash(rel))
	if err := os.MkdirAll(p, 0o755); err != nil {
		t.Fatal(err)
	}
	return p
}

type fixture struct {
	home string
	rec  *recorder
	bin  *fakeBin
	env  Env
	// exclusions are returned by env.Settings.
	exclusions []string
}

func newFixture(t *testing.T) *fixture {
	t.Helper()
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	fx := &fixture{home: home, rec: &recorder{}}
	fx.bin = &fakeBin{rec: fx.rec, usage: trash.TrashUsage{Bytes: 5000, Items: 3}}
	fx.env = Env{
		Home:      home,
		Downloads: filepath.Join(home, "Downloads"),
		TrashPath: mkdir(t, home, ".Trash"),
		Bin:       fx.bin,
		Running:   func(context.Context) (Running, error) { return Running{}, nil },
		Settings: func() settings.AppSettings {
			return settings.AppSettings{Cleanup: settings.CleanupSettings{Exclusions: fx.exclusions}}
		},
		Deleter: fakeDeleter{rec: fx.rec},
		Now:     func() time.Time { return now },
		Getenv:  func(string) string { return "" },
	}
	return fx
}

// darwinHome builds a macOS home with junk, things that look like junk but are not offered, and things that are not junk.
func darwinHome(t *testing.T, home string) {
	t.Helper()
	day := 24 * time.Hour
	for _, rel := range []string{
		"Library/Caches/com.example.app/data.bin",
		"Library/Caches/com.running.app/x",
		"Library/Caches/com.running.app.ShipIt/y",
		"Library/Caches/com.apple.bird/z",
		"Library/Caches/com.apple.iCloudHelper/z",
		"Library/Caches/CloudKit/z",
		"Library/Caches/Homebrew/z",
		"Library/Caches/go-build/z",
		"Library/Caches/gopls/z",
		"Library/Caches/Mozilla.sccache/z",
		"Library/Caches/ai.yldm.spruce/z",
		"Library/Caches/.hidden",
		"Library/Caches/Google/Chrome/Default/Cache/f",
		"Library/Caches/Google/Chrome/Default/Code Cache/f",
		"Library/Application Support/Google/Chrome/Default/GPUCache/f",
		"Library/Application Support/Google/Chrome/Default/Cookies",
		"Library/Application Support/Google/Chrome/Default/History",
		"Library/Application Support/Google/Chrome/GrShaderCache/f",
		"Library/Application Support/Google/Chrome/NotAProfile/Cache/f",
		"Library/Caches/Firefox/Profiles/abc.default/cache2/f",
		"Library/Caches/Firefox/Profiles/abc.default/cookies.sqlite",
		"Library/Caches/com.apple.Safari/WebKitCache/f",
		"Library/Logs/App/log.txt",
		"Library/Logs/DiagnosticReports/a.crash",
	} {
		write(t, home, rel, 2000, 3*day)
	}
	mkdir(t, home, "Library/Caches/empty")
	outside := mkdir(t, home, "precious")
	write(t, outside, "keep.txt", 2000, 0)
	if err := os.Symlink(outside, filepath.Join(home, "Library/Caches/link")); err != nil {
		t.Fatal(err)
	}
	write(t, home, "Downloads/a.crdownload", 2000, 2*day)
	write(t, home, "Downloads/b.part", 2000, time.Hour)
	write(t, home, "Downloads/c.txt", 2000, 2*day)
	write(t, home, "Downloads/d.download/data", 2000, 2*day)
	setAge(t, filepath.Join(home, "Downloads/d.download"), 2*day)
}

func scan(t *testing.T, c *Cleaner) cleanup.ScanResult {
	t.Helper()
	res, err := c.Scan(t.Context(), nil)
	if err != nil {
		t.Fatal(err)
	}
	return res
}

func category(t *testing.T, res cleanup.ScanResult, id string) cleanup.Category {
	t.Helper()
	for _, c := range res.Categories {
		if c.ID == id {
			return c
		}
	}
	t.Fatalf("no category %q in %v", id, ids(res))
	return cleanup.Category{}
}

func ids(res cleanup.ScanResult) []string {
	var out []string
	for _, c := range res.Categories {
		out = append(out, c.ID)
	}
	return out
}

// rels lists a category's item paths relative to base, sorted.
func rels(t *testing.T, base string, c cleanup.Category) []string {
	t.Helper()
	var out []string
	for _, it := range c.Items {
		r, err := filepath.Rel(base, it.Path)
		if err != nil {
			t.Fatal(err)
		}
		out = append(out, filepath.ToSlash(r))
	}
	slices.Sort(out)
	return out
}

func notes(c cleanup.Category) map[string]string {
	out := map[string]string{}
	for _, it := range c.Items {
		out[filepath.Base(it.Path)] = it.Note
	}
	return out
}

func TestDarwinScan(t *testing.T) {
	fx := newFixture(t)
	darwinHome(t, fx.home)
	fx.env.Running = func(context.Context) (Running, error) {
		return NewRunning("com.running.app", "org.mozilla.firefox"), nil
	}
	var events []cleanup.Progress
	c := newCleaner(fx.env, darwinSpecs)
	res, err := c.Scan(t.Context(), func(p cleanup.Progress) { events = append(events, p) })
	if err != nil {
		t.Fatal(err)
	}

	want := []string{catAppCaches, catAppCachesRunning, catBrowserCaches, catBrowserRunning, catLogs, catCrashReports, catIncompleteDownload, catTrash}
	if got := ids(res); !slices.Equal(got, want) {
		t.Fatalf("categories %v, want %v", got, want)
	}
	check := func(id string, wantRels []string, selected bool, deletion cleanup.DeletionMode) cleanup.Category {
		t.Helper()
		cat := category(t, res, id)
		if got := rels(t, fx.home, cat); !slices.Equal(got, wantRels) {
			t.Errorf("%s items %v, want %v", id, got, wantRels)
		}
		if cat.Selected != selected || cat.Deletion != deletion || cat.Title == "" || cat.Description == "" || cat.Icon == "" {
			t.Errorf("%s %+v", id, cat)
		}
		for _, it := range cat.Items {
			if it.Size <= 0 && id != catTrash {
				t.Errorf("%s: %s has size %d", id, it.Path, it.Size)
			}
		}
		return cat
	}
	check(catAppCaches, []string{"Library/Caches/com.example.app"}, true, cleanup.DeletePermanent)
	busy := check(catAppCachesRunning, []string{"Library/Caches/com.running.app", "Library/Caches/com.running.app.ShipIt"}, false, cleanup.DeletePermanent)
	if busy.Warning == "" || notes(busy)["com.running.app"] != "应用正在运行" {
		t.Errorf("running app caches %+v", busy)
	}
	browsers := check(catBrowserCaches, []string{
		"Library/Application Support/Google/Chrome/Default/GPUCache",
		"Library/Application Support/Google/Chrome/GrShaderCache",
		"Library/Caches/Google/Chrome/Default/Cache",
		"Library/Caches/Google/Chrome/Default/Code Cache",
		"Library/Caches/com.apple.Safari/WebKitCache",
	}, true, cleanup.DeletePermanent)
	if n := notes(browsers); n["Cache"] != "Google Chrome · Default" || n["GrShaderCache"] != "Google Chrome" || n["WebKitCache"] != "Safari" {
		t.Errorf("browser notes %v", n)
	}
	ff := check(catBrowserRunning, []string{"Library/Caches/Firefox/Profiles/abc.default/cache2"}, false, cleanup.DeletePermanent)
	if ff.Warning == "" || notes(ff)["cache2"] != "Firefox · abc.default 正在运行，请先退出" {
		t.Errorf("running browser %+v", ff)
	}
	check(catLogs, []string{"Library/Logs/App"}, true, cleanup.DeletePermanent)
	check(catCrashReports, []string{"Library/Logs/DiagnosticReports/a.crash"}, true, cleanup.DeletePermanent)
	check(catIncompleteDownload, []string{"Downloads/a.crdownload", "Downloads/d.download"}, true, cleanup.DeleteTrash)
	tr := check(catTrash, []string{".Trash"}, false, cleanup.DeletePermanent)
	if tr.Items[0].Size != 5000 || tr.Items[0].Note != "共 3 项" || tr.Warning == "" || tr.Kind != cleanup.KindTrash {
		t.Errorf("trash %+v", tr)
	}
	if res.Size == 0 || res.Items != 14 || res.Denied != 0 {
		t.Errorf("totals size %d items %d denied %d", res.Size, res.Items, res.Denied)
	}

	if len(events) == 0 {
		t.Fatal("no progress")
	}
	last := events[len(events)-1]
	if last.Phase != cleanup.PhaseScanning || last.Total == 0 || last.Done != last.Total || last.Bytes == 0 {
		t.Errorf("last progress %+v", last)
	}
	for i := 1; i < len(events); i++ {
		if events[i].Done < events[i-1].Done {
			t.Errorf("progress went back: %+v then %+v", events[i-1], events[i])
		}
	}
}

func TestScanRespectsExclusions(t *testing.T) {
	fx := newFixture(t)
	darwinHome(t, fx.home)
	fx.exclusions = []string{
		// An excluded file inside an item keeps the whole item.
		filepath.Join(fx.home, "Library/Caches/com.example.app/data.bin"),
		"*.crdownload",
		filepath.Join(fx.home, "Library/Logs/App"),
		"GrShaderCache",
	}
	res := scan(t, newCleaner(fx.env, darwinSpecs))
	for _, c := range res.Categories {
		for _, it := range c.Items {
			for _, bad := range []string{"com.example.app", "a.crdownload", "Logs/App", "GrShaderCache"} {
				if strings.Contains(filepath.ToSlash(it.Path), bad) {
					t.Errorf("excluded %s offered in %s", it.Path, c.ID)
				}
			}
		}
	}
	if got := rels(t, fx.home, category(t, res, catIncompleteDownload)); !slices.Equal(got, []string{"Downloads/d.download"}) {
		t.Errorf("downloads %v", got)
	}
}

func TestTrashCategoryStates(t *testing.T) {
	cases := map[string]struct {
		usage   trash.TrashUsage
		err     error
		listed  bool
		warning string
	}{
		"empty":     {listed: false},
		"no access": {err: trash.ErrNoAccess, listed: true, warning: "完全磁盘访问权限"},
		"broken":    {err: errors.New("io"), listed: true, warning: "无法读取"},
	}
	for name, tc := range cases {
		t.Run(name, func(t *testing.T) {
			fx := newFixture(t)
			fx.bin.usage, fx.bin.sizeErr = tc.usage, tc.err
			res := scan(t, newCleaner(fx.env, darwinSpecs))
			listed := slices.Contains(ids(res), catTrash)
			if listed != tc.listed {
				t.Fatalf("listed %v, categories %v", listed, ids(res))
			}
			if listed {
				cat := category(t, res, catTrash)
				if len(cat.Items) != 0 || !strings.Contains(cat.Warning, tc.warning) {
					t.Errorf("trash %+v", cat)
				}
			}
		})
	}
}

func TestScanCancelled(t *testing.T) {
	fx := newFixture(t)
	darwinHome(t, fx.home)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := newCleaner(fx.env, darwinSpecs).Scan(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
}

func TestScanSurvivesRunningAppsError(t *testing.T) {
	fx := newFixture(t)
	darwinHome(t, fx.home)
	fx.env.Running = func(context.Context) (Running, error) { return nil, errors.New("lsappinfo failed") }
	res := scan(t, newCleaner(fx.env, darwinSpecs))
	if slices.Contains(ids(res), catAppCachesRunning) {
		t.Errorf("categories %v", ids(res))
	}
	if got := len(category(t, res, catAppCaches).Items); got != 3 {
		t.Errorf("app caches %d", got)
	}
}

func TestRunningOwns(t *testing.T) {
	r := NewRunning("com.tinyspeck.slackmacgap", "Spotify", "google")
	for name, want := range map[string]bool{
		"com.tinyspeck.slackmacgap":        true,
		"com.tinyspeck.slackmacgap.ShipIt": true,
		"COM.TINYSPECK.SLACKMACGAP":        true,
		"spotify":                          true,
		"com.tinyspeck":                    false,
		"com.tinyspeck.slackmacgapx":       false,
		"Google.Drive":                     false,
		"Homebrew":                         false,
	} {
		if got := r.owns(name); got != want {
			t.Errorf("owns(%q) = %v", name, got)
		}
	}
}

func TestAppleDenylist(t *testing.T) {
	for name, want := range map[string]bool{
		"cloudkit": true, "com.apple.bird": true, "com.apple.akd": true, "familycircle": true, "com.apple.homekit": true,
		"com.apple.safari.safebrowsing": true, "com.apple.icloudmailagent": true, "com.apple.itunescloud": true,
		"com.apple.ids.foo": true, "com.apple.security.agent": true,
		"com.apple.python": false, "com.apple.helpd": false, "geoservices": false, "com.example.cloud": false,
	} {
		if got := appleDenied(name); got != want {
			t.Errorf("appleDenied(%q) = %v", name, got)
		}
	}
}

func TestAssembleCapsAndChecks(t *testing.T) {
	dir, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	p := write(t, dir, "root/f", 10, 0)
	info, err := os.Lstat(p)
	if err != nil {
		t.Fatal(err)
	}
	var found []candidate
	for i := range maxItems + 5 {
		found = append(found, candidate{path: filepath.Join(dir, "root", fmt.Sprintf("f%d", i)), info: info, size: int64(i + 1)})
	}
	// Outside the roots: never offered.
	found = append(found, candidate{path: filepath.Join(dir, "elsewhere"), info: info, size: 1 << 40})
	s := spec{cat: cleanup.Category{ID: "x", Deletion: cleanup.DeletePermanent, Roots: []string{filepath.Join(dir, "root")}}}
	cats := assemble(s, found, slog.New(slog.DiscardHandler))
	if len(cats) != 1 || len(cats[0].Items) != maxItems || cats[0].Items[0].Size != maxItems+5 {
		t.Fatalf("items %d, first %+v", len(cats[0].Items), cats[0].Items[0])
	}
}

// cleanFixture scans the macOS fixture and returns the cleaner and the result.
func cleanFixture(t *testing.T) (*fixture, *Cleaner, cleanup.ScanResult) {
	t.Helper()
	fx := newFixture(t)
	darwinHome(t, fx.home)
	c := newCleaner(fx.env, darwinSpecs)
	return fx, c, scan(t, c)
}

func TestCleanEmptiesTrashBeforeMovingToIt(t *testing.T) {
	fx, c, res := cleanFixture(t)
	var events []cleanup.Progress
	out, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catIncompleteDownload, catTrash}}, func(p cleanup.Progress) { events = append(events, p) })
	if err != nil {
		t.Fatal(err)
	}
	log := fx.rec.entries()
	if len(log) != 3 || log[0] != "empty" || !slices.Contains(log, "trash:a.crdownload") || !slices.Contains(log, "trash:d.download") {
		t.Fatalf("order %v", log)
	}
	if out.Removed != 3 || out.Freed != 5000 || out.Trashed != category(t, res, catIncompleteDownload).Size || out.Partial {
		t.Errorf("result %+v", out)
	}
	if len(out.Categories) != 2 || out.Categories[0].CategoryID != catIncompleteDownload || out.Categories[1].CategoryID != catTrash || out.Categories[1].Freed != 5000 {
		t.Errorf("categories %+v", out.Categories)
	}
	last := events[len(events)-1]
	if events[0].CategoryID != catTrash || last.Done != 3 || last.Total != 3 || last.Bytes != out.Freed+out.Trashed {
		t.Errorf("progress %+v", events)
	}
	if _, err := os.Stat(filepath.Join(fx.home, "Downloads/c.txt")); err != nil {
		t.Errorf("an unlisted download went: %v", err)
	}
	// The Trash is gone from the remembered scan.
	if _, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catTrash}, ItemIDs: []string{res.Categories[len(res.Categories)-1].Items[0].ID}}, nil); !errors.Is(err, cleanup.ErrStale) {
		t.Errorf("emptying twice: %v", err)
	}
}

func TestCleanDeletesPermanentlyInsideRoots(t *testing.T) {
	fx, c, res := cleanFixture(t)
	app := category(t, res, catAppCaches).Items[0]
	out, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catAppCaches, catLogs}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Removed != 4 || out.Freed == 0 || out.Partial {
		t.Errorf("result %+v", out)
	}
	if _, err := os.Lstat(app.Path); !errors.Is(err, fs.ErrNotExist) {
		t.Errorf("app cache still there: %v", err)
	}
	for _, keep := range []string{"Library/Caches", "Library/Logs", "Library/Logs/DiagnosticReports/a.crash", "Library/Caches/com.apple.bird/z", "precious/keep.txt"} {
		if _, err := os.Stat(filepath.Join(fx.home, keep)); err != nil {
			t.Errorf("%s went: %v", keep, err)
		}
	}
	if slices.Contains(fx.rec.entries(), "empty") {
		t.Error("the Trash was emptied without being asked")
	}
}

func TestCleanRejectsBeforeEmptying(t *testing.T) {
	fx, c, res := cleanFixture(t)
	trashID := category(t, res, catTrash).Items[0].ID
	logID := category(t, res, catLogs).Items[0].ID
	cases := map[string]struct {
		req  cleanup.CleanRequest
		want error
	}{
		"unknown category":           {cleanup.CleanRequest{CategoryIDs: []string{catTrash, "gone"}}, cleanup.ErrStale},
		"unknown item":               {cleanup.CleanRequest{CategoryIDs: []string{catTrash, catLogs}, ItemIDs: []string{trashID, "deadbeef"}}, cleanup.ErrStale},
		"trash item, category unset": {cleanup.CleanRequest{CategoryIDs: []string{catLogs}, ItemIDs: []string{trashID, logID}}, cleanup.ErrStale},
		"nothing":                    {cleanup.CleanRequest{}, cleanup.ErrNothingSelected},
	}
	for name, tc := range cases {
		if _, err := c.Clean(t.Context(), tc.req, nil); !errors.Is(err, tc.want) {
			t.Errorf("%s: %v", name, err)
		}
	}
	if log := fx.rec.entries(); len(log) != 0 {
		t.Fatalf("a rejected request did %v", log)
	}
}

func TestCleanTrashAloneByItem(t *testing.T) {
	fx, c, res := cleanFixture(t)
	trashID := category(t, res, catTrash).Items[0].ID
	// The logs category is named but none of its items: with ItemIDs set, it contributes nothing.
	out, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catLogs, catTrash}, ItemIDs: []string{trashID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if log := fx.rec.entries(); !slices.Equal(log, []string{"empty"}) {
		t.Errorf("did %v", log)
	}
	if out.Removed != 1 || out.Freed != 5000 || len(out.Categories) != 1 {
		t.Errorf("result %+v", out)
	}
	if _, err := os.Stat(filepath.Join(fx.home, "Library/Logs/App")); err != nil {
		t.Errorf("logs went: %v", err)
	}
}

func TestCleanTrashFailure(t *testing.T) {
	fx, c, _ := cleanFixture(t)
	fx.bin.emptyErr = trash.ErrNoAccess
	out, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catTrash}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Removed != 0 || out.Freed != 100 || !out.Partial || len(out.Failed) != 1 || !strings.Contains(out.Failed[0].Reason, "完全磁盘访问权限") || out.Categories[0].Failed != 1 {
		t.Errorf("result %+v", out)
	}
}

func TestCleanWithoutTrashAccess(t *testing.T) {
	fx := newFixture(t)
	darwinHome(t, fx.home)
	fx.bin.sizeErr = trash.ErrNoAccess
	c := newCleaner(fx.env, darwinSpecs)
	scan(t, c)
	if _, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catTrash}}, nil); !errors.Is(err, cleanup.ErrNothingSelected) {
		t.Errorf("err %v", err)
	}
	if log := fx.rec.entries(); len(log) != 0 {
		t.Errorf("did %v", log)
	}
}

func TestSplitTrashKeepsItemFilter(t *testing.T) {
	item := &cleanup.Item{ID: "t"}
	empty, rest, err := splitTrash(cleanup.CleanRequest{CategoryIDs: []string{"a", catTrash}, ItemIDs: []string{"x", "t"}}, item, true)
	if err != nil || !empty || !slices.Equal(rest.CategoryIDs, []string{"a"}) || !slices.Equal(rest.ItemIDs, []string{"x"}) {
		t.Errorf("got %v %+v %v", empty, rest, err)
	}
	// Some category is partly checked and the Trash is not among the named items.
	empty, rest, err = splitTrash(cleanup.CleanRequest{CategoryIDs: []string{"a", catTrash}, ItemIDs: []string{"x"}}, item, true)
	if err != nil || empty || !slices.Equal(rest.ItemIDs, []string{"x"}) {
		t.Errorf("got %v %+v %v", empty, rest, err)
	}
	empty, rest, err = splitTrash(cleanup.CleanRequest{CategoryIDs: []string{"a"}}, item, true)
	if err != nil || empty || !slices.Equal(rest.CategoryIDs, []string{"a"}) || rest.ItemIDs != nil {
		t.Errorf("got %v %+v %v", empty, rest, err)
	}
	if _, _, err := splitTrash(cleanup.CleanRequest{CategoryIDs: []string{catTrash}}, nil, false); !errors.Is(err, cleanup.ErrStale) {
		t.Errorf("unlisted trash: %v", err)
	}
}

func TestBusy(t *testing.T) {
	fx := newFixture(t)
	release := make(chan struct{})
	entered := make(chan struct{})
	fx.env.Running = func(context.Context) (Running, error) {
		close(entered)
		<-release
		return Running{}, nil
	}
	c := newCleaner(fx.env, darwinSpecs)
	done := make(chan error)
	go func() {
		_, err := c.Scan(t.Context(), nil)
		done <- err
	}()
	<-entered
	if _, err := c.Scan(t.Context(), nil); !errors.Is(err, ErrBusy) {
		t.Errorf("second scan: %v", err)
	}
	if _, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catTrash}}, nil); !errors.Is(err, ErrBusy) {
		t.Errorf("clean during scan: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

// windowsEnv lays out a Windows profile under home (with the platform's separators, as the plan builds them) and points the environment at it.
func windowsEnv(t *testing.T, fx *fixture) string {
	t.Helper()
	day := 24 * time.Hour
	local := mkdir(t, fx.home, "AppData/Local")
	vars := map[string]string{
		"LOCALAPPDATA": local,
		"TEMP":         filepath.Join(local, "Temp"),
		// The same folder spelled differently: listed once.
		"TMP": strings.ToUpper(filepath.Join(local, "Temp")),
	}
	fx.env.Getenv = func(k string) string { return vars[k] }
	for _, rel := range []string{
		"Temp/old.tmp",
		"Temp/olddir/old.bin",
		"Microsoft/Windows/INetCache/IE/f",
		"D3DSCache/abc/f",
		"NVIDIA/DXCache/f.bin",
		"Microsoft/Windows/Explorer/thumbcache_256.db",
		"Microsoft/Windows/WebCache/WebCacheV01.dat",
		"Microsoft/Windows/INetCookies/c",
		"Microsoft/Windows/WER/ReportArchive/AppCrash_1/Report.wer",
		"Microsoft/Windows/WER/ReportQueue/AppHang_2/Report.wer",
		"CrashDumps/app.exe.123.dmp",
		"Google/Chrome/User Data/Default/Cache/Cache_Data/f",
		"Google/Chrome/User Data/Default/Cookies",
		"Google/Chrome/User Data/Profile 1/Code Cache/js/f",
		"Google/Chrome/User Data/ShaderCache/f",
		"Mozilla/Firefox/Profiles/x.default-release/cache2/entries/f",
		"Package Cache/{guid}/setup.exe",
	} {
		write(t, local, rel, 2000, 3*day)
	}
	write(t, local, "Temp/new.tmp", 2000, time.Hour)
	write(t, local, "Temp/busydir/new.bin", 2000, time.Hour)
	setAge(t, filepath.Join(local, "Temp/olddir"), 3*day)
	setAge(t, filepath.Join(local, "Temp/busydir"), 3*day)
	write(t, fx.home, "Downloads/setup.exe.crdownload", 2000, 3*day)
	return local
}

func TestWindowsScan(t *testing.T) {
	fx := newFixture(t)
	local := windowsEnv(t, fx)
	fx.env.Running = func(context.Context) (Running, error) { return NewRunning("chrome.exe", "explorer.exe"), nil }
	res := scan(t, newCleaner(fx.env, windowsSpecs))
	want := []string{catTempFiles, catAppCaches, catBrowserCaches, catBrowserRunning, catErrorReports, catIncompleteDownload, catTrash}
	if got := ids(res); !slices.Equal(got, want) {
		t.Fatalf("categories %v, want %v", got, want)
	}
	expect := map[string][]string{
		catTempFiles:     {"Temp/old.tmp", "Temp/olddir"},
		catAppCaches:     {"D3DSCache/abc", "Microsoft/Windows/INetCache/IE", "NVIDIA/DXCache/f.bin"},
		catBrowserCaches: {"Mozilla/Firefox/Profiles/x.default-release/cache2"},
		catBrowserRunning: {
			"Google/Chrome/User Data/Default/Cache",
			"Google/Chrome/User Data/Profile 1/Code Cache",
			"Google/Chrome/User Data/ShaderCache",
		},
		catErrorReports: {"CrashDumps/app.exe.123.dmp", "Microsoft/Windows/WER/ReportArchive/AppCrash_1", "Microsoft/Windows/WER/ReportQueue/AppHang_2"},
	}
	for id, wantRels := range expect {
		if got := rels(t, local, category(t, res, id)); !slices.Equal(got, wantRels) {
			t.Errorf("%s items %v, want %v", id, got, wantRels)
		}
	}
	if n := notes(category(t, res, catAppCaches)); n["IE"] != "Internet 临时文件" || n["abc"] != "DirectX 着色器缓存" {
		t.Errorf("app cache notes %v", n)
	}
	if n := notes(category(t, res, catErrorReports)); n["app.exe.123.dmp"] != "崩溃转储" || n["AppCrash_1"] != "Windows 错误报告" {
		t.Errorf("error report notes %v", n)
	}
	if got := rels(t, fx.home, category(t, res, catIncompleteDownload)); !slices.Equal(got, []string{"Downloads/setup.exe.crdownload"}) {
		t.Errorf("downloads %v", got)
	}
	if temp := category(t, res, catTempFiles); len(temp.Roots) != 1 {
		t.Errorf("temp roots %v", temp.Roots)
	}

	// A cleaner that has not scanned deletes nothing.
	if out, err := newCleaner(fx.env, windowsSpecs).Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catTempFiles}}, nil); !errors.Is(err, cleanup.ErrNoScan) {
		t.Errorf("a fresh cleaner cleaned %+v, %v", out, err)
	}
}

func TestWindowsCleanTemp(t *testing.T) {
	fx := newFixture(t)
	local := windowsEnv(t, fx)
	c := newCleaner(fx.env, windowsSpecs)
	scan(t, c)
	out, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catTempFiles, catErrorReports}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Removed != 5 || out.Partial {
		t.Errorf("result %+v", out)
	}
	for _, gone := range []string{"Temp/old.tmp", "Temp/olddir", "CrashDumps/app.exe.123.dmp"} {
		if _, err := os.Lstat(filepath.Join(local, gone)); !errors.Is(err, fs.ErrNotExist) {
			t.Errorf("%s still there: %v", gone, err)
		}
	}
	for _, keep := range []string{"Temp/new.tmp", "Temp/busydir/new.bin", "Temp", "CrashDumps", "Microsoft/Windows/WER/ReportArchive"} {
		if _, err := os.Stat(filepath.Join(local, keep)); err != nil {
			t.Errorf("%s went: %v", keep, err)
		}
	}
}

func TestWindowsWithoutLocalAppData(t *testing.T) {
	fx := newFixture(t)
	write(t, fx.home, "Downloads/x.part", 10, 48*time.Hour)
	res := scan(t, newCleaner(fx.env, windowsSpecs))
	if got := ids(res); !slices.Equal(got, []string{catIncompleteDownload, catTrash}) {
		t.Errorf("categories %v", got)
	}
}

func TestTempDirsWindows(t *testing.T) {
	home := filepath.Join(string(filepath.Separator), "Users", "me")
	local := filepath.Join(home, "AppData", "Local")
	vars := map[string]string{
		"TEMP": filepath.Join(string(filepath.Separator), "Windows", "Temp"),
		"TMP":  "relative",
	}
	got := tempDirsWindows(Env{Home: home, Getenv: func(k string) string { return vars[k] }}, local)
	if !slices.Equal(got, []string{filepath.Join(local, "Temp")}) {
		t.Errorf("temp dirs %v", got)
	}
}

func TestScanListsAFolderOnce(t *testing.T) {
	fx := newFixture(t)
	local := windowsEnv(t, fx)
	// %TEMP% pointing into the INetCache folder: its children are reachable as temporary files and as app caches.
	inet := filepath.Join(local, "Microsoft", "Windows", "INetCache")
	vars := map[string]string{"LOCALAPPDATA": local, "TEMP": inet}
	fx.env.Getenv = func(k string) string { return vars[k] }
	setAge(t, filepath.Join(inet, "IE"), 3*24*time.Hour)
	res := scan(t, newCleaner(fx.env, windowsSpecs))
	seen := map[string]string{}
	for _, c := range res.Categories {
		for _, it := range c.Items {
			if prev, dup := seen[it.ID]; dup {
				t.Errorf("%s listed in %s and %s", it.Path, prev, c.ID)
			}
			seen[it.ID] = c.ID
		}
	}
	if seen[cleanup.ItemID(filepath.Join(inet, "IE"))] != catTempFiles {
		t.Errorf("IE listed in %q", seen[cleanup.ItemID(filepath.Join(inet, "IE"))])
	}
}
