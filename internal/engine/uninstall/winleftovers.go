package uninstall

import (
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"
)

// windowsSharedNames are folder names under %APPDATA% and %LOCALAPPDATA% that hold many programs' data (or Windows' own), so a program or publisher called that way never claims them.
var windowsSharedNames = []string{
	"microsoft", "windows", "packages", "programs", "temp", "google", "mozilla", "adobe", "apple", "apple computer",
	"intel", "nvidia", "nvidia corporation", "amd", "jetbrains", "local", "roaming", "locallow", "publishers",
	"crashdumps", "d3dscache", "connecteddevicesplatform", "comms", "history", "virtualstore", "application data",
}

// WindowsLeftovers applies the Windows leftover rules to a program the uninstaller removed: folders named exactly after it, alone or under its publisher, in appData (%APPDATA%) and localAppData (%LOCALAPPDATA%), and its install folder when it was a direct child of %LOCALAPPDATA%\Programs and is still there. Names that cannot be a single folder name, are too short, or belong to many programs match nothing. Only existing folders (never symlinks or junctions) are returned.
func WindowsLeftovers(appData, localAppData string, e Entry) []Candidate {
	name := folderName(e.DisplayName)
	publisher := folderName(e.Publisher)
	var out []Candidate
	add := func(kind LeftoverKind, root string, parts ...string) {
		if root == "" || !filepath.IsAbs(root) {
			return
		}
		p := filepath.Join(append([]string{root}, parts...)...)
		if isRealDir(p) && !slices.ContainsFunc(out, func(c Candidate) bool { return strings.EqualFold(c.Path, p) }) {
			out = append(out, Candidate{Path: p, Kind: kind, Root: root, Selected: true})
		}
	}
	if name != "" {
		for _, loc := range []struct {
			kind LeftoverKind
			root string
		}{{KindAppData, appData}, {KindLocalAppData, localAppData}} {
			if publisher != "" && !strings.EqualFold(publisher, name) {
				add(loc.kind, loc.root, publisher, name)
			}
			add(loc.kind, loc.root, name)
		}
	}
	if localAppData != "" && e.InstallLocation != "" {
		programs := filepath.Join(localAppData, "Programs")
		loc := filepath.Clean(e.InstallLocation)
		if strings.EqualFold(filepath.Dir(loc), programs) && folderName(filepath.Base(loc)) != "" {
			add(KindPrograms, programs, filepath.Base(loc))
		}
	}
	return out
}

// folderName returns s if it can be matched as one folder name, else "".
func folderName(s string) string {
	s = strings.TrimSpace(s)
	if len([]rune(s)) < minNameLen || strings.ContainsAny(s, `\/:*?"<>|`) || strings.Trim(s, ".") == "" {
		return ""
	}
	if slices.Contains(windowsSharedNames, strings.ToLower(s)) {
		return ""
	}
	return s
}

// isRealDir reports whether p is a folder itself, not a symlink, junction or other reparse point.
func isRealDir(p string) bool {
	st, err := os.Lstat(p)
	return err == nil && st.Mode().Type() == fs.ModeDir
}
