package settings

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"
)

// Files reveals the data folder in Finder or Explorer: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	Files Files
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.settings", Deps{})
