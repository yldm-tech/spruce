package systemjunk

import (
	"context"
	"errors"
	"path/filepath"
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
