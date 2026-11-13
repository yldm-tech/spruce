package elevate

import (
	"context"
	"fmt"
	"os"
	"path/filepath"
	"regexp"
	"strings"

	"github.com/yldm-tech/spruce/internal/engine/execx"
)

// The macOS child prints its response on stdout, which "do shell script" returns to osascript and osascript prints to its own stdout. The elevated process writes nothing to disk.
const platformChannel = viaStdout

const osascriptPath = "/usr/bin/osascript"

// authPrompt is the text of the macOS authentication dialog, which replaces the default "osascript wants to make changes." line. It is passed as the first argument of the script, so it must not start with "-" (osascript would read it as an option).
const authPrompt = "Spruce 需要管理员权限来执行所选的维护任务。"

// elevateScript is the constant AppleScript that osascript runs. The prompt, the executable and its arguments arrive as the run handler's argv and are turned into shell words with "quoted form of", so no value is ever spliced into AppleScript or shell source. "without altering line endings" keeps the child's JSON byte for byte instead of turning LF into CR.
var elevateScript = []string{
	"on run argv",
	"set promptText to item 1 of argv",
	"set cmd to quoted form of (item 2 of argv)",
	"repeat with i from 3 to count of argv",
	"set cmd to cmd & space & quoted form of (item i of argv)",
	"end repeat",
	"return do shell script cmd with prompt promptText with administrator privileges without altering line endings",
	"end run",
}

// userCanceled matches the error number osascript prints when the user cancels the authentication dialog: "execution error: User canceled. (-128)". The message is localized, the number is not.
var userCanceled = regexp.MustCompile(`\(-128\)\s*$`)

// NewLauncher returns the macOS Launcher: it runs the app's own executable as root through osascript's "do shell script … with administrator privileges", which shows the system authentication dialog. runner runs osascript.
func NewLauncher(runner execx.Runner) Launcher {
	return &osascriptLauncher{runner: runner, self: selfExecutable}
}

type osascriptLauncher struct {
	runner execx.Runner
	// self returns the resolved path of the running executable.
	self func() (string, error)
}

// Launch implements Launcher.
func (l *osascriptLauncher) Launch(ctx context.Context, exe string, args []string) ([]byte, error) {
	if err := checkSelf(exe, l.self); err != nil {
		return nil, err
	}
	if !inAppBundle(exe) {
		return nil, fmt.Errorf("elevate: %q is not the executable of an app bundle (<name>.app/Contents/MacOS/<file>)", exe)
	}
	out, err := l.runner.Run(ctx, execx.Command{Name: osascriptPath, Args: osascriptArgs(exe, args)})
	if err != nil {
		if userCanceled.Match(out.Stderr) {
			return nil, ErrCancelled
		}
		return nil, err
	}
	return out.Stdout, nil
}

// osascriptArgs returns the arguments of osascript: the script, one -e per line, then the run handler's argv (prompt, exe, args...).
func osascriptArgs(exe string, args []string) []string {
	out := make([]string, 0, 2*len(elevateScript)+2+len(args))
	for _, line := range elevateScript {
		out = append(out, "-e", line)
	}
	out = append(out, authPrompt, exe)
	return append(out, args...)
}

// inAppBundle reports whether exe is <bundle>.app/Contents/MacOS/<file>, the layout of an app's main executable (mygo dev runs a real bundle too).
func inAppBundle(exe string) bool {
	macOS := filepath.Dir(exe)
	contents := filepath.Dir(macOS)
	return filepath.Base(macOS) == "MacOS" && filepath.Base(contents) == "Contents" && strings.HasSuffix(filepath.Dir(contents), ".app")
}

func samePath(a, b string) bool { return a == b }

func platformChildEnv() childEnv {
	return childEnv{
		channel:  viaStdout,
		stdout:   os.Stdout,
		stderr:   os.Stderr,
		elevated: func() bool { return os.Geteuid() == 0 },
	}
}
