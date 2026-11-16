package settings

import (
	"encoding/json"
	"os"
	"path/filepath"
	"runtime"
	"testing"
	"time"
)

func newStore(t *testing.T) (*Store, string, string) {
	t.Helper()
	home := t.TempDir()
	path := filepath.Join(t.TempDir(), "nested", "settings.json")
	s, err := Open(path, home, func() AppSettings { return Defaults(home) })
	if err != nil {
		t.Fatal(err)
	}
	return s, path, home
}

func TestDefaults(t *testing.T) {
	home := t.TempDir()
	if err := os.MkdirAll(filepath.Join(home, "Projects"), 0o755); err != nil {
		t.Fatal(err)
	}
	d := Defaults(home)
	if d.Health != (HealthSettings{CPUThreshold: 40, SustainMinutes: 10, Enabled: true}) ||
		d.Cleanup.DevArtifactMinAgeDays != 30 || d.Cleanup.LargeFileMinMB != 500 ||
		d.History.RetentionDays != 180 || !d.General.ConfirmBeforeClean || d.General.LaunchAtLogin {
		t.Errorf("defaults %+v", d)
	}
	// Only the developer folders that exist.
	if len(d.Cleanup.DevProjectRoots) != 1 || d.Cleanup.DevProjectRoots[0] != filepath.Join(home, "Projects") {
		t.Errorf("dev roots %v", d.Cleanup.DevProjectRoots)
	}
	if len(d.Cleanup.InstallerRoots) != 2 || d.Cleanup.Exclusions == nil {
		t.Errorf("cleanup %+v", d.Cleanup)
	}
}

func TestNormalizeClampsAndCleans(t *testing.T) {
	home := filepath.FromSlash("/home/me")
	abs := filepath.FromSlash("/data/x")
	if runtime.GOOS == "windows" {
		home, abs = `C:\Users\me`, `D:\data\x`
	}
	s := Normalize(AppSettings{
		Health: HealthSettings{CPUThreshold: 1, SustainMinutes: 1000},
		Cleanup: CleanupSettings{
			Exclusions:      []string{"~/Keep", abs + string(filepath.Separator) + "..", "relative/dir", "node_modules", "*.photoslibrary", "", "NODE_MODULES"},
			DevProjectRoots: []string{abs, abs, "relative"},
			LargeFileMinMB:  -5,
		},
		History: HistorySettings{RetentionDays: 99999},
	}, home)
	if s.Version != SchemaVersion || s.Health.CPUThreshold != MinCPUThreshold || s.Health.SustainMinutes != MaxSustainMinutes ||
		s.Cleanup.LargeFileMinMB != MinLargeFileMB || s.History.RetentionDays != MaxRetentionDays {
		t.Errorf("clamped %+v", s)
	}
	wantEx := []string{filepath.Join(home, "Keep"), filepath.Dir(abs), "node_modules", "*.photoslibrary"}
	if len(s.Cleanup.Exclusions) != len(wantEx) {
		t.Fatalf("exclusions %q, want %q", s.Cleanup.Exclusions, wantEx)
	}
	for i := range wantEx {
		if s.Cleanup.Exclusions[i] != wantEx[i] {
			t.Errorf("exclusions %q, want %q", s.Cleanup.Exclusions, wantEx)
		}
	}
	if len(s.Cleanup.DevProjectRoots) != 1 || s.Cleanup.InstallerRoots == nil {
		t.Errorf("roots %+v", s.Cleanup)
	}
}

