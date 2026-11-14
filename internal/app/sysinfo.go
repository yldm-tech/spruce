package app

import (
	"context"

	"github.com/yldm-tech/spruce/internal/engine/sysinfo"
)

// Sysinfo is the controller behind the 健康监测 dashboard. The CPU alert on the same page comes from Health.
type Sysinfo struct {
	sampler *sysinfo.Sampler
}

// NewSysinfo returns the service over sampler.
func NewSysinfo(sampler *sysinfo.Sampler) *Sysinfo {
	return &Sysinfo{sampler: sampler}
}

// Info returns what does not change while the app runs: the OS, the model, the CPU and its cores, and the boot time. Fields that could not be read are empty.
func (s *Sysinfo) Info(ctx context.Context) sysinfo.SystemInfo {
	return s.sampler.Info(ctx)
}

// Watch sends the machine's vitals through send: first the samples kept from the last two minutes (oldest first), then a new one every two seconds, until ctx is done or send returns an error. Sampling runs only while at least one page watches. send runs on Watch's goroutine.
func (s *Sysinfo) Watch(ctx context.Context, send func(sysinfo.Vitals) error) {
	watch(ctx, s.sampler, send)
}

// watch sends the history and then every new sample through send until ctx is done or send fails.
func watch(ctx context.Context, sampler *sysinfo.Sampler, send func(sysinfo.Vitals) error) {
	history, ch, unsubscribe := sampler.Subscribe()
	defer unsubscribe()
	for _, v := range history {
		if send(v) != nil {
			return
		}
	}
	for {
		select {
		case <-ctx.Done():
			return
		case v, ok := <-ch:
			if !ok || send(v) != nil {
				return
			}
		}
	}
}
