package uninstall

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"howett.net/plist"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/execx"
)

// iconSize is the edge, in pixels, icons are converted to: sharp at the page's largest tile (56 px) on a 2x display.
const iconSize = 128

// Workers measuring sizes and converting icons at once. Sizes walk whole bundles; icons start sips.
const (
	sizeWorkers = 3
	iconWorkers = 4
)

// maxInfoPlist bounds the Info.plist read from a bundle.
const maxInfoPlist = 4 << 20

// platformApp is what only macOS knows of an app.
type platformApp struct {
	// names are the exact names leftovers may match by: the display name and the bundle's file name.
	names []string
}

func (e *Engine) appDirs() []string {
	if e.cfg.AppDirs != nil {
		return e.cfg.AppDirs
	}
	return []string{"/Applications", filepath.Join(e.cfg.Home, "Applications")}
}

// listApps finds the .app bundles in the app folders and in their direct subfolders, without descending into bundles or following symlinks.
func (e *Engine) listApps(ctx context.Context) ([]App, error) {
	var apps []App
	read := 0
	var firstErr error
	for _, dir := range e.appDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			if !errors.Is(err, fs.ErrNotExist) && firstErr == nil {
				firstErr = err
			}
			continue
		}
		read++
		for _, ent := range entries {
			if err := ctx.Err(); err != nil {
				return nil, err
			}
			if ent.Type() != fs.ModeDir || strings.HasPrefix(ent.Name(), ".") {
				continue
			}
			p := filepath.Join(dir, ent.Name())
			if isBundleName(ent.Name()) {
				if a, ok := e.readBundle(p); ok {
					apps = append(apps, a)
				}
				continue
			}
			sub, err := os.ReadDir(p)
			if err != nil {
				continue
			}
			for _, s := range sub {
				if s.Type() == fs.ModeDir && isBundleName(s.Name()) && !strings.HasPrefix(s.Name(), ".") {
					if a, ok := e.readBundle(filepath.Join(p, s.Name())); ok {
						apps = append(apps, a)
					}
				}
			}
		}
	}
	if read == 0 && firstErr != nil {
		return nil, fmt.Errorf("uninstall: read the app folders: %w", firstErr)
	}
	return apps, nil
}

func isBundleName(name string) bool {
	return len(name) > len(".app") && strings.EqualFold(filepath.Ext(name), ".app")
}

// readBundle describes the bundle at path. It reports false for bundles the page must not offer: Apple's own and Spruce.
func (e *Engine) readBundle(path string) (App, bool) {
	file := strings.TrimSuffix(filepath.Base(path), filepath.Ext(path))
	info := readInfoPlist(filepath.Join(path, "Contents", "Info.plist"))
	id := info["CFBundleIdentifier"]
	if strings.HasPrefix(id, "com.apple.") || id == SelfBundleID || strings.HasPrefix(id, SelfBundleID+".") {
		return App{}, false
	}
	if e.cfg.Self != "" && inside(e.cfg.Self, path) {
		return App{}, false
	}
	name := firstNonEmpty(info["CFBundleDisplayName"], info["CFBundleName"], file)
	app := App{
		ID:       cleanup.ItemID(path),
		Name:     name,
		Version:  firstNonEmpty(info["CFBundleShortVersionString"], info["CFBundleVersion"]),
		BundleID: id,
		Path:     path,
		iconFile: bundleIcon(path, info["CFBundleIconFile"], info["CFBundleIconName"]),
	}
	app.names = uniqueNames(name, file)
	if st, err := os.Lstat(filepath.Join(path, "Contents", "_MASReceipt", "receipt")); err == nil && st.Mode().IsRegular() {
		app.AppStore = true
	}
	return app, true
}

// readInfoPlist returns the string values of the top level of an Info.plist (XML or binary); an unreadable one gives an empty map.
func readInfoPlist(path string) map[string]string {
	out := make(map[string]string)
	st, err := os.Lstat(path)
	if err != nil || !st.Mode().IsRegular() || st.Size() > maxInfoPlist {
		return out
	}
	b, err := os.ReadFile(path)
	if err != nil {
		return out
	}
	var raw map[string]any
	if _, err := plist.Unmarshal(b, &raw); err != nil {
		return out
	}
	for k, v := range raw {
		if s, ok := v.(string); ok {
			out[k] = strings.TrimSpace(s)
		}
	}
	return out
}

