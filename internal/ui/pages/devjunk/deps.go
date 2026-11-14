package devjunk

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
)

// Files shows items in Finder or Explorer: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	// Cleanup scans and cleans through the shared cleanup controller: *app.Cleanup.
	Cleanup cleanflow.Cleaner
	Files   Files
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.devjunk", Deps{})

// reveal shows a path in Finder or Explorer, as CategoryList takes it.
type reveal = func(ctx context.Context, path string) error

// revealer returns files' Reveal, nil without files.
func revealer(files Files) reveal {
	if files == nil {
		return nil
	}
	return files.Reveal
}
