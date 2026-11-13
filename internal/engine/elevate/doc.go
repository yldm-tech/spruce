// Package elevate runs maintenance tasks that need administrator rights in a second, elevated copy of the app's own executable, behind one system prompt per run.
//
// # Parent side
//
// Elevator is what the maintenance runner depends on. ProcessElevator implements it: it creates a private temporary directory (0700), writes RequestFile into it, asks its Launcher to start the app's executable elevated as
//
//	<executable> --spruce-elevated <directory>
//
// waits for it to exit, and reads ResponseFile back. A declined prompt is ErrCancelled.
//
// # Platform launchers (owned by the platform agents)
//
// The platform-specific part is a Launcher, declared in elevate_darwin.go and elevate_windows.go by the macOS and Windows agents respectively. Each must provide exactly this constructor, which main.go calls:
//
//	// NewLauncher returns the Launcher of this platform. runner is used for the helper programs the launcher runs (osascript on macOS); the Windows launcher may ignore it.
//	func NewLauncher(runner execx.Runner) Launcher
//
// Launch must start exe with args with administrator rights, wait for the process to exit, and return nil when it ran (whatever its exit status: the response file is authoritative), ErrCancelled when the user declined or dismissed the prompt (macOS: osascript error -128 "User canceled"; Windows: ShellExecuteEx with the "runas" verb failing with ERROR_CANCELLED, 1223), and any other error when the prompt could not be shown. On macOS use `do shell script "<quoted command>" with administrator privileges` through osascript, quoting exe and every argument for the shell inside the AppleScript string; never build the command from anything but exe and args. On Windows use ShellExecuteEx with the "runas" verb, SEE_MASK_NOCLOSEPROCESS and SW_HIDE, then wait on the process handle (honouring ctx) and close it.
//
// # Child side (main.go)
//
// The elevated child is the same executable started with Flag as its first argument. main.go must check IsChild(os.Args[1:]) first thing, before mygo.Bind, mygo.App or any window code runs, and exit with the code ServeChild returns:
//
//	if elevate.IsChild(os.Args[1:]) {
//		os.Exit(elevate.ServeChild(ctx, os.Args[1:], maintenance.ElevatedHandler(tasks, env)))
//	}
//
// # Wire format
//
// RequestFile holds a Request and ResponseFile a Response, both JSON, Version WireVersion. The request carries task IDs only, never commands, paths or arguments: the child runs nothing but the admin tasks of its own compiled-in catalog whose IDs the request names, so a request tampered with by another process of the same user cannot make the elevated child do anything the app could not do anyway. The child validates the request at this boundary (size, strict decoding, version, ID syntax, duplicates) and writes the response with a temporary file renamed into place through an os.Root, so a symlink planted in the directory cannot redirect its write.
package elevate
