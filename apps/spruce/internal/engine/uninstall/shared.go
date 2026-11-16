package uninstall

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"

	"github.com/shirou/gopsutil/v4/process"

	"spruce/apps/spruce/internal/engine/fsx"
)

// systemProcesses returns the executable path of every process this user may inspect.
func systemProcesses(ctx context.Context) ([]string, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, fmt.Errorf("uninstall: list processes: %w", err)
	}
	exes := make([]string, 0, len(procs))
	for _, p := range procs {
		if exe, err := p.ExeWithContext(ctx); err == nil && exe != "" {
			exes = append(exes, exe)
		}
	}
	return exes, nil
}

// maxIconBytes bounds a cached icon read back from disk.
const maxIconBytes = 1 << 20

// iconKey names the cache file of an icon made from source, as it was at modTime, for the app identified by owner.
func iconKey(owner, source string, modTime int64, index int) string {
	sum := sha256.Sum256(fmt.Appendf(nil, "%s\x00%s\x00%d\x00%d", owner, source, modTime, index))
	return hex.EncodeToString(sum[:12]) + ".png"
}

// cachedIcon returns the data URL of the icon cached as key, making it with mk on a miss. mk writes the PNG to the path it is given, which Engine then renames into place, so a half-written icon is never read.
func (e *Engine) cachedIcon(key string, mk func(dst string) error) (string, error) {
	if e.cfg.IconCache == "" {
		return "", errors.New("uninstall: no icon cache")
	}
	final := filepath.Join(e.cfg.IconCache, key)
	if b, err := readSmall(final); err == nil {
		return dataURL(b), nil
	}
	if err := os.MkdirAll(e.cfg.IconCache, 0o700); err != nil {
		return "", fmt.Errorf("uninstall: icon cache: %w", err)
	}
	tmp, err := os.CreateTemp(e.cfg.IconCache, "icon-*.png.tmp")
	if err != nil {
		return "", fmt.Errorf("uninstall: icon cache: %w", err)
	}
	tmpPath := tmp.Name()
	_ = tmp.Close()
	defer os.Remove(tmpPath)
	if err := mk(tmpPath); err != nil {
		return "", err
	}
	b, err := readSmall(tmpPath)
	if err != nil {
		return "", err
	}
	if !isPNG(b) {
		return "", errors.New("uninstall: the converted icon is not a PNG")
	}
	if err := os.Rename(tmpPath, final); err != nil {
		return "", fmt.Errorf("uninstall: icon cache: %w", err)
	}
	return dataURL(b), nil
}

// pruneIcons removes the cached icons not in keep (those of app versions no longer installed).
func (e *Engine) pruneIcons(keep map[string]bool) {
	if e.cfg.IconCache == "" {
		return
	}
	entries, err := os.ReadDir(e.cfg.IconCache)
	if err != nil {
		return
	}
	for _, ent := range entries {
		name := ent.Name()
		if ent.Type().IsRegular() && (strings.HasSuffix(name, ".png") || strings.HasSuffix(name, ".tmp")) && !keep[name] {
			_ = os.Remove(filepath.Join(e.cfg.IconCache, name))
		}
	}
}

func readSmall(path string) ([]byte, error) {
	info, err := os.Lstat(path)
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() || info.Size() == 0 || info.Size() > maxIconBytes {
		return nil, fmt.Errorf("uninstall: %s is not a usable icon: %w", path, fs.ErrInvalid)
	}
	return os.ReadFile(path)
}

func isPNG(b []byte) bool {
	return len(b) > 8 && string(b[:8]) == "\x89PNG\r\n\x1a\n"
}

func dataURL(png []byte) string {
	return "data:image/png;base64," + base64.StdEncoding.EncodeToString(png)
}

// dirSize returns the space path takes on disk; see fsx.DirSize.
func dirSize(ctx context.Context, path string) (int64, error) {
	res, err := fsx.DirSize(ctx, path, fsx.SizeOptions{Workers: 4})
	return res.Bytes, err
}

// pool runs fn for every app with at most n at once, until ctx ends.
func pool(ctx context.Context, apps []App, n int, fn func(App)) {
	ch := make(chan App)
	var wg sync.WaitGroup
	for range n {
		wg.Go(func() {
			for a := range ch {
				fn(a)
			}
		})
	}
	for _, a := range apps {
		select {
		case ch <- a:
		case <-ctx.Done():
		}
		if ctx.Err() != nil {
			break
		}
	}
	close(ch)
	wg.Wait()
}
