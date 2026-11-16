package maintenance

import (
	"context"
	"io/fs"
	"slices"
	"sync"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/execx/execxtest"
	"spruce/apps/spruce/internal/engine/taskresult"
)

const (
	mdutilEnabled      = "/:\n\tIndexing enabled. \n"
	mdutilDisabled     = "/:\n\tIndexing disabled.\n"
	mdutilOff          = "/:\n\tIndexing and searching disabled.\n"
	mdutilUnknown      = "/:\n\tError: unknown indexing state.\n"
	mdutilTransition   = "/:\n\tError: unexpected indexing state.  kMDConfigSearchLevelTransitioning\n"
	mdutilCheckinError = "Error mdsManageCheckin failed (5)\n"
)

func TestParseSpotlightStatus(t *testing.T) {
	tests := map[string]spotlightState{
		mdutilEnabled:      spotlightEnabled,
		mdutilDisabled:     spotlightDisabled,
		mdutilOff:          spotlightDisabled,
		mdutilUnknown:      spotlightBroken,
		mdutilTransition:   spotlightTransient,
		mdutilCheckinError: spotlightUnparsed,
		// A volume that is off stays off, whatever else mdutil says about it.
		"/:\n\tIndexing disabled. Error: unknown indexing state.\n":                 spotlightDisabled,
		"/:\n\tIndexing enabled. \nError could not create client port\n":            spotlightUnparsed,
		"/:\n\tError: unable to perform operation.  Try as root. (1)\n":             spotlightUnparsed,
		"Volume / is transitioning, waiting 5 seconds\n/:\n\tIndexing enabled. \n":  spotlightTransient,
		"/:\n\tError: unknown indexing state.\nError mdsManageCheckin failed (5)\n": spotlightUnparsed,
		"":                       spotlightUnparsed,
		"/:\n\tSomething new.\n": spotlightUnparsed,
		"/:\n\tNo index.\n":      spotlightUnparsed,
	}
	for out, want := range tests {
		if got := parseSpotlightStatus(out); got != want {
			t.Errorf("parseSpotlightStatus(%q) = %v, want %v", out, got, want)
		}
	}
}

// mdutilStatusSequence answers mdutil -s / with outs in turn, repeating the last one.
func mdutilStatusSequence(f *execxtest.Fake, outs ...string) {
	var mu sync.Mutex
	n := 0
	f.On(pathMdutil, "-s", "/").Do(func(context.Context, execx.Command) (execx.Output, error) {
		mu.Lock()
		defer mu.Unlock()
		out := outs[min(n, len(outs)-1)]
		n++
		return execx.Output{Stdout: []byte(out)}, nil
	})
}

// noSleep stands in for the wait between status reads in the child.
func noSleep(context.Context, time.Duration) error { return nil }

// assertOnlyStatusRead fails unless every command the task ran was mdutil -s /.
func assertOnlyStatusRead(t *testing.T, f *execxtest.Fake) {
	t.Helper()
	for _, c := range f.Calls() {
		if !slices.Equal(c.Args, []string{"-s", "/"}) {
			t.Errorf("ran %s, want only status reads", c.String())
		}
	}
}

func TestSpotlightPrecheck(t *testing.T) {
	stops := []struct {
		name, out string
		want      taskresult.Outcome
		message   string
	}{
		{"healthy index ends unchanged without a prompt", mdutilEnabled, taskresult.Unchanged, "无需修复"},
		{"indexing the user turned off is left alone", mdutilDisabled, taskresult.Skipped, "已被关闭"},
		{"indexing and searching off is left alone", mdutilOff, taskresult.Skipped, "已被关闭"},
		{"a transitioning volume is left alone", mdutilTransition, taskresult.Skipped, "正在变化"},
		{"mds unreachable changes nothing", mdutilCheckinError, taskresult.Failed, "未做改动"},
		{"unknown output changes nothing", "/:\n\tSomething new.\n", taskresult.Failed, "未做改动"},
	}
	for _, c := range stops {
		t.Run(c.name, func(t *testing.T) {
			f := execxtest.New()
			f.On(pathMdutil, "-s", "/").Return(c.out, 0)
			res := spotlightTask().Precheck(t.Context(), macEnv(f))
			if res == nil {
				t.Fatal("precheck sent the task to the elevated batch")
			}
			assertOutcome(t, *res, c.want, c.message)
			assertOnlyStatusRead(t, f)
		})
	}
	t.Run("broken index goes to the elevated batch", func(t *testing.T) {
		f := execxtest.New()
		f.On(pathMdutil, "-s", "/").Return(mdutilUnknown, 0)
		if res := spotlightTask().Precheck(t.Context(), macEnv(f)); res != nil {
			t.Errorf("precheck stopped the task: %+v", *res)
		}
	})
	t.Run("mdutil missing", func(t *testing.T) {
		f := execxtest.New()
		f.On(pathMdutil, "-s", "/").Fail(&fs.PathError{Op: "fork/exec", Path: pathMdutil, Err: fs.ErrNotExist})
		res := spotlightTask().Precheck(t.Context(), macEnv(f))
		if res == nil {
			t.Fatal("precheck let the task run without mdutil")
		}
		assertOutcome(t, *res, taskresult.Unavailable)
	})
	t.Run("non-zero exit naming a broken state still repairs", func(t *testing.T) {
		f := execxtest.New()
		f.On(pathMdutil, "-s", "/").ReturnOutput(execx.Output{Stderr: []byte(mdutilUnknown), ExitCode: 1})
		if res := spotlightTask().Precheck(t.Context(), macEnv(f)); res != nil {
			t.Errorf("precheck stopped the task: %+v", *res)
		}
	})
	t.Run("non-zero exit with a checkin failure is not a broken index", func(t *testing.T) {
		f := execxtest.New()
		f.On(pathMdutil, "-s", "/").ReturnOutput(execx.Output{Stderr: []byte(mdutilCheckinError), ExitCode: 1})
		res := spotlightTask().Precheck(t.Context(), macEnv(f))
		if res == nil {
			t.Fatal("precheck sent an unreachable mds to the elevated batch")
		}
		assertOutcome(t, *res, taskresult.Failed)
	})
}

