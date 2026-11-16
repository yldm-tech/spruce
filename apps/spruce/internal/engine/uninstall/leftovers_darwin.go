package uninstall

import (
	"context"
	"errors"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strings"

	"howett.net/plist"
)

// sharedNames are folder names that belong to a vendor or a framework rather than one app, so an app called that way never claims them.
var sharedNames = []string{
	"apple", "google", "microsoft", "adobe", "mozilla", "jetbrains", "electron", "intel", "nvidia", "amd",
	"java", "oracle", "python", "node", "chromium", "webkit", "steam", "cef", "qt", "unity",
}

// maxAgentPlist bounds a LaunchAgent plist read for matching.
const maxAgentPlist = 1 << 20

// Notes on leftovers, shown under their path.
const (
	noteByName       = "按应用名称匹配"
	noteAgent        = "登录时启动的后台项目，它运行的程序在应用内"
	noteAgentOutside = "登录时启动的后台项目，按标识匹配；它运行的程序不在应用内，请确认后再勾选"
)

// macLeftovers applies the leftover rules of an app to the user's Library folder lib: exact bundle ID matches, and exact name matches where the rule allows a name. It returns the candidates and how many locations could not be read for lack of permission.
func macLeftovers(ctx context.Context, lib string, app App) ([]Candidate, int64) {
	var out []Candidate
	var denied int64
	id := app.BundleID
	names := nameKeys(app.names)

	// list returns the entry names of a Library subfolder; a missing folder has none.
	list := func(rel string) []fs.DirEntry {
		entries, err := os.ReadDir(filepath.Join(lib, rel))
		if err != nil && errors.Is(err, fs.ErrPermission) {
			denied++
		}
		return entries
	}
	add := func(kind LeftoverKind, rel, name, note string) {
		root := filepath.Join(lib, rel)
		out = append(out, Candidate{Path: filepath.Join(root, name), Kind: kind, Root: root, Selected: true, Note: note})
	}
	// folders adds the entries of rel named exactly id (byID) or exactly one of the app's names (byName).
	folders := func(kind LeftoverKind, rel string, byID, byName bool) {
		for _, ent := range list(rel) {
			n := ent.Name()
			switch {
			case byID && id != "" && n == id:
				add(kind, rel, n, "")
			case byName && slices.Contains(names, n):
				add(kind, rel, n, noteByName)
			}
		}
	}

	folders(KindSupport, "Application Support", true, true)
	folders(KindCaches, "Caches", true, true)
	folders(KindLogs, "Logs", true, true)
	if id != "" {
		folders(KindContainers, "Containers", true, false)
		folders(KindHTTPStorage, "HTTPStorages", true, false)
		folders(KindWebKit, "WebKit", true, false)
		for _, ent := range list("Preferences") {
			if n := ent.Name(); n == id+".plist" || isIDPlist(n, id) {
				add(KindPreferences, "Preferences", n, "")
			}
		}
		for _, ent := range list(filepath.Join("Preferences", "ByHost")) {
			if n := ent.Name(); isIDPlist(n, id) {
				add(KindPreferences, filepath.Join("Preferences", "ByHost"), n, "")
			}
		}
		for _, ent := range list("Group Containers") {
			if n := ent.Name(); n == id || strings.HasSuffix(n, "."+id) {
				add(KindGroupContainers, "Group Containers", n, "")
			}
		}
		for _, ent := range list("Saved Application State") {
			if ent.Name() == id+".savedState" {
				add(KindSavedState, "Saved Application State", ent.Name(), "")
			}
		}
		for _, ent := range list("Cookies") {
			if ent.Name() == id+".binarycookies" {
				add(KindCookies, "Cookies", ent.Name(), "")
			}
		}
	}
	if ctx.Err() != nil {
		return nil, denied
	}
	out = append(out, launchAgents(filepath.Join(lib, "LaunchAgents"), app, out, &denied)...)
	return out, denied
}

// nameKeys returns the names leftovers may match by: long enough and not a vendor's or framework's shared folder.
func nameKeys(names []string) []string {
	var out []string
	for _, n := range names {
		if len([]rune(n)) < minNameLen || slices.Contains(sharedNames, strings.ToLower(n)) || strings.ContainsAny(n, `/\`) || n == "." || n == ".." {
			continue
		}
		out = append(out, n)
	}
	return out
}

// isIDPlist reports whether name is <id>.<something>.plist.
func isIDPlist(name, id string) bool {
	return strings.HasPrefix(name, id+".") && strings.HasSuffix(name, ".plist") && len(name) > len(id)+len("..plist")
}

// launchAgent is the part of a launchd plist that says what it runs.
type launchAgent struct {
	Label            string
	Program          string
	ProgramArguments []string
}

// launchAgents returns the plists in dir that start the app's code: those whose program or arguments lie inside the bundle (selected), and those whose Label is the bundle ID or below it (selected only when what they run is inside the bundle or one of the other selected leftovers, which go too).
func launchAgents(dir string, app App, others []Candidate, denied *int64) []Candidate {
	entries, err := os.ReadDir(dir)
	if err != nil {
		if errors.Is(err, fs.ErrPermission) {
			*denied++
		}
		return nil
	}
	var out []Candidate
	for _, ent := range entries {
		if !ent.Type().IsRegular() || !strings.HasSuffix(ent.Name(), ".plist") {
			continue
		}
		p := filepath.Join(dir, ent.Name())
		agent, ok := readAgent(p)
		if !ok {
			continue
		}
		runs := append([]string{agent.Program}, agent.ProgramArguments...)
		inBundle := slices.ContainsFunc(runs, func(s string) bool { return app.Path != "" && (s == app.Path || inside(s, app.Path)) })
		byLabel := app.BundleID != "" && (agent.Label == app.BundleID || strings.HasPrefix(agent.Label, app.BundleID+"."))
		switch {
		case inBundle:
			out = append(out, Candidate{Path: p, Kind: KindLaunchAgents, Root: dir, Selected: true, Note: noteAgent})
		case byLabel:
			inLeftover := slices.ContainsFunc(runs, func(s string) bool {
				return s != "" && slices.ContainsFunc(others, func(c Candidate) bool { return c.Selected && (s == c.Path || inside(s, c.Path)) })
			})
			note := noteAgentOutside
			if inLeftover {
				note = "登录时启动的后台项目，它运行的程序也会一起移除"
			}
			out = append(out, Candidate{Path: p, Kind: KindLaunchAgents, Root: dir, Selected: inLeftover, Note: note})
		}
	}
	return out
}

// readAgent parses a launchd plist; ProgramArguments entries that are not strings are left out.
func readAgent(path string) (launchAgent, bool) {
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxAgentPlist {
		return launchAgent{}, false
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return launchAgent{}, false
	}
	var raw map[string]any
	if _, err := plist.Unmarshal(b, &raw); err != nil {
		return launchAgent{}, false
	}
	var a launchAgent
	a.Label, _ = raw["Label"].(string)
	a.Program, _ = raw["Program"].(string)
	if args, ok := raw["ProgramArguments"].([]any); ok {
		for _, v := range args {
			if s, ok := v.(string); ok {
				a.ProgramArguments = append(a.ProgramArguments, s)
			}
		}
	}
	return a, true
}
