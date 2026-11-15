package app

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/schedule"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
	"github.com/yldm-tech/spruce/internal/host/instance"
)

// heldElsewhere is an Activity whose lock another Spruce process holds.
func heldElsewhere(t *testing.T) *schedule.Activity {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clean.lock")
	release, held, err := instance.LockFile(path)
	if err != nil || !held {
		t.Fatal(held, err)
	}
	t.Cleanup(func() { _ = release() })
	return schedule.NewActivity(func() (func() error, bool, error) { return instance.LockFile(path) })
}

func wantBusyElsewhere(t *testing.T, what string, err error) {
	t.Helper()
	if !errors.Is(err, schedule.ErrBusyElsewhere) || err.Error() != "另一个 Spruce 正在清理，请稍后再试" {
		t.Errorf("%s: %v", what, err)
	}
}

// TestSpaceUninstallAndMaintenanceRunInTheGate: none of them moves, removes or runs anything while another Spruce process cleans.
func TestSpaceUninstallAndMaintenanceRunInTheGate(t *testing.T) {
	f := newSpaceFixture(t)
	sum := f.scan(t)
	f.svc.activity = heldElsewhere(t)
	_, err := f.svc.Trash(t.Context(), sum.ScanID, []string{filepath.Join(f.data, "videos", "film.mov")})
	wantBusyElsewhere(t, "space", err)
	if len(f.bin.moved) != 0 {
		t.Errorf("space moved %v", f.bin.moved)
	}

	eng := &fakeUninstallEngine{}
	u, store, _ := newTestUninstall(t, eng)
	u.activity = heldElsewhere(t)
	_, err = u.Remove(t.Context(), UninstallRequest{AppID: "a"}, nil)
	wantBusyElsewhere(t, "uninstall", err)
	if len(store.appended) != 0 {
		t.Error("uninstall recorded a removal")
	}

	ran := false
	task := maintenance.TaskImpl{Task: maintenance.Task{ID: "t", Title: "t", Icon: maintenance.IconNetwork},
		Run: func(context.Context, maintenance.Env) taskresult.Result {
			ran = true
			return taskresult.Result{Outcome: taskresult.Applied}
		}}
	svc, err := maintenance.NewService([]maintenance.TaskImpl{task}, maintenance.Env{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMaintenance(svc, &fakeStore{}, slog.New(slog.DiscardHandler), heldElsewhere(t))
	_, err = m.Run(t.Context(), []string{"t"}, nil)
	wantBusyElsewhere(t, "maintenance", err)
	if ran {
		t.Error("a maintenance task ran")
	}
}
