package maintenance

import (
	"context"
	"time"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/taskresult"
)

// winDNSTask flushes the DNS Client resolver cache with "ipconfig /flushdns". A standard user may flush it, so the task needs no administrator rights. ipconfig's output is localised, so only its exit status is used.
func winDNSTask(sys *winSystem) TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "dns",
			Title:            "刷新 DNS 缓存",
			Description:      "网页打不开、域名解析异常时有用",
			Icon:             IconNetwork,
			DefaultOn:        true,
			EstimatedSeconds: 2,
		},
		Run: func(ctx context.Context, env Env) taskresult.Result {
			return winRunSystemTool(ctx, env, sys, "ipconfig.exe", []string{"/flushdns"}, 30*time.Second,
				taskresult.Result{Outcome: taskresult.Applied, Message: "已刷新 DNS 缓存"},
				"未能刷新 DNS 缓存")
		},
	}
}

// winIconsTask asks the shell to refresh its icon cache with "ie4uinit.exe -show", the Windows 10/11 form ("-ClearIconCache" is the Windows 7/8 form). It works in place for the current user: Explorer is not restarted and iconcache_*.db is not deleted, so an icon Explorer is already drawing may only change after the user signs in again.
func winIconsTask(sys *winSystem) TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "icons",
			Title:            "刷新图标缓存",
			Description:      "桌面或文件夹图标显示错乱时有用，不会重启资源管理器",
			Icon:             IconIcons,
			DefaultOn:        true,
			EstimatedSeconds: 3,
		},
		Run: func(ctx context.Context, env Env) taskresult.Result {
			return winRunSystemTool(ctx, env, sys, "ie4uinit.exe", []string{"-show"}, 60*time.Second,
				taskresult.Result{Outcome: taskresult.Applied, Message: "已刷新图标缓存，个别图标可能要重新登录后才更新"},
				"未能刷新图标缓存")
		},
	}
}

// winRunSystemTool runs System32\name with args and returns ok when it exits 0, or the failure Result built by winToolFailure.
func winRunSystemTool(ctx context.Context, env Env, sys *winSystem, name string, args []string, timeout time.Duration, ok taskresult.Result, failed string) taskresult.Result {
	exe, err := sys.systemTool(name)
	if err != nil {
		return taskresult.Result{Outcome: taskresult.Failed, Message: failed, Detail: err.Error()}
	}
	cmd := execx.Command{Name: exe, Args: args, Timeout: timeout}
	out, err := env.Runner.Run(ctx, cmd)
	if err != nil {
		env.Log.Warn("maintenance tool failed", "command", cmd.String(), "exitCode", out.ExitCode, "err", err)
		return winToolFailure(out, err, failed)
	}
	ok.Detail = cmd.String()
	return ok
}
