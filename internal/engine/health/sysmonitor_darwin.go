package health

import (
	"context"
	"fmt"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/execx"
)

// openTimeout bounds "open", which returns as soon as LaunchServices has the app launching or activated.
const openTimeout = 15 * time.Second

// OpenSystemMonitor opens Activity Monitor, by bundle identifier so the localized app name does not matter.
func OpenSystemMonitor(ctx context.Context, runner execx.Runner) error {
	cmd := execx.Command{Name: "/usr/bin/open", Args: []string{"-b", "com.apple.ActivityMonitor"}, Timeout: openTimeout}
	if _, err := runner.Run(ctx, cmd); err != nil {
		return fmt.Errorf("health: open Activity Monitor: %w", err)
	}
	return nil
}
