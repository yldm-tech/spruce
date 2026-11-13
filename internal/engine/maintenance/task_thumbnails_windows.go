package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path"
	"strings"

	"golang.org/x/sys/windows"

	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// winThumbcachePattern matches the per-user thumbnail cache files (thumbcache_32.db, thumbcache_idx.db, …). The icon cache files in the same directory (iconcache_*.db) do not match.
const winThumbcachePattern = "thumbcache_*.db"

// winThumbnailsTask deletes the thumbnail cache of the current user, which Explorer rebuilds on demand. It does what Disk Cleanup's "Thumbnails" handler does and needs no administrator rights. A file Explorer holds open without delete sharing cannot be removed; it is counted and reported, and Explorer is never closed to free it.
func winThumbnailsTask(sys *winSystem) TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "thumbnails",
			Title:            "清除缩略图缓存",
			Description:      "图片、视频缩略图空白或显示错误时有用，资源管理器会重新生成",
			Icon:             IconPreview,
			DefaultOn:        true,
			EstimatedSeconds: 2,
		},
		Run: func(_ context.Context, env Env) taskresult.Result {
			dir, err := sys.explorerCacheDir()
			if err != nil {
				return taskresult.Result{Outcome: taskresult.Failed, Message: "找不到缩略图缓存所在的文件夹", Detail: err.Error()}
			}
			res := winClearThumbnails(sys, dir)
			env.Log.Info("thumbnail cache", "dir", dir, "outcome", res.Outcome, "detail", res.Detail)
			return res
		},
	}
}

// winClearThumbnails removes every regular file matching winThumbcachePattern directly inside dir.
func winClearThumbnails(sys *winSystem, dir string) taskresult.Result {
	root, err := os.OpenRoot(dir)
	if errors.Is(err, fs.ErrNotExist) {
		return taskresult.Result{Outcome: taskresult.Unchanged, Message: "没有需要清除的缩略图缓存", Detail: dir + " does not exist"}
	}
	if err != nil {
		return taskresult.Result{Outcome: taskresult.Failed, Message: "无法打开缩略图缓存所在的文件夹", Detail: err.Error()}
	}
	defer root.Close()
	entries, err := fs.ReadDir(root.FS(), ".")
	if err != nil {
		return taskresult.Result{Outcome: taskresult.Failed, Message: "无法读取缩略图缓存所在的文件夹", Detail: err.Error()}
	}

	var removed, inUse, failed []string
	var errs []string
	for _, e := range entries {
		// Match case-insensitively, as the file system does; skip directories, links and anything else that is not a plain file.
		if ok, _ := path.Match(winThumbcachePattern, strings.ToLower(e.Name())); !ok || !e.Type().IsRegular() {
			continue
		}
		err := sys.removeFile(root, e.Name())
		switch {
		case err == nil, errors.Is(err, fs.ErrNotExist):
			removed = append(removed, e.Name())
		case winFileInUse(err):
			inUse = append(inUse, e.Name())
		default:
			failed = append(failed, e.Name())
			errs = append(errs, err.Error())
		}
	}

	detail := fmt.Sprintf("%s: removed %d %v, in use %d %v, failed %d %v", dir, len(removed), removed, len(inUse), inUse, len(failed), failed)
	if len(errs) > 0 {
		detail += "; " + strings.Join(errs, "; ")
	}
	switch {
	case len(removed) == 0 && len(inUse) == 0 && len(failed) == 0:
		return taskresult.Result{Outcome: taskresult.Unchanged, Message: "没有需要清除的缩略图缓存", Detail: detail}
	case len(removed) == 0 && len(failed) == 0:
		return taskresult.Result{Outcome: taskresult.Skipped, Message: "缩略图缓存正被资源管理器占用，未能清除；重新登录 Windows 后再运行一次", Detail: detail}
	case len(removed) == 0:
		return taskresult.Result{Outcome: taskresult.Failed, Message: "未能清除缩略图缓存", Detail: detail}
	case len(inUse) == 0 && len(failed) == 0:
		return taskresult.Result{Outcome: taskresult.Applied, Message: fmt.Sprintf("已清除 %d 个缩略图缓存文件", len(removed)), Detail: detail}
	}
	msg := fmt.Sprintf("已清除 %d 个缩略图缓存文件，另有 %d 个正被资源管理器占用，重新登录 Windows 后再运行一次可全部清除", len(removed), len(inUse))
	if len(failed) > 0 {
		msg = fmt.Sprintf("已清除 %d 个缩略图缓存文件，另有 %d 个未能清除", len(removed), len(inUse)+len(failed))
	}
	return taskresult.Result{Outcome: taskresult.Applied, Message: msg, Detail: detail}
}

// winFileInUse reports whether a deletion failed because another process holds the file open.
func winFileInUse(err error) bool {
	return errors.Is(err, windows.ERROR_SHARING_VIOLATION) || errors.Is(err, windows.ERROR_LOCK_VIOLATION)
}
