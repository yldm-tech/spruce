package uninstall

import (
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
)

func TestWindowsLeftovers(t *testing.T) {
	root := t.TempDir()
	appData := filepath.Join(root, "Roaming")
	local := filepath.Join(root, "Local")
	programs := filepath.Join(local, "Programs")
	want := []string{
		filepath.Join(appData, "Foo Software", "Foo Editor"),
		filepath.Join(appData, "Foo Editor"),
		filepath.Join(local, "Foo Software", "Foo Editor"),
		filepath.Join(local, "Foo Editor"),
		filepath.Join(programs, "foo-editor"),
	}
	nearMiss := []string{
		filepath.Join(appData, "Foo Software"),
		filepath.Join(appData, "Foo Editor 2"),
		filepath.Join(appData, "Foo"),
		filepath.Join(local, "Foo Software", "Foo Editor Beta"),
		filepath.Join(programs, "other-app"),
	}
	for _, p := range append(slices.Clone(want), nearMiss...) {
		mkdir(t, p)
	}
	// A file named like the program is not a folder of it.
	if err := os.WriteFile(filepath.Join(local, "Foo Editor.txt"), nil, 0o644); err != nil {
		t.Fatal(err)
	}
	e := Entry{DisplayName: "Foo Editor", Publisher: "Foo Software", InstallLocation: filepath.Join(programs, "foo-editor") + string(filepath.Separator)}
	got := WindowsLeftovers(appData, local, e)
	var paths []string
	for _, c := range got {
		paths = append(paths, c.Path)
		if !c.Selected || !exists(c.Path) {
			t.Errorf("%+v", c)
		}
		if filepath.Dir(c.Path) != c.Root && filepath.Dir(filepath.Dir(c.Path)) != c.Root {
			t.Errorf("%s is not just below its root %s", c.Path, c.Root)
		}
	}
	if !slices.Equal(paths, want) {
		t.Errorf("leftovers =\n%v\nwant\n%v", paths, want)
	}
	kinds := []LeftoverKind{KindAppData, KindAppData, KindLocalAppData, KindLocalAppData, KindPrograms}
	for i, c := range got {
		if i < len(kinds) && c.Kind != kinds[i] {
			t.Errorf("%s kind %q", c.Path, c.Kind)
		}
	}

	// The install folder counts only as a direct child of Programs.
	deep := Entry{DisplayName: "Zed Tool", InstallLocation: filepath.Join(programs, "vendor", "zed")}
	mkdir(t, deep.InstallLocation)
	if got := WindowsLeftovers(appData, local, deep); len(got) != 0 {
		t.Errorf("deep install folder: %+v", got)
	}

	// Names that are not one specific folder match nothing; relative roots are ignored.
	for _, name := range []string{"Microsoft", "Go", "Foo\\Editor", "..", "Packages", "Foo: Editor"} {
		// Windows refuses a colon in a folder name, so there that folder cannot exist; the name must match nothing all the same.
		if runtime.GOOS != "windows" || !strings.Contains(name, ":") {
			mkdir(t, filepath.Join(appData, "Bar", name))
		}
		if got := WindowsLeftovers(appData, local, Entry{DisplayName: name, Publisher: "Bar"}); len(got) != 0 {
			t.Errorf("name %q: %+v", name, got)
		}
	}
	if got := WindowsLeftovers("Roaming", "Local", e); len(got) != 0 {
		t.Errorf("relative roots: %+v", got)
	}
	// A shared publisher folder is not used, but the program's own folder still is.
	mkdir(t, filepath.Join(appData, "Microsoft", "Foo Editor"))
	ms := Entry{DisplayName: "Foo Editor", Publisher: "Microsoft"}
	got = WindowsLeftovers(appData, local, ms)
	if len(got) != 2 || got[0].Path != filepath.Join(appData, "Foo Editor") {
		t.Errorf("shared publisher: %+v", got)
	}
}

func TestWindowsLeftoversSkipSymlinks(t *testing.T) {
	root := t.TempDir()
	appData := filepath.Join(root, "Roaming")
	target := filepath.Join(root, "elsewhere")
	mkdir(t, target)
	mkdir(t, appData)
	if err := os.Symlink(target, filepath.Join(appData, "Foo Editor")); err != nil {
		t.Skip("symlinks unavailable:", err)
	}
	if got := WindowsLeftovers(appData, "", Entry{DisplayName: "Foo Editor"}); len(got) != 0 {
		t.Errorf("a symlink was offered: %+v", got)
	}
}
