package fulldisk

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/fsx"
)

// settingsURL opens System Settings (System Preferences before macOS 13) at Privacy & Security > Full Disk Access.
const settingsURL = "x-apple.systempreferences:com.apple.preference.security?Privacy_AllFiles"

// The guide's timing: how often it checks the permission and System Settings, how many checks System Settings may take to open (10 seconds), and how many checks in a row must miss it before the guide takes it as closed.
const (
	pollEvery      = time.Second
	settingsWait   = 10
	settingsMisses = 2
)

// Check reports whether the app holds Full Disk Access.
func Check() State {
	home, err := os.UserHomeDir()
	if err != nil {
		return Unknown
	}
	return probe(home)
}

// Guide opens System Settings at Full Disk Access, shows Spruce.app selected in Finder so the user can drag it into the list, and returns the state once the app holds the permission, ctx ends, or System Settings closes (or never opened within 10 seconds). An app run outside a bundle (a development build) only opens System Settings.
func Guide(ctx context.Context, runner execx.Runner) (State, error) {
	return guide(ctx, runner, Check, bundlePath(), pollEvery)
}

func guide(ctx context.Context, runner execx.Runner, check func() State, bundle string, every time.Duration) (State, error) {
	if st := check(); st == Granted {
		return st, nil
	}
	if _, err := runner.Run(ctx, execx.Command{Name: "/usr/bin/open", Args: []string{settingsURL}, Timeout: 15 * time.Second}); err != nil {
		return check(), fmt.Errorf("fulldisk: open System Settings: %w", err)
	}
	if bundle != "" {
		// Finder shows the app to drag; System Settings works without it, so a failure only matters to the log the caller keeps.
		if err := fsx.Reveal(ctx, runner, bundle); err != nil && ctx.Err() == nil {
			return check(), fmt.Errorf("fulldisk: reveal %s: %w", bundle, err)
		}
	}
	t := time.NewTicker(every)
	defer t.Stop()
	checks := 0
	seen, misses := false, 0
	for {
		select {
		case <-ctx.Done():
			return check(), nil
		case <-t.C:
		}
		checks++
		if st := check(); st == Granted {
			return st, nil
		}
		running, err := settingsRunning(ctx, runner)
		if err != nil {
			if ctx.Err() != nil {
				return check(), nil
			}
			return check(), err
		}
		switch {
		case running:
			seen, misses = true, 0
		case seen:
			if misses++; misses >= settingsMisses {
				return check(), nil
			}
		case checks >= settingsWait:
			return check(), nil
		}
	}
}

// settingsRunning reports whether System Settings (System Preferences) runs, through pgrep, which exits with 1 when nothing matches.
func settingsRunning(ctx context.Context, runner execx.Runner) (bool, error) {
	_, err := runner.Run(ctx, execx.Command{Name: "/usr/bin/pgrep", Args: []string{"-x", "System Settings|System Preferences"}, Timeout: 5 * time.Second})
	if err == nil {
		return true, nil
	}
	var exit *execx.ExitError
	if errors.As(err, &exit) && exit.ExitCode == 1 {
		return false, nil
	}
	return false, fmt.Errorf("fulldisk: look for System Settings: %w", err)
}

// bundlePath returns the .app bundle the running executable is in, or "" outside one.
func bundlePath() string {
	exe, err := os.Executable()
	if err != nil {
		return ""
	}
	if real, err := filepath.EvalSymlinks(exe); err == nil {
		exe = real
	}
	return bundleOf(exe)
}

// bundleOf returns the bundle of an executable at <bundle>.app/Contents/MacOS/<name>, or "".
func bundleOf(exe string) string {
	macos := filepath.Dir(exe)
	contents := filepath.Dir(macos)
	app := filepath.Dir(contents)
	if filepath.Base(macos) != "MacOS" || filepath.Base(contents) != "Contents" || !strings.HasSuffix(app, ".app") {
		return ""
	}
	return app
}

// probe tries to read files only apps with Full Disk Access may read, under the user's home folder home: TCC's own database of the user, then Safari's folder. A refusal of either means Denied; a read means Granted; anything else (neither exists) is Unknown.
func probe(home string) State {
	f, err := os.Open(filepath.Join(home, "Library", "Application Support", "com.apple.TCC", "TCC.db"))
	if err == nil {
		_ = f.Close()
		return Granted
	}
	if errors.Is(err, fs.ErrPermission) {
		return Denied
	}
	if _, err := os.ReadDir(filepath.Join(home, "Library", "Safari")); err == nil {
		return Granted
	} else if errors.Is(err, fs.ErrPermission) {
		return Denied
	}
	return Unknown
}
