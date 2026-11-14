package scancache_test

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/scancache"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

var t0 = time.Date(2026, 11, 16, 9, 0, 0, 0, time.UTC)

func open(t *testing.T) (*scancache.Store, string) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "data", "overview-cache.json")
	s, err := scancache.Open(path)
	if err != nil {
		t.Fatal(err)
	}
	s.SetNow(func() time.Time { return t0 })
	return s, path
}

func sample() scancache.File {
	cats := []cleanup.Category{{
		ID: "system-junk:app-caches", Title: "应用缓存", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent, Selected: true,
		Roots: []string{"/Users/x/Library/Caches"},
		Items: []cleanup.Item{{ID: "a", Path: "/Users/x/Library/Caches/a", Size: 300, Dir: true, Selected: true}, {ID: "b", Path: "/Users/x/Library/Caches/b", Size: 100}},
	}}
	return scancache.File{AppVersion: "2.0.0", SavedAt: t0.Add(-time.Hour), Mode: "background", SettingsKey: "k", Skipped: []string{"/Users/x/Desktop"}, Scan: cleanup.NewResult(cats, t0)}
}

func TestOpenDoesNoIO(t *testing.T) {
	dir := filepath.Join(t.TempDir(), "missing")
	if _, err := scancache.Open(filepath.Join(dir, "overview-cache.json")); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(dir); !os.IsNotExist(err) {
		t.Errorf("folder created: %v", err)
	}
	if _, err := scancache.Open(""); err == nil {
		t.Error("no error for an empty path")
	}
}

func TestRoundTrip(t *testing.T) {
	s, path := open(t)
	if _, ok, err := s.Load(); ok || err != nil {
		t.Fatalf("missing file: ok %v, %v", ok, err)
	}
	f := sample()
	if err := s.Save(f); err != nil {
		t.Fatal(err)
	}
	got, ok, err := s.Load()
	if !ok || err != nil {
		t.Fatalf("ok %v, %v", ok, err)
	}
	if got.Version != scancache.Version || got.AppVersion != "2.0.0" || got.Mode != "background" || got.SettingsKey != "k" || !got.SavedAt.Equal(f.SavedAt) ||
		len(got.Skipped) != 1 || got.Truncated != 0 || got.Scan.Size != 400 || len(got.Scan.Categories) != 1 || len(got.Scan.Categories[0].Items) != 2 {
		t.Errorf("loaded %+v", got)
	}
	// The deletion boundary never reaches the file.
	if got.Scan.Categories[0].Roots != nil {
		t.Errorf("roots %v", got.Scan.Categories[0].Roots)
	}
	b, _ := os.ReadFile(path)
	if strings.Contains(string(b), "Library/Caches\"]") || strings.Contains(string(b), "roots") {
		t.Errorf("roots in the file: %s", b)
	}
	if runtime.GOOS != "windows" {
		info, _ := os.Stat(path)
		if info.Mode().Perm() != 0o600 {
			t.Errorf("mode %v", info.Mode())
		}
	}
	if entries, _ := os.ReadDir(filepath.Dir(path)); len(entries) != 1 {
		t.Errorf("left temporary files: %v", entries)
	}
	if err := s.Clear(); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Load(); ok {
		t.Error("loaded after Clear")
	}
	if err := s.Clear(); err != nil {
		t.Errorf("second Clear: %v", err)
	}
}

func TestLoadRejects(t *testing.T) {
	for name, write := range map[string]func(path string) error{
		"corrupt": func(p string) error { return os.WriteFile(p, []byte("{"), 0o600) },
		"other version": func(p string) error {
			f := sample()
			f.Version = 2
			b, _ := json.Marshal(f)
			return os.WriteFile(p, b, 0o600)
		},
		"older than seven days": func(p string) error {
			f := sample()
			f.Version = scancache.Version
			f.SavedAt = t0.Add(-scancache.MaxAge - time.Minute)
			b, _ := json.Marshal(f)
			return os.WriteFile(p, b, 0o600)
		},
	} {
		t.Run(name, func(t *testing.T) {
			s, path := open(t)
			if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
				t.Fatal(err)
			}
			if err := write(path); err != nil {
				t.Fatal(err)
			}
			if _, ok, err := s.Load(); ok || err != nil {
				t.Errorf("ok %v, %v", ok, err)
			}
		})
	}
	// Six days is still shown.
	s, _ := open(t)
	f := sample()
	f.SavedAt = t0.Add(-6 * 24 * time.Hour)
	if err := s.Save(f); err != nil {
		t.Fatal(err)
	}
	if _, ok, _ := s.Load(); !ok {
		t.Error("six days old not loaded")
	}
}

