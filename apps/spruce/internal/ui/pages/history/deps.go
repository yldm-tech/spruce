package history

import (
	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/app"
)

// Catalog names the maintenance tasks a maintenance run lists: *app.Maintenance.
type Catalog interface {
	Catalog() []app.TaskView
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	Catalog Catalog
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.history", Deps{})
