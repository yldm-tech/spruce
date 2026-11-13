package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"

	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// winThumbnailsTask clears the thumbnail cache of the current user through the system's registered "Thumbnail Cache" Disk Cleanup handler (IEmptyVolumeCache), the handler Disk Cleanup's "Thumbnails" entry uses. The handler clears the cache while Explorer keeps running and needs no administrator rights; Explorer rebuilds thumbnails on demand. Deleting thumbcache_*.db directly is not done: Explorer keeps those files open without delete sharing for as long as the shell runs, so it only works after ending explorer.exe, which this task never does.
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
		Run: func(ctx context.Context, env Env) taskresult.Result {
			dir, err := sys.explorerCacheDir()
			if err != nil {
				return taskresult.Result{Outcome: taskresult.Failed, Message: "找不到缩略图缓存所在的文件夹", Detail: err.Error()}
			}
			res := winClearThumbnails(ctx, sys, filepath.VolumeName(dir)+`\`)
			env.Log.Info("thumbnail cache", "dir", dir, "outcome", res.Outcome, "detail", res.Detail)
			return res
		},
	}
}

// winClearThumbnails asks the Disk Cleanup handler to empty the thumbnail cache on volume and turns its answer into the task's Result.
func winClearThumbnails(ctx context.Context, sys *winSystem, volume string) taskresult.Result {
	p, err := sys.purgeThumbnailCache(ctx, volume)
	detail := fmt.Sprintf("volume %s: %d bytes used, %d bytes freed", volume, p.used, p.freed)
	switch {
	case err != nil && ctx.Err() != nil:
		if p.freed > 0 {
			return taskresult.Result{Outcome: taskresult.Applied, Partial: true, Message: "已清除部分缩略图缓存，维护已中断", Detail: detail + "; " + err.Error()}
		}
		return interrupted()
	case errors.Is(err, fs.ErrNotExist):
		return taskresult.Result{Outcome: taskresult.Unavailable, Message: "此版本的 Windows 没有注册缩略图缓存清理程序", Detail: err.Error()}
	case errors.Is(err, errors.ErrUnsupported):
		return taskresult.Result{Outcome: taskresult.Unavailable, Message: "此版本的 Windows 不支持清除缩略图缓存", Detail: err.Error()}
	case err != nil:
		return taskresult.Result{Outcome: taskresult.Failed, Message: "未能清除缩略图缓存", Detail: detail + "; " + err.Error()}
	case p.empty:
		return taskresult.Result{Outcome: taskresult.Unchanged, Message: "没有需要清除的缩略图缓存", Detail: detail}
	}
	return taskresult.Result{Outcome: taskresult.Applied, Message: "已清除缩略图缓存，资源管理器会按需重新生成", Detail: detail}
}
