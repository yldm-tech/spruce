package overview

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"
)

// Files shows items in Finder or Explorer: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
}

// Deps are what the page reads besides the stores (its state and actions come from store.Overview), provided by the shell with DepsKey.
type Deps struct {
	Files Files
	// Home is the user's home folder, which 空间变化 writes as ~.
	Home string
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.overview", Deps{})
