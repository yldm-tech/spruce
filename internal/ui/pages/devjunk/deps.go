package devjunk

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// Cleaner scans and cleans through the shared cleanup controller: *app.Cleanup.
type Cleaner interface {
	Scan(ctx context.Context, id app.CleanerID, progress func(cleanup.Progress)) (cleanup.ScanResult, error)
	Clean(ctx context.Context, id app.CleanerID, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error)
}

// Files shows items in Finder or Explorer: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	Cleanup Cleaner
	Files   Files
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.devjunk", Deps{})
