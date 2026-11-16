// Package loginitem starts Spruce when the user logs in, as app.LoginItem: on macOS 13 and later the app is the user's login item through SMAppService.mainAppService (System Settings > General > Login Items), on macOS 11 and 12 a launch agent in ~/Library/LaunchAgents opens it with LoginArg, and on Windows a value in the user's Run key starts it. The calls are synchronous and report the system's error, so the 设置 page can refuse to save the other settings when this one fails. WatchLaunch and LaunchedAtLogin tell whether the system started this run at login, so Spruce can stay in the menu bar without opening its window.
//
// The locations are those earlier builds of Spruce used (their framework's login item code for macOS and Windows), so a user who turned it on there sees it on here: the same SMAppService, the same launch agent label and the same Run value name, the app ID. Their Run value carried an extra --…-opened-at-login argument; any value under the name counts as on, and the next change rewrites it.
package loginitem

// Item is the app's login item.
type Item struct {
	// id is the app's ID, as nsgo.yaml's: the launch agent's label and the Run value's name.
	id string
}

// New returns the login item of the app with ID id ("ai.yldm.spruce").
func New(id string) *Item { return &Item{id: id} }

// LoginArg is the argument the macOS 11 and 12 launch agent passes, so Spruce knows the system started it at login.
const LoginArg = "--login"

// atLoginArg reports whether args, the command line after the program, hold LoginArg.
func atLoginArg(args []string) bool {
	for _, a := range args {
		if a == LoginArg {
			return true
		}
	}
	return false
}

// disabledInTaskManager reports whether Task Manager's Startup page turned the entry off: its StartupApproved value's first byte is odd then.
func disabledInTaskManager(state []byte) bool {
	return len(state) > 0 && state[0]&1 == 1
}
