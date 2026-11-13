package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/sys/windows"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// PlatformTasks returns this platform's maintenance tasks in display order.
//
// Windows 10/11 tasks, and why each is shaped the way it is:
//
//   - dns: "ipconfig /flushdns" works for a standard user, so it runs in the app process without a UAC prompt.
//   - search: checks the Windows Search service (WSearch) from the app process and only asks for administrator rights when the service is stopped although it is set to start automatically. It never rebuilds the index: a rebuild re-indexes every file for hours and leaves search incomplete meanwhile, which is not a maintenance step to run unasked.
//   - thumbnails: deletes the per-user thumbcache_*.db files, as Disk Cleanup's "Thumbnails" handler does. Files Explorer holds open are reported, never forced: Explorer is not restarted.
//   - icons: "ie4uinit.exe -show" asks the shell to refresh its icon cache in place. Deleting iconcache_*.db would need an Explorer restart, so it is not done.
//   - associations: removes only HKCU "Open with" entries (FileExts\<ext>\OpenWithList and OpenWithProgids) that point to programs or ProgIDs that no longer exist on a fixed drive. UserChoice (the hash-protected default app) is never opened for writing.
func PlatformTasks() []TaskImpl {
	return winTasks(newWinSystem())
}

// winTasks builds the catalog over sys, so tests can substitute the operating system.
func winTasks(sys *winSystem) []TaskImpl {
	return []TaskImpl{
		winDNSTask(sys),
		winSearchTask(sys),
		winThumbnailsTask(sys),
		winIconsTask(sys),
		winAssociationsTask(sys),
	}
}

// winSystem is what the Windows tasks need from the operating system besides Env.Runner. newWinSystem fills it with the real implementations; tests substitute fakes.
type winSystem struct {
	// systemDir returns the System32 directory. Tools are run by absolute path from there, so a program planted earlier in PATH is never picked.
	systemDir func() (string, error)
	// explorerCacheDir returns %LOCALAPPDATA%\Microsoft\Windows\Explorer, where the thumbnail and icon caches live.
	explorerCacheDir func() (string, error)
	// removeFile deletes name inside root. os.Root keeps the deletion inside the cache directory even if an entry is a link.
	removeFile func(root *os.Root, name string) error
	// services queries and starts Windows services.
	services winServiceManager
	// registry reads and edits the registry.
	registry winRegistry
	// pathState tells whether a program path exists, see winPathState.
	pathState func(path string) winPathState
	// pollInterval and startTimeout bound the wait for a service to reach the running state.
	pollInterval time.Duration
	startTimeout time.Duration
}

func newWinSystem() *winSystem {
	return &winSystem{
		systemDir: windows.GetSystemDirectory,
		explorerCacheDir: func() (string, error) {
			dir, err := windows.KnownFolderPath(windows.FOLDERID_LocalAppData, 0)
			if err != nil {
				return "", err
			}
			return filepath.Join(dir, "Microsoft", "Windows", "Explorer"), nil
		},
		removeFile:   (*os.Root).Remove,
		services:     scmServices{},
		registry:     osRegistry{},
		pathState:    osPathState,
		pollInterval: 500 * time.Millisecond,
		startTimeout: 30 * time.Second,
	}
}

// systemTool returns the absolute path of a program in System32.
func (s *winSystem) systemTool(name string) (string, error) {
	dir, err := s.systemDir()
	if err != nil {
		return "", fmt.Errorf("locate the system directory: %w", err)
	}
	return filepath.Join(dir, name), nil
}

// winToolFailure turns a failed tool run into a Result: Unavailable when the program is missing, Skipped when the run was cancelled, Failed otherwise. failed is the user-facing message for the Failed case.
func winToolFailure(out execx.Output, err error, failed string) taskresult.Result {
	// err already names the command (execx wraps every error with it).
	detail := err.Error()
	// Console tools such as ipconfig print their errors on stdout, in the console code page; keep the tail as valid UTF-8 for the history.
	if stdout := execx.Tail(out.Stdout, 512); stdout != "" {
		detail += "; stdout: " + strings.ToValidUTF8(stdout, "?")
	}
	switch {
	case errors.Is(err, exec.ErrNotFound), errors.Is(err, fs.ErrNotExist):
		return taskresult.Result{Outcome: taskresult.Unavailable, Message: "此版本的 Windows 缺少所需的系统工具", Detail: detail}
	case errors.Is(err, context.Canceled):
		return taskresult.Result{Outcome: taskresult.Skipped, Message: "维护已中断", Detail: detail}
	}
	return taskresult.Result{Outcome: taskresult.Failed, Message: failed, Detail: detail}
}
