package shell_test

import (
	"testing"
	"time"
)

// Only macOS has the menu bar icon to stay in (the shell makes the tray icon and honours 关闭窗口后继续在菜单栏运行 on darwin only).
func TestLoginLaunchStaysInTheMenuBar(t *testing.T) {
	a, views := startLaunch(t, func() (bool, bool) { return true, true }, true)
	// The launch is known to be at login and the window is to stay in the menu bar: it never opens, and two seconds later still has not.
	a.Advance(2 * time.Second)
	if *views != 0 || a.Exists("id:sidebar") {
		t.Fatalf("the window opened at a login start (%d views)", *views)
	}
}
