package maintenance

import (
	"context"
	"fmt"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// fakeServices is a scripted winServiceManager for WSearch. After Start, Query walks through afterStart (the last entry repeats).
type fakeServices struct {
	mu         sync.Mutex
	info       winServiceInfo
	queryErr   error
	startErr   error
	afterStart []winServiceInfo
	starts     int
	queries    int
}

func (f *fakeServices) Query(name string) (winServiceInfo, error) {
	f.mu.Lock()
	defer f.mu.Unlock()
	if name != winSearchService {
		return winServiceInfo{}, fmt.Errorf("%s: %w", name, errServiceMissing)
	}
	f.queries++
	if f.queryErr != nil {
		return winServiceInfo{}, f.queryErr
	}
	if f.starts > 0 && len(f.afterStart) > 0 {
		next := f.afterStart[0]
		if len(f.afterStart) > 1 {
			f.afterStart = f.afterStart[1:]
		}
		return next, nil
	}
	return f.info, nil
}

func (f *fakeServices) Start(name string) error {
	f.mu.Lock()
	defer f.mu.Unlock()
	f.starts++
	return f.startErr
}

func searchTask(t *testing.T, s *fakeServices) TaskImpl {
	t.Helper()
	sys := newTestWinSystem(t)
	sys.services = s
	return winSearchTask(sys)
}

func TestSearchPrecheck(t *testing.T) {
	stoppedAuto := winServiceInfo{State: svc.Stopped, StartType: mgr.StartAutomatic, DelayedAutoStart: true, Win32ExitCode: 1067}
	cases := []struct {
		name     string
		services *fakeServices
		want     taskresult.Outcome // "" means the precheck lets the task run
	}{
		{"running", &fakeServices{info: winServiceInfo{State: svc.Running, StartType: mgr.StartAutomatic}}, taskresult.Unchanged},
		{"starting", &fakeServices{info: winServiceInfo{State: svc.StartPending, StartType: mgr.StartAutomatic}}, taskresult.Unchanged},
		{"stopping", &fakeServices{info: winServiceInfo{State: svc.StopPending, StartType: mgr.StartAutomatic}}, taskresult.Skipped},
		{"disabled by the user", &fakeServices{info: winServiceInfo{State: svc.Stopped, StartType: mgr.StartDisabled}}, taskresult.Skipped},
		{"manual and stopped", &fakeServices{info: winServiceInfo{State: svc.Stopped, StartType: mgr.StartManual}}, taskresult.Unchanged},
		{"not installed", &fakeServices{queryErr: fmt.Errorf("WSearch: %w", errServiceMissing)}, taskresult.Unavailable},
		{"access denied", &fakeServices{queryErr: windows.ERROR_ACCESS_DENIED}, taskresult.Failed},
		{"stopped but automatic", &fakeServices{info: stoppedAuto}, ""},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			res := searchTask(t, c.services).Precheck(context.Background(), testEnv(execxtest.New()))
			switch {
			case c.want == "" && res != nil:
				t.Fatalf("precheck stopped the task: %+v", *res)
			case c.want != "" && res == nil:
				t.Fatalf("precheck let the task run, want %q", c.want)
			case c.want != "" && res.Outcome != c.want:
				t.Fatalf("outcome %q (%s), want %q", res.Outcome, res.Message, c.want)
			}
			if c.services.starts != 0 {
				t.Error("the precheck must never start the service")
			}
		})
	}
}

