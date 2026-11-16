package devjunk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/execx/execxtest"
	"spruce/apps/spruce/internal/engine/fsx"
	"spruce/apps/spruce/internal/engine/settings"
)

// testDeleter deletes for real with fsx.SafeRemove (tests only ever point it at temporary folders) and records Trash calls without touching the Trash.
type testDeleter struct {
	mu      sync.Mutex
	removed map[string][]string
	trashed map[string][]string
}

func newTestDeleter() *testDeleter {
	return &testDeleter{removed: map[string][]string{}, trashed: map[string][]string{}}
}

func (d *testDeleter) Remove(ctx context.Context, path string, roots []string) (fsx.Removal, error) {
	d.mu.Lock()
	d.removed[path] = roots
	d.mu.Unlock()
	return fsx.SafeRemove(ctx, path, roots)
}

func (d *testDeleter) Trash(_ context.Context, path string, roots []string) (cleanup.Trashed, error) {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.trashed[path] = roots
	return cleanup.Trashed{}, nil
}

type fixture struct {
	home    string
	runner  *execxtest.Fake
	del     *testDeleter
	cfg     settings.CleanupSettings
	opts    Options
	cleaner *Cleaner
}

// newFixture builds a Cleaner over a temporary home; bins are the binaries LookPath finds (by name or path).
func newFixture(t *testing.T, goos string, bins map[string]string) *fixture {
	t.Helper()
	f := &fixture{home: t.TempDir(), runner: execxtest.New(), del: newTestDeleter()}
	f.cfg = settings.CleanupSettings{DevArtifactMinAgeDays: 30, Exclusions: []string{}}
	f.opts = Options{
		Home: f.home, Runner: f.runner, GOOS: goos,
		Settings:      func() settings.CleanupSettings { return f.cfg },
		Getenv:        envOf(nil),
		LookPath:      lookPathOf(bins),
		Deleter:       f.del,
		VSCodeRunning: func(context.Context) bool { return false },
	}
	return f
}

func (f *fixture) build() *Cleaner {
	f.cleaner = New(f.opts)
	return f.cleaner
}

func (f *fixture) scan(t *testing.T) cleanup.ScanResult {
	t.Helper()
	var mu sync.Mutex
	var events []cleanup.Progress
	res, err := f.build().Scan(context.Background(), func(p cleanup.Progress) {
		mu.Lock()
		events = append(events, p)
		mu.Unlock()
	})
	if err != nil {
		t.Fatalf("scan: %v", err)
	}
	if len(events) == 0 || events[len(events)-1].Done != events[len(events)-1].Total {
		t.Errorf("scan progress did not finish: %v", events)
	}
	return res
}

func categoryByID(res cleanup.ScanResult, id string) *cleanup.Category {
	for i := range res.Categories {
		if res.Categories[i].ID == id {
			return &res.Categories[i]
		}
	}
	return nil
}

func exists(p string) bool {
	_, err := os.Lstat(p)
	return err == nil
}

// age sets the modification time of every file directly in dir (and of .git/HEAD and .git/index, if present).
func age(t *testing.T, dir string, d time.Duration) {
	t.Helper()
	when := time.Now().Add(-d)
	des, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	for _, de := range des {
		if de.Type().IsRegular() {
			if err := os.Chtimes(filepath.Join(dir, de.Name()), when, when); err != nil {
				t.Fatal(err)
			}
		}
	}
}

func TestCommandSkippedWhenExclusionInside(t *testing.T) {
	f := newFixture(t, "darwin", map[string]string{"/opt/homebrew/bin/npm": "/opt/homebrew/bin/npm", "/opt/homebrew/bin/pnpm": "/opt/homebrew/bin/pnpm"})
	writeFile(t, f.home, ".npm/_cacache/content-v2/x", 1000)
	writeFile(t, f.home, ".npm/_cacache/index-v5/x", 1000)
	store := filepath.Join(f.home, "pnpm-store")
	writeFile(t, store, "files/00/x", 1000)
	f.runner.On("/opt/homebrew/bin/pnpm", "store", "path").Return(store+"\n", 0)
	f.cfg.Exclusions = []string{"index-v5", filepath.Join(store, "files")}
	res := f.scan(t)
	npm := categoryByID(res, "tool-npm")
	if npm == nil || len(npm.Items) != 1 || filepath.Base(npm.Items[0].Path) != "content-v2" {
		t.Fatalf("npm = %+v, want its entries without the excluded one", npm)
	}
	if categoryByID(res, "tool-pnpm") != nil {
		t.Error("pnpm store prune offered although an exclusion lies inside the store")
	}
	if npm.Command != "" {
		t.Errorf("npm falls back to its entries but names the command %q", npm.Command)
	}
}

