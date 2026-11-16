package devjunk

import (
	"os"
	"path/filepath"
	"slices"
	"testing"
)

// mkdirs creates each path (relative to base) as a folder.
func mkdirs(t *testing.T, base string, rels ...string) {
	t.Helper()
	for _, r := range rels {
		if err := os.MkdirAll(filepath.Join(base, r), 0o755); err != nil {
			t.Fatal(err)
		}
	}
}

// writeFile creates base/rel with n bytes, making its folders.
func writeFile(t *testing.T, base, rel string, n int) string {
	t.Helper()
	p := filepath.Join(base, rel)
	if err := os.MkdirAll(filepath.Dir(p), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p, make([]byte, n), 0o644); err != nil {
		t.Fatal(err)
	}
	return p
}

// selectedNames maps each entry's base name to whether it is checked.
func selectedNames(es []entry) map[string]bool {
	out := map[string]bool{}
	for _, e := range es {
		out[filepath.Base(e.path)] = e.selected
	}
	return out
}

func TestParseVersion(t *testing.T) {
	for s, want := range map[string][]int{"17.0.3": {17, 0, 3}, "2024.1": {2024, 1}, "8": {8}, "": nil, "1.x": nil} {
		if got := parseVersion(s); !slices.Equal(got, want) {
			t.Errorf("parseVersion(%q) = %v, want %v", s, got, want)
		}
	}
	if got := dottedVersion.FindString("iPhone15,2 17.0.3 (21A360)"); got != "17.0.3" {
		t.Errorf("device support version = %q", got)
	}
}

func TestListDeviceSupportKeepsNewestPerPlatform(t *testing.T) {
	xcode := t.TempDir()
	mkdirs(t, xcode,
		"iOS DeviceSupport/16.4.1 (20E252)",
		"iOS DeviceSupport/iPhone15,2 17.0.3 (21A360)",
		"iOS DeviceSupport/17.0 (21A329)",
		"watchOS DeviceSupport/Watch6,1 9.6 (20U73)",
		"watchOS DeviceSupport/Watch6,1 10.1 (21S71)",
		"DerivedData/App-abc",
	)
	writeFile(t, xcode, "iOS DeviceSupport/.DS_Store", 10)
	es, err := listDeviceSupport(xcode)
	if err != nil {
		t.Fatal(err)
	}
	got := selectedNames(es)
	want := map[string]bool{
		"16.4.1 (20E252)":            true,
		"17.0 (21A329)":              true,
		"iPhone15,2 17.0.3 (21A360)": false,
		"Watch6,1 9.6 (20U73)":       true,
		"Watch6,1 10.1 (21S71)":      false,
	}
	if len(got) != len(want) {
		t.Fatalf("entries = %v, want %v", got, want)
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("%s selected = %v, want %v", k, got[k], v)
		}
	}
	for _, e := range es {
		if e.root != filepath.Dir(e.path) {
			t.Errorf("%s has root %s, want its platform folder", e.path, e.root)
		}
	}
}

func TestListJetBrains(t *testing.T) {
	base := t.TempDir()
	mkdirs(t, base, "IntelliJIdea2023.3/caches", "IntelliJIdea2024.1/caches", "IntelliJIdea2024.1/index", "GoLand2024.2/tmp", "Toolbox/apps", "consentOptions")
	mac, err := listJetBrains(base, false)
	if err != nil {
		t.Fatal(err)
	}
	if got := selectedNames(mac); len(got) != 3 || !got["IntelliJIdea2023.3"] || got["IntelliJIdea2024.1"] || got["GoLand2024.2"] {
		t.Errorf("macOS entries = %v, want the old IntelliJ checked and the newest of each IDE kept", got)
	}

	win, err := listJetBrains(base, true)
	if err != nil {
		t.Fatal(err)
	}
	var paths []string
	for _, e := range win {
		rel, _ := filepath.Rel(base, e.path)
		paths = append(paths, rel)
		if e.root != filepath.Dir(e.path) {
			t.Errorf("%s has root %s, want its IDE folder", e.path, e.root)
		}
		old := filepath.Base(e.root) == "IntelliJIdea2023.3"
		if e.selected != old {
			t.Errorf("%s selected = %v", rel, e.selected)
		}
	}
	slices.Sort(paths)
	want := []string{filepath.Join("GoLand2024.2", "tmp"), filepath.Join("IntelliJIdea2023.3", "caches"), filepath.Join("IntelliJIdea2024.1", "caches"), filepath.Join("IntelliJIdea2024.1", "index")}
	if !slices.Equal(paths, want) {
		t.Errorf("windows entries = %v, want only the cache folders %v", paths, want)
	}
}

func TestListGradleDistsLeavesOutNewest(t *testing.T) {
	dists := t.TempDir()
	mkdirs(t, dists, "gradle-7.6-bin", "gradle-8.5-bin", "gradle-8.10.2-bin", "gradle-8.10.2-all", "other")
	es, err := listGradleDists(dists)
	if err != nil {
		t.Fatal(err)
	}
	got := selectedNames(es)
	if len(got) != 2 || got["gradle-7.6-bin"] || got["gradle-8.5-bin"] {
		t.Errorf("entries = %v, want 7.6 and 8.5, unchecked", got)
	}
	if _, ok := got["gradle-8.10.2-all"]; ok {
		t.Error("the newest version is listed")
	}
	if es, err := listGradleDists(filepath.Join(dists, "missing")); err != nil || len(es) != 0 {
		t.Errorf("missing folder = %v, %v", es, err)
	}
}

func TestListDirsChecksPerFolder(t *testing.T) {
	base := t.TempDir()
	writeFile(t, base, "registry/cache/index/a.crate", 10)
	writeFile(t, base, "git/checkouts/repo/x", 10)
	tl := &tool{selected: true, dirs: []cacheDir{{path: filepath.Join(base, "registry", "cache")}, {path: filepath.Join(base, "git", "checkouts"), off: true}, {path: filepath.Join(base, "missing")}}}
	es, err := listDirs(tl)
	if err != nil {
		t.Fatal(err)
	}
	if got := selectedNames(es); len(got) != 2 || !got["index"] || got["repo"] {
		t.Errorf("entries = %v", got)
	}
}
