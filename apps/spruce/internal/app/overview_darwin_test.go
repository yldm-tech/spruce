package app

import (
	"context"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/growth"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/rules"
	"spruce/apps/spruce/internal/engine/schedule"
	"spruce/apps/spruce/internal/engine/settings"
	"spruce/apps/spruce/internal/engine/trash"
	"spruce/apps/spruce/internal/engine/undo"
	"spruce/apps/spruce/internal/host/folderaccess"
)

// newTrashFixture is newOvFixture over a temporary home with its own Trash (trashHome), cleans that move into it, the undo ledger and 放回原处.
func newTrashFixture(t *testing.T) (*ovFixture, string, *undo.Ledger) {
	t.Helper()
	home, bin, l := trashHome(t)
	f := newOvFixture(t, ovOptions{home: home, deleter: &binDeleter{bin: bin}})
	SetLedger(f.cleanups, l)
	f.cfg.Ledger = l
	f.cfg.Restore = NewRestore(l, trash.SystemRestorer(), f.history, f.activity, home, slog.New(slog.DiscardHandler))
	return f, bin, l
}

func autoRuns(runs []history.Run) []history.Run {
	return slices.DeleteFunc(slices.Clone(runs), func(r history.Run) bool { return r.Kind != history.KindAuto })
}

