package health

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/shirou/gopsutil/v4/process"
)

// NewProcessSource returns the gopsutil-backed ProcessSource of this platform.
func NewProcessSource() ProcessSource {
	return &gopsutilSource{names: make(map[procKey]string)}
}

// DisplayName is the name the monitor gives p in its alerts (on macOS the app bundle a helper belongs to, on Windows the executable's FileDescription), so other views name a process the way the alert does. It is empty when the process is gone.
func DisplayName(ctx context.Context, p *process.Process) string {
	return displayName(ctx, p)
}

type gopsutilSource struct {
	mu sync.Mutex
	// names caches display names by process, so the executable path (and on Windows its version resource) is read once per process rather than once per sample.
	names map[procKey]string
}

// Sample lists every process and reads its cumulative CPU time. Processes that exit mid-sample or that this user may not inspect are left out.
func (s *gopsutilSource) Sample(ctx context.Context) (Snapshot, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return Snapshot{}, fmt.Errorf("health: list processes: %w", err)
	}
	at := time.Now()

	s.mu.Lock()
	defer s.mu.Unlock()
	names := make(map[procKey]string, len(procs))
	out := make([]ProcessSample, 0, len(procs))
	for _, p := range procs {
		if err := ctx.Err(); err != nil {
			return Snapshot{}, err
		}
		createdMS, err := p.CreateTimeWithContext(ctx)
		if err != nil || createdMS <= 0 {
			continue
		}
		times, err := p.TimesWithContext(ctx)
		if err != nil {
			continue
		}
		created := time.UnixMilli(createdMS)
		key := procKey{pid: p.Pid, created: created.UnixNano()}
		name, ok := s.names[key]
		if !ok {
			if name = displayName(ctx, p); name == "" {
				continue
			}
		}
		names[key] = name
		out = append(out, ProcessSample{
			PID:        p.Pid,
			Name:       name,
			CPUTime:    time.Duration((times.User + times.System) * float64(time.Second)),
			CreateTime: created,
		})
	}
	// Keeping only this sample's processes bounds the cache by the number of running processes.
	s.names = names
	return Snapshot{At: at, Processes: out}, nil
}
