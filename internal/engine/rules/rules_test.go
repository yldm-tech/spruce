package rules_test

import (
	"fmt"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/advice"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/rules"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

const (
	day = 24 * time.Hour
	mb  = int64(1) << 20
	gb  = int64(1) << 30
)

var (
	now       = time.Date(2026, 11, 16, 10, 0, 0, 0, time.UTC)
	home      = filepath.FromSlash("/Users/x")
	downloads = filepath.Join(home, "Downloads")
)

func TestCatalog(t *testing.T) {
	want := map[rules.ID]struct {
		title         string
		cats          []string
		minAge, every time.Duration
		git, dl, apps bool
	}{
		rules.AppCaches:           {"旧的应用缓存", []string{"system-junk:app-caches"}, 30 * day, 7 * day, false, false, false},
		rules.Installers:          {"旧安装包", []string{"installers:disk-images", "installers:installer-packages"}, 7 * day, day, false, true, true},
		rules.BuildOutput:         {"闲置项目的构建产物", []string{"dev-junk:artifact-*"}, 90 * day, 7 * day, true, false, false},
		rules.IncompleteDownloads: {"未完成的下载", []string{"system-junk:incomplete-downloads"}, 7 * day, day, false, false, false},
	}
	cat := rules.Catalog()
	var order []rules.ID
	for _, s := range cat {
		order = append(order, s.ID)
		w := want[s.ID]
		if s.Title != w.title || !slices.Equal(s.Categories, w.cats) || s.MinAge != w.minAge || s.Every != w.every || s.NeedGit != w.git || s.InDownloads != w.dl || s.NeedInstalled != w.apps {
			t.Errorf("%s: %+v", s.ID, s)
		}
		if got, ok := rules.Lookup(s.ID); !ok || got.Title != s.Title {
			t.Errorf("Lookup %s: %+v %v", s.ID, got, ok)
		}
	}
	if !slices.Equal(order, []rules.ID{rules.AppCaches, rules.Installers, rules.BuildOutput, rules.IncompleteDownloads}) {
		t.Errorf("order %v", order)
	}
	if _, ok := rules.Lookup("other"); ok {
		t.Error("unknown rule found")
	}
	// Catalog hands out copies.
	cat[0].Categories[0] = "changed"
	if rules.Catalog()[0].Categories[0] != "system-junk:app-caches" {
		t.Error("Catalog shares its slices")
	}
}

// TestCatalogMatchesCards: each rule covers exactly the categories of the card that names it (advice templates), so 「以后自动处理」 on a card turns on the rule that takes that card's items.
func TestCatalogMatchesCards(t *testing.T) {
	ids := []string{
		"system-junk:app-caches", "system-junk:incomplete-downloads", "system-junk:browser-caches", "system-junk:logs",
		"installers:disk-images", "installers:installer-packages",
		"dev-junk:artifact-node-modules", "dev-junk:artifact-node-modules-trash", "dev-junk:artifact-cargo-target",
	}
	var cats []cleanup.Category
	for i, id := range ids {
		p := filepath.Join(downloads, fmt.Sprintf("f%d.dmg", i))
		cats = append(cats, cleanup.Category{ID: id, Kind: cleanup.KindCache, Selected: true, Items: []cleanup.Item{{ID: cleanup.ItemID(p), Path: p, Size: gb, Project: home}}})
	}
	res := cleanup.NewResult(cats, now)
	seen := map[string]bool{}
	for _, rec := range advice.Derive(res, advice.Inputs{Now: now, Home: home}) {
		seen[rec.Rule] = true
		for _, c := range rec.Categories {
			covered := ""
			for _, s := range rules.Catalog() {
				for _, sc := range s.Categories {
					if c == sc || (sc[len(sc)-1] == '*' && len(c) >= len(sc)-1 && c[:len(sc)-1] == sc[:len(sc)-1]) {
						covered = string(s.ID)
					}
				}
			}
			if covered != rec.Rule {
				t.Errorf("card %s category %s: rule %q, catalog %q", rec.ID, c, rec.Rule, covered)
			}
		}
	}
	for _, s := range rules.Catalog() {
		if !seen[string(s.ID)] {
			t.Errorf("no card names rule %s", s.ID)
		}
	}
}

func TestEnabled(t *testing.T) {
	if got := rules.Enabled(settings.AutomationSettings{WeeklySummary: true}); len(got) != 0 {
		t.Errorf("none: %v", got)
	}
	got := rules.Enabled(settings.AutomationSettings{IncompleteDownloads: true, AppCaches: true})
	if !slices.Equal(got, []rules.ID{rules.AppCaches, rules.IncompleteDownloads}) {
		t.Errorf("two: %v", got)
	}
	all := rules.Enabled(settings.AutomationSettings{AppCaches: true, Installers: true, BuildOutput: true, IncompleteDownloads: true})
	if len(all) != 4 {
		t.Errorf("all: %v", all)
	}
}

func TestDue(t *testing.T) {
	enabled := []rules.ID{rules.AppCaches, rules.Installers}
	due := func(runs map[string]time.Time) []rules.ID {
		var out []rules.ID
		for _, s := range rules.Due(enabled, runs, now) {
			out = append(out, s.ID)
		}
		return out
	}
	if got := due(nil); !slices.Equal(got, enabled) {
		t.Errorf("never run: %v", got)
	}
	if got := due(map[string]time.Time{"app-caches": now.Add(-6 * day), "installers": now.Add(-23 * time.Hour)}); len(got) != 0 {
		t.Errorf("recent: %v", got)
	}
	if got := due(map[string]time.Time{"app-caches": now.Add(-7 * day), "installers": now.Add(-day)}); !slices.Equal(got, enabled) {
		t.Errorf("at the interval: %v", got)
	}
	if got := due(map[string]time.Time{"app-caches": now.Add(30 * day), "installers": now.Add(-time.Hour)}); !slices.Equal(got, []rules.ID{rules.AppCaches}) {
		t.Errorf("future run: %v", got)
	}
	// A disabled rule is never due.
	if got := rules.Due(nil, nil, now); len(got) != 0 {
		t.Errorf("disabled: %v", got)
	}
}

func spec(id rules.ID) rules.Spec {
	s, _ := rules.Lookup(id)
	return s
}

func TestAge(t *testing.T) {
	build, caches := spec(rules.BuildOutput), spec(rules.AppCaches)
	for name, tc := range map[string]struct {
		spec rules.Spec
		it   cleanup.Item
		want time.Duration
	}{
		"build output goes by ModTime":   {build, cleanup.Item{ModTime: now.Add(-40 * day), Newest: now.Add(-100 * day)}, 40 * day},
		"build output ignores Newest":    {build, cleanup.Item{ModTime: now.Add(-100 * day), Newest: now.Add(-day)}, 100 * day},
		"caches take the later, Newest":  {caches, cleanup.Item{ModTime: now.Add(-60 * day), Newest: now.Add(-day)}, day},
		"caches take the later, ModTime": {caches, cleanup.Item{ModTime: now.Add(-2 * day), Newest: now.Add(-60 * day)}, 2 * day},
		"no time is new":                 {caches, cleanup.Item{}, 0},
		"a future time is new":           {caches, cleanup.Item{ModTime: now.Add(day)}, 0},
	} {
		if got := rules.Age(tc.spec, tc.it, now); got != tc.want {
			t.Errorf("%s: %v, want %v", name, got, tc.want)
		}
	}
}

// item is a selected item of size bytes, last changed age ago.
func item(path string, size int64, age time.Duration) cleanup.Item {
	return cleanup.Item{ID: cleanup.ItemID(path), Path: path, Size: size, ModTime: now.Add(-age), Selected: true}
}

func cat(id string, kind cleanup.CategoryKind, items ...cleanup.Item) cleanup.Category {
	return cleanup.Category{ID: id, Kind: kind, Items: items}
}

func planned(t *testing.T, res cleanup.ScanResult, id rules.ID, in rules.Inputs) (rules.Planned, bool) {
	t.Helper()
	got := rules.Plan(res, []rules.Spec{spec(id)}, in)
	switch {
	case len(got) == 0:
		return rules.Planned{}, false
	case len(got) > 1 || got[0].Rule != id:
		t.Errorf("planned %+v for %s", got, id)
	}
	return got[0], true
}

func paths(res cleanup.ScanResult, p rules.Planned) []string {
	byID := map[string]string{}
	for _, c := range res.Categories {
		for _, it := range c.Items {
			byID[it.ID] = filepath.Base(it.Path)
		}
	}
	var out []string
	for _, id := range p.Req.ItemIDs {
		out = append(out, byID[id])
	}
	slices.Sort(out)
	return out
}

func TestPlanAppCaches(t *testing.T) {
	caches := filepath.Join(home, "Library", "Caches")
	old := item(filepath.Join(caches, "old"), 3*mb, 40*day)
	recentInside := item(filepath.Join(caches, "touched"), 5*mb, 60*day)
	recentInside.Newest = now.Add(-day)
	young := item(filepath.Join(caches, "young"), 2*mb, 10*day)
	unselected := item(filepath.Join(caches, "unselected"), 2*mb, 90*day)
	unselected.Selected = false
	held := item(filepath.Join(caches, "held"), 2*mb, 90*day)
	held.Hold = "不能清理"
	restored := item(filepath.Join(caches, "restored"), 2*mb, 90*day)
	running := item(filepath.Join(caches, "running"), 9*mb, 90*day)
	res := cleanup.ScanResult{Categories: []cleanup.Category{
		cat("system-junk:app-caches", cleanup.KindCache, old, recentInside, young, unselected, held, restored),
		cat("system-junk:app-caches-running", cleanup.KindCache, running),
	}}
	in := rules.Inputs{Now: now, Restored: map[string]time.Time{restored.ID: now.Add(-day)}}
	p, ok := planned(t, res, rules.AppCaches, in)
	if !ok {
		t.Fatal("nothing planned")
	}
	if got := paths(res, p); !slices.Equal(got, []string{"old"}) {
		t.Errorf("items %v", got)
	}
	if p.Items != 1 || p.Bytes != 3*mb || p.Req.Mode != cleanup.ModeTrashOnly || !slices.Equal(p.Req.CategoryIDs, []string{"system-junk:app-caches"}) {
		t.Errorf("planned %+v", p)
	}

	// A category the scanner was not sure about is skipped whole.
	res.Categories[0].Warning = "无法确认哪些应用正在运行"
	if p, ok := planned(t, res, rules.AppCaches, in); ok {
		t.Errorf("warned category planned: %+v", p)
	}
}

func TestPlanBuildOutput(t *testing.T) {
	proj := func(name string, size int64, active time.Duration, git cleanup.GitState) cleanup.Item {
		it := item(filepath.Join(home, "code", name, "node_modules"), size, active)
		it.Project = filepath.Join(home, "code", name)
		it.Git = git
		return it
	}
	clean := proj("clean", 5*mb, 100*day, cleanup.GitClean)
	none := proj("none", 4*mb, 120*day, cleanup.GitNone)
	dirty := proj("dirty", 6*mb, 200*day, cleanup.GitDirty)
	unknown := proj("unknown", 7*mb, 200*day, cleanup.GitUnknown)
	unset := proj("unset", 7*mb, 200*day, "")
	tracked := proj("tracked", 8*mb, 200*day, cleanup.GitClean)
	tracked.Hold = "构建产物文件夹里有纳入版本控制的文件"
	// Newest 100 days ago but last activity 40 days ago: the project is active.
	active := proj("active", 9*mb, 40*day, cleanup.GitClean)
	active.Newest = now.Add(-100 * day)
	inDocs := proj("docs", 3*mb, 100*day, cleanup.GitNone)
	res := cleanup.ScanResult{Categories: []cleanup.Category{
		cat("dev-junk:artifact-node-modules", cleanup.KindProjectArtifact, clean, none, dirty, unknown, unset, tracked, active),
		cat("dev-junk:tool-npm", cleanup.KindDevCache, item(filepath.Join(home, ".npm"), gb, 200*day)),
		cat("dev-junk:artifact-node-modules-trash", cleanup.KindProjectArtifact, inDocs),
	}}
	p, ok := planned(t, res, rules.BuildOutput, rules.Inputs{Now: now})
	if !ok {
		t.Fatal("nothing planned")
	}
	got := paths(res, p)
	if len(got) != 3 || p.Bytes != 12*mb {
		t.Errorf("items %v, bytes %d", got, p.Bytes)
	}
	if !slices.Equal(p.Req.CategoryIDs, []string{"dev-junk:artifact-node-modules", "dev-junk:artifact-node-modules-trash"}) {
		t.Errorf("categories %v", p.Req.CategoryIDs)
	}
	want := map[string]bool{clean.ID: true, none.ID: true, inDocs.ID: true}
	for _, id := range p.Req.ItemIDs {
		if !want[id] {
			t.Errorf("planned %s", id)
		}
	}
	// Largest first.
	if p.Req.ItemIDs[0] != clean.ID {
		t.Errorf("order %v", p.Req.ItemIDs)
	}
}

func TestPlanInstallers(t *testing.T) {
	apps := []string{"Visual Studio Code", "Firefox", "Slack"}
	dmg := item(filepath.Join(downloads, "Firefox 132.0.dmg"), 100*mb, 10*day)
	nested := item(filepath.Join(downloads, "apps", "Slack-4.41.pkg"), 90*mb, 30*day)
	vscode := item(filepath.Join(downloads, "VSCode-darwin-arm64.dmg"), 80*mb, 9*day)
	notInstalled := item(filepath.Join(downloads, "Blender-4.2.dmg"), 300*mb, 30*day)
	young := item(filepath.Join(downloads, "Firefox 133.0.dmg"), 100*mb, 3*day)
	desktop := item(filepath.Join(home, "Desktop", "Firefox 131.0.dmg"), 100*mb, 30*day)
	zip := item(filepath.Join(downloads, "Firefox-mac.zip"), 100*mb, 30*day)
	iso := item(filepath.Join(downloads, "Firefox.iso"), 100*mb, 30*day)
	recent := item(filepath.Join(downloads, "Slack-4.40.dmg"), 100*mb, 30*day)
	recent.Selected = false
	res := cleanup.ScanResult{Categories: []cleanup.Category{
		cat("installers:disk-images", cleanup.KindInstaller, dmg, vscode, notInstalled, young, desktop),
		cat("installers:installer-packages", cleanup.KindInstaller, nested),
		cat("installers:archives", cleanup.KindInstaller, zip),
		cat("installers:system-images", cleanup.KindInstaller, iso),
		cat("installers:recent-installers", cleanup.KindInstaller, recent),
	}}
	in := rules.Inputs{Now: now, Downloads: downloads, InstalledApps: apps}
	p, ok := planned(t, res, rules.Installers, in)
	if !ok {
		t.Fatal("nothing planned")
	}
	if got := paths(res, p); !slices.Equal(got, []string{"Firefox 132.0.dmg", "Slack-4.41.pkg", "VSCode-darwin-arm64.dmg"}) {
		t.Errorf("items %v", got)
	}
	if !slices.Equal(p.Req.CategoryIDs, []string{"installers:disk-images", "installers:installer-packages"}) {
		t.Errorf("categories %v", p.Req.CategoryIDs)
	}
	// Installed apps that could not be read plan nothing; nor does an unknown Downloads folder.
	if p, ok := planned(t, res, rules.Installers, rules.Inputs{Now: now, Downloads: downloads}); ok {
		t.Errorf("planned without installed apps: %+v", p)
	}
	if p, ok := planned(t, res, rules.Installers, rules.Inputs{Now: now, InstalledApps: apps}); ok {
		t.Errorf("planned without Downloads: %+v", p)
	}
	// An empty list was read and matches nothing.
	if p, ok := planned(t, res, rules.Installers, rules.Inputs{Now: now, Downloads: downloads, InstalledApps: []string{}}); ok {
		t.Errorf("planned with no apps: %+v", p)
	}
	// Attached images that could not be listed keep the disk images out.
	res.Categories[0].Warning = "无法确认哪些磁盘映像正在使用"
	p, _ = planned(t, res, rules.Installers, in)
	if got := paths(res, p); !slices.Equal(got, []string{"Slack-4.41.pkg"}) {
		t.Errorf("with a warning %v", got)
	}
	// Downloads itself is not inside Downloads.
	res = cleanup.ScanResult{Categories: []cleanup.Category{cat("installers:disk-images", cleanup.KindInstaller, item(downloads, gb, 30*day))}}
	if p, ok := planned(t, res, rules.Installers, rules.Inputs{Now: now, Downloads: downloads, InstalledApps: []string{"Downloads"}}); ok {
		t.Errorf("Downloads planned: %+v", p)
	}
}

func TestPlanIncompleteDownloadsAndRefusals(t *testing.T) {
	part := item(filepath.Join(downloads, "a.crdownload"), mb, 8*day)
	fresh := item(filepath.Join(downloads, "b.part"), mb, 2*day)
	res := cleanup.ScanResult{Categories: []cleanup.Category{cat("system-junk:incomplete-downloads", cleanup.KindCache, part, fresh)}}
	p, ok := planned(t, res, rules.IncompleteDownloads, rules.Inputs{Now: now})
	if !ok || !slices.Equal(p.Req.ItemIDs, []string{part.ID}) {
		t.Errorf("planned %+v %v", p, ok)
	}
	// A matching category that runs a command, or holds the Trash, is never taken.
	res.Categories[0].Command = "rm -rf ~/Downloads"
	if p, ok := planned(t, res, rules.IncompleteDownloads, rules.Inputs{Now: now}); ok {
		t.Errorf("command category planned: %+v", p)
	}
	res.Categories[0].Command = ""
	res.Categories[0].Kind = cleanup.KindTrash
	if p, ok := planned(t, res, rules.IncompleteDownloads, rules.Inputs{Now: now}); ok {
		t.Errorf("Trash category planned: %+v", p)
	}
	// Rules that are not due are not planned.
	if got := rules.Plan(res, nil, rules.Inputs{Now: now}); len(got) != 0 {
		t.Errorf("planned %v", got)
	}
}

func TestPlanCapsItems(t *testing.T) {
	var items []cleanup.Item
	for i := range rules.MaxItems + 10 {
		items = append(items, item(filepath.Join(downloads, fmt.Sprintf("%05d.part", i)), int64(i+1), 30*day))
	}
	res := cleanup.ScanResult{Categories: []cleanup.Category{cat("system-junk:incomplete-downloads", cleanup.KindCache, items...)}}
	p, ok := planned(t, res, rules.IncompleteDownloads, rules.Inputs{Now: now})
	if !ok || p.Items != rules.MaxItems || len(p.Req.ItemIDs) != rules.MaxItems {
		t.Fatalf("planned %d items", p.Items)
	}
	// The largest are kept: the 10 smallest are left.
	for _, id := range p.Req.ItemIDs {
		for _, small := range items[:10] {
			if id == small.ID {
				t.Fatalf("kept a small item %s", small.Path)
			}
		}
	}
}

func TestSummarize(t *testing.T) {
	from, to := now.Add(-7*day), now
	run := func(id string, kind history.Kind, rule string, at time.Duration, items int, bytes int64) history.Run {
		return history.Run{ID: id, Kind: kind, Rule: rule, StartedAt: now.Add(at), Summary: &history.Totals{Items: items, Trashed: bytes}}
	}
	restore := run("r1", history.KindRestore, "", -day, 1, 1*gb)
	restore.RestoreOf = "a2"
	unrelated := run("r2", history.KindRestore, "", -day, 5, 5*gb)
	unrelated.RestoreOf = "manual"
	runs := []history.Run{
		run("a1", history.KindAuto, "app-caches", -6*day, 10, 2*gb),
		run("a2", history.KindAuto, "build-output", -2*day, 3, 3*gb),
		run("a3", history.KindAuto, "app-caches", -day, 5, gb),
		run("old", history.KindAuto, "installers", -8*day, 1, gb),
		run("edge", history.KindAuto, "installers", 0, 1, gb),
		run("manual", history.KindSmartScan, "", -day, 9, 9*gb),
		restore, unrelated,
	}
	s := rules.Summarize(runs, from, to)
	if s.Runs != 3 || s.Items != 17 || s.Trashed != 5*gb {
		t.Errorf("summary %+v", s)
	}
	want := []rules.RuleTotal{{Rule: rules.AppCaches, Items: 15, Bytes: 3 * gb}, {Rule: rules.BuildOutput, Items: 2, Bytes: 2 * gb}}
	if !slices.Equal(s.ByRule, want) {
		t.Errorf("by rule %+v", s.ByRule)
	}
	// A run put back whole drops out of ByRule.
	all := run("r3", history.KindRestore, "", 0, 3, 3*gb)
	all.RestoreOf = "a2"
	s = rules.Summarize(append(runs, all), from, to)
	if len(s.ByRule) != 1 || s.ByRule[0].Rule != rules.AppCaches || s.Trashed != 3*gb || s.Runs != 3 {
		t.Errorf("restored whole: %+v", s)
	}
	if s := rules.Summarize(nil, from, to); s.Runs != 0 || s.ByRule != nil {
		t.Errorf("empty %+v", s)
	}
}