// TestSearchDelayedStartWindow covers WSearch's default "Automatic (Delayed Start)": right after sign-in it is stopped and has never run, which needs no UAC prompt.
func TestSearchDelayedStartWindow(t *testing.T) {
	neverStarted := uint32(windows.ERROR_SERVICE_NEVER_STARTED)
	cases := []struct {
		name    string
		info    winServiceInfo
		uptime  time.Duration
		elevate bool
	}{
		{"delayed, never started, just booted", winServiceInfo{State: svc.Stopped, StartType: mgr.StartAutomatic, DelayedAutoStart: true, Win32ExitCode: neverStarted}, 90 * time.Second, false},
		{"delayed, never started, long after boot", winServiceInfo{State: svc.Stopped, StartType: mgr.StartAutomatic, DelayedAutoStart: true, Win32ExitCode: neverStarted}, 30 * time.Minute, true},
		{"not delayed, never started, just booted", winServiceInfo{State: svc.Stopped, StartType: mgr.StartAutomatic, Win32ExitCode: neverStarted}, 90 * time.Second, true},
		{"delayed, crashed, just booted", winServiceInfo{State: svc.Stopped, StartType: mgr.StartAutomatic, DelayedAutoStart: true, Win32ExitCode: 1067}, 90 * time.Second, true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			s := &fakeServices{info: c.info}
			sys := newTestWinSystem(t)
			sys.services = s
			sys.uptime = func() time.Duration { return c.uptime }
			res := winSearchTask(sys).Precheck(context.Background(), testEnv(execxtest.New()))
			if c.elevate != (res == nil) {
				t.Fatalf("precheck result %+v, want elevation %v", res, c.elevate)
			}
			if res != nil && res.Outcome != taskresult.Unchanged {
				t.Errorf("outcome %q (%s), want unchanged", res.Outcome, res.Message)
			}
			if s.starts != 0 {
				t.Error("the precheck must never start the service")
			}
		})
	}
}

func TestSearchRun(t *testing.T) {
	stopped := winServiceInfo{State: svc.Stopped, StartType: mgr.StartAutomatic}
	cases := []struct {
		name       string
		services   *fakeServices
		want       taskresult.Outcome
		wantStarts int
	}{
		{"starts", &fakeServices{info: stopped, afterStart: []winServiceInfo{{State: svc.StartPending}, {State: svc.Running}}}, taskresult.Applied, 1},
		{"already running when the batch ran", &fakeServices{info: winServiceInfo{State: svc.Running, StartType: mgr.StartAutomatic}}, taskresult.Unchanged, 0},
		{"started by someone else meanwhile", &fakeServices{info: stopped, startErr: windows.ERROR_SERVICE_ALREADY_RUNNING, afterStart: []winServiceInfo{{State: svc.Running}}}, taskresult.Applied, 1},
		{"start refused", &fakeServices{info: stopped, startErr: windows.ERROR_ACCESS_DENIED}, taskresult.Failed, 1},
		{"stops again", &fakeServices{info: stopped, afterStart: []winServiceInfo{{State: svc.StartPending}, {State: svc.Stopped, Win32ExitCode: 1067}}}, taskresult.Failed, 1},
		{"never starts", &fakeServices{info: stopped, afterStart: []winServiceInfo{{State: svc.StartPending}}}, taskresult.Failed, 1},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			env := testEnv(execxtest.New())
			env.Elevated = true
			res := searchTask(t, c.services).Run(context.Background(), env)
			if res.Outcome != c.want {
				t.Fatalf("outcome %q (%s; %s), want %q", res.Outcome, res.Message, res.Detail, c.want)
			}
			if c.services.starts != c.wantStarts {
				t.Errorf("Start called %d times, want %d", c.services.starts, c.wantStarts)
			}
		})
	}
}

func TestSearchRunCancelledWhileWaiting(t *testing.T) {
	s := &fakeServices{info: winServiceInfo{State: svc.Stopped, StartType: mgr.StartAutomatic}, afterStart: []winServiceInfo{{State: svc.StartPending}}}
	sys := newTestWinSystem(t)
	sys.services = s
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	res := winSearchTask(sys).Run(ctx, testEnv(execxtest.New()))
	if res.Outcome != taskresult.Skipped {
		t.Fatalf("outcome %q (%s), want skipped", res.Outcome, res.Message)
	}
}
