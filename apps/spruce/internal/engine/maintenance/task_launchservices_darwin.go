package maintenance

import (
	"context"
	"fmt"
	"os"
	"time"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/taskresult"
)

// lsregisterCandidates are where lsregister lives inside the LaunchServices framework, the short path through the framework's symlinks first and the fully versioned path second, for a system where the symlinks are missing. lsregister is not on PATH.
var lsregisterCandidates = []string{
	"/System/Library/Frameworks/CoreServices.framework/Frameworks/LaunchServices.framework/Support/lsregister",
	"/System/Library/Frameworks/CoreServices.framework/Versions/A/Frameworks/LaunchServices.framework/Versions/A/Support/lsregister",
}

const launchServicesTimeout = 3 * time.Minute

// launchServicesTask garbage-collects the current user's Launch Services database (lsregister -gc), which drops the registrations of apps that no longer exist and compacts the database, so deleted apps disappear from 「打开方式」.
//
// It never runs lsregister -r, -R, -f, -u, -seed, -delete or -kill: re-registering every app domain-wide drops Network Extension VPN registrations (a running VPN reads as reinstalled) and triggers Apple Intelligence and Spotlight re-indexing. The only command this task can run is "<lsregister> -gc".
func launchServicesTask(candidates []string) TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "launchservices",
			Title:            "修复「打开方式」",
			Description:      "右键菜单里出现重复或失效的应用",
			Icon:             IconOpenWith,
			DefaultOn:        true,
			NeedsAdmin:       false,
			EstimatedSeconds: 15,
		},
		Run: func(ctx context.Context, env Env) taskresult.Result {
			exe, err := locateExecutable(candidates)
			if err != nil {
				return taskresult.Result{Outcome: taskresult.Unavailable, Message: "这台 Mac 上找不到 Launch Services 工具，无法执行", Detail: err.Error()}
			}
			cmd := execx.Command{Name: exe, Args: []string{"-gc"}, Timeout: launchServicesTimeout}
			if _, err := env.Runner.Run(ctx, cmd); err != nil {
				return commandFailure("清理「打开方式」记录失败", cmd, err)
			}
			return taskresult.Result{Outcome: taskresult.Applied, Message: "已清理已删除应用留下的「打开方式」记录"}
		},
	}
}

// locateExecutable returns the first candidate that is a regular file with an execute bit set.
func locateExecutable(candidates []string) (string, error) {
	for _, p := range candidates {
		fi, err := os.Stat(p)
		if err == nil && fi.Mode().IsRegular() && fi.Mode().Perm()&0o111 != 0 {
			return p, nil
		}
	}
	return "", fmt.Errorf("no executable lsregister among %v", candidates)
}
