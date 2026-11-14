package space

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
)

// Service is the 空间透视 flow: *app.Space.
type Service interface {
	Locations(ctx context.Context) app.SpaceLocations
	Scan(ctx context.Context, path string, progress func(app.SpaceProgress)) (app.SpaceSummary, error)
	Current() *app.SpaceSummary
	Children(scanID string, id int) (app.SpaceListing, error)
	LargeFiles(scanID string) ([]app.SpaceEntry, error)
	Trash(ctx context.Context, scanID string, paths []string) (app.SpaceTrashResult, error)
}

// Files shows items in Finder or Explorer: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	Space Service
	Files Files
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.space", Deps{})