func TestStoreDefaultsThenPersists(t *testing.T) {
	s, path, home := newStore(t)
	if _, err := os.Stat(filepath.Dir(path)); !os.IsNotExist(err) {
		t.Fatalf("Open touched the disk: %v", err)
	}
	if got := s.Get(); got.Health.CPUThreshold != 40 {
		t.Fatalf("Get = %+v", got)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("Get wrote the file: %v", err)
	}
	got, err := s.Update(func(a *AppSettings) {
		a.Health.CPUThreshold = 75
		a.Cleanup.Exclusions = []string{"~/Movies"}
	})
	if err != nil {
		t.Fatal(err)
	}
	if got.Health.CPUThreshold != 75 || got.Cleanup.Exclusions[0] != filepath.Join(home, "Movies") {
		t.Errorf("Update returned %+v", got)
	}
	if runtime.GOOS != "windows" {
		info, err := os.Stat(path)
		if err != nil {
			t.Fatal(err)
		}
		if info.Mode().Perm() != fileMode {
			t.Errorf("mode %v", info.Mode().Perm())
		}
	}
	entries, _ := os.ReadDir(filepath.Dir(path))
	if len(entries) != 1 {
		t.Errorf("leftover files %v", entries)
	}

	// A new Store reads it back.
	s2, err := Open(path, home, func() AppSettings { return Defaults(home) })
	if err != nil {
		t.Fatal(err)
	}
	if g := s2.Get(); g.Health.CPUThreshold != 75 || len(g.Cleanup.Exclusions) != 1 {
		t.Errorf("reloaded %+v", g)
	}
	r, err := s2.Reset()
	if err != nil || r.Health.CPUThreshold != 40 || len(r.Cleanup.Exclusions) != 0 {
		t.Errorf("Reset = %+v, %v", r, err)
	}
}

// Resetting the settings leaves the history as it is: a longer 保留天数 stays, since the default would delete the older records at once; a shorter one goes back to the default, which deletes nothing.
func TestResetKeepsALongerRetention(t *testing.T) {
	s, _, _ := newStore(t)
	for days, want := range map[int]int{365: 365, 30: 180} {
		if _, err := s.Update(func(a *AppSettings) { a.History.RetentionDays = days; a.Health.CPUThreshold = 75 }); err != nil {
			t.Fatal(err)
		}
		r, err := s.Reset()
		if err != nil || r.History.RetentionDays != want || r.Health.CPUThreshold != 40 {
			t.Errorf("Reset from %d days = %+v, %v", days, r.History, err)
		}
	}
}

func TestStoreReadsPartialAndCorruptFiles(t *testing.T) {
	s, path, home := newStore(t)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	// A file from an older or newer version: missing fields keep their defaults, unknown ones are ignored, out-of-range ones are clamped.
	if err := os.WriteFile(path, []byte(`{"version":9,"health":{"cpuThreshold":5000},"future":{"x":1}}`), 0o600); err != nil {
		t.Fatal(err)
	}
	g := s.Get()
	if g.Health.CPUThreshold != MaxCPUThreshold || g.Health.SustainMinutes != 10 || !g.Health.Enabled || g.History.RetentionDays != 180 || g.Version != SchemaVersion {
		t.Errorf("partial file read as %+v", g)
	}

	if err := os.WriteFile(path, []byte(`{not json`), 0o600); err != nil {
		t.Fatal(err)
	}
	s2, _ := Open(path, home, func() AppSettings { return Defaults(home) })
	if g := s2.Get(); g.Health.CPUThreshold != 40 {
		t.Errorf("corrupt file read as %+v", g)
	}
	if _, err := s2.Update(func(a *AppSettings) { a.History.RetentionDays = 30 }); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	var back AppSettings
	if err := json.Unmarshal(data, &back); err != nil || back.History.RetentionDays != 30 {
		t.Errorf("rewritten file %s: %v", data, err)
	}
}

func TestSubscribe(t *testing.T) {
	s, _, _ := newStore(t)
	ch, stop := s.Subscribe()
	first := <-ch
	if first.Health.CPUThreshold != 40 {
		t.Fatalf("first %+v", first)
	}
	for _, v := range []int{50, 60} {
		if _, err := s.Update(func(a *AppSettings) { a.Health.CPUThreshold = v }); err != nil {
			t.Fatal(err)
		}
	}
	select {
	case got := <-ch:
		if got.Health.CPUThreshold != 60 {
			t.Errorf("latest %+v", got)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("no change delivered")
	}
	// An unchanged value notifies nobody.
	if _, err := s.Update(func(*AppSettings) {}); err != nil {
		t.Fatal(err)
	}
	select {
	case got := <-ch:
		t.Errorf("unexpected %+v", got)
	default:
	}
	stop()
	stop()
	if _, ok := <-ch; ok {
		t.Error("channel open after unsubscribe")
	}
}
