package installers

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"path/filepath"
	"syscall"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"howett.net/plist"
)

// fillPlatform sets the macOS defaults of what env leaves zero.
func fillPlatform(env *Env) {
	if env.Downloads == "" {
		env.Downloads = filepath.Join(env.Home, "Downloads")
	}
	if env.Mounted == nil && env.Runner != nil {
		runner := env.Runner
		env.Mounted = func(ctx context.Context) ([]string, error) { return attachedImages(ctx, runner) }
	}
}

// attachedImages lists the disk images attached now, from `hdiutil info -plist`.
func attachedImages(ctx context.Context, runner execx.Runner) ([]string, error) {
	if runner == nil {
		return nil, errors.New("installers: no runner")
	}
	out, err := runner.Run(ctx, execx.Command{Name: "/usr/bin/hdiutil", Args: []string{"info", "-plist"}, Timeout: 15 * time.Second})
	if err != nil {
		return nil, err
	}
	return parseHdiutilInfo(out.Stdout)
}

// hdiutilInfo is the part of `hdiutil info -plist` this package reads.
type hdiutilInfo struct {
	Images []struct {
		ImagePath string `plist:"image-path"`
	} `plist:"images"`
}

// parseHdiutilInfo returns the image paths of `hdiutil info -plist`.
func parseHdiutilInfo(b []byte) ([]string, error) {
	var info hdiutilInfo
	if _, err := plist.Unmarshal(b, &info); err != nil {
		return nil, fmt.Errorf("installers: read hdiutil info: %w", err)
	}
	var out []string
	for _, img := range info.Images {
		if img.ImagePath != "" {
			out = append(out, img.ImagePath)
		}
	}
	return out, nil
}

// ufHidden is UF_HIDDEN of <sys/stat.h>: Finder hides the item.
const ufHidden = 0x8000

// hidden reports whether Finder hides the item (chflags hidden).
func hidden(info fs.FileInfo) bool {
	st, ok := info.Sys().(*syscall.Stat_t)
	return ok && st.Flags&ufHidden != 0
}

// created is the file's birth time, or the zero time.
func created(info fs.FileInfo) time.Time {
	st, ok := info.Sys().(*syscall.Stat_t)
	if !ok {
		return time.Time{}
	}
	return time.Unix(st.Birthtimespec.Unix())
}
