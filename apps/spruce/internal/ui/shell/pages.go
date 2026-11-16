package shell

import (
	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/cleanflow"
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
)

// pages are the page components by route.
var pages = map[nav.Route]func(cx *core.Scope) core.Node{
	nav.Overview:    overview.Page,
	nav.SystemJunk:  systemjunk.Page,
	nav.DevJunk:     devjunk.Page,
	nav.Installers:  installers.Page,
	nav.Uninstall:   uninstall.Page,
	nav.Space:       space.Page,
	nav.Maintenance: maintenance.Page,
	nav.Health:      health.Page,
	nav.History:     history.Page,
	nav.Settings:    settings.Page,
}

// providePages gives each page its Deps and makes what it keeps beyond one showing (each package's Provide), in cx, the app's own scope.
func providePages(cx *core.Scope, d Deps) {
	cleanflow.Provide(cx)
	overview.Provide(cx, d.Overview)
	systemjunk.Provide(cx, d.SystemJunk)
	devjunk.Provide(cx, d.DevJunk)
	installers.Provide(cx, d.Installers)
	uninstall.Provide(cx, d.Uninstall)
	space.Provide(cx, d.Space)
	maintenance.Provide(cx, d.Maintenance)
	health.Provide(cx, d.Health)
	history.Provide(cx, d.History)
	settings.Provide(cx, d.Settings)
}
