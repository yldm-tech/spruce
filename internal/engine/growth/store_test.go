package growth

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"
)

func openStore(t *testing.T) (*Store, string) {
	t.Helper()
	dir := filepath.Join(t.TempDir(), "growth")
	s, err := Open(dir)
	if err != nil {
		t.Fatal(err)
	}
	return s, dir
}

func TestStoreRoundTrip(t *testing.T) {
	s, dir := openStore(t)
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("Open made the folder: %v", err)
	}
	if times, err := s.List(); err != nil || len(times) != 0 {
		t.Fatalf("List before any Save: %v %v", times, err)
	}
	at := time.Date(2026, 11, 14, 3, 4, 5, 600, time.FixedZone("JST", 9*3600))
	in := Snapshot{At: at, Free: 40 * gb, Total: 500 * gb, Locations: []Location{
		{ID: "library-caches", Path: "/u/Library/Caches", Bytes: 5 * gb, Children: []Entry{{"com.apple.Safari", 2 * gb}}, Rest: 3 * gb},
		{ID: "desktop", Path: "/u/Desktop", Skipped: true},
	}}
	if err := s.Save(in); err != nil {
		t.Fatal(err)
	}
	name := filepath.Join(dir, "20261113T180405Z.json")
	info, err := os.Stat(name)
	if err != nil {
		t.Fatal(err)
	}
	if runtime.GOOS != "windows" {
		if info.Mode().Perm() != 0o600 {
			t.Errorf("file mode %v", info.Mode())
		}
		if d, _ := os.Stat(dir); d.Mode().Perm() != 0o700 {
			t.Errorf("folder mode %v", d.Mode())
		}
	}
	times, err := s.List()
	if err != nil || len(times) != 1 || !times[0].Equal(at.Truncate(time.Second)) {
		t.Fatalf("List %v %v", times, err)
	}
	out, err := s.Load(times[0])
	if err != nil {
		t.Fatal(err)
	}
	if out.Version != Version || !out.At.Equal(at) || out.Free != in.Free || len(out.Locations) != 2 || out.Locations[0].Children[0] != in.Locations[0].Children[0] ||
		!out.Locations[1].Skipped || out.Locations[0].Rest != 3*gb {
		t.Errorf("loaded %+v", out)
	}
	// Leftovers and foreign files are not snapshots.
	for _, n := range []string{".snapshot-1.tmp", "notes.txt", "garbage.json"} {
		if err := os.WriteFile(filepath.Join(dir, n), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if times, _ := s.List(); len(times) != 1 {
		t.Errorf("List %v", times)
	}
	if err := s.Save(Snapshot{}); err == nil {
		t.Error("saved a snapshot without a time")
	}
}

func TestStoreLoadRefusesCorruptAndOtherVersions(t *testing.T) {
	s, dir := openStore(t)
	at := time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC)
	if err := s.Save(Snapshot{At: at}); err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "20261101T000000Z.json")
	if err := os.WriteFile(path, []byte(`{"version":`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(at); err == nil {
		t.Error("a corrupt snapshot loaded")
	}
	if err := os.WriteFile(path, []byte(`{"version":2,"at":"2026-11-01T00:00:00Z"}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Load(at); err == nil || !strings.Contains(err.Error(), "version 2") {
		t.Errorf("another version: %v", err)
	}
}

func TestStorePrune(t *testing.T) {
	s, dir := openStore(t)
	now := time.Date(2026, 11, 15, 12, 0, 0, 0, time.UTC)
	big := Snapshot{Locations: []Location{{ID: "x", Path: "/x", Children: make([]Entry, 200)}}}
	var saved []time.Time
	for d := range 100 {
		at := now.AddDate(0, 0, -d).Add(-time.Hour)
		big.At = at
		if err := s.Save(big); err != nil {
			t.Fatal(err)
		}
		saved = append(saved, at)
	}
	removed, err := s.Prune(now, DefaultRetention())
	if err != nil {
		t.Fatal(err)
	}
	times, _ := s.List()
	if removed+len(times) != 100 {
		t.Errorf("removed %d, left %d", removed, len(times))
	}
	want := Keep(saved, now, DefaultRetention())
	if !slices.EqualFunc(times, want, func(a, b time.Time) bool { return a.Equal(b) }) {
		t.Errorf("left %v, want %v", times, want)
	}

	// Over MaxBytes the oldest go first and the newest stays.
	info, _ := os.Stat(filepath.Join(dir, times[0].UTC().Format(fileLayout)+".json"))
	if _, err := s.Prune(now, Retention{Daily: 14, Weekly: 12, MaxFiles: 40, MaxBytes: 3 * info.Size()}); err != nil {
		t.Fatal(err)
	}
	left, _ := s.List()
	if len(left) != 3 || !left[2].Equal(times[len(times)-1]) {
		t.Errorf("left %v", left)
	}
	if _, err := s.Prune(now, Retention{Daily: 14, Weekly: 12, MaxFiles: 40, MaxBytes: 1}); err != nil {
		t.Fatal(err)
	}
	if left, _ := s.List(); len(left) != 1 || !left[0].Equal(times[len(times)-1]) {
		t.Errorf("the newest went: %v", left)
	}
}

func TestStoreClear(t *testing.T) {
	s, dir := openStore(t)
	if err := s.Clear(); err != nil {
		t.Errorf("Clear without a folder: %v", err)
	}
	for i := range 3 {
		if err := s.Save(Snapshot{At: t0.Add(time.Duration(i) * time.Hour)}); err != nil {
			t.Fatal(err)
		}
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if times, _ := s.List(); len(times) != 0 {
		t.Errorf("left %v", times)
	}
	if _, err := os.Stat(dir); err != nil {
		t.Errorf("the folder went: %v", err)
	}
}

func TestStoreChanges(t *testing.T) {
	s, _ := openStore(t)
	now := t0.Add(10 * 24 * time.Hour)
	if _, err := s.Changes(now, 7*24*time.Hour, 3); !errors.Is(err, ErrNoHistory) {
		t.Errorf("no snapshots: %v", err)
	}
	loc := func(bytes int64) Location { return Location{ID: "downloads", Path: "/u/Downloads", Bytes: bytes} }
	if err := s.Save(snap(now.Add(-time.Hour), loc(9*gb))); err != nil {
		t.Fatal(err)
	}
	if _, err := s.Changes(now, 7*24*time.Hour, 3); !errors.Is(err, ErrNoHistory) {
		t.Errorf("one snapshot: %v", err)
	}
	for _, s0 := range []Snapshot{snap(now.Add(-9*24*time.Hour), loc(2*gb)), snap(now.Add(-5*24*time.Hour), loc(4*gb)), snap(now.Add(-20*time.Hour), loc(6*gb))} {
		if err := s.Save(s0); err != nil {
			t.Fatal(err)
		}
	}
	cases := []struct {
		window time.Duration
		before int64
	}{{24 * time.Hour, 4 * gb}, {7 * 24 * time.Hour, 2 * gb}, {30 * 24 * time.Hour, 2 * gb}}
	for _, c := range cases {
		got, err := s.Changes(now, c.window, 3)
		if err != nil {
			t.Fatal(err)
		}
		if len(got) != 1 || got[0].Before != c.before || got[0].After != 9*gb {
			t.Errorf("window %v: %+v", c.window, got)
		}
	}
	// The 24-hour window compares with the newest snapshot at least a day old, not the one of 20 hours ago.
	got, _ := s.Changes(now, 24*time.Hour, 3)
	if !got[0].Since.Equal(now.Add(-5 * 24 * time.Hour)) {
		t.Errorf("since %v", got[0].Since)
	}

	// A snapshot that does not load is passed over: a damaged newest gives way to the one before it, a damaged baseline to the next one the window allows.
	damage := func(at time.Time) {
		t.Helper()
		if err := os.WriteFile(s.path(at), []byte("{not json"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	damage(now.Add(-5 * 24 * time.Hour))
	got, err := s.Changes(now, 24*time.Hour, 3)
	if err != nil || len(got) != 1 || got[0].Before != 2*gb || got[0].After != 9*gb {
		t.Errorf("damaged baseline: %+v, %v", got, err)
	}
	damage(now.Add(-time.Hour))
	got, err = s.Changes(now, 7*24*time.Hour, 3)
	if err != nil || len(got) != 1 || got[0].Before != 2*gb || got[0].After != 6*gb {
		t.Errorf("damaged newest: %+v, %v", got, err)
	}
	damage(now.Add(-9 * 24 * time.Hour))
	if _, err := s.Changes(now, 7*24*time.Hour, 3); !errors.Is(err, ErrNoHistory) {
		t.Errorf("one snapshot that loads: %v", err)
	}
}
