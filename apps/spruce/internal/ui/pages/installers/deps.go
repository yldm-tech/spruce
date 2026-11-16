package installers

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"
)

// Files shows items in Finder or Explorer: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
}

// Deps are what the view reads besides the stores (its categories and its clean come from store.Overview), provided by the shell with DepsKey.
type Deps struct {
	Files Files
}

// DepsKey provides the view's Deps.
var DepsKey = core.NewKey[Deps]("spruce.installers", Deps{})

// revealer returns files' Reveal, nil without files.
func revealer(files Files) func(ctx context.Context, path string) error {
	if files == nil {
		return nil
	}
	return files.Reveal
}
