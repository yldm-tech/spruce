// Package elevate runs maintenance tasks that need administrator rights in a second, elevated copy of the app's own executable, behind one system prompt per run.
//
// # Parent side
//
// Elevator is what the maintenance runner depends on. ProcessElevator implements it: it validates the task IDs, asks its Launcher to start the app's own executable elevated as
//
//	<executable> --spruce-elevated --tasks <id>[,<id>...]                (macOS)
//	<executable> --spruce-elevated --tasks <id>[,<id>...] --out <dir>    (Windows)
//
// waits for it to exit and reads the child's Response back: from the child's standard output on macOS, from <dir>/response.json on Windows, where <dir> is a fresh directory the parent made with os.MkdirTemp in the system temp directory and removes afterwards. Results are matched to the requested IDs and returned in their order. A declined prompt is ErrCancelled.
//
// # Launchers
//
// NewLauncher(runner) returns the Launcher of the running platform; main.go passes it to New. Both launchers refuse to elevate anything but the running executable (os.Executable resolved through symlinks), and the macOS one also requires it to be the main executable of an app bundle (<name>.app/Contents/MacOS/<file>; mygo dev runs a real bundle too).
//
// macOS (elevate_darwin.go): /usr/bin/osascript runs a constant AppleScript whose run handler receives the prompt text, the executable and its arguments as argv, turns each into a shell word with "quoted form of", and calls `do shell script <command> with prompt <text> with administrator privileges without altering line endings`. No value is ever spliced into AppleScript or shell source. The system authentication dialog shows the prompt text ("Spruce 需要管理员权限来执行所选的维护任务。") with user name and password fields; the command then runs as root (euid 0) and its standard output comes back as osascript's output. osascript error -128 ("User canceled") is ErrCancelled.
//
// Windows (elevate_windows.go): ShellExecuteExW with the "runas" verb, SEE_MASK_NOCLOSEPROCESS | SEE_MASK_NOASYNC and SW_HIDE, owned by the app's foreground window when it has one, so the UAC prompt (consent for an administrator, credentials for a standard user) names the Spruce executable. Launch then waits on the process handle, polling ctx, and reads the exit code. ERROR_CANCELLED (1223) is ErrCancelled. x/sys/windows binds ShellExecuteW but not ShellExecuteExW, which alone returns the process handle, so the SHELLEXECUTEINFOW struct and the call are declared here.
//
// # Child side (main.go)
//
// The elevated child is the same executable started with Flag as its first argument. main.go checks IsChild(os.Args[1:]) first thing, before any mygo call (importing mygo only locks the main thread and registers bindings), and exits with the code ServeChild returns:
//
//	if elevate.IsChild(os.Args[1:]) {
//		os.Exit(elevate.ServeChild(ctx, os.Args[1:], adminTaskIDs, maintenance.ElevatedHandler(tasks, env)))
//	}
//
// ServeChild treats its arguments as untrusted, since any process can start the executable with Flag:
//
//   - The arguments must match the grammar above exactly. The request carries task IDs only, never commands, paths to run or options, and every ID must be in the allowlist main.go passes (the NeedsAdmin task IDs of the compiled-in catalog) and appear once. Anything else exits with ExitBadRequest before anything runs.
//   - On Windows the exchange directory must be a clean absolute path to a plain directory (not a symlink or junction) named spruce-elevated-<digits> directly inside the child's os.TempDir, and response.json is created inside it through an os.Root with O_EXCL, so an existing file or a planted symlink makes the child stop with ExitIOError instead of writing through it. On macOS the elevated child writes nothing to disk.
//   - It refuses to run any task unless the process really is elevated (euid 0 on macOS, an elevated token on Windows): it answers with Response.Error and ExitNotElevated.
//
// # Known limitations
//
// This is the one-prompt-per-run relaunch approach. The production follow-up is a privileged helper registered once: an SMAppService launch daemon talking XPC with code-signing requirements on macOS, and a Windows service (or a scheduled task) with an authenticated pipe on Windows. Until then:
//
//   - macOS: the root child is the app's own executable, so a process of the same user that can rewrite the app bundle (an app the user dragged to /Applications is user-writable) between the prompt and the exec gets root. The dialog is attributed to osascript, not to Spruce, and shows no task list.
//   - Windows: the child accepts the exchange directory only inside its own temp directory, so over-the-shoulder elevation (a standard user typing an administrator's credentials, which runs the child as that administrator with another temp directory) is refused with ExitIOError and the batch fails. The check between Lstat and os.OpenRoot is not atomic; a same-user process that wins that race can make the child create a response.json (JSON results only) in a directory it chose.
//   - Cancelling ctx kills osascript on macOS; the root child it already started is not a child of the app and may run to completion. On Windows Launch tries TerminateProcess, which the handle may not permit.
//
// # Wire format
//
// Response is JSON, Version WireVersion, at most 1 MiB, one line. Response.Error is set when the child refused the request as a whole.
package elevate
