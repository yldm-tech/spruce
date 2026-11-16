package fulldisk

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"sync/atomic"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/execx/execxtest"
)

func TestProbe(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads unreadable files")
	}
	tcc := filepath.Join("Library", "Application Support", "com.apple.TCC", "TCC.db")
	safari := filepath.Join("Library", "Safari")
	tests := []struct {
		name  string
		setup func(t *testing.T, home string)
		want  State
	}{
		{"nothing to read", func(*testing.T, string) {}, Unknown},
		{"TCC database readable", func(t *testing.T, home string) { writeFile(t, filepath.Join(home, tcc), 0o600) }, Granted},
		{"TCC database refused", func(t *testing.T, home string) { writeFile(t, filepath.Join(home, tcc), 0o000) }, Denied},
		{"Safari readable", func(t *testing.T, home string) { mkdir(t, filepath.Join(home, safari), 0o700) }, Granted},
		{"Safari refused", func(t *testing.T, home string) { mkdir(t, filepath.Join(home, safari), 0o000) }, Denied},
		{"TCC refusal wins over Safari", func(t *testing.T, home string) {
			writeFile(t, filepath.Join(home, tcc), 0o000)
			mkdir(t, filepath.Join(home, safari), 0o700)
		}, Denied},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			home := t.TempDir()
			tt.setup(t, home)
			if got := probe(home); got != tt.want {
				t.Errorf("probe = %v, want %v", got, tt.want)
			}
		})
	}
}

func writeFile(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	mkdir(t, filepath.Dir(path), 0o700)
	if err := os.WriteFile(path, []byte("x"), mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o600) })
}

func mkdir(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.MkdirAll(path, 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(path, mode); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(path, 0o700) })
}

func TestBundleOf(t *testing.T) {
	for exe, want := range map[string]string{
		"/Applications/Spruce.app/Contents/MacOS/Spruce": "/Applications/Spruce.app",
		"/Users/me/go/bin/spruce":                        "",
		"/tmp/Spruce/Contents/MacOS/Spruce":              "",
	} {
		if got := bundleOf(exe); got != want {
			t.Errorf("bundleOf(%q) = %q, want %q", exe, got, want)
		}
	}
}

// settingsUntil answers pgrep: System Settings runs for the first n checks, then is gone.
func settingsUntil(f *execxtest.Fake, n int32) *atomic.Int32 {
	var calls atomic.Int32
	f.On("/usr/bin/pgrep", "-x", "System Settings|System Preferences").Do(func(context.Context, execx.Command) (execx.Output, error) {
		if calls.Add(1) <= n {
			return execx.Output{}, nil
		}
		return execx.Output{ExitCode: 1}, &execx.ExitError{Command: "pgrep", ExitCode: 1}
	})
	return &calls
}

func TestGuideReturnsOnceGranted(t *testing.T) {
	f := execxtest.New()
	f.On("/usr/bin/open", settingsURL).Return("", 0)
	settingsUntil(f, 100)
	var checks atomic.Int32
	check := func() State {
		if checks.Add(1) >= 4 {
			return Granted
		}
		return Denied
	}
	st, err := guide(t.Context(), f, check, "", time.Millisecond)
	if err != nil || st != Granted {
		t.Fatalf("guide = %v, %v; want granted", st, err)
	}
}

func TestGuideReturnsWhenSystemSettingsCloses(t *testing.T) {
	f := execxtest.New()
	f.On("/usr/bin/open", settingsURL).Return("", 0)
	calls := settingsUntil(f, 3)
	st, err := guide(t.Context(), f, func() State { return Denied }, "", time.Millisecond)
	if err != nil || st != Denied {
		t.Fatalf("guide = %v, %v; want denied", st, err)
	}
	if got := calls.Load(); got != 3+settingsMisses {
		t.Errorf("checked System Settings %d times, want %d", got, 3+settingsMisses)
	}
}

func TestGuideRevealsTheBundle(t *testing.T) {
	app := filepath.Join(t.TempDir(), "Spruce.app")
	if err := os.Mkdir(app, 0o700); err != nil {
		t.Fatal(err)
	}
	f := execxtest.New()
	f.On("/usr/bin/open", settingsURL).Return("", 0)
	f.On("/usr/bin/open", "-R", app).Return("", 0)
	settingsUntil(f, 0)
	ctx, cancel := context.WithTimeout(t.Context(), 5*time.Second)
	defer cancel()
	if _, err := guide(ctx, f, func() State { return Denied }, app, time.Millisecond); err != nil {
		t.Fatal(err)
	}
	var revealed bool
	for _, c := range f.Calls() {
		if c.Name == "/usr/bin/open" && len(c.Args) == 2 && c.Args[0] == "-R" && c.Args[1] == app {
			revealed = true
		}
	}
	if !revealed {
		t.Errorf("Spruce.app was not shown in Finder; calls: %v", f.Calls())
	}
}

func TestGuideFailsWhenSystemSettingsDoesNotOpen(t *testing.T) {
	f := execxtest.New()
	f.On("/usr/bin/open", settingsURL).Fail(errors.New("no handler"))
	if _, err := guide(t.Context(), f, func() State { return Denied }, "", time.Millisecond); err == nil {
		t.Fatal("guide succeeded although System Settings did not open")
	}
}

func TestGuideStopsWithItsContext(t *testing.T) {
	f := execxtest.New()
	f.On("/usr/bin/open", settingsURL).Return("", 0)
	settingsUntil(f, 1<<30)
	ctx, cancel := context.WithCancel(t.Context())
	go func() { time.Sleep(20 * time.Millisecond); cancel() }()
	st, err := guide(ctx, f, func() State { return Denied }, "", time.Millisecond)
	if err != nil || st != Denied {
		t.Fatalf("guide = %v, %v; want denied, nil", st, err)
	}
}
