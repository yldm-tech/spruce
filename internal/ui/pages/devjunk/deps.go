package devjunk

import (
	"context"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Files shows items in Finder or Explorer: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
}

// Deps are what the view reads besides the stores (its categories, its projects and its clean come from store.Overview), provided by the shell with DepsKey.
type Deps struct {
	Files Files
}

// DepsKey provides the view's Deps.
var DepsKey = core.NewKey[Deps]("spruce.devjunk", Deps{})

// timeNow is the clock the projects' idle time counts from.
var timeNow = time.Now

// reveal shows a path in Finder or Explorer, as CategoryList takes it.
type reveal = func(ctx context.Context, path string) error

// revealer returns files' Reveal, nil without files.
func revealer(files Files) reveal {
	if files == nil {
		return nil
	}
	return files.Reveal
}
