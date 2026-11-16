package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"os/exec"
	"strings"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/taskresult"
)

// Absolute paths of the system programs the macOS tasks run. Tasks never look a program up in PATH: the elevated child runs as root, and a PATH entry the user can write must not decide what root executes.
const (
	pathDscacheutil = "/usr/bin/dscacheutil"
	pathGetconf     = "/usr/bin/getconf"
	pathKillall     = "/usr/bin/killall"
	pathLaunchctl   = "/bin/launchctl"
	pathMdutil      = "/usr/bin/mdutil"
	pathPlutil      = "/usr/bin/plutil"
	pathQlmanage    = "/usr/bin/qlmanage"
	pathRoute       = "/sbin/route"
	pathScutil      = "/usr/sbin/scutil"
	pathSqlite3     = "/usr/bin/sqlite3"
	pathSudo        = "/usr/bin/sudo"
)

// cLocale is passed to programs whose output is parsed, so that their messages do not follow the user's language.
var cLocale = []string{"LC_ALL=C"}

// PlatformTasks returns this platform's maintenance tasks in display order.
//
// Which part of each task runs with administrator rights (NeedsAdmin tasks run their Run in the elevated child; their Precheck always runs in the app process):
//
//   - dns: elevated. The VPN probe (scutil, route) runs in Precheck as the user; the child reads the mDNSResponder label, flushes the directory cache and signals mDNSResponder.
//   - spotlight: elevated, but only when needed. Precheck reads the index state with mdutil -s as the user and stops the task as Unchanged when indexing is healthy, so a healthy Mac never sees the prompt; a disabled or transitioning index is left alone (Skipped in Precheck); only the "unknown indexing state" error reaches the child, which re-reads the state and runs mdutil -E / to rebuild the index.
//   - quicklook, icons, launchservices, databases: user only, they touch nothing but the user's own caches and data.
func PlatformTasks() []TaskImpl {
	return []TaskImpl{
		dnsTask(),
		spotlightTask(),
		quickLookTask(),
		iconsTask(),
		launchServicesTask(lsregisterCandidates),
		databasesTask(defaultDatabaseConfig()),
	}
}

// toolMissing reports whether err means the program does not exist on this system.
func toolMissing(err error) bool {
	return errors.Is(err, exec.ErrNotFound) || errors.Is(err, fs.ErrNotExist)
}

// commandFailure turns the error of a command into the task's Result: Skipped when the run was cancelled, Unavailable when the program is missing, Failed otherwise with msg as the user-facing text.
func commandFailure(msg string, cmd execx.Command, err error) taskresult.Result {
	switch {
	case errors.Is(err, context.Canceled):
		return taskresult.Result{Outcome: taskresult.Skipped, Message: "维护已中断，未执行", Detail: err.Error()}
	case toolMissing(err):
		return taskresult.Result{Outcome: taskresult.Unavailable, Message: "这台 Mac 缺少所需的系统工具，无法执行", Detail: fmt.Sprintf("%s: %v", cmd.Name, err)}
	}
	return taskresult.Result{Outcome: taskresult.Failed, Message: msg, Detail: err.Error()}
}

// combinedOutput joins a command's stdout and stderr for matching messages that a program may print on either stream.
func combinedOutput(out execx.Output) string {
	return string(out.Stdout) + "\n" + string(out.Stderr)
}

// joinDetail joins non-empty diagnostic lines for Result.Detail.
func joinDetail(lines ...string) string {
	var kept []string
	for _, l := range lines {
		if l = strings.TrimSpace(l); l != "" {
			kept = append(kept, l)
		}
	}
	return strings.Join(kept, "; ")
}
