package settings

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/trash"
)

// Files reveals the data folder and measures and empties the Trash: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
	TrashSize(ctx context.Context) (trash.TrashUsage, error)
	EmptyTrash(ctx context.Context) (trash.TrashUsage, error)
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	Files Files
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.settings", Deps{})
