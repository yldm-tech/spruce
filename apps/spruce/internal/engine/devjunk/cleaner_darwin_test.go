package devjunk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/execx"
)

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

	if !oldItem.Selected || newItem.Selected {
		t.Error("only the inactive project's node_modules should start checked")
	}
	if old, newest := itemByName(support, "16.0 (20A362)"), itemByName(support, "17.0 (21A329)"); !old.Selected || newest.Selected || support.Selected {
		t.Error("device support: the old version should be checked, the newest kept, the category checked in part")
	}
	jb := categoryByID(res, "tool-jetbrains")
	if jb == nil || !itemByName(jb, "GoLand2023.3").Selected || itemByName(jb, "GoLand2024.2").Selected {
		t.Errorf("jetbrains = %+v", jb)
	}
	if arch := categoryByID(res, "tool-xcode-archives"); arch.Selected || arch.Items[0].Selected {
		t.Error("archives start checked")
	}
	if !derived.Selected || !derived.Items[0].Selected {
		t.Errorf("derived data = %+v, want it checked as a whole", derived)
	}
	var cmds [][2]string
	for _, c := range res.Categories {
		if c.Command != "" {
			cmds = append(cmds, [2]string{c.ID, c.Command})
		}
	}
	if want := [][2]string{{"tool-xcode-simulators", "xcrun simctl delete unavailable"}, {"tool-npm", "npm cache clean --force"}}; !slices.Equal(cmds, want) {
		t.Errorf("commands = %v, want %v", cmds, want)
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

func itemByName(c *cleanup.Category, name string) *cleanup.Item {
	for i := range c.Items {
		if filepath.Base(c.Items[i].Path) == name || strings.HasSuffix(c.Items[i].Path, string(filepath.Separator)+name) {
			return &c.Items[i]
		}
	}
	return nil
}