// bundleIcon resolves the bundle's .icns in Contents/Resources from CFBundleIconFile (with or without its extension), else CFBundleIconName; "" when the icon is only in Assets.car.
func bundleIcon(bundle, iconFile, iconName string) string {
	res := filepath.Join(bundle, "Contents", "Resources")
	for _, n := range []string{iconFile, iconName} {
		if n == "" || strings.ContainsAny(n, `/\`) {
			continue
		}
		if filepath.Ext(n) == "" {
			n += ".icns"
		}
		p := filepath.Join(res, n)
		if st, err := os.Lstat(p); err == nil && st.Mode().IsRegular() {
			return p
		}
	}
	return ""
}

func (e *Engine) bundleCandidate(app App) (Candidate, bool) {
	for _, root := range e.appDirs() {
		if inside(app.Path, root) {
			return Candidate{Path: app.Path, Kind: KindApp, Root: root, Selected: true}, true
		}
	}
	return Candidate{}, false
}

// details sends when each app was last used (one mdls per batch), then its icon and size as they are made, from two pools of workers. Once every icon was made it drops the cached icons of apps no longer installed.
func (e *Engine) details(ctx context.Context, apps []App, send func(Details)) error {
	for _, d := range e.lastUsed(ctx, apps) {
		send(d)
	}
	var wg sync.WaitGroup
	var keepMu sync.Mutex
	keep := make(map[string]bool)
	wg.Go(func() {
		pool(ctx, apps, iconWorkers, func(a App) {
			key, url := e.bundleIconURL(ctx, a)
			if key != "" {
				keepMu.Lock()
				keep[key] = true
				keepMu.Unlock()
			}
			if url != "" {
				send(Details{ID: a.ID, Icon: url})
			}
		})
	})
	wg.Go(func() {
		pool(ctx, apps, sizeWorkers, func(a App) {
			res, err := dirSize(ctx, a.Path)
			if err == nil || res > 0 {
				send(Details{ID: a.ID, HasSize: true, Size: res})
			}
		})
	})
	wg.Wait()
	if err := ctx.Err(); err != nil {
		return err
	}
	e.pruneIcons(keep)
	return nil
}

// bundleIconURL returns the cache key and data URL of the app's icon, converting its .icns with sips on a cache miss. sips reads every flavour of .icns (PNG, JPEG 2000 and the old RLE bitmaps), which no maintained pure-Go decoder does, and the cache means it runs once per app version.
func (e *Engine) bundleIconURL(ctx context.Context, a App) (string, string) {
	if a.iconFile == "" {
		return "", ""
	}
	st, err := os.Stat(a.iconFile)
	if err != nil {
		return "", ""
	}
	key := iconKey(a.BundleID+"\x00"+a.Path, a.iconFile, st.ModTime().UnixNano(), 0)
	url, err := e.cachedIcon(key, func(dst string) error {
		_, err := e.cfg.Runner.Run(ctx, execx.Command{
			Name:    "/usr/bin/sips",
			Args:    []string{"-s", "format", "png", "--resampleHeightWidthMax", fmt.Sprint(iconSize), a.iconFile, "--out", dst},
			Timeout: 20 * time.Second,
		})
		return err
	})
	if err != nil {
		return key, ""
	}
	return key, url
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v != "" {
			return v
		}
	}
	return ""
}

func uniqueNames(names ...string) []string {
	var out []string
	for _, n := range names {
		if n != "" && !containsFold(out, n) {
			out = append(out, n)
		}
	}
	return out
}

func containsFold(list []string, s string) bool {
	for _, v := range list {
		if strings.EqualFold(v, s) {
			return true
		}
	}
	return false
}

// leftovers runs the macOS rules (leftovers_darwin.go).
func (e *Engine) leftovers(ctx context.Context, app App) ([]Candidate, int64) {
	return macLeftovers(ctx, filepath.Join(e.cfg.Home, "Library"), app)
}

func (e *Engine) runUninstaller(context.Context, App, func()) (UninstallerResult, error) {
	return UninstallerResult{}, errors.ErrUnsupported
}

func (e *Engine) removedNow(App) (bool, error) {
	return false, errors.ErrUnsupported
}
