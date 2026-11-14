package smartscan

import (
	"context"
	"time"

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
var DepsKey = core.NewKey[Deps]("spruce.smartscan", Deps{})

// timeNow is the clock the last clean's and the last 维护's ages count from.
var timeNow = time.Now

// revealer returns files' Reveal, nil without files.
func revealer(files Files) func(ctx context.Context, path string) error {
	if files == nil {
		return nil
	}
	return files.Reveal
}