func TestArtifactsInProtectedPlacesGoToTrash(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	docs := filepath.Join(f.home, "Documents")
	writeFile(t, docs, "app/package.json", 10)
	writeFile(t, docs, "app/node_modules/x", 1000)
	writeFile(t, docs, "secret/Cargo.toml", 10)
	writeFile(t, docs, "secret/target/x", 1000)
	age(t, filepath.Join(docs, "app"), 60*24*time.Hour)
	f.cfg.DevProjectRoots = []string{docs}
	f.opts.Removable = func(string, []string) error { return fsx.ErrOutsideRoots }
	f.opts.Trashable = func(path string, _ []string) error {
		if strings.Contains(path, "secret") {
			return fsx.ErrProtected
		}
		return nil
	}
	res := f.scan(t)
	if categoryByID(res, "artifact-node-modules") != nil {
		t.Error("output that cannot be deleted for good is in the permanent category")
	}
	cat := categoryByID(res, "artifact-node-modules-trash")
	if cat == nil || cat.Deletion != cleanup.DeleteTrash || !cat.Selected || !strings.Contains(cat.Title, "废纸篓") {
		t.Fatalf("trash category = %+v", cat)
	}
	if categoryByID(res, "artifact-rust-target") != nil || categoryByID(res, "artifact-rust-target-trash") != nil {
		t.Error("output that may be neither deleted nor trashed is offered")
	}
	out, err := f.cleaner.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{cat.ID}}, nil)
	if err != nil || out.Removed != 1 || out.Trashed != cat.Size {
		t.Fatalf("clean = %+v, %v", out, err)
	}
	if roots := f.del.trashed[cat.Items[0].Path]; !slices.Equal(roots, []string{filepath.Join(docs, "app")}) {
		t.Errorf("trashed with roots %v", roots)
	}
	if len(f.del.removed) != 0 {
		t.Errorf("deleted for good: %v", f.del.removed)
	}
}

func TestMinAgeZeroChecksEverything(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	ws := filepath.Join(f.home, "code")
	writeFile(t, ws, "p/Cargo.toml", 10)
	writeFile(t, ws, "p/target/x", 1000)
	f.cfg.DevProjectRoots = []string{ws}
	f.cfg.DevArtifactMinAgeDays = 0
	res := f.scan(t)
	cat := categoryByID(res, "artifact-rust-target")
	if cat == nil || !cat.Selected || strings.Contains(cat.Items[0].Note, "最近") {
		t.Errorf("rust target = %+v", cat)
	}
}

