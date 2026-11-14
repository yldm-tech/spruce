package services

import (
	"context"

	"github.com/egoist/mygo"
	"github.com/yldm-tech/spruce/internal/engine/health"
)

// HealthAlerts carries the complete current alert list to every window whenever it changes; an empty list clears the banner.
var HealthAlerts = mygo.NewEvent[[]health.Alert]("health:alerts")

// Health is the bound service behind the CPU alert banner.
type Health struct {
	mon         health.Monitor
	openMonitor func(ctx context.Context) error
}

// NewHealth returns the service over mon. openMonitor opens the system's process monitor (health.OpenSystemMonitor bound to a runner).
func NewHealth(mon health.Monitor, openMonitor func(ctx context.Context) error) *Health {
	return &Health{mon: mon, openMonitor: openMonitor}
}

// Alerts returns the current alerts, most CPU first; the page calls it once and then follows events.healthAlerts.
func (h *Health) Alerts() []health.Alert {
	return h.mon.Alerts()
}

// OpenSystemMonitor opens Activity Monitor on macOS or Task Manager on Windows, for 查看详情 until the 健康监测 page exists.
func (h *Health) OpenSystemMonitor(ctx context.Context) error {
	return h.openMonitor(ctx)
}

// BroadcastAlerts forwards every alert list mon publishes to all windows as HealthAlerts until ctx is done. main.go starts it, with mon.Run, once the app is ready.
func BroadcastAlerts(ctx context.Context, mon health.Monitor) {
	ch, unsubscribe := mon.Subscribe()
	defer unsubscribe()
	for {
		select {
		case <-ctx.Done():
			return
		case alerts, ok := <-ch:
			if !ok {
				return
			}
			// Broadcast fails only when the payload cannot be encoded as JSON, which never happens for a list of health.Alert.
			_ = HealthAlerts.Broadcast(alerts)
		}
	}
}
