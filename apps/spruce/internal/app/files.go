package app

import (
	"context"
	"errors"
	"io/fs"
	"log/slog"
	"runtime"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/fsx"
	"spruce/apps/spruce/internal/engine/trash"
)

// Files is the controller for single files and the Trash, shared by every page.
type Files struct {
	runner execx.Runner
	bin    trash.Bin
	log    *slog.Logger
}

// NewFiles returns the service revealing through runner and measuring and emptying bin (trash.System() in the app).
func NewFiles(runner execx.Runner, bin trash.Bin, log *slog.Logger) *Files {
	return &Files{runner: runner, bin: bin, log: log}
}

// binName is what the system calls its Trash.
func binName() string {
	if runtime.GOOS == "windows" {
		return "回收站"
	}
	return "废纸篓"
}

// Reveal shows path selected in Finder or Explorer.
func (f *Files) Reveal(ctx context.Context, path string) error {
	if err := fsx.Reveal(ctx, f.runner, path); err != nil {
		if errors.Is(err, fs.ErrNotExist) {
			return errors.New("这个项目已经不存在了")
		}
		f.log.Warn("reveal failed", "path", path, "err", err)
		if runtime.GOOS == "windows" {
			return errors.New("无法在资源管理器中显示")
		}
		return errors.New("无法在访达中显示")
	}
	return nil
}

// TrashSize returns how much the Trash (Recycle Bin) holds.
func (f *Files) TrashSize(ctx context.Context) (trash.TrashUsage, error) {
	u, err := f.bin.Size(ctx)
	if err != nil {
		return trash.TrashUsage{}, f.trashError("measure trash", err)
	}
	return u, nil
}

// EmptyTrash deletes everything in the Trash for good and returns what it freed. It cannot be undone; the page confirms first.
func (f *Files) EmptyTrash(ctx context.Context) (trash.TrashUsage, error) {
	u, err := f.bin.Empty(ctx)
	if err != nil {
		return u, f.trashError("empty trash", err)
	}
	return u, nil
}

func (f *Files) trashError(op string, err error) error {
	f.log.Warn(op, "err", err)
	if errors.Is(err, fs.ErrPermission) {
		return errors.New("需要为 Spruce 开启「完全磁盘访问权限」才能读取" + binName())
	}
	return errors.New("无法读取" + binName() + "，详情已记录到日志")
}
