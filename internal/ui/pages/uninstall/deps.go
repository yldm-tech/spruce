package uninstall

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// Service is the 应用卸载 flow: *app.Uninstall.
type Service interface {
	Apps(ctx context.Context) ([]app.UninstallApp, error)
	Measure(ctx context.Context, details func(app.UninstallAppDetails)) error
	Leftovers(ctx context.Context, appID string) (app.UninstallPlan, error)
	Remove(ctx context.Context, req app.UninstallRequest, progress func(cleanup.Progress)) (app.UninstallOutcome, error)
	RunUninstaller(ctx context.Context, appID string, status func(app.UninstallerStatus)) (app.UninstallerOutcome, error)
	CheckRemoved(ctx context.Context, appID string) (app.UninstallerOutcome, error)
}

// Files shows items in Finder or Explorer: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	Uninstall Service
	Files     Files
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.uninstall", Deps{})
