package shell

import (
	"context"
	"log/slog"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/engine/schedule"
	"spruce/apps/spruce/internal/host/instance"
	"spruce/apps/spruce/internal/ui/nav"
	"spruce/apps/spruce/internal/ui/pages/devjunk"
	"spruce/apps/spruce/internal/ui/pages/health"
	"spruce/apps/spruce/internal/ui/pages/history"
	"spruce/apps/spruce/internal/ui/pages/installers"
	"spruce/apps/spruce/internal/ui/pages/maintenance"
	"spruce/apps/spruce/internal/ui/pages/overview"
	"spruce/apps/spruce/internal/ui/pages/settings"
	"spruce/apps/spruce/internal/ui/pages/space"
	"spruce/apps/spruce/internal/ui/pages/systemjunk"
	"spruce/apps/spruce/internal/ui/pages/uninstall"
	"spruce/apps/spruce/internal/ui/store"
)

// Deps are everything the shell and the pages read; main.go builds them from the engine (wire), tests from fakes.
type Deps struct {
	Stores store.Deps

	Overview    overview.Deps
	SystemJunk  systemjunk.Deps
	DevJunk     devjunk.Deps
	Installers  installers.Deps
	Uninstall   uninstall.Deps
	Space       space.Deps
	Maintenance maintenance.Deps
	Health      health.Deps
	History     history.Deps
	Settings    settings.Deps

	// Background is the work that runs while the app is up (the health monitor, FollowSettings): each starts on its own goroutine as the app opens, with a context that ends as it quits.
	Background []func(ctx context.Context)
	// Instance, when set, is the single-instance lock: a later copy started by the user brings the window to the front.
	Instance *instance.Instance
	// DataDir is the data folder, where the window's frame is kept; "" keeps none.
	DataDir string
	// SetMainView shows the root view: nsgo.SetMainView in the app, core's in tests.
	SetMainView func(cx *core.Scope, view func(cx *core.Scope) core.Node)
	Log         *slog.Logger
	// Start is the page the window opens on; "" means nav.Start. Screen tests of the drill-down views, which have no sidebar entry, open on them this way.
	Start nav.Route
	// Activity is the gate every clean and the background work go through: main ends it as the app quits, and the tray's 退出 says when a clean is running.
	Activity *schedule.Activity
	// LaunchedAtLogin reports whether the system started this run at login, and whether that is known yet (loginitem.LaunchedAtLogin); nil counts as a launch the user made.
	LaunchedAtLogin func() (atLogin, known bool)
	// TrayIcon and TrayAlertIcon are the menu bar icon's template PNGs, the second for low space (macOS); without TrayIcon there is no menu bar icon.
	TrayIcon, TrayAlertIcon []byte

	// host replaces the main window's platform in the shell's own tests.
	host windowHost
}
