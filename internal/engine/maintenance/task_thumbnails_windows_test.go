package maintenance

import (
	"context"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"testing"

	"golang.org/x/sys/windows"

	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// thumbDir creates an Explorer cache directory with the given files and returns it with a winSystem pointing at it. Files named in locked fail to delete with a sharing violation, as when Explorer holds them open.
func thumbDir(t *testing.T, files []string, locked ...string) (string, *winSystem) {
	t.Helper()
	dir := t.TempDir()
	for _, f := range files {
		if err := os.WriteFile(filepath.Join(dir, f), []byte("x"), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	sys := newTestWinSystem(t)
	sys.explorerCacheDir = func() (string, error) { return dir, nil }
	sys.removeFile = func(root *os.Root, name string) error {
		if slices.Contains(locked, name) {
			return &fs.PathError{Op: "remove", Path: name, Err: windows.ERROR_SHARING_VIOLATION}
		}
		return root.Remove(name)
	}
	return dir, sys
}

func remaining(t *testing.T, dir string) []string {
	t.Helper()
	entries, err := os.ReadDir(dir)
	if err != nil {
		t.Fatal(err)
	}
	var out []string
	for _, e := range entries {
		out = append(out, e.Name())
	}
	return out
}

func TestThumbnailsRemovesOnlyThumbcache(t *testing.T) {
	dir, sys := thumbDir(t, []string{"thumbcache_32.db", "thumbcache_idx.db", "Thumbcache_256.DB", "iconcache_32.db", "notes.txt"})
	if err := os.Mkdir(filepath.Join(dir, "thumbcache_dir.db"), 0o700); err != nil {
		t.Fatal(err)
	}
	res := winThumbnailsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Applied || res.Message != "已清除 3 个缩略图缓存文件" {
		t.Fatalf("result %+v", res)
	}
	got := remaining(t, dir)
	want := []string{"iconcache_32.db", "notes.txt", "thumbcache_dir.db"}
	if !slices.Equal(got, want) {
		t.Errorf("left %v, want %v", got, want)
	}
}

func TestThumbnailsReportsLockedFiles(t *testing.T) {
	dir, sys := thumbDir(t, []string{"thumbcache_32.db", "thumbcache_idx.db"}, "thumbcache_idx.db")
	res := winThumbnailsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Applied {
		t.Fatalf("result %+v", res)
	}
	if want := "已清除 1 个缩略图缓存文件，另有 1 个正被资源管理器占用，重新登录 Windows 后再运行一次可全部清除"; res.Message != want {
		t.Errorf("message %q, want %q", res.Message, want)
	}
	if got := remaining(t, dir); !slices.Equal(got, []string{"thumbcache_idx.db"}) {
		t.Errorf("left %v", got)
	}
}

func TestThumbnailsAllLockedIsSkipped(t *testing.T) {
	_, sys := thumbDir(t, []string{"thumbcache_32.db"}, "thumbcache_32.db")
	res := winThumbnailsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Skipped {
		t.Fatalf("result %+v, want skipped", res)
	}
}

func TestThumbnailsOtherErrorsFail(t *testing.T) {
	_, sys := thumbDir(t, []string{"thumbcache_32.db"})
	sys.removeFile = func(*os.Root, string) error { return windows.ERROR_ACCESS_DENIED }
	res := winThumbnailsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Failed || res.Detail == "" {
		t.Fatalf("result %+v, want failed with detail", res)
	}
}

func TestThumbnailsNothingToDo(t *testing.T) {
	_, sys := thumbDir(t, []string{"iconcache_32.db"})
	res := winThumbnailsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Unchanged {
		t.Fatalf("result %+v, want unchanged", res)
	}

	sys.explorerCacheDir = func() (string, error) { return filepath.Join(t.TempDir(), "missing"), nil }
	res = winThumbnailsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Unchanged {
		t.Fatalf("missing directory: result %+v, want unchanged", res)
	}
}

func TestThumbnailsRunsNoCommands(t *testing.T) {
	_, sys := thumbDir(t, []string{"thumbcache_32.db"})
	f := execxtest.New()
	winThumbnailsTask(sys).Run(context.Background(), testEnv(f))
	if calls := f.Calls(); len(calls) != 0 {
		t.Errorf("the thumbnail task must not run programs (such as taskkill explorer.exe): %v", calls)
	}
}
