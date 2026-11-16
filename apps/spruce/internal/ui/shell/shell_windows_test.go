package shell_test

import (
	"testing"
	"time"
)

// Windows has no menu bar icon to stay in, so a login start opens the window even with a stored KeepInTray, which there would leave no way in.
func TestLoginLaunchOpensTheWindowWithoutAMenuBar(t *testing.T) {
	a, views := startLaunch(t, func() (bool, bool) { return true, true }, true)
	a.Advance(2 * time.Second)
	a.WaitFor("id:sidebar", wait)
	if *views != 1 {
		t.Errorf("%d main views, want 1", *views)
	}
}
