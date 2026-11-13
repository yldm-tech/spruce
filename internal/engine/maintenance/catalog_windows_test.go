package maintenance

import (
	"context"
	"errors"
	"log/slog"
	"os"
	"strings"
	"testing"
	"time"

	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/yldm-tech/spruce/internal/engine/elevate"
	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

const testSystemDir = `C:\Windows\System32`

// newTestWinSystem returns a winSystem whose every OS dependency is a fake; tests replace the parts they exercise.
func newTestWinSystem(t *testing.T) *winSystem {
	t.Helper()
	return &winSystem{
		systemDir:        func() (string, error) { return testSystemDir, nil },
		explorerCacheDir: func() (string, error) { return t.TempDir(), nil },
		removeFile:       (*os.Root).Remove,
		services:         &fakeServices{},
		registry:         newFakeRegistry(),
		pathState:        func(string) winPathState { return winPathUnknown },
		pollInterval:     time.Millisecond,
		startTimeout:     time.Second,
	}
}

func testEnv(r execx.Runner) Env {
	return Env{Runner: r, Log: slog.New(slog.DiscardHandler)}
}

func TestPlatformTasksCatalog(t *testing.T) {
	tasks := PlatformTasks()
	want := []struct {
		id         string
		defaultOn  bool
		needsAdmin bool
		precheck   bool
	}{
		{"dns", true, false, false},
		{"search", false, true, true},
		{"thumbnails", true, false, false},
		{"icons", true, false, false},
		{"associations", true, false, false},
	}
	if len(tasks) != len(want) {
		t.Fatalf("got %d tasks, want %d", len(tasks), len(want))
	}
	for i, w := range want {
		task := tasks[i]
		if task.ID != w.id || task.DefaultOn != w.defaultOn || task.NeedsAdmin != w.needsAdmin || (task.Precheck != nil) != w.precheck {
			t.Errorf("task %d = {%s defaultOn=%v needsAdmin=%v precheck=%v}, want %+v", i, task.ID, task.DefaultOn, task.NeedsAdmin, task.Precheck != nil, w)
		}
		if !elevate.ValidTaskID(task.ID) {
			t.Errorf("task %q: invalid ID", task.ID)
		}
		if task.Title == "" || task.Description == "" || task.EstimatedSeconds <= 0 {
			t.Errorf("task %q: incomplete card %+v", task.ID, task.Task)
		}
		if strings.Contains(task.Title+task.Description, "Mole") {
			t.Errorf("task %q mentions a name the product must not use", task.ID)
		}
	}
	if _, err := NewService(tasks, testEnv(execxtest.New()), &fakeElevator{outcome: taskresult.Applied}); err != nil {
		t.Fatalf("NewService rejected the Windows catalog: %v", err)
	}
}

// The elevated child must be able to run every admin task and refuse the others.
func TestPlatformTasksElevatedHandler(t *testing.T) {
	sys := newTestWinSystem(t)
	svcs := &fakeServices{info: winServiceInfo{State: svc.Running, StartType: mgr.StartAutomatic}}
	sys.services = svcs
	tasks := winTasks(sys)
	h := ElevatedHandler(tasks, testEnv(execxtest.New()))
	res := h(context.Background(), []string{"search", "dns"})
	if res[0].Outcome != taskresult.Unchanged {
		t.Errorf("search in the child = %+v, want unchanged", res[0])
	}
	if res[1].Outcome != taskresult.Failed {
		t.Errorf("dns is not an admin task, the child must refuse it: %+v", res[1])
	}
}

// A healthy search service must not cause an elevation prompt.
func TestServiceRunSkipsElevationWhenSearchIsHealthy(t *testing.T) {
	sys := newTestWinSystem(t)
	sys.services = &fakeServices{info: winServiceInfo{State: svc.Running, StartType: mgr.StartAutomatic}}
	el := &fakeElevator{outcome: taskresult.Applied}
	s, err := NewService(winTasks(sys), testEnv(execxtest.New()), el)
	if err != nil {
		t.Fatal(err)
	}
	res, err := s.Run(context.Background(), []string{"search"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if res[0].Outcome != taskresult.Unchanged {
		t.Errorf("search = %+v, want unchanged", res[0])
	}
	if len(el.batches) != 0 {
		t.Errorf("elevator ran %v for a healthy service", el.batches)
	}
}

func TestToolFailureClassification(t *testing.T) {
	cases := []struct {
		name string
		err  error
		want taskresult.Outcome
	}{
		{"missing", os.ErrNotExist, taskresult.Unavailable},
		{"cancelled", context.Canceled, taskresult.Skipped},
		{"timeout", context.DeadlineExceeded, taskresult.Failed},
		{"exit", &execx.ExitError{Command: "x", ExitCode: 1}, taskresult.Failed},
		{"other", errors.New("boom"), taskresult.Failed},
	}
	for _, c := range cases {
		res := winToolFailure(execx.Output{Stdout: []byte("\xff bad bytes")}, c.err, "failed")
		if res.Outcome != c.want {
			t.Errorf("%s: outcome %q, want %q", c.name, res.Outcome, c.want)
		}
		if !strings.Contains(res.Detail, "stdout:") || !strings.Contains(res.Detail, "bad bytes") {
			t.Errorf("%s: detail %q lacks stdout", c.name, res.Detail)
		}
	}
}