func TestScanAndCleanWindowsVariant(t *testing.T) {
	// On Windows the folders come from LOCALAPPDATA and APPDATA; left unset they fall back to the home folder's AppData, which here is a temporary folder on macOS.
	npmCmd := `C:\Program Files\nodejs\npm.cmd`
	f := newFixture(t, "windows", map[string]string{"npm": npmCmd})
	local := filepath.Join(f.home, "AppData", "Local")
	writeFile(t, local, "npm-cache/_cacache/content-v2/x", 3000)
	writeFile(t, local, "NuGet/v3-cache/abc/x", 2000)
	writeFile(t, local, "JetBrains/Rider2023.3/caches/x", 1000)
	writeFile(t, local, "JetBrains/Rider2023.3/plugins-data/x", 1000)
	writeFile(t, local, "JetBrains/Rider2024.1/index/x", 1000)
	writeFile(t, f.home, "AppData/Roaming/Code/CachedData/abc/x", 1000)
	writeFile(t, f.home, "Library/Developer/Xcode/DerivedData/App/x", 1000) // not looked at on Windows
	f.runner.On(npmCmd, "cache", "clean", "--force").ReturnOutput(execx.Output{ExitCode: 1, Stderr: []byte("npm ERR! code EPERM\nnpm ERR! operation not permitted\n")})
	f.opts.VSCodeRunning = func(context.Context) bool { return true }

	res := f.scan(t)
	if categoryByID(res, "tool-xcode-derived-data") != nil {
		t.Error("Xcode caches scanned on Windows")
	}
	if c := categoryByID(res, "tool-nuget-cache"); c == nil || !c.Selected {
		t.Errorf("nuget = %+v", c)
	}
	jb := categoryByID(res, "tool-jetbrains")
	if jb == nil || len(jb.Items) != 2 {
		t.Fatalf("jetbrains = %+v, want caches of 2023.3 and index of 2024.1 only", jb)
	}
	for _, it := range jb.Items {
		if strings.Contains(it.Path, "plugins-data") {
			t.Errorf("non-cache folder offered: %s", it.Path)
		}
	}
	if vs := categoryByID(res, "tool-vscode"); vs == nil || vs.Selected || vs.Warning == "" {
		t.Errorf("vscode while running = %+v", vs)
	}
	npm := categoryByID(res, "tool-npm")
	if npm == nil || npm.Items[0].Path != filepath.Join(local, "npm-cache", "_cacache") {
		t.Fatalf("npm = %+v", npm)
	}
	out, err := f.cleaner.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{npm.ID}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if out.Removed != 0 || len(out.Failed) != 1 || !out.Partial {
		t.Fatalf("clean = %+v", out)
	}
	if r := out.Failed[0].Reason; r != "npm cache clean --force 没有成功：npm ERR! operation not permitted" {
		t.Errorf("reason = %q", r)
	}
	if !strings.Contains(out.Failed[0].Detail, "EPERM") {
		t.Errorf("detail = %q, want the command's output", out.Failed[0].Detail)
	}
	if !exists(filepath.Join(local, "npm-cache", "_cacache", "content-v2", "x")) {
		t.Error("a failed command's folder was deleted anyway")
	}
}

func TestCleanStopsBetweenCommands(t *testing.T) {
	// The catalog of the OS the test runs on, so that its paths are absolute there; npm and go are found on PATH.
	bin := filepath.Join(t.TempDir(), "bin")
	npmBin, goBin := filepath.Join(bin, "npm"), filepath.Join(bin, "go")
	f := newFixture(t, runtime.GOOS, map[string]string{"npm": npmBin, "go": goBin})
	goCache := filepath.Join(f.home, "gocache")
	writeFile(t, goCache, "00/x", 1000)
	npmCache := filepath.Join(f.home, ".npm", "_cacache")
	if runtime.GOOS == "windows" {
		npmCache = filepath.Join(f.home, "AppData", "Local", "npm-cache", "_cacache")
	}
	writeFile(t, npmCache, "content-v2/x", 1000)
	f.runner.On(goBin, "env", "GOCACHE", "GOMODCACHE").Return(goCache+"\n"+filepath.Join(f.home, "gomod")+"\n", 0)
	res := f.scan(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.runner.On(npmBin, "cache", "clean", "--force").Do(func(c context.Context, _ execx.Command) (execx.Output, error) {
		cancel()
		if c.Err() != nil {
			t.Error("the running command's context was cancelled with the clean")
		}
		return execx.Output{}, nil
	})
	f.runner.On(goBin, "clean", "-cache").Return("", 0)
	out, err := f.cleaner.Clean(ctx, cleanup.CleanRequest{CategoryIDs: []string{"tool-npm", "tool-go-build"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.runner.Called(goBin, "clean", "-cache") {
		t.Error("the second command ran after the clean was stopped")
	}
	if !out.Interrupted || out.Removed != 1 || len(out.Categories) != 2 {
		t.Errorf("clean = %+v", out)
	}
	if categoryByID(res, "tool-go-build") == nil {
		t.Error("go build cache not found")
	}
}

func TestScanCancelled(t *testing.T) {
	f := newFixture(t, "darwin", nil)
	writeFile(t, f.home, "Library/Developer/Xcode/DerivedData/App/x", 1000)
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := f.build().Scan(ctx, nil); !errors.Is(err, context.Canceled) {
		t.Errorf("scan = %v, want context.Canceled", err)
	}
	if _, err := f.cleaner.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{"tool-xcode-derived-data"}}, nil); !errors.Is(err, cleanup.ErrNoScan) {
		t.Errorf("clean after a cancelled scan = %v, want ErrNoScan", err)
	}
}
