package main

import (
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// TestScreensOverviewDenied shows 概览 without Full Disk Access (H): what it could read, and the guide. Full Disk Access is macOS only; on Windows 概览 shows no such notice.
func TestScreensOverviewDenied(t *testing.T) {
	st := overviewState(overviewScan())
	st.Result.Scan.Denied = 4
	a := overviewApp(t, storefake.NewOverview(st), maintenance.PermissionDenied)
	a.WaitFor("id:denied-notice", 10*time.Second)
	a.Screenshot("overview-denied")
}
