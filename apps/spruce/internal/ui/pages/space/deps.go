package space

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
)

// Service is the 空间透视 flow: *app.Space. The start screen's disks and folders come from store.Locations.
type Service interface {
	Scan(ctx context.Context, path string, progress func(app.SpaceProgress)) (app.SpaceSummary, error)
	Current() *app.SpaceSummary
	Children(scanID string, id int) (app.SpaceListing, error)
	Folder(scanID, path string) (app.SpaceListing, error)
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
