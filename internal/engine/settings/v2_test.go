package settings

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultsV2(t *testing.T) {
	d := Defaults(t.TempDir())
	want := GeneralSettings{ConfirmBeforeClean: true, KeepInTray: true, LowDiskGB: 20}
	if d.Version != 2 || d.General != want || d.Background != (BackgroundSettings{Enabled: true}) || d.Automation != (AutomationSettings{WeeklySummary: true}) {
		t.Errorf("defaults %+v", d)
	}
}

// TestVersion1FileMigrates: a file written by version 1 keeps what it says and gets the defaults of every field version 2 added.
func TestVersion1FileMigrates(t *testing.T) {
	s, path, _ := newStore(t)
	v1 := `{"version":1,"health":{"cpuThreshold":80,"sustainMinutes":5,"enabled":false},"cleanup":{"exclusions":[],"devProjectRoots":[],"devArtifactMinAgeDays":10,"installerRoots":[],"largeFileMinMB":100},"history":{"retentionDays":30},"general":{"launchAtLogin":true,"confirmBeforeClean":false}}`
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(v1), 0o600); err != nil {
		t.Fatal(err)
	}
	got := s.Get()
	if got.Version != SchemaVersion || got.Health.CPUThreshold != 80 || got.Cleanup.DevArtifactMinAgeDays != 10 {
		t.Errorf("kept %+v", got)
	}
	want := GeneralSettings{LaunchAtLogin: true, ConfirmBeforeClean: false, KeepInTray: true, LowDiskGB: 20}
	if got.General != want || got.Background != (BackgroundSettings{Enabled: true}) || got.Automation != (AutomationSettings{WeeklySummary: true}) {
		t.Errorf("new fields %+v %+v %+v", got.General, got.Background, got.Automation)
	}
}

func TestNormalizeClampsLowDiskGB(t *testing.T) {
	for in, want := range map[int]int{0: MinLowDiskGB, -3: MinLowDiskGB, 20: 20, 501: MaxLowDiskGB, 100000: MaxLowDiskGB} {
		if got := Normalize(AppSettings{General: GeneralSettings{LowDiskGB: in}}, "").General.LowDiskGB; got != want {
			t.Errorf("LowDiskGB %d → %d, want %d", in, got, want)
		}
	}
}

// TestEqualSeesEveryNewSwitch: a change to any rule, the summary, scheduled checks or the alert is a change (the Store writes and notifies).
func TestEqualSeesEveryNewSwitch(t *testing.T) {
	base := Defaults(t.TempDir())
	changes := []func(*AppSettings){
		func(s *AppSettings) { s.Automation.AppCaches = true },
		func(s *AppSettings) { s.Automation.Installers = true },
		func(s *AppSettings) { s.Automation.BuildOutput = true },
		func(s *AppSettings) { s.Automation.IncompleteDownloads = true },
		func(s *AppSettings) { s.Automation.WeeklySummary = false },
		func(s *AppSettings) { s.Background.Enabled = false },
		func(s *AppSettings) { s.General.KeepInTray = false },
		func(s *AppSettings) { s.General.LowDiskAlert = true },
		func(s *AppSettings) { s.General.LowDiskGB = 50 },
	}
	for i, change := range changes {
		next := clone(base)
		change(&next)
		if equal(base, next) {
			t.Errorf("change %d not seen", i)
		}
	}
	if !equal(base, clone(base)) {
		t.Error("a clone differs")
	}
}
