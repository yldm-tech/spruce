package shell

import (
	"github.com/nsgo-dev/nsgo/core"

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

// providePages gives each page its Deps, in cx, the app's own scope (空间透视 also its controller, which outlives the page).
func providePages(cx *core.Scope, d Deps) {
	overview.DepsKey.Provide(cx, d.Overview)
	systemjunk.DepsKey.Provide(cx, d.SystemJunk)
	devjunk.DepsKey.Provide(cx, d.DevJunk)
	installers.DepsKey.Provide(cx, d.Installers)
	uninstall.DepsKey.Provide(cx, d.Uninstall)
	space.Provide(cx, d.Space)
	maintenance.DepsKey.Provide(cx, d.Maintenance)
	health.DepsKey.Provide(cx, d.Health)
	history.DepsKey.Provide(cx, d.History)
	settings.DepsKey.Provide(cx, d.Settings)
}
