package systemjunk

import (
	"context"
	"path/filepath"
	"strings"

	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/windows"
)

// platformSpecs is the plan of this platform.
var platformSpecs = windowsSpecs

// fillPlatform sets the Windows defaults of what env leaves zero: the Downloads known folder (which the user may have moved), the Recycle Bin of the system drive, and the process list.
func fillPlatform(env *Env) {
	if env.Downloads == "" {
		if dir, err := windows.KnownFolderPath(windows.FOLDERID_Downloads, windows.KF_FLAG_DEFAULT); err == nil && dir != "" {
			env.Downloads = dir
		} else {
			env.Downloads = filepath.Join(env.Home, "Downloads")
		}
	}
	if env.TrashPath == "" {
		drive := env.Getenv("SystemDrive")
		if drive == "" {
			drive = filepath.VolumeName(env.Home)
		}
		env.TrashPath = drive + `\$Recycle.Bin`
	}
	if env.Running == nil {
		env.Running = runningProcesses
	}
}

// runningProcesses lists the executable names of the running processes (chrome.exe).
func runningProcesses(ctx context.Context) (Running, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	r := Running{}
	for _, p := range procs {
		if name, err := p.NameWithContext(ctx); err == nil && name != "" {
			r[strings.ToLower(name)] = true
		}
	}
	return r, nil
}
