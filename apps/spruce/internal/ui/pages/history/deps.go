package history

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
)

// Catalog names the maintenance tasks a maintenance run lists: *app.Maintenance.
type Catalog interface {
	Catalog() []app.TaskView
}

// Files reveals a path a run's details name in Finder or Explorer: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	Catalog Catalog
	Files   Files
	// Home is the user's home folder, which a detail line writes ~.
	Home string
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.history", Deps{})
