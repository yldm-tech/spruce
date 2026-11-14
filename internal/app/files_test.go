package app

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/trash"
)

type fakeBin struct {
	usage trash.TrashUsage
	err   error
	empty int
}

func (b *fakeBin) MoveToTrash(context.Context, []string) ([]trash.Outcome, error) {
	return nil, errors.New("not used")
}

func (b *fakeBin) Size(context.Context) (trash.TrashUsage, error) { return b.usage, b.err }

func (b *fakeBin) Empty(context.Context) (trash.TrashUsage, error) {
	b.empty++
	return b.usage, b.err
}

func TestFilesTrash(t *testing.T) {
	bin := &fakeBin{usage: trash.TrashUsage{Bytes: 1 << 20, Items: 3}}
	f := NewFiles(nil, bin, slog.New(slog.DiscardHandler))
	if u, err := f.TrashSize(t.Context()); err != nil || u.Items != 3 {
		t.Errorf("TrashSize = %+v, %v", u, err)
	}
	if u, err := f.EmptyTrash(t.Context()); err != nil || u.Bytes != 1<<20 || bin.empty != 1 {
		t.Errorf("EmptyTrash = %+v, %v", u, err)
	}
	bin.err = trash.ErrNoAccess
	if _, err := f.TrashSize(t.Context()); err == nil || err.Error() != "需要为 Spruce 开启「完全磁盘访问权限」才能读取"+binName() {
		t.Errorf("no access: %v", err)
	}
	bin.err = errors.New("io")
	if _, err := f.EmptyTrash(t.Context()); err == nil || err.Error() != "无法读取"+binName()+"，详情已记录到日志" {
		t.Errorf("other error: %v", err)
	}
	if err := f.Reveal(t.Context(), filepath.Join(t.TempDir(), "gone")); err == nil || err.Error() != "这个项目已经不存在了" {
		t.Errorf("reveal missing: %v", err)
	}
}
