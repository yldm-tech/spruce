package health

import (
	"context"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/sysinfo"
)

// Service is the dashboard's sampler: *app.Sysinfo.
type Service interface {
	Info(ctx context.Context) sysinfo.SystemInfo
	Watch(ctx context.Context, send func(sysinfo.Vitals) error)
}

// Deps are what the page reads, provided by the shell with DepsKey.
type Deps struct {
	Sysinfo Service
}

// DepsKey provides the page's Deps.
var DepsKey = core.NewKey[Deps]("spruce.health", Deps{})
