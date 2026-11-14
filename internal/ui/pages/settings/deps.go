package settings

import (
	"cmp"
	"context"
	"runtime"

	"github.com/nsgo-dev/nsgo/core"
)

// Files reveals the data folder in Finder or Explorer: *app.Files.
type Files interface {
	Reveal(ctx context.Context, path string) error
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	Files Files
	// Icon is the app's icon as a PNG, which 关于 shows; without it 关于 shows the page's tile.
	Icon []byte
	// GOOS is the platform the page lays out for, runtime.GOOS when "": Windows has no menu bar icon to stay in and no automatic rules. Tests set it to see the other platform's page.
	GOOS string
}

// windows reports whether the page lays out for Windows.
func (d Deps) windows() bool { return cmp.Or(d.GOOS, runtime.GOOS) == "windows" }

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.settings", Deps{})
