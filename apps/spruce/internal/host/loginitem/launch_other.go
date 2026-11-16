//go:build !darwin

package loginitem

// WatchLaunch does nothing outside macOS: Spruce opens its window at login there, as it has no menu bar icon to stay in.
func WatchLaunch() {}

// LaunchedAtLogin reports a launch that was not at login, known at once.
func LaunchedAtLogin() (atLogin, known bool) { return false, true }
