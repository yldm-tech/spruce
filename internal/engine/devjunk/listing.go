package devjunk

import (
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
)

// readEntries lists dir's entries with their Lstat information; a missing dir has none.
func readEntries(dir string) ([]entry, error) {
	des, err := os.ReadDir(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	out := make([]entry, 0, len(des))
	for _, de := range des {
		info, err := de.Info()
		if err != nil {
			continue // vanished since the directory was read
		}
		out = append(out, entry{path: filepath.Join(dir, de.Name()), root: dir, info: info})
	}
	return out, nil
}

// listDirs is the default listing: every entry of the tool's folders, checked with the tool unless its folder is off.
func listDirs(t *tool) ([]entry, error) {
	var out []entry
	var firstErr error
	for _, d := range t.dirs {
		es, err := readEntries(d.path)
		if err != nil {
			firstErr = cmpErr(firstErr, err)
			continue
		}
		for _, e := range es {
			e.selected = t.selected && !d.off
			out = append(out, e)
		}
	}
	return out, firstErr
}

// cmpErr keeps the first error.
func cmpErr(first, err error) error {
	if first != nil {
		return first
	}
	return err
}

// keepNewest checks every entry but the newest of each group, which it notes as kept.
func keepNewest(groups map[string][]entry, versions map[string][]versioned, newestNote string) []entry {
	var out []entry
	for key, es := range groups {
		newest := newestIndex(versions[key])
		for i, e := range es {
			e.selected = i != newest
			if i == newest {
				e.note = newestNote
			}
			out = append(out, e)
		}
	}
	return out
}

// listDeviceSupport lists the versions in every ~/Library/Developer/Xcode/*DeviceSupport folder (iOS, watchOS, tvOS, visionOS …): all but the newest of each platform are checked.
func listDeviceSupport(xcode string) ([]entry, error) {
	platforms, err := readEntries(xcode)
	if err != nil {
		return nil, err
	}
	groups := map[string][]entry{}
	versions := map[string][]versioned{}
	var firstErr error
	for _, p := range platforms {
		if !p.info.IsDir() || !strings.HasSuffix(filepath.Base(p.path), "DeviceSupport") {
			continue
		}
		es, err := readEntries(p.path)
		if err != nil {
			firstErr = cmpErr(firstErr, err)
			continue
		}
		for _, e := range es {
			if !e.info.IsDir() {
				continue
			}
			groups[p.path] = append(groups[p.path], e)
			versions[p.path] = append(versions[p.path], versioned{version: parseVersion(dottedVersion.FindString(filepath.Base(e.path))), modTime: e.info.ModTime()})
		}
	}
	return keepNewest(groups, versions, "这个平台最新的版本，默认保留"), firstErr
}

// listJetBrains lists the caches of each JetBrains IDE version: all but the newest version of each IDE are checked. On macOS an item is the version's whole folder in ~/Library/Caches/JetBrains, which holds only caches; on Windows %LOCALAPPDATA%\JetBrains\<IDE><version> also holds other data, so the items are its caches, index and tmp folders. Folders that are not an IDE version (Toolbox and the like) are left alone.
func listJetBrains(base string, windows bool) ([]entry, error) {
	products, err := readEntries(base)
	if err != nil {
		return nil, err
	}
	groups := map[string][]entry{}
	versions := map[string][]versioned{}
	for _, p := range products {
		m := jetBrainsDir.FindStringSubmatch(filepath.Base(p.path))
		if m == nil || !p.info.IsDir() {
			continue
		}
		product, v := m[1], versioned{version: parseVersion(m[2]), modTime: p.info.ModTime()}
		if !windows {
			groups[product] = append(groups[product], p)
			versions[product] = append(versions[product], v)
			continue
		}
		for _, sub := range []string{"caches", "index", "tmp"} {
			path := filepath.Join(p.path, sub)
			info, err := os.Lstat(path)
			if err != nil || !info.IsDir() {
				continue
			}
			groups[product] = append(groups[product], entry{path: path, root: p.path, info: info, note: filepath.Base(p.path)})
			versions[product] = append(versions[product], v)
		}
	}
	// On Windows one version has several items; keep all of the newest version's.
	out := make([]entry, 0)
	for product, es := range groups {
		vs := versions[product]
		newest := newestIndex(vs)
		for i, e := range es {
			same := compareVersioned(versioned{version: vs[i].version}, versioned{version: vs[newest].version}) == 0
			e.selected = !same
			if same {
				e.note = strings.TrimPrefix(e.note+" · ", " · ") + "最新版本，默认保留"
			}
			out = append(out, e)
		}
	}
	return out, nil
}

// listGradleDists lists the Gradle wrapper distributions in dists except the newest version, none checked.
func listGradleDists(dists string) ([]entry, error) {
	es, err := readEntries(dists)
	if err != nil {
		return nil, err
	}
	var keep []entry
	var vs []versioned
	for _, e := range es {
		m := gradleDist.FindStringSubmatch(filepath.Base(e.path))
		if m == nil || !e.info.IsDir() {
			continue
		}
		keep = append(keep, e)
		vs = append(vs, versioned{version: parseVersion(m[1])})
	}
	newest := newestIndex(vs)
	var out []entry
	for i, e := range keep {
		if compareVersioned(vs[i], vs[newest]) == 0 {
			continue // the newest version, bin and all alike
		}
		out = append(out, e)
	}
	return out, nil
}
