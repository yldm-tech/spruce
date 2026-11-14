package space

import (
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

// mkParent creates the folder holding path, which fsx resolves; the path itself need not exist.
func mkParent(t *testing.T, path string) {
	t.Helper()
	if filepath.IsAbs(path) {
		if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

func TestGuard(t *testing.T) {
	dir := tempRoot(t)
	home := filepath.Join(dir, "home")
	target := filepath.Join(dir, "scanned")
	system := filepath.Join(dir, "scanned", "sys")
	g := Guard{Home: home, Target: target, Exclusions: []string{"keepme", filepath.Join(home, "Pinned")}, System: []string{system}}
	cases := []struct {
		path string
		want error
	}{
		{filepath.Join(home, "Projects", "big.iso"), nil},
		{filepath.Join(home, "Projects", "a", "b"), nil},
		{filepath.Join(target, "x", "y.bin"), nil},
		{home, ErrOutside},
		{target, ErrOutside},
		{filepath.Join(dir, "elsewhere", "file"), ErrOutside},
		{"relative/file", ErrOutside},
		{filepath.Join(home, "Projects"), ErrTopLevel},
		// Directly in the scanned folder is fine: its parent is an allowed root fsx can judge.
		{filepath.Join(target, "top.bin"), nil},
		{filepath.Join(system, "lib", "x"), ErrSystem},
		{filepath.Join(home, "Projects", "keepme", "x"), ErrExcluded},
		{filepath.Join(home, "Pinned", "x"), ErrExcluded},
	}
	for _, c := range cases {
		mkParent(t, c.path)
		err := g.Check(c.path)
		if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("Check(%q) = %v, want %v", c.path, err, c.want)
		}
	}
}

func TestGuardWholeStartupDisk(t *testing.T) {
	dir := tempRoot(t)
	home := filepath.Join(dir, "Users", "me")
	apps := filepath.Join(dir, "Applications")
	g := Guard{Home: home, Target: dir, StartupRoot: dir, Apps: apps}
	cases := []struct {
		path string
		want error
	}{
		// The whole disk was scanned, but only the home folder and the applications are the user's to move.
		{filepath.Join(home, "Movies", "film.mov"), nil},
		{filepath.Join(apps, "Big.app"), nil},
		{filepath.Join(dir, "Users", "Shared", "x"), ErrOutside},
		{filepath.Join(dir, "Library", "Caches", "x"), ErrOutside},
	}
	for _, c := range cases {
		mkParent(t, c.path)
		err := g.Check(c.path)
		if c.want == nil && err != nil || c.want != nil && !errors.Is(err, c.want) {
			t.Errorf("Check(%q) = %v, want %v", c.path, err, c.want)
		}
	}
	if runtime.GOOS == "windows" {
		g.Apps = ""
		if err := g.Check(filepath.Join(apps, "Big.app")); !errors.Is(err, ErrOutside) {
			t.Errorf("Windows has no applications folder to move from: %v", err)
		}
	}
}

func TestGuardRefusesVolumeRootsAndSystemOnThisPlatform(t *testing.T) {
	home := filepath.Join(tempRoot(t), "home")
	switch runtime.GOOS {
	case "darwin":
		g := NewGuard(home, "/", nil)
		for path, want := range map[string]error{
			"/Applications":                ErrOutside,
			"/Library/Caches/com.example":  ErrOutside,
			"/System/Library/Fonts/x.ttf":  ErrOutside,
			"/Applications/Example.app":    nil,
			"/Applications/Utilities/x.sh": nil,
		} {
			err := g.Check(path)
			if want == nil && err != nil || want != nil && !errors.Is(err, want) {
				t.Errorf("Check(%q) = %v, want %v", path, err, want)
			}
		}
		g = NewGuard(home, "/Library", nil)
		if err := g.Check("/Library/Caches/com.example"); !errors.Is(err, ErrSystem) {
			t.Errorf("a scan of /Library: %v", err)
		}
		g = NewGuard(home, "/Volumes/External", nil)
		if err := g.Check("/Volumes/External/top"); !errors.Is(err, ErrTopLevel) {
			t.Errorf("an item at the root of a volume: %v", err)
		}
	case "windows":
		g := NewGuard(home, `C:\`, nil)
		if err := g.Check(`C:\Windows\Temp\x`); !errors.Is(err, ErrOutside) {
			t.Errorf("C:\\Windows: %v", err)
		}
		g = NewGuard(home, `C:\Program Files`, nil)
		if err := g.Check(`C:\Program Files\Example\app.exe`); !errors.Is(err, ErrSystem) {
			t.Errorf("Program Files: %v", err)
		}
	}
}