func TestSaveTruncatesLargeResults(t *testing.T) {
	s, path := open(t)
	// Two categories of 30000 items with long paths make a file well over 8 MB.
	var cats []cleanup.Category
	for c := range 2 {
		cat := cleanup.Category{ID: fmt.Sprintf("system-junk:c%d", c), Title: "x"}
		for i := range 30000 {
			p := fmt.Sprintf("/Users/someone/Library/Caches/%s/item-%06d", strings.Repeat("deep/", 20), i)
			cat.Items = append(cat.Items, cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: int64(i + 1)})
		}
		cats = append(cats, cat)
	}
	f := sample()
	f.Scan = cleanup.NewResult(cats, t0)
	want := f.Scan.Size
	if err := s.Save(f); err != nil {
		t.Fatal(err)
	}
	info, err := os.Stat(path)
	if err != nil || info.Size() > scancache.MaxFileBytes {
		t.Fatalf("file %v, %v", info.Size(), err)
	}
	got, ok, err := s.Load()
	if !ok || err != nil {
		t.Fatalf("ok %v, %v", ok, err)
	}
	kept := 0
	for i, c := range got.Scan.Categories {
		if len(c.Items) > scancache.MaxItemsPerCat {
			t.Errorf("category %d keeps %d items", i, len(c.Items))
		}
		// The largest items stay, largest first, and the category size still counts every item.
		if c.Items[0].Size != 30000 || c.Size != f.Scan.Categories[i].Size {
			t.Errorf("category %d: first %d, size %d", i, c.Items[0].Size, c.Size)
		}
		kept += len(c.Items)
	}
	if got.Truncated != 60000-kept || got.Scan.Size != want || got.Scan.Items != 60000 {
		t.Errorf("truncated %d, kept %d, size %d, items %d", got.Truncated, kept, got.Scan.Size, got.Scan.Items)
	}
	// The caller's result is untouched.
	if len(f.Scan.Categories[0].Items) != 30000 {
		t.Errorf("caller's result changed: %d", len(f.Scan.Categories[0].Items))
	}
}

func TestKey(t *testing.T) {
	base := settings.AppSettings{Cleanup: settings.CleanupSettings{
		Exclusions: []string{"~/keep"}, DevProjectRoots: []string{"/Users/x/code"}, DevArtifactMinAgeDays: 30, InstallerRoots: []string{"/Users/x/Downloads"},
	}}
	k := scancache.Key(base)
	if len(k) != 16 || k != scancache.Key(base) {
		t.Fatalf("key %q", k)
	}
	for name, change := range map[string]func(s *settings.AppSettings){
		"exclusions": func(s *settings.AppSettings) { s.Cleanup.Exclusions = []string{"~/other"} },
		"project root": func(s *settings.AppSettings) {
			s.Cleanup.DevProjectRoots = append([]string{"/Users/x/src"}, s.Cleanup.DevProjectRoots...)
		},
		"min age":    func(s *settings.AppSettings) { s.Cleanup.DevArtifactMinAgeDays = 60 },
		"installers": func(s *settings.AppSettings) { s.Cleanup.InstallerRoots = nil },
	} {
		s := base
		s.Cleanup.Exclusions = append([]string(nil), base.Cleanup.Exclusions...)
		change(&s)
		if scancache.Key(s) == k {
			t.Errorf("%s: same key", name)
		}
	}
	// Settings a scan does not depend on leave the key alone.
	other := base
	other.Automation.AppCaches = true
	other.General.LowDiskGB = 50
	other.Cleanup.LargeFileMinMB = 999
	if scancache.Key(other) != k {
		t.Error("key changed with unrelated settings")
	}
	empty := settings.AppSettings{}
	emptyLists := settings.AppSettings{Cleanup: settings.CleanupSettings{Exclusions: []string{}, DevProjectRoots: []string{}, InstallerRoots: []string{}}}
	if scancache.Key(empty) != scancache.Key(emptyLists) {
		t.Error("nil and empty lists differ")
	}
}

func TestStale(t *testing.T) {
	f := scancache.File{SettingsKey: "k", AppVersion: "2.0.0", SavedAt: t0}
	for name, tc := range map[string]struct {
		key, version string
		now          time.Time
		want         bool
	}{
		"fresh":               {key: "k", version: "2.0.0", now: t0.Add(time.Hour)},
		"exactly a day":       {key: "k", version: "2.0.0", now: t0.Add(24 * time.Hour)},
		"over a day":          {key: "k", version: "2.0.0", now: t0.Add(24*time.Hour + time.Second), want: true},
		"other settings":      {key: "j", version: "2.0.0", now: t0, want: true},
		"other build":         {key: "k", version: "2.0.1", now: t0, want: true},
		"saved in the future": {key: "k", version: "2.0.0", now: t0.Add(-time.Hour), want: true},
	} {
		if got := scancache.Stale(f, tc.key, tc.version, tc.now); got != tc.want {
			t.Errorf("%s: %v", name, got)
		}
	}
}
