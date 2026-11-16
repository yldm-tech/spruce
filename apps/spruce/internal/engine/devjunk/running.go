package devjunk

import (
	"context"
	"runtime"
	"strings"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// vscodeRunning reports whether VS Code runs for any user this process may inspect: on macOS a process started from inside "Visual Studio Code.app", on Windows a Code.exe. When the processes cannot be listed it says yes, so the VS Code caches start unchecked rather than be deleted under a running editor.
func vscodeRunning(ctx context.Context) bool {
	ctx, cancel := context.WithTimeout(ctx, 5*time.Second)
	defer cancel()
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return true
	}
	for _, p := range procs {
		if runtime.GOOS == "windows" {
			if name, err := p.NameWithContext(ctx); err == nil && strings.EqualFold(name, "Code.exe") {
				return true
			}
			continue
		}
		if exe, err := p.ExeWithContext(ctx); err == nil && strings.Contains(exe, "/Visual Studio Code.app/Contents/") {
			return true
		}
	}
	return false
}
