package maintenance

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
)

// Service is the 维护 page's controller: *app.Maintenance.
type Service interface {
	Catalog() []app.TaskView
	Run(ctx context.Context, ids []string, progress func(app.ProgressEvent)) (app.RunSummary, error)
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	Maintenance Service
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.maintenance", Deps{})
