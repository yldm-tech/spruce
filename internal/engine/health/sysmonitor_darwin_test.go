package health

import (
	"context"
	"errors"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
)

func TestOpenSystemMonitor(t *testing.T) {
	f := execxtest.New()
	f.On("/usr/bin/open", "-b", "com.apple.ActivityMonitor").Return("", 0)
	if err := OpenSystemMonitor(context.Background(), f); err != nil {
		t.Fatal(err)
	}
	f.AssertCalled(t, "/usr/bin/open", "-b", "com.apple.ActivityMonitor")
}

func TestOpenSystemMonitorFailure(t *testing.T) {
	f := execxtest.New()
	f.On("/usr/bin/open", "-b", "com.apple.ActivityMonitor").Return("", 1)
	err := OpenSystemMonitor(context.Background(), f)
	var exitErr *execx.ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v, want the ExitError wrapped", err)
	}
}
