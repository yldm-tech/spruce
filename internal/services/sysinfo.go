package services

import (
	"context"

	"github.com/egoist/mygo"
	"github.com/yldm-tech/spruce/internal/engine/sysinfo"
)

// Sysinfo is the bound service behind the 健康监测 dashboard. The CPU alert on the same page comes from Health.
//
// Bound API (TypeScript view):
//
//	Sysinfo.info(): Promise<SystemInfo>
//	Sysinfo.watch(samples: Channel<Vitals>): Promise<void>
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

// Watch streams the machine's vitals to the page: first the samples kept from the last two minutes (oldest first), then a new one every two seconds, until the page closes the channel or goes away. Sampling runs only while at least one page watches.
func (s *Sysinfo) Watch(ctx context.Context, samples *mygo.Channel[sysinfo.Vitals]) {
	watch(ctx, s.sampler, samples.Send)
}

// watch sends the history and then every new sample through send until ctx is done or send fails (the page closed the channel).
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
