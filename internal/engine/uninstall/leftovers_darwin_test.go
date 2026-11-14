package uninstall

import (
	"context"
	"errors"
	"io/fs"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// leftoverFixture lays out a Library with the leftovers of com.example.foo ("Foo Pro", file Foo.app) and near misses that must not match.
func leftoverFixture(t *testing.T, f fixture) (app string, want, nearMiss []string) {
	t.Helper()
	app = f.bundle(t, f.apps, "Foo.app", map[string]any{"CFBundleIdentifier": "com.example.foo", "CFBundleDisplayName": "Foo Pro"})
	lib := f.lib
	j := func(parts ...string) string { return filepath.Join(append([]string{lib}, parts...)...) }
	want = []string{
		j("Application Support", "com.example.foo"),
		j("Application Support", "Foo Pro"),
		j("Application Support", "Foo"),
		j("Caches", "com.example.foo"),
		j("Caches", "Foo"),
		j("Preferences", "com.example.foo.plist"),
		j("Preferences", "com.example.foo.LSSharedFileList.plist"),
		j("Preferences", "ByHost", "com.example.foo.0123-4567.plist"),
		j("Containers", "com.example.foo"),
		j("Group Containers", "ABCDE12345.com.example.foo"),
		j("Group Containers", "group.com.example.foo"),
		j("Saved Application State", "com.example.foo.savedState"),
		j("HTTPStorages", "com.example.foo"),
		j("WebKit", "com.example.foo"),
		j("Logs", "Foo Pro"),
		j("Logs", "com.example.foo"),
		j("Cookies", "com.example.foo.binarycookies"),
	}
	nearMiss = []string{
		j("Application Support", "com.example.foobar"),
		j("Application Support", "com.example.fo"),
		j("Application Support", "com.example.foo.helper"),
		j("Application Support", "Foo Pro Helper"),
		j("Application Support", "Google"),
		j("Caches", "com.example.foo2"),
		j("Caches", "Foo Pro Beta"),
		j("Preferences", "com.example.foobar.plist"),
		j("Preferences", "com.example.foo.plist.lockfile"),
		j("Preferences", "xcom.example.foo.plist"),
		j("Preferences", "ByHost", "com.example.foo.plist"),
		j("Preferences", "ByHost", "com.example.foobar.0123.plist"),
		j("Containers", "com.example.foo.helper"),
		j("Containers", "Foo Pro"),
		j("Group Containers", "ABCDE12345.com.example.foobar"),
		j("Group Containers", "ABCDE12345.com.example"),
		j("Group Containers", "com.example.foo.shared"),
		j("Saved Application State", "com.example.foobar.savedState"),
		j("HTTPStorages", "com.example.foo.binarycookies"),
		j("WebKit", "Foo Pro"),
		j("Logs", "Foo Pro Updater"),
		j("Cookies", "com.example.foobar.binarycookies"),
	}
	for _, p := range append(slices.Clone(want), nearMiss...) {
		if strings.HasSuffix(p, ".plist") || strings.HasSuffix(p, ".binarycookies") || strings.HasSuffix(p, ".lockfile") {
			writeFile(t, p, "data")
		} else {
			writeFile(t, filepath.Join(p, "data.bin"), strings.Repeat("y", 2048))
		}
	}
	return app, want, nearMiss
}

func TestLeftoverRules(t *testing.T) {
	f := newFixture(t)
	appPath, want, nearMiss := leftoverFixture(t, f)
	e := f.engine(t, Config{})
	apps, err := e.Apps(t.Context())
	if err != nil || len(apps) != 1 {
		t.Fatalf("apps: %v %v", apps, err)
	}
	if apps[0].Path != appPath {
		t.Fatalf("app path %q", apps[0].Path)
	}
	cands, denied := macLeftovers(t.Context(), f.lib, apps[0])
	if denied != 0 {
		t.Errorf("denied = %d", denied)
	}
	var got []string
	for _, c := range cands {
		got = append(got, c.Path)
		if !c.Selected {
			t.Errorf("%s is not selected", c.Path)
		}
		if !strings.HasPrefix(c.Path, c.Root+string(filepath.Separator)) {
			t.Errorf("%s is not inside its root %s", c.Path, c.Root)
		}
	}
	for _, w := range want {
		if !slices.Contains(got, w) {
			t.Errorf("missing %s", w)
		}
	}
	for _, n := range nearMiss {
		if slices.Contains(got, n) {
			t.Errorf("near miss %s matched", n)
		}
	}
	if len(got) != len(want) {
		t.Errorf("got %d leftovers, want %d: %v", len(got), len(want), got)
	}
	for _, c := range cands {
		byName := strings.HasSuffix(c.Path, "Foo Pro") || strings.HasSuffix(c.Path, string(filepath.Separator)+"Foo")
		if byName != (c.Note == noteByName) {
			t.Errorf("%s note %q", c.Path, c.Note)
		}
	}
}

func TestLeftoverNamesNeedToBeSpecific(t *testing.T) {
	if got := nameKeys([]string{"Go", "Google", "electron", "Fo/o", "Notion"}); !slices.Equal(got, []string{"Notion"}) {
		t.Errorf("nameKeys = %v", got)
	}
}

func TestLaunchAgents(t *testing.T) {
	f := newFixture(t)
	app := f.bundle(t, f.apps, "Foo.app", map[string]any{"CFBundleIdentifier": "com.example.foo"})
	support := filepath.Join(f.lib, "Application Support", "com.example.foo")
	mkdir(t, support)
	agents := filepath.Join(f.lib, "LaunchAgents")
	writePlist(t, filepath.Join(agents, "inside.plist"), map[string]any{"Label": "com.vendor.updater", "ProgramArguments": []any{filepath.Join(app, "Contents", "MacOS", "Updater"), "--daemon"}})
	writePlist(t, filepath.Join(agents, "program.plist"), map[string]any{"Label": "com.vendor.other", "Program": filepath.Join(app, "Contents", "Library", "LoginItems", "Helper.app", "Contents", "MacOS", "Helper")})
	writePlist(t, filepath.Join(agents, "open.plist"), map[string]any{"Label": "com.vendor.open", "ProgramArguments": []any{"/usr/bin/open", "-a", app}})
	writePlist(t, filepath.Join(agents, "label-support.plist"), map[string]any{"Label": "com.example.foo.agent", "Program": filepath.Join(support, "agent")})
	writePlist(t, filepath.Join(agents, "label-only.plist"), map[string]any{"Label": "com.example.foo", "Program": "/usr/local/bin/foo-agent"})
	// Near misses: another app's agent, a bundle path prefix that is a different app, a label that only starts like the ID, junk.
	writePlist(t, filepath.Join(agents, "other.plist"), map[string]any{"Label": "com.example.foobar", "ProgramArguments": []any{"/Applications/Foo Two.app/Contents/MacOS/x"}})
	writePlist(t, filepath.Join(agents, "prefix.plist"), map[string]any{"Label": "com.vendor.prefix", "Program": app + "x/Contents/MacOS/x"})
	writeFile(t, filepath.Join(agents, "junk.plist"), "not a plist")
	writePlist(t, filepath.Join(agents, "notes.txt"), map[string]any{"Label": "com.example.foo"})

	e := f.engine(t, Config{})
	apps, err := e.Apps(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	cands, _ := macLeftovers(t.Context(), f.lib, apps[0])
	got := make(map[string]Candidate)
	for _, c := range cands {
		if c.Kind == KindLaunchAgents {
			got[filepath.Base(c.Path)] = c
		}
	}
	for name, selected := range map[string]bool{"inside.plist": true, "program.plist": true, "open.plist": true, "label-support.plist": true, "label-only.plist": false} {
		c, ok := got[name]
		if !ok {
			t.Errorf("%s not matched", name)
			continue
		}
		if c.Selected != selected {
			t.Errorf("%s selected = %v, want %v", name, c.Selected, selected)
		}
		if c.Note == "" {
			t.Errorf("%s has no note", name)
		}
	}
	if len(got) != 5 {
		t.Errorf("matched %d agents: %v", len(got), got)
	}
}

func TestPlanAndRemove(t *testing.T) {
	f := newFixture(t)
	appPath, want, _ := leftoverFixture(t, f)
	excluded := filepath.Join(f.lib, "Caches", "Foo")
	// An exclusion inside a leftover keeps the whole leftover, which would otherwise take it along.
	holder := filepath.Join(f.lib, "Logs", "Foo Pro")
	writeFile(t, filepath.Join(holder, "keep.me"), "x")
	del := newFakeDeleter(t)
	e := f.engine(t, Config{Deleter: del, Exclusions: func() []string { return []string{excluded, "*.me"} }})
	apps, err := e.Apps(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	id := apps[0].ID
	plan, err := e.Plan(t.Context(), id)
	if err != nil {
		t.Fatal(err)
	}
	if plan.Bundle == nil || plan.Bundle.Path != appPath || plan.Bundle.Size <= 0 || !plan.Bundle.Selected {
		t.Fatalf("bundle = %+v", plan.Bundle)
	}
	if len(plan.Leftovers) != len(want)-2 {
		t.Fatalf("%d leftovers, want %d (one excluded, one holding an exclusion)", len(plan.Leftovers), len(want)-2)
	}
	for i, l := range plan.Leftovers {
		if l.Path == excluded || l.Path == holder {
			t.Errorf("%s is offered", l.Path)
		}
		if l.Size <= 0 {
			t.Errorf("%s has no size", l.Path)
		}
		if i > 0 && slices.Index(kindOrder, plan.Leftovers[i-1].Kind) > slices.Index(kindOrder, l.Kind) {
			t.Errorf("leftovers out of order at %s", l.Path)
		}
	}

	// A request for a leftover the plan does not have, or for another app, moves nothing.
	if _, err := e.Remove(t.Context(), Request{AppID: id, LeftoverIDs: []string{"nope"}}, nil); !errors.Is(err, ErrStalePlan) {
		t.Errorf("unknown leftover: %v", err)
	}
	if _, err := e.Remove(t.Context(), Request{AppID: "other"}, nil); !errors.Is(err, ErrStalePlan) {
		t.Errorf("other app: %v", err)
	}
	if len(del.moved()) != 0 {
		t.Fatal("a rejected request moved something")
	}

	keep := plan.Leftovers[0]
	var ids []string
	for _, l := range plan.Leftovers[1:] {
		ids = append(ids, l.ID)
	}
	var events []cleanup.Progress
	res, err := e.Remove(t.Context(), Request{AppID: id, LeftoverIDs: ids}, func(p cleanup.Progress) { events = append(events, p) })
	if err != nil {
		t.Fatal(err)
	}
	if !res.BundleRemoved || res.BundleFailure != "" || res.App != "Foo Pro" {
		t.Errorf("result = %+v", res)
	}
	if res.Clean.Removed != 1+len(ids) || len(res.Clean.Failed) != 0 || res.Clean.Trashed <= 0 {
		t.Errorf("clean = %+v", res.Clean)
	}
	moved := del.moved()
	if len(moved) == 0 || moved[0] != appPath {
		t.Fatalf("the bundle must go first: %v", moved)
	}
	if exists(appPath) || !exists(keep.Path) {
		t.Error("the bundle stayed or an unchecked leftover went")
	}
	last := events[len(events)-1]
	if last.Total != 1+len(ids) || last.Done != last.Total || last.Bytes != res.Clean.Trashed {
		t.Errorf("last progress = %+v", last)
	}
	for _, p := range events {
		if p.Total != last.Total || p.Done > p.Total {
			t.Errorf("progress %+v", p)
		}
	}
	// The plan is used up.
	if _, err := e.Remove(t.Context(), Request{AppID: id}, nil); !errors.Is(err, ErrStalePlan) {
		t.Errorf("second removal: %v", err)
	}
}

func TestRemoveKeepsLeftoversWhenTheBundleStays(t *testing.T) {
	f := newFixture(t)
	appPath, _, _ := leftoverFixture(t, f)
	del := newFakeDeleter(t)
	del.failing[appPath] = &fs.PathError{Op: "trash", Path: appPath, Err: fs.ErrPermission}
	e := f.engine(t, Config{Deleter: del})
	apps, _ := e.Apps(t.Context())
	plan, err := e.Plan(t.Context(), apps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	var ids []string
	for _, l := range plan.Leftovers {
		ids = append(ids, l.ID)
	}
	res, err := e.Remove(t.Context(), Request{AppID: apps[0].ID, LeftoverIDs: ids}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res.BundleRemoved || !strings.Contains(res.BundleFailure, "管理员") || !strings.Contains(res.BundleFailure, "访达") {
		t.Errorf("result = %+v", res)
	}
	if len(del.moved()) != 0 {
		t.Errorf("leftovers moved although the app stayed: %v", del.moved())
	}
	if len(res.Clean.Failed) != 1 || res.Clean.Removed != 0 {
		t.Errorf("clean = %+v", res.Clean)
	}

	// Another failure names its reason and still points to Finder.
	del.failing[appPath] = errors.New("boom")
	res, err = e.Remove(t.Context(), Request{AppID: apps[0].ID}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if strings.Contains(res.BundleFailure, "管理员") || !strings.Contains(res.BundleFailure, "访达") {
		t.Errorf("failure = %q", res.BundleFailure)
	}
}

func TestRemoveRefusesARunningApp(t *testing.T) {
	f := newFixture(t)
	appPath := f.bundle(t, f.apps, "Foo.app", map[string]any{"CFBundleIdentifier": "com.example.foo", "CFBundleName": "Foo"})
	running := false
	del := newFakeDeleter(t)
	e := f.engine(t, Config{Deleter: del, Processes: func(context.Context) ([]string, error) {
		if running {
			return []string{filepath.Join(appPath, "Contents", "MacOS", "Foo")}, nil
		}
		return nil, nil
	}})
	apps, _ := e.Apps(t.Context())
	if _, err := e.Plan(t.Context(), apps[0].ID); err != nil {
		t.Fatal(err)
	}
	running = true
	_, err := e.Remove(t.Context(), Request{AppID: apps[0].ID}, nil)
	var ue *cleanup.UserError
	if !errors.As(err, &ue) || ue.Message != "请先退出「Foo」再卸载" {
		t.Fatalf("err = %v", err)
	}
	if !exists(appPath) || len(del.moved()) != 0 {
		t.Error("a running app was moved")
	}
	plan, err := e.Plan(t.Context(), apps[0].ID)
	if err != nil || !plan.Running {
		t.Errorf("plan of a running app: %+v %v", plan, err)
	}
}

func TestPlanOfASharedCopyStartsUnchecked(t *testing.T) {
	f := newFixture(t)
	f.bundle(t, f.apps, "Foo.app", map[string]any{"CFBundleIdentifier": "com.example.foo", "CFBundleName": "Foo"})
	copyPath := f.bundle(t, f.userApps, "Foo.app", map[string]any{"CFBundleIdentifier": "com.example.foo", "CFBundleName": "Foo"})
	mkdir(t, filepath.Join(f.lib, "Caches", "com.example.foo"))
	e := f.engine(t, Config{})
	apps, _ := e.Apps(t.Context())
	if len(apps) != 2 {
		t.Fatalf("apps = %v", apps)
	}
	plan, err := e.Plan(t.Context(), apps[0].ID)
	if err != nil {
		t.Fatal(err)
	}
	if plan.SharedWith == "" || len(plan.Leftovers) != 1 || plan.Leftovers[0].Selected {
		t.Errorf("plan = %+v", plan)
	}
	if plan.SharedWith != apps[1].Path || !slices.Contains([]string{apps[0].Path, apps[1].Path}, copyPath) {
		t.Errorf("shared with %q", plan.SharedWith)
	}
}

func TestPlanUnknownApp(t *testing.T) {
	f := newFixture(t)
	e := f.engine(t, Config{})
	if _, err := e.Plan(t.Context(), "nope"); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("err = %v", err)
	}
	if _, err := e.RunUninstaller(t.Context(), "nope", nil); !errors.Is(err, ErrUnknownApp) {
		t.Errorf("RunUninstaller: %v", err)
	}
}

func TestRemoveRefusesWhenRunningCannotBeChecked(t *testing.T) {
	f := newFixture(t)
	appPath := f.bundle(t, f.apps, "Foo.app", map[string]any{"CFBundleIdentifier": "com.example.foo", "CFBundleName": "Foo"})
	broken := false
	del := newFakeDeleter(t)
	e := f.engine(t, Config{Deleter: del, Processes: func(context.Context) ([]string, error) {
		if broken {
			return nil, errors.New("no process list")
		}
		return nil, nil
	}})
	apps, _ := e.Apps(t.Context())
	if _, err := e.Plan(t.Context(), apps[0].ID); err != nil {
		t.Fatal(err)
	}
	broken = true
	var ue *cleanup.UserError
	if _, err := e.Remove(t.Context(), Request{AppID: apps[0].ID}, nil); !errors.As(err, &ue) || !strings.Contains(ue.Message, "无法确认") {
		t.Fatalf("err = %v", err)
	}
	if !exists(appPath) || len(del.moved()) != 0 {
		t.Error("the app was moved without knowing whether it runs")
	}
}

func TestPlanRefusesABundleHoldingAnExclusion(t *testing.T) {
	f := newFixture(t)
	appPath := f.bundle(t, f.apps, "Foo.app", map[string]any{"CFBundleIdentifier": "com.example.foo", "CFBundleName": "Foo"})
	keep := filepath.Join(appPath, "Contents", "Resources", "keep")
	writeFile(t, keep, "x")
	e := f.engine(t, Config{Exclusions: func() []string { return []string{keep} }})
	apps, _ := e.Apps(t.Context())
	var ue *cleanup.UserError
	if _, err := e.Plan(t.Context(), apps[0].ID); !errors.As(err, &ue) || !strings.Contains(ue.Message, "排除") {
		t.Errorf("err = %v", err)
	}
}
