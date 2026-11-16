package fulldisk

import (
	"context"

	"spruce/apps/spruce/internal/engine/execx"
)

// Check reports NotRequired: Windows has no Full Disk Access.
func Check() State { return NotRequired }

// Guide reports NotRequired at once.
func Guide(context.Context, execx.Runner) (State, error) { return NotRequired, nil }
