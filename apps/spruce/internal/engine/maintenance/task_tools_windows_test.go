package maintenance

import (
	"context"
	"errors"
	"os/exec"
	"testing"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/execx/execxtest"
	"spruce/apps/spruce/internal/engine/taskresult"
)

func TestDNSTask(t *testing.T) {
	const ipconfig = testSystemDir + `\ipconfig.exe`
	cases := []struct {
		name    string
		script  func(*execxtest.Fake)
		outcome taskresult.Outcome
	}{
		{"flushed", func(f *execxtest.Fake) {
			f.On(ipconfig, "/flushdns").Return("Successfully flushed the DNS Resolver Cache.\r\n", 0)
		}, taskresult.Applied},
		{"dns client disabled", func(f *execxtest.Fake) {
			f.On(ipconfig, "/flushdns").ReturnOutput(execx.Output{Stdout: []byte("Could not flush the DNS Resolver Cache: Function failed during execution.\r\n"), ExitCode: 1})
		}, taskresult.Failed},
		{"missing tool", func(f *execxtest.Fake) {
			f.On(ipconfig, "/flushdns").Fail(exec.ErrNotFound)
		}, taskresult.Unavailable},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := execxtest.New()
			c.script(f)
			res := winDNSTask(newTestWinSystem(t)).Run(context.Background(), testEnv(f))
			if res.Outcome != c.outcome {
				t.Fatalf("outcome %q (%s; %s), want %q", res.Outcome, res.Message, res.Detail, c.outcome)
			}
			f.AssertCalled(t, ipconfig, "/flushdns")
			if c.outcome == taskresult.Failed && res.Detail == "" {
				t.Error("a failure must carry a detail")
			}
		})
	}
}

func TestDNSTaskRunsWithTimeout(t *testing.T) {
	f := execxtest.New()
	f.On(testSystemDir+`\ipconfig.exe`, "/flushdns")
	winDNSTask(newTestWinSystem(t)).Run(context.Background(), testEnv(f))
	if calls := f.Calls(); len(calls) != 1 || calls[0].Timeout <= 0 {
		t.Errorf("calls %+v, want one bounded call", calls)
	}
}

func TestDNSTaskWithoutSystemDir(t *testing.T) {
	sys := newTestWinSystem(t)
	sys.systemDir = func() (string, error) { return "", errors.New("no system dir") }
	f := execxtest.New()
	res := winDNSTask(sys).Run(context.Background(), testEnv(f))
	if res.Outcome != taskresult.Failed || len(f.Calls()) != 0 {
		t.Errorf("result %+v, calls %v", res, f.Calls())
	}
}

func TestIconsTask(t *testing.T) {
	const ie4uinit = testSystemDir + `\ie4uinit.exe`
	cases := []struct {
		name    string
		script  func(*execxtest.Fake)
		outcome taskresult.Outcome
	}{
		{"refreshed", func(f *execxtest.Fake) { f.On(ie4uinit, "-show").Return("", 0) }, taskresult.Applied},
		{"non-zero exit", func(f *execxtest.Fake) { f.On(ie4uinit, "-show").Return("", 2) }, taskresult.Failed},
		{"missing tool", func(f *execxtest.Fake) { f.On(ie4uinit, "-show").Fail(exec.ErrNotFound) }, taskresult.Unavailable},
		{"cancelled", func(f *execxtest.Fake) { f.On(ie4uinit, "-show").Fail(context.Canceled) }, taskresult.Skipped},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			f := execxtest.New()
			c.script(f)
			res := winIconsTask(newTestWinSystem(t)).Run(context.Background(), testEnv(f))
			if res.Outcome != c.outcome {
				t.Fatalf("outcome %q (%s; %s), want %q", res.Outcome, res.Message, res.Detail, c.outcome)
			}
			// Only the in-place refresh may run: never the Windows 7 flag, never anything that restarts Explorer.
			for _, call := range f.Calls() {
				if call.Name != ie4uinit || len(call.Args) != 1 || call.Args[0] != "-show" {
					t.Errorf("unexpected command %s", call.String())
				}
			}
		})
	}
}