func TestSpotlightRun(t *testing.T) {
	t.Run("broken is rebuilt without touching the enabled setting", func(t *testing.T) {
		f := execxtest.New()
		mdutilStatusSequence(f, mdutilUnknown, mdutilEnabled)
		f.On(pathMdutil, "-E", "/")
		res := runSpotlight(t.Context(), macEnv(f), noSleep)
		assertOutcome(t, res, taskresult.Applied, "重建")
		if res.Partial {
			t.Errorf("a rebuild that settled is partial: %+v", res)
		}
		f.AssertCalled(t, pathMdutil, "-E", "/")
		if f.Called(pathMdutil, "-i", "on", "/") {
			t.Error("the child switched indexing on")
		}
	})
	t.Run("a transition after the rebuild is waited for", func(t *testing.T) {
		f := execxtest.New()
		mdutilStatusSequence(f, mdutilUnknown, mdutilTransition, mdutilTransition, mdutilEnabled)
		f.On(pathMdutil, "-E", "/")
		slept := 0
		res := runSpotlight(t.Context(), macEnv(f), func(context.Context, time.Duration) error { slept++; return nil })
		assertOutcome(t, res, taskresult.Applied, "重建")
		if slept != 2 {
			t.Errorf("slept %d times, want 2", slept)
		}
	})
	t.Run("a transition that does not settle is still applied", func(t *testing.T) {
		f := execxtest.New()
		mdutilStatusSequence(f, mdutilUnknown, mdutilTransition)
		f.On(pathMdutil, "-E", "/")
		res := runSpotlight(t.Context(), macEnv(f), noSleep)
		assertOutcome(t, res, taskresult.Applied, "后台切换")
	})
	t.Run("disabled after the rebuild is partial, not switched on", func(t *testing.T) {
		f := execxtest.New()
		mdutilStatusSequence(f, mdutilUnknown, mdutilDisabled)
		f.On(pathMdutil, "-E", "/")
		res := runSpotlight(t.Context(), macEnv(f), noSleep)
		assertOutcome(t, res, taskresult.Applied, "未重新开启")
		if !res.Partial {
			t.Errorf("result %+v, want partial", res)
		}
	})
	for name, out := range map[string]string{
		"healthy in the child does nothing":            mdutilEnabled,
		"disabled in the child is left alone":          mdutilDisabled,
		"transitioning in the child is left alone":     mdutilTransition,
		"mds unreachable in the child changes nothing": mdutilCheckinError,
	} {
		t.Run(name, func(t *testing.T) {
			f := execxtest.New()
			mdutilStatusSequence(f, out)
			res := runSpotlight(t.Context(), macEnv(f), noSleep)
			if res.Outcome == taskresult.Applied {
				t.Errorf("result %+v", res)
			}
			if len(f.Calls()) != 1 {
				t.Errorf("ran %v, want only the status read", f.Calls())
			}
		})
	}
	t.Run("mdutil -E fails", func(t *testing.T) {
		f := execxtest.New()
		mdutilStatusSequence(f, mdutilUnknown)
		f.On(pathMdutil, "-E", "/").Return("", 1)
		res := runSpotlight(t.Context(), macEnv(f), noSleep)
		assertOutcome(t, res, taskresult.Failed, "重建 Spotlight 索引失败")
	})
}
