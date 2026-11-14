package systemjunk

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/execx"
)

// platformSpecs is the plan of this platform.
var platformSpecs = darwinSpecs

// fillPlatform sets the macOS defaults of what env leaves zero.
func fillPlatform(env *Env) {
	if env.Downloads == "" {
		env.Downloads = filepath.Join(env.Home, "Downloads")
	}
	if env.TrashPath == "" {
		env.TrashPath = filepath.Join(env.Home, ".Trash")
	}
	if env.Running == nil && env.Runner != nil {
		runner := env.Runner
		env.Running = func(ctx context.Context) (Running, error) { return runningApps(ctx, runner) }
	}
}

// runningApps lists the running apps with lsappinfo, which LaunchServices answers for the user's session without any permission.
func runningApps(ctx context.Context, runner execx.Runner) (Running, error) {
	if runner == nil {
		return nil, errors.New("systemjunk: no runner")
	}
	out, err := runner.Run(ctx, execx.Command{Name: "/usr/bin/lsappinfo", Args: []string{"list"}, Timeout: 10 * time.Second})
	if err != nil {
		return nil, err
	}
	return parseLSAppInfo(out.Stdout), nil
}

var (
	lsappinfoName   = regexp.MustCompile(`^\s*\d+\)\s+"(.*)"\s+ASN:`)
	lsappinfoBundle = regexp.MustCompile(`^\s*bundleID="([^"]+)"`)
)

// parseLSAppInfo reads the app names and bundle IDs out of `lsappinfo list`.
func parseLSAppInfo(out []byte) Running {
	r := Running{}
	s := bufio.NewScanner(bytes.NewReader(out))
	s.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	for s.Scan() {
		line := s.Text()
		if m := lsappinfoName.FindStringSubmatch(line); m != nil {
			if n := strings.TrimSpace(m[1]); n != "" {
				r[strings.ToLower(n)] = true
			}
			continue
		}
		if m := lsappinfoBundle.FindStringSubmatch(line); m != nil {
			r[strings.ToLower(m[1])] = true
		}
	}
	return r
}
