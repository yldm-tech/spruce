package shell

import (
	"context"
	"log/slog"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/schedule"
	"github.com/yldm-tech/spruce/internal/host/instance"
	"github.com/yldm-tech/spruce/internal/ui/nav"
	"github.com/yldm-tech/spruce/internal/ui/pages/devjunk"
	"github.com/yldm-tech/spruce/internal/ui/pages/health"
	"github.com/yldm-tech/spruce/internal/ui/pages/history"
	"github.com/yldm-tech/spruce/internal/ui/pages/installers"
	"github.com/yldm-tech/spruce/internal/ui/pages/maintenance"
	"github.com/yldm-tech/spruce/internal/ui/pages/settings"
	"github.com/yldm-tech/spruce/internal/ui/pages/smartscan"
	"github.com/yldm-tech/spruce/internal/ui/pages/space"
	"github.com/yldm-tech/spruce/internal/ui/pages/systemjunk"
	"github.com/yldm-tech/spruce/internal/ui/pages/uninstall"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// Deps are everything the shell and the pages read; main.go builds them from the engine (wire), tests from fakes.
type Deps struct {
	Stores store.Deps

	SmartScan   smartscan.Deps
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
}
