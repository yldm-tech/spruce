package app

import (
	"context"

	"spruce/apps/spruce/internal/engine/health"
)

// Health is the controller behind the CPU alert banner.
type Health struct {
	mon         health.Monitor
	openMonitor func(ctx context.Context) error
}

// NewHealth returns the service over mon. openMonitor opens the system's process monitor (health.OpenSystemMonitor bound to a runner).
func NewHealth(mon health.Monitor, openMonitor func(ctx context.Context) error) *Health {
	return &Health{mon: mon, openMonitor: openMonitor}
}

// Alerts returns the current alerts, most CPU first; the UI reads it once and then follows Subscribe.
func (h *Health) Alerts() []health.Alert {
	return h.mon.Alerts()
}

// OpenSystemMonitor opens Activity Monitor on macOS or Task Manager on Windows, for 查看详情.
func (h *Health) OpenSystemMonitor(ctx context.Context) error {
	return h.openMonitor(ctx)
}

// Subscribe returns the monitor's subscription: the complete current alert list at once and then on every change, and the function that ends it.
func (h *Health) Subscribe() (<-chan []health.Alert, func()) {
	return h.mon.Subscribe()
}
