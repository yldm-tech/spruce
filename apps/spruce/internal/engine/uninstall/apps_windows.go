package uninstall

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"golang.org/x/sys/windows"

	"spruce/apps/spruce/internal/engine/cleanup"
)

// Workers measuring install folders and extracting icons at once.
const (
	sizeWorkers = 3
	iconWorkers = 2
)

// lateRemoval is how long RunUninstaller keeps reading the registry after the uninstaller exited, for uninstallers that remove their entry a moment later.
const lateRemoval = 3 * time.Second

// platformApp is what only Windows knows of an app.
type platformApp struct {
	// entry is the program's registry entry.
	entry Entry
	// iconIndex is the DisplayIcon index.
	iconIndex int
}

// listApps reads the programs of the Uninstall keys.
func (e *Engine) listApps(ctx context.Context) ([]App, error) {
	entries, err := readEntries()
	if err != nil {
		return nil, err
	}
	apps := make([]App, 0, len(entries))
	for _, en := range entries {
		if err := ctx.Err(); err != nil {
			return nil, err
		}
		a := App{
			ID:            en.ID(),
			Name:          en.DisplayName,
			Version:       en.DisplayVersion,
			Publisher:     en.Publisher,
			InstallDate:   en.InstallDate,
			EstimatedSize: en.EstimatedSize,
			iconFile:      en.IconPath,
			platformApp:   platformApp{entry: en, iconIndex: en.IconIndex},
		}
		if loc := en.InstallLocation; loc != "" && filepath.IsAbs(loc) && isRealDir(loc) && !broadFolder(loc) {
			a.Path = filepath.Clean(loc)
		}
		apps = append(apps, a)
	}
	return apps, nil
}

// details sends each program's icon and size: the size of its install folder when the registry names one, else the registry's EstimatedSize. Windows keeps no record of when a program was last used, so no usage is sent.
func (e *Engine) details(ctx context.Context, apps []App, send func(Details)) error {
	var wg sync.WaitGroup
	var keepMu sync.Mutex
	keep := make(map[string]bool)
	wg.Go(func() {
		pool(ctx, apps, iconWorkers, func(a App) {
			if a.iconFile == "" || !filepath.IsAbs(a.iconFile) {
				return
			}
			mod, ok := iconSourceTime(a.iconFile)
			if !ok {
				return
			}
			key := iconKey(a.ID, a.iconFile, mod, a.iconIndex)
			keepMu.Lock()
			keep[key] = true
			keepMu.Unlock()
			url, err := e.cachedIcon(key, func(dst string) error {
				b, err := extractIconPNG(a.iconFile, a.iconIndex)
				if err != nil {
					return err
				}
				return os.WriteFile(dst, b, 0o600)
			})
			if err == nil {
				send(Details{ID: a.ID, Icon: url})
			}
		})
	})
	wg.Go(func() {
		pool(ctx, apps, sizeWorkers, func(a App) {
			if a.Path != "" {
				if n, err := dirSize(ctx, a.Path); err == nil || n > 0 {
					send(Details{ID: a.ID, HasSize: true, Size: n})
					return
				}
			}
			if a.EstimatedSize > 0 {
				send(Details{ID: a.ID, HasSize: true, Size: a.EstimatedSize})
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

// bundleCandidate: on Windows the program's own uninstaller removes it, so there is no bundle to move.
func (e *Engine) bundleCandidate(App) (Candidate, bool) { return Candidate{}, false }

// leftovers runs the Windows rules (WindowsLeftovers) with the user's folders.
func (e *Engine) leftovers(_ context.Context, app App) ([]Candidate, int64) {
	appData, _ := windows.KnownFolderPath(windows.FOLDERID_RoamingAppData, windows.KF_FLAG_DEFAULT)
	local, _ := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, windows.KF_FLAG_DEFAULT)
	return WindowsLeftovers(appData, local, app.entry), 0
}

// runUninstaller starts the program's uninstaller, waits for it and reads the registry again, a few times over lateRemoval if the entry is still there.
func (e *Engine) runUninstaller(ctx context.Context, app App, started func()) (UninstallerResult, error) {
	sysRoot, err := windows.GetSystemWindowsDirectory()
	if err != nil {
		sysRoot = `C:\Windows`
	}
	cmd, err := UninstallCommand(app.entry, sysRoot, regularFile)
	if err != nil {
		return UninstallerResult{}, cleanup.NewUserError("「"+app.Name+"」没有登记卸载程序", err)
	}
	process, err := shellExecute(cmd.Program, cmd.Params)
	switch {
	case errors.Is(err, errUACDeclined):
		return UninstallerResult{}, cleanup.NewUserError("没有获得管理员授权，卸载程序没有运行", err)
	case errors.Is(err, windows.ERROR_FILE_NOT_FOUND), errors.Is(err, windows.ERROR_PATH_NOT_FOUND):
		return UninstallerResult{}, cleanup.NewUserError("找不到「"+app.Name+"」的卸载程序，它可能已经被删除了", err)
	case err != nil:
		return UninstallerResult{}, cleanup.NewUserError("没能打开「"+app.Name+"」的卸载程序", err)
	}
	started()
	res := UninstallerResult{Waited: true}
	if process != 0 {
		res.Waited, err = waitExit(ctx, process)
		windows.CloseHandle(process)
		if err != nil {
			return UninstallerResult{}, err
		}
	}
	deadline := time.Now().Add(lateRemoval)
	for {
		removed, err := e.removedNow(app)
		if err != nil {
			return res, err
		}
		if removed || !res.Waited || time.Now().After(deadline) || ctx.Err() != nil {
			res.Removed = removed
			return res, nil
		}
		select {
		case <-ctx.Done():
		case <-time.After(500 * time.Millisecond):
		}
	}
}

// removedNow reports whether the program's entry is gone from the registry.
func (e *Engine) removedNow(app App) (bool, error) {
	present, err := entryPresent(app.entry.Source, app.entry.DisplayName)
	return !present, err
}

// broadFolder reports whether loc is a drive root or a folder that holds many programs (an InstallLocation some installers set by mistake), which must not be measured or matched as the program's own.
func broadFolder(loc string) bool {
	loc = filepath.Clean(loc)
	if filepath.Dir(loc) == loc {
		return true
	}
	local := os.Getenv("LOCALAPPDATA")
	for _, p := range []string{
		os.Getenv("ProgramFiles"), os.Getenv("ProgramFiles(x86)"), os.Getenv("ProgramW6432"), os.Getenv("SystemRoot"),
		os.Getenv("ProgramData"), os.Getenv("USERPROFILE"), os.Getenv("APPDATA"), local, filepath.Join(local, "Programs"),
	} {
		if p != "" && strings.EqualFold(filepath.Clean(p), loc) {
			return true
		}
	}
	return false
}

func regularFile(p string) bool {
	st, err := os.Stat(p)
	return err == nil && st.Mode().IsRegular()
}
