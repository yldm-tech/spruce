package devjunk

import (
	"context"
	"encoding/json"
	"os"
	"path/filepath"
	"strings"

	"spruce/apps/spruce/internal/engine/execx"
)

// xcrunPath and xcodeSelectPath are the developer tool launchers macOS ships.
const (
	xcrunPath       = "/usr/bin/xcrun"
	xcodeSelectPath = "/usr/bin/xcode-select"
)

// binaryCandidates are the names and paths tried, in order, for the tool name: the bare name (looked up on PATH), then the usual install folders. A GUI app on macOS starts with PATH=/usr/bin:/bin:/usr/sbin:/sbin, so Homebrew, Volta, mise and pnpm's own folders have to be tried by path.
func binaryCandidates(e env, name string) []string {
	out := []string{name}
	if e.windows() {
		pf := e.dirVar("ProgramFiles", `C:\Program Files`)
		switch name {
		case "go":
			out = append(out, filepath.Join(pf, "Go", "bin", "go.exe"))
		case "npm":
			out = append(out, filepath.Join(pf, "nodejs", "npm.cmd"))
		case "pnpm":
			out = append(out, filepath.Join(e.localAppData(), "pnpm", "pnpm.exe"))
		case "git":
			out = append(out, filepath.Join(pf, "Git", "cmd", "git.exe"))
		}
		return out
	}
	for _, dir := range []string{
		"/opt/homebrew/bin", "/usr/local/bin",
		filepath.Join(e.home, ".local", "share", "mise", "shims"), filepath.Join(e.home, ".local", "bin"),
		filepath.Join(e.home, ".volta", "bin"), filepath.Join(e.home, "Library", "pnpm"),
	} {
		out = append(out, filepath.Join(dir, name))
	}
	if name == "go" {
		out = append(out, "/usr/local/go/bin/go")
	}
	return out
}

// acceptBinary rejects binaries that must not be run: on macOS everything in /usr/bin, where pip3 and friends are stubs that open the Command Line Tools installer when the tools are missing.
func acceptBinary(e env, path string) bool {
	return e.windows() || !strings.HasPrefix(path, "/usr/bin/")
}

// findBinary returns the absolute path of the tool name, or "".
func findBinary(e env, lookPath func(string) (string, error), name string) string {
	for _, c := range binaryCandidates(e, name) {
		p, err := lookPath(c)
		if err != nil || !e.isAbs(p) || !acceptBinary(e, p) {
			continue
		}
		return p
	}
	return ""
}

// locate finds the tools' binaries and asks them where their caches are. A tool that is missing or does not answer leaves its fields empty; the catalog then falls back to the default folders. devicesDir is the CoreSimulator devices folder: xcode-select runs only when it exists, or to find git. wantGit also looks for git (for the projects' Git state): on PATH and in the usual install folders, then on macOS in the selected developer folder (xcode-select -p, which never opens the tools installer, unlike the /usr/bin/git stub).
func locate(ctx context.Context, e env, runner execx.Runner, lookPath func(string) (string, error), devicesDir string, wantGit bool) located {
	l := located{bins: map[string]string{}}
	names := []string{"npm", "pnpm", "go"}
	if e.windows() {
		names = append(names, "pip")
	} else {
		names = append(names, "brew", "pip3")
	}
	if wantGit {
		names = append(names, "git")
	}
	for _, n := range names {
		if p := findBinary(e, lookPath, n); p != "" {
			l.bins[n] = p
		}
	}
	query := func(name string, args ...string) []string {
		cmd := e.commandFor(l, name, args...)
		if cmd == nil {
			return nil
		}
		cmd.Timeout = queryTimeout
		out, err := runner.Run(ctx, *cmd)
		if err != nil {
			return nil
		}
		return absLines(e, string(out.Stdout))
	}
	if lines := query("go", "env", "GOCACHE", "GOMODCACHE"); len(lines) == 2 {
		l.goCache, l.goModCache = lines[0], lines[1]
	}
	if lines := query("pnpm", "store", "path"); len(lines) == 1 {
		l.pnpmStore = lines[0]
	}
	if !e.windows() {
		if lines := query("brew", "--cache"); len(lines) == 1 {
			l.brewCache = lines[0]
		}
		var devDir string
		asked := false
		developer := func() string {
			if !asked {
				asked = true
				devDir = developerDir(ctx, runner)
			}
			return devDir
		}
		if _, err := os.Stat(devicesDir); err == nil {
			if dev := developer(); dev != "" {
				if _, err := os.Stat(filepath.Join(dev, "usr", "bin", "simctl")); err == nil {
					l.xcrun = xcrunPath
				}
			}
		}
		if wantGit && l.bins["git"] == "" {
			if dev := developer(); dev != "" {
				p := filepath.Join(dev, "usr", "bin", "git")
				if info, err := os.Stat(p); err == nil && info.Mode().IsRegular() {
					l.bins["git"] = p
				}
			}
		}
	}
	return l
}

// absLines returns the non-empty lines of s, or nil when any of them is not an absolute path (a tool printing a warning instead of an answer).
func absLines(e env, s string) []string {
	var out []string
	for line := range strings.Lines(s) {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		if !e.isAbs(line) {
			return nil
		}
		out = append(out, filepath.Clean(line))
	}
	return out
}

// developerDir is the selected developer folder (xcode-select -p): Xcode's, whose usr/bin has simctl and git, or the Command Line Tools', which has git only; "" when neither is installed. xcode-select -p only prints the folder; it never opens the tools installer.
func developerDir(ctx context.Context, runner execx.Runner) string {
	out, err := runner.Run(ctx, execx.Command{Name: xcodeSelectPath, Args: []string{"-p"}, Timeout: queryTimeout})
	if err != nil {
		return ""
	}
	dev := strings.TrimSpace(string(out.Stdout))
	if !filepath.IsAbs(dev) {
		return ""
	}
	return dev
}

// simDevice is one device of simctl list devices -j.
type simDevice struct {
	UDID        string `json:"udid"`
	Name        string `json:"name"`
	IsAvailable bool   `json:"isAvailable"`
}

// unavailableSimulators returns the UDIDs of the simulators simctl marks unavailable.
func unavailableSimulators(ctx context.Context, runner execx.Runner, xcrun string) ([]string, error) {
	out, err := runner.Run(ctx, execx.Command{Name: xcrun, Args: []string{"simctl", "list", "devices", "-j"}, Timeout: queryTimeout})
	if err != nil {
		return nil, err
	}
	var list struct {
		Devices map[string][]simDevice `json:"devices"`
	}
	if err := json.Unmarshal(out.Stdout, &list); err != nil {
		return nil, err
	}
	var udids []string
	for _, devs := range list.Devices {
		for _, d := range devs {
			// A UDID becomes a folder name; anything else is not a simctl device.
			if !d.IsAvailable && d.UDID != "" && !strings.ContainsAny(d.UDID, `/\.`) {
				udids = append(udids, d.UDID)
			}
		}
	}
	return udids, nil
}
