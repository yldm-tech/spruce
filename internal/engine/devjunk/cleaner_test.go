package devjunk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/fsx"
	"github.com/yldm-tech/spruce/internal/engine/settings"
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

func (d *testDeleter) Trash(_ context.Context, path string, roots []string) error {
	d.mu.Lock()
	defer d.mu.Unlock()
	d.trashed[path] = roots
	return nil
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

func itemByName(c *cleanup.Category, name string) *cleanup.Item {
	for i := range c.Items {
		if filepath.Base(c.Items[i].Path) == name || strings.HasSuffix(c.Items[i].Path, string(filepath.Separator)+name) {
			return &c.Items[i]
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

const simctlJSON = `{"devices": {
  "com.apple.CoreSimulator.SimRuntime.iOS-16-0": [{"udid": "AAAA-1111", "name": "iPhone 14", "isAvailable": false}],
  "com.apple.CoreSimulator.SimRuntime.iOS-18-0": [{"udid": "BBBB-2222", "name": "iPhone 16", "isAvailable": true}]
}}`

func TestScanAndCleanDarwin(t *testing.T) {
	f := newFixture(t, "darwin", map[string]string{"/opt/homebrew/bin/npm": "/opt/homebrew/bin/npm"})
	h := f.home
	writeFile(t, h, "Library/Developer/Xcode/DerivedData/App-abc/Build/x", 4000)
	writeFile(t, h, "Library/Developer/Xcode/Archives/2024-01-02/App.xcarchive/x", 3000)
	writeFile(t, h, "Library/Developer/Xcode/iOS DeviceSupport/16.0 (20A362)/Symbols/x", 2000)
	writeFile(t, h, "Library/Developer/Xcode/iOS DeviceSupport/17.0 (21A329)/Symbols/x", 2000)
	writeFile(t, h, "Library/Caches/Homebrew/downloads/pkg.tar.gz", 5000)
	writeFile(t, h, "Library/Caches/Yarn/v6/pkg/x", 1000)
	writeFile(t, h, "Library/Caches/Yarn/v6/keep/x", 1000)
	writeFile(t, h, ".npm/_cacache/content-v2/x", 6000)
	writeFile(t, h, "Library/Developer/CoreSimulator/Devices/AAAA-1111/data/x", 7000)
	writeFile(t, h, "Library/Developer/CoreSimulator/Devices/BBBB-2222/data/x", 7000)
	devDir := filepath.Join(h, "Xcode.app", "Contents", "Developer")
	writeFile(t, devDir, "usr/bin/simctl", 1)
	writeFile(t, h, "Library/Caches/JetBrains/GoLand2023.3/caches/x", 1000)
	writeFile(t, h, "Library/Caches/JetBrains/GoLand2024.2/caches/x", 1000)

	ws := filepath.Join(h, "Workspace")
	writeFile(t, ws, "old/package.json", 10)
	writeFile(t, ws, "old/node_modules/dep/index.js", 8000)
	age(t, filepath.Join(ws, "old"), 90*24*time.Hour)
	writeFile(t, ws, "new/package.json", 10)
	writeFile(t, ws, "new/node_modules/dep/index.js", 8000)
	f.cfg.DevProjectRoots = []string{ws}
	// An exclusion inside the Yarn cache keeps that whole entry out.
	f.cfg.Exclusions = []string{filepath.Join(h, "Library", "Caches", "Yarn", "v6", "keep", "x")}

	f.runner.On(xcodeSelectPath, "-p").Return(devDir+"\n", 0)
	f.runner.On(xcrunPath, "simctl", "list", "devices", "-j").Return(simctlJSON, 0)
	devices := filepath.Join(h, "Library", "Developer", "CoreSimulator", "Devices")
	f.runner.On(xcrunPath, "simctl", "delete", "unavailable").Do(func(ctx context.Context, _ execx.Command) (execx.Output, error) {
		return execx.Output{}, os.RemoveAll(filepath.Join(devices, "AAAA-1111"))
	})
	npmCache := filepath.Join(h, ".npm", "_cacache")
	f.runner.On("/opt/homebrew/bin/npm", "cache", "clean", "--force").Do(func(ctx context.Context, cmd execx.Command) (execx.Output, error) {
		if cmd.Timeout != cleanTimeout || !slices.Contains(cmd.Env, "PATH=/opt/homebrew/bin") {
			t.Errorf("npm ran as %+v", cmd)
		}
		return execx.Output{}, os.RemoveAll(filepath.Join(npmCache, "content-v2"))
	})

	res := f.scan(t)

	derived := categoryByID(res, "tool-xcode-derived-data")
	if derived == nil || derived.Kind != cleanup.KindDevCache || derived.Deletion != cleanup.DeletePermanent || !derived.Selected || len(derived.Items) != 1 {
		t.Fatalf("derived data = %+v", derived)
	}
	if archives := categoryByID(res, "tool-xcode-archives"); archives == nil || archives.Deletion != cleanup.DeleteTrash || archives.Selected {
		t.Errorf("archives = %+v", archives)
	}
	support := categoryByID(res, "tool-xcode-device-support")
	if support == nil || support.Selected || len(support.Items) != 2 {
		t.Fatalf("device support = %+v, want two versions, partly checked", support)
	}
	if brew := categoryByID(res, "tool-homebrew"); brew == nil || len(brew.Items) != 1 || filepath.Base(brew.Items[0].Path) != "downloads" {
		t.Errorf("homebrew without brew = %+v, want its entries", brew)
	}
	yarn := categoryByID(res, "tool-yarn")
	if yarn != nil {
		t.Errorf("yarn = %+v, want nothing: its only entry holds an excluded file", yarn.Items)
	}
	npm := categoryByID(res, "tool-npm")
	if npm == nil || len(npm.Items) != 1 || npm.Items[0].Path != npmCache || npm.Description == "" || !strings.Contains(npm.Description, "npm cache clean") {
		t.Fatalf("npm = %+v, want one command item", npm)
	}
	sims := categoryByID(res, "tool-xcode-simulators")
	if sims == nil || len(sims.Items) != 1 || sims.Items[0].Path != devices || sims.Items[0].Note != "1 台不可用的模拟器" || sims.Size < 7000 || sims.Size >= 14000 {
		t.Fatalf("simulators = %+v, want the one unavailable device", sims)
	}
	nm := categoryByID(res, "artifact-node-modules")
	if nm == nil || nm.Kind != cleanup.KindProjectArtifact || nm.Selected || len(nm.Items) != 2 {
		t.Fatalf("node_modules = %+v", nm)
	}
	oldItem, newItem := itemByName(nm, filepath.Join("old", "node_modules")), itemByName(nm, filepath.Join("new", "node_modules"))
	if oldItem == nil || newItem == nil {
		t.Fatalf("node_modules items = %+v", nm.Items)
	}
	if oldItem.Note != "old" || !strings.HasPrefix(newItem.Note, "new · 最近 30 天内有改动") {
		t.Errorf("notes = %q, %q", oldItem.Note, newItem.Note)
	}
	if time.Since(oldItem.ModTime) < 80*24*time.Hour {
		t.Errorf("old project's time = %v, want its last activity", oldItem.ModTime)
	}

	d := f.cleaner.Details()
	defaults := map[string]bool{}
	for _, id := range d.DefaultItems {
		defaults[id] = true
	}
	if !defaults[oldItem.ID] || defaults[newItem.ID] {
		t.Error("only the inactive project's node_modules should start checked")
	}
	if old, newest := itemByName(support, "16.0 (20A362)"), itemByName(support, "17.0 (21A329)"); !defaults[old.ID] || defaults[newest.ID] {
		t.Error("device support: the old version should be checked, the newest kept")
	}
	jb := categoryByID(res, "tool-jetbrains")
	if jb == nil || !defaults[itemByName(jb, "GoLand2023.3").ID] || defaults[itemByName(jb, "GoLand2024.2").ID] {
		t.Errorf("jetbrains = %+v", jb)
	}
	if defaults[categoryByID(res, "tool-xcode-archives").Items[0].ID] {
		t.Error("archives start checked")
	}
	wantCmds := []Command{{CategoryID: "tool-xcode-simulators", Command: "xcrun simctl delete unavailable"}, {CategoryID: "tool-npm", Command: "npm cache clean --force"}}
	if !slices.Equal(d.Commands, wantCmds) {
		t.Errorf("commands = %+v, want %+v", d.Commands, wantCmds)
	}

	req := cleanup.CleanRequest{
		CategoryIDs: []string{"tool-xcode-derived-data", "tool-npm", "tool-xcode-simulators", "artifact-node-modules"},
		ItemIDs:     []string{derived.Items[0].ID, npm.Items[0].ID, sims.Items[0].ID, oldItem.ID},
	}
	var events []cleanup.Progress
	out, err := f.cleaner.Clean(context.Background(), req, func(p cleanup.Progress) { events = append(events, p) })
	if err != nil {
		t.Fatal(err)
	}
	if out.Removed != 4 || len(out.Failed) != 0 || out.Partial || out.Interrupted {
		t.Fatalf("clean = %+v", out)
	}
	if exists(derived.Items[0].Path) || exists(oldItem.Path) || !exists(newItem.Path) {
		t.Error("the clean deleted the wrong folders")
	}
	if exists(filepath.Join(devices, "AAAA-1111")) || !exists(filepath.Join(devices, "BBBB-2222")) {
		t.Error("simctl fake did not run")
	}
	if roots := f.del.removed[oldItem.Path]; !slices.Equal(roots, []string{filepath.Join(ws, "old")}) {
		t.Errorf("node_modules deleted with roots %v, want only its project", roots)
	}
	if roots := f.del.removed[derived.Items[0].Path]; !slices.Equal(roots, []string{filepath.Join(h, "Library", "Developer", "Xcode", "DerivedData")}) {
		t.Errorf("derived data deleted with roots %v, want the cache folder", roots)
	}
	if out.Freed < 4000+6000+7000+8000 {
		t.Errorf("freed %d", out.Freed)
	}
	var ids []string
	for _, c := range out.Categories {
		ids = append(ids, c.CategoryID)
		if c.Removed != 1 {
			t.Errorf("%s removed %d", c.CategoryID, c.Removed)
		}
	}
	if !slices.Equal(ids, []string{"tool-xcode-derived-data", "tool-xcode-simulators", "tool-npm", "artifact-node-modules"}) {
		t.Errorf("outcomes in %v, want scan order", ids)
	}
	last := events[len(events)-1]
	if last.Done != 4 || last.Total != 4 {
		t.Errorf("last progress = %+v", last)
	}
	for _, e := range events {
		if e.Total != 4 || e.Phase != cleanup.PhaseCleaning {
			t.Errorf("progress %+v", e)
		}
	}

	// A command that ran is not run again without a new scan, and nothing else is deleted.
	if _, err := f.cleaner.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{"tool-npm"}}, nil); !errors.Is(err, cleanup.ErrStale) {
		t.Errorf("second npm clean = %v, want ErrStale", err)
	}
	if _, err := f.cleaner.Clean(context.Background(), cleanup.CleanRequest{CategoryIDs: []string{"tool-xcode-archives"}, ItemIDs: []string{npm.Items[0].ID}}, nil); !errors.Is(err, cleanup.ErrStale) {
		t.Errorf("an item outside its categories = %v, want ErrStale", err)
	}
	if _, err := f.cleaner.Clean(context.Background(), cleanup.CleanRequest{}, nil); !errors.Is(err, cleanup.ErrNothingSelected) {
		t.Errorf("empty request = %v", err)
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
	if d := f.cleaner.Details(); len(d.Commands) != 0 {
		t.Errorf("commands = %+v", d.Commands)
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
	f := newFixture(t, "darwin", map[string]string{"/opt/homebrew/bin/npm": "/opt/homebrew/bin/npm", "/opt/homebrew/bin/go": "/opt/homebrew/bin/go"})
	goCache := filepath.Join(f.home, "gocache")
	writeFile(t, goCache, "00/x", 1000)
	writeFile(t, f.home, ".npm/_cacache/content-v2/x", 1000)
	f.runner.On("/opt/homebrew/bin/go", "env", "GOCACHE", "GOMODCACHE").Return(goCache+"\n"+filepath.Join(f.home, "gomod")+"\n", 0)
	res := f.scan(t)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	f.runner.On("/opt/homebrew/bin/npm", "cache", "clean", "--force").Do(func(c context.Context, _ execx.Command) (execx.Output, error) {
		cancel()
		if c.Err() != nil {
			t.Error("the running command's context was cancelled with the clean")
		}
		return execx.Output{}, nil
	})
	f.runner.On("/opt/homebrew/bin/go", "clean", "-cache").Return("", 0)
	out, err := f.cleaner.Clean(ctx, cleanup.CleanRequest{CategoryIDs: []string{"tool-npm", "tool-go-build"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if f.runner.Called("/opt/homebrew/bin/go", "clean", "-cache") {
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