func TestOverviewRulesRunOnlyWhenDue(t *testing.T) {
	f, bin, l := newTrashFixture(t)
	f.update(func(s *settings.AppSettings) { s.Automation.AppCaches, s.Automation.Installers = true, true })
	o := f.start()
	f.clock.Add(3 * time.Minute)
	f.step()

	auto := autoRuns(f.runs())
	if len(auto) != 1 {
		t.Fatalf("automatic runs %+v", auto)
	}
	run := auto[0]
	if run.Rule != string(rules.AppCaches) || run.Undo == nil || run.Undo.Items != 1 || run.Summary == nil || run.Summary.Trashed != cacheSize || run.Summary.Freed != 0 {
		t.Fatalf("automatic run %+v undo %+v summary %+v", run, run.Undo, run.Summary)
	}
	if len(run.Details) == 0 || run.Details[0] == "" {
		t.Errorf("details %q", run.Details)
	}
	if _, err := l.Load(run.ID); err != nil {
		t.Errorf("no ledger entry: %v", err)
	}
	// The cache, deleted for good by a manual clean, went to the Trash; the installer is too recent for its rule.
	if _, err := os.Stat(filepath.Join(bin, "com.example.cache")); err != nil {
		t.Errorf("the cache is not in the Trash: %v", err)
	}
	if _, err := os.Stat(filepath.Join(f.home, "Downloads", "App.dmg")); err != nil {
		t.Errorf("the recent installer was moved: %v", err)
	}
	st := o.State()
	if st.LastAuto == nil || !slices.Equal(st.LastAuto.RunIDs, []string{run.ID}) || st.LastAuto.Trashed != cacheSize {
		t.Errorf("last automatic run %+v", st.LastAuto)
	}
	if len(st.Result.Recommendations) != 1 || st.Result.Recommendations[0].ID != "installers" {
		t.Errorf("cards after the rule: %+v", st.Result.Recommendations)
	}
	for _, r := range st.Rules {
		switch r.ID {
		case string(rules.AppCaches):
			if !r.Enabled || !r.LastRun.Equal(f.clock.Now()) || r.LastTrashed != cacheSize {
				t.Errorf("app-caches status %+v", r)
			}
		case string(rules.Installers):
			if !r.Enabled || !r.LastRun.Equal(f.clock.Now()) || r.LastTrashed != 0 {
				t.Errorf("installers status %+v", r)
			}
		}
	}
	saved, err := schedule.OpenState(filepath.Join(f.data, "background.json")).Load()
	if err != nil || len(saved.RuleRuns) != 2 {
		t.Errorf("background.json %+v %v", saved, err)
	}

	// The cache comes back; the weekly rule waits its week although a check runs.
	f.writeCache("com.example.cache", 40*24*time.Hour)
	f.clock.Add(7 * time.Hour)
	f.step()
	if got := autoRuns(f.runs()); len(got) != 1 {
		t.Fatalf("the rule ran again within its week: %+v", got)
	}
	if st := o.State(); len(st.Result.Recommendations) != 2 {
		t.Errorf("cards %+v", st.Result.Recommendations)
	}
	// A week later it runs again, also after a restart (background.json); the installer is now old enough for the daily rule, its app being installed.
	f.clock.Add(7 * 24 * time.Hour)
	o = f.start()
	f.clock.Add(3 * time.Minute)
	f.step()
	got := autoRuns(f.runs())
	var ruled []string
	for _, r := range got[:2] {
		ruled = append(ruled, r.Rule)
	}
	slices.Sort(ruled)
	if len(got) != 3 || !slices.Equal(ruled, []string{string(rules.AppCaches), string(rules.Installers)}) {
		t.Errorf("after a week: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(bin, "App.dmg")); err != nil {
		t.Errorf("the old installer is not in the Trash: %v", err)
	}
	if st := o.State(); st.LastAuto == nil || len(st.LastAuto.RunIDs) != 2 || st.LastAuto.Trashed != cacheSize+installerSize || len(st.Result.Recommendations) != 0 {
		t.Errorf("last automatic run %+v, cards %+v", st.LastAuto, st.Result.Recommendations)
	}
}

// TestOverviewRulesFollowTheSettingsOfNow: the rules run under the settings as they are once the scheduled check ends, not as they were when it started.
func TestOverviewRulesFollowTheSettingsOfNow(t *testing.T) {
	for _, tc := range []struct {
		name   string
		change func(*settings.AppSettings, string)
	}{
		{"rule turned off", func(s *settings.AppSettings, _ string) { s.Automation.AppCaches = false }},
		{"scheduled checks turned off", func(s *settings.AppSettings, _ string) { s.Background.Enabled = false }},
		{"exclusion added", func(s *settings.AppSettings, home string) {
			s.Cleanup.Exclusions = append(s.Cleanup.Exclusions, filepath.Join(home, "Library", "Caches", "com.example.cache"))
		}},
	} {
		t.Run(tc.name, func(t *testing.T) {
			f, _, _ := newTrashFixture(t)
			f.update(func(s *settings.AppSettings) { s.Automation.AppCaches = true })
			o := f.start()
			f.bgCaches.hold, f.bgCaches.started = make(chan struct{}), make(chan struct{}, 1)
			f.clock.Add(3 * time.Minute)
			stepped := make(chan struct{})
			go func() {
				defer close(stepped)
				f.step()
			}()
			<-f.bgCaches.started
			f.update(func(s *settings.AppSettings) { tc.change(s, f.home) })
			close(f.bgCaches.hold)
			<-stepped
			if got := autoRuns(f.runs()); len(got) != 0 {
				t.Errorf("automatic runs %+v", got)
			}
			if _, err := os.Stat(filepath.Join(f.home, "Library", "Caches", "com.example.cache")); err != nil {
				t.Errorf("the cache was moved: %v", err)
			}
			// The rule did not run, so it stays due.
			for _, r := range o.State().Rules {
				if r.ID == string(rules.AppCaches) && !r.LastRun.IsZero() {
					t.Errorf("recorded as run: %+v", r)
				}
			}
		})
	}
}

func TestOverviewRulesOnlyOnMacOS(t *testing.T) {
	home, bin, l := trashHome(t)
	f := newOvFixture(t, ovOptions{home: home, deleter: &binDeleter{bin: bin}, goos: "windows"})
	SetLedger(f.cleanups, l)
	f.update(func(s *settings.AppSettings) { s.Automation.AppCaches = true })
	f.start()
	f.clock.Add(3 * time.Minute)
	f.step()
	if got := autoRuns(f.runs()); len(got) != 0 {
		t.Errorf("automatic runs outside macOS: %+v", got)
	}
	if _, err := os.Stat(filepath.Join(home, "Library", "Caches", "com.example.cache")); err != nil {
		t.Errorf("the cache was touched: %v", err)
	}
}

func TestOverviewUndoPutsBackAndChecksAgain(t *testing.T) {
	f, bin, _ := newTrashFixture(t)
	o := f.start()
	if !o.State().CanUndo {
		t.Fatal("cannot undo on macOS")
	}
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	inst := rec(t, o.State(), "installers")
	out, err := o.Clean(t.Context(), inst.req, CleanOrigin{Recommendation: "installers"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.Undoable || out.NotUndoable != 0 || out.RunID == "" || out.Result.Trashed != installerSize {
		t.Fatalf("outcome %+v", out)
	}
	if _, err := os.Stat(filepath.Join(bin, "App.dmg")); err != nil {
		t.Fatalf("the installer is not in the Trash: %v", err)
	}
	if st := o.State(); len(st.Result.Recommendations) != 1 {
		t.Errorf("cards after the clean %+v", st.Result.Recommendations)
	}
	scans := f.fgInst.count()

	back, err := o.Undo(t.Context(), out.RunID)
	if err != nil {
		t.Fatal(err)
	}
	if back.Restored != 1 || back.Failed != 0 || back.RunID == "" {
		t.Errorf("undo %+v", back)
	}
	if _, err := os.Stat(filepath.Join(f.home, "Downloads", "App.dmg")); err != nil {
		t.Errorf("the installer is not back: %v", err)
	}
	runs := f.runs()
	if r := runs[0]; r.Kind != history.KindRestore || r.RestoreOf != out.RunID || r.ID != back.RunID {
		t.Errorf("restore run %+v", r)
	}
	// The cards are not pieced back: a foreground check runs.
	f.step()
	if f.fgInst.count() != scans+1 {
		t.Fatal("no check after the undo")
	}
	if st := o.State(); st.Result.Mode != CheckForeground || len(st.Result.Recommendations) != 2 {
		t.Errorf("after the check: %+v", st.Result.Recommendations)
	}
}

// TestOverviewUndoDuringACheckChecksAgain: an undo that ends while a foreground check runs stops that check, which may have passed the restored items, and runs it again; no scheduled check takes its place in between.
func TestOverviewUndoDuringACheckChecksAgain(t *testing.T) {
	f, _, _ := newTrashFixture(t)
	o := f.start()
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	out, err := o.Clean(t.Context(), rec(t, o.State(), "installers").req, CleanOrigin{Recommendation: "installers"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	// A 重新检查 is held inside its scan; a scheduled check is due too.
	f.clock.Add(7 * time.Hour)
	f.fgInst.hold, f.fgInst.started = make(chan struct{}), make(chan struct{}, 1)
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	stepped := make(chan struct{})
	go func() {
		defer close(stepped)
		f.step()
	}()
	<-f.fgInst.started
	scans := f.fgInst.count()
	if _, err := o.Undo(t.Context(), out.RunID); err != nil {
		t.Fatal(err)
	}
	<-stepped
	f.fgInst.hold, f.fgInst.started = nil, nil
	if f.bgInst.count() != 0 {
		t.Fatal("a scheduled check ran before the asked one")
	}
	if st := o.State(); st.Phase != OverviewChecking {
		t.Errorf("phase %q, want the check asked for again", st.Phase)
	}
	f.step()
	if f.fgInst.count() != scans+1 {
		t.Fatalf("foreground scans %d, want %d", f.fgInst.count(), scans+1)
	}
	if st := o.State(); st.Result.Mode != CheckForeground || len(st.Result.Recommendations) != 2 {
		t.Errorf("after the check: %+v", st.Result.Recommendations)
	}
}

// TestOverviewSnapshots: after a scheduled check comes a space snapshot a day at most, read through the read gate (a prompting folder no check of the user entered is skipped), and the old ones are pruned.
func TestOverviewSnapshots(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	store, err := growth.Open(filepath.Join(f.data, "growth"))
	if err != nil {
		t.Fatal(err)
	}
	access := folderaccess.Open(filepath.Join(f.data, "folder-access.json"), f.home, func() bool { return false })
	gate := NewGateRecorder(access)
	downloads := filepath.Join(f.home, "Downloads")
	f.fgInst.mayRead, f.fgInst.reads = gate.Foreground, []string{downloads}
	f.bgInst.mayRead, f.bgInst.reads = gate.Background, []string{downloads}
	f.cfg.Access, f.cfg.Gate, f.cfg.Growth = access, gate, store
	f.cfg.Watches = func(settings.AppSettings) []growth.Watch {
		return []growth.Watch{
			{ID: "library-caches", Path: filepath.Join(f.home, "Library", "Caches"), Children: true},
			{ID: "downloads", Path: downloads, Children: true},
		}
	}
	// A snapshot of long ago, which the first prune drops.
	old := f.clock.Now().AddDate(0, -6, 0)
	if err := store.Save(growth.Snapshot{At: old, Locations: []growth.Location{}}); err != nil {
		t.Fatal(err)
	}
	snapshots := func() []time.Time {
		t.Helper()
		times, err := store.List()
		if err != nil {
			t.Fatal(err)
		}
		return times
	}
	location := func(at time.Time, id string) growth.Location {
		t.Helper()
		snap, err := store.Load(at)
		if err != nil {
			t.Fatal(err)
		}
		for _, l := range snap.Locations {
			if l.ID == id {
				return l
			}
		}
		t.Fatalf("no %s in %+v", id, snap.Locations)
		return growth.Location{}
	}
	o := f.start()
	f.clock.Add(3 * time.Minute)
	f.step()
	times := snapshots()
	if len(times) != 1 || !times[0].Equal(f.clock.Now().Truncate(time.Second)) {
		t.Fatalf("snapshots %v, want one at %v (the old one pruned)", times, f.clock.Now())
	}
	if l := location(times[0], "downloads"); !l.Skipped {
		t.Errorf("a background snapshot read Downloads: %+v", l)
	}
	if l := location(times[0], "library-caches"); l.Skipped || l.Bytes == 0 {
		t.Errorf("caches %+v", l)
	}
	// Within a day the next scheduled check takes none.
	f.clock.Add(7 * time.Hour)
	f.step()
	if got := snapshots(); len(got) != 1 {
		t.Fatalf("a second snapshot within a day: %v", got)
	}
	// A check the user started enters Downloads; the next day's snapshot measures it.
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	f.clock.Add(20 * time.Hour)
	f.step()
	times = snapshots()
	if len(times) != 2 {
		t.Fatalf("snapshots %v, want a second one", times)
	}
	if l := location(times[1], "downloads"); l.Skipped || l.Bytes == 0 {
		t.Errorf("Downloads after the user's check: %+v", l)
	}
}

func TestOverviewTrashInfo(t *testing.T) {
	f, _, _ := newTrashFixture(t)
	f.cfg.TrashSize = func(_ context.Context) (trash.TrashUsage, error) { return trash.TrashUsage{Items: 7, Bytes: 9e9}, nil }
	o := f.start()
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	if _, err := o.Clean(t.Context(), rec(t, o.State(), "installers").req, CleanOrigin{Recommendation: "installers"}, nil); err != nil {
		t.Fatal(err)
	}
	info, err := o.TrashInfo(t.Context())
	if err != nil || info.Items != 7 || info.Bytes != 9e9 || info.Restorable != 1 {
		t.Errorf("trash info %+v %v", info, err)
	}
}

func TestOverviewSkippedPromptingFolders(t *testing.T) {
	f := newOvFixture(t, ovOptions{})
	access := folderaccess.Open(filepath.Join(f.data, "folder-access.json"), f.home, func() bool { return false })
	gate := NewGateRecorder(access)
	f.cfg.Access, f.cfg.Gate = access, gate
	downloads := filepath.Join(f.home, "Downloads")
	f.bgInst.mayRead, f.bgInst.reads = gate.Background, []string{downloads}
	f.fgInst.mayRead, f.fgInst.reads = gate.Foreground, []string{downloads}
	o := f.start()
	f.clock.Add(3 * time.Minute)
	f.step()
	if r := o.State().Result; r == nil || !slices.Equal(r.Skipped, []string{downloads}) {
		t.Fatalf("skipped %+v", r)
	}
	if file, ok, _ := f.cache.Load(); !ok || !slices.Equal(file.Skipped, []string{downloads}) {
		t.Errorf("cached skipped %+v", file.Skipped)
	}
	// A check the user started reads it; scheduled ones may then too.
	if err := o.CheckNow(); err != nil {
		t.Fatal(err)
	}
	f.step()
	if r := o.State().Result; r.Mode != CheckForeground || len(r.Skipped) != 0 {
		t.Errorf("foreground skipped %+v", r.Skipped)
	}
	if !access.Allow(downloads) {
		t.Fatal("the folder the foreground check entered is not allowed")
	}
	f.clock.Add(7 * time.Hour)
	f.step()
	if r := o.State().Result; r.Mode != CheckBackground || len(r.Skipped) != 0 {
		t.Errorf("background skipped after the foreground check %+v", r.Skipped)
	}
}
