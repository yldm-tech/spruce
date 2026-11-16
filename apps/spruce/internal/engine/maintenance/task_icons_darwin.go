package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"syscall"
	"time"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/taskresult"
)

const (
	// iconServicesCacheName is the per-user icon cache directory of iconservicesagent inside the user's cache directory (getconf DARWIN_USER_CACHE_DIR, /var/folders/…/C/). Its contents are removed; the directory itself and its permissions are kept for the agent.
	iconServicesCacheName = "com.apple.iconservices"
	// dockIconCacheName is the Dock's own icon cache file in the same directory.
	dockIconCacheName = "com.apple.dock.iconcache"

	// The processes restarted after the caches are cleared. Both are per-user launchd agents that launchd starts again; `launchctl kickstart -k` on their labels is refused with "Operation not permitted while System Integrity Protection is engaged" (exit 150), so they are ended with killall, which only signals the current user's own processes. iconservicesagent ignores SIGTERM (checked on macOS 27: the process survives it), so it gets SIGKILL and launchd starts it again on the next icon request; the Dock quits cleanly on the default SIGTERM and launchd relaunches it at once.
	iconServicesAgentProcess = "iconservicesagent"
	dockProcess              = "Dock"

	iconsCommandTimeout = 15 * time.Second
)

// iconsTask rebuilds the current user's icon caches, which is the least disruptive refresh that fixes wrong or blank icons in the Dock and Finder:
//
//  1. Remove the contents of the per-user iconservices cache and the Dock's icon cache file, both in the user's own cache directory (no administrator rights needed).
//  2. End the user's iconservicesagent and Dock with killall; launchd starts them again and they rebuild from scratch. Restarting the Dock only redraws it; open windows and Spaces stay as they are.
//
// Deliberately not done:
//
//   - Finder is not restarted: a restart would abort a copy or move in progress. Finder picks up the rebuilt icons in new windows, and fully after logging out and in again, which the Result message says.
//   - The system-wide store under /Library/Caches/com.apple.iconservices.store is left alone: it belongs to the root daemon iconservicesd, so clearing it would need the administrator prompt and a restart of a system daemon for a cache that iconservicesd keeps in step with app bundles itself.
func iconsTask() TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "icons",
			Title:            "重建图标缓存",
			Description:      "Dock 和访达里的图标错乱时修复",
			Icon:             IconIcons,
			DefaultOn:        true,
			NeedsAdmin:       false,
			EstimatedSeconds: 5,
		},
		Run: runIcons,
	}
}

func runIcons(ctx context.Context, env Env) taskresult.Result {
	getconf := execx.Command{Name: pathGetconf, Args: []string{"DARWIN_USER_CACHE_DIR"}, Timeout: iconsCommandTimeout}
	out, err := env.Runner.Run(ctx, getconf)
	if err != nil {
		return commandFailure("找不到图标缓存所在的目录", getconf, err)
	}
	dir, err := validateUserCacheDir(strings.TrimSpace(string(out.Stdout)))
	if err != nil {
		return taskresult.Result{Outcome: taskresult.Failed, Message: "找不到图标缓存所在的目录", Detail: err.Error()}
	}

	removed, err := clearIconCaches(dir)
	if err != nil {
		return taskresult.Result{Outcome: taskresult.Failed, Message: "清除图标缓存失败", Detail: err.Error()}
	}
	env.Log.Debug("icon caches cleared", "dir", dir, "removed", removed)

	detail := fmt.Sprintf("removed %d cache entries in %s", removed, dir)
	var restartErrs []string
	for _, args := range [][]string{{"-KILL", iconServicesAgentProcess}, {dockProcess}} {
		name := args[len(args)-1]
		cmd := execx.Command{Name: pathKillall, Args: args, Timeout: iconsCommandTimeout}
		if _, err := env.Runner.Run(ctx, cmd); err != nil {
			// killall exits 1 when the user has no such process: an agent that is not running has nothing in memory to refresh and starts from the cleared cache on demand.
			var exitErr *execx.ExitError
			if errors.As(err, &exitErr) && exitErr.ExitCode == 1 && ctx.Err() == nil {
				continue
			}
			if ctx.Err() != nil {
				if removed == 0 {
					return interrupted()
				}
				// The caches are already gone; report that change instead of "未执行", so the history matches the disk.
				return taskresult.Result{
					Outcome: taskresult.Applied,
					Partial: true,
					Message: "已清除图标缓存，维护中断，未能刷新程序坞；注销并重新登录后生效",
					Detail:  joinDetail(detail, "interrupted before restarting "+name+": "+err.Error()),
				}
			}
			restartErrs = append(restartErrs, err.Error())
		}
	}
	if len(restartErrs) > 0 {
		// The agents keep their old icons in memory until they restart, so the user has to log out; Partial makes the page show this message.
		return taskresult.Result{
			Outcome: taskresult.Applied,
			Partial: true,
			Message: "已清除图标缓存，但未能重启图标服务和程序坞；注销并重新登录后生效",
			Detail:  joinDetail(append([]string{detail}, restartErrs...)...),
		}
	}
	return taskresult.Result{Outcome: taskresult.Applied, Message: "已重建图标缓存并刷新程序坞；如果访达里仍有旧图标，注销并重新登录即可", Detail: detail}
}

// validateUserCacheDir checks the output of getconf DARWIN_USER_CACHE_DIR before anything is removed under it: an absolute path whose last element is "C", naming a real directory (not a symlink) owned by the current user.
func validateUserCacheDir(p string) (string, error) {
	if p == "" || !filepath.IsAbs(p) {
		return "", fmt.Errorf("user cache directory %q is not an absolute path", p)
	}
	p = filepath.Clean(p)
	if filepath.Base(p) != "C" {
		return "", fmt.Errorf("user cache directory %q does not end in /C", p)
	}
	fi, err := os.Lstat(p)
	if err != nil {
		return "", err
	}
	if !fi.IsDir() {
		return "", fmt.Errorf("user cache directory %q is not a directory (mode %v)", p, fi.Mode())
	}
	st, ok := fi.Sys().(*syscall.Stat_t)
	if !ok || int(st.Uid) != os.Getuid() {
		return "", fmt.Errorf("user cache directory %q is not owned by the current user", p)
	}
	return p, nil
}

// clearIconCaches removes the entries inside the iconservices cache directory and the Dock icon cache file under dir, through an os.Root so that a symlink cannot lead the removal outside dir. Missing caches are not an error. It returns how many entries it removed.
func clearIconCaches(dir string) (int, error) {
	root, err := os.OpenRoot(dir)
	if err != nil {
		return 0, err
	}
	defer root.Close()

	removed := 0
	fi, err := root.Lstat(iconServicesCacheName)
	switch {
	case errors.Is(err, fs.ErrNotExist):
	case err != nil:
		return 0, err
	case fi.IsDir():
		cache, err := root.OpenRoot(iconServicesCacheName)
		if err != nil {
			return 0, err
		}
		defer cache.Close()
		entries, err := fs.ReadDir(cache.FS(), ".")
		if err != nil {
			return 0, err
		}
		for _, e := range entries {
			if err := cache.RemoveAll(e.Name()); err != nil {
				return removed, err
			}
			removed++
		}
	default:
		return 0, fmt.Errorf("%s in %s is not a directory (mode %v)", iconServicesCacheName, dir, fi.Mode())
	}

	switch err := root.Remove(dockIconCacheName); {
	case err == nil:
		removed++
	case !errors.Is(err, fs.ErrNotExist):
		return removed, err
	}
	return removed, nil
}
