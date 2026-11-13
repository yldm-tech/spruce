package maintenance

import (
	"context"
	"io/fs"
	"sync"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

const (
	mdutilEnabled  = "/:\n\tIndexing enabled. \n"
	mdutilDisabled = "/:\n\tIndexing disabled.\n"
	mdutilOff      = "/:\n\tIndexing and searching disabled.\n"
	mdutilUnknown  = "/:\n\tError: unknown indexing state.\n"
)

func TestParseSpotlightStatus(t *testing.T) {
	tests := map[string]spotlightState{
		mdutilEnabled:  spotlightEnabled,
		mdutilDisabled: spotlightDisabled,
		mdutilOff:      spotlightDisabled,
		mdutilUnknown:  spotlightBroken,
		"/:\n\tIndexing disabled. Error: unknown indexing state.\n": spotlightBroken,
		"":                       spotlightUnparsed,
		"/:\n\tSomething new.\n": spotlightUnparsed,
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

func TestSpotlightPrecheck(t *testing.T) {
	t.Run("healthy index ends unchanged without a prompt", func(t *testing.T) {
		f := execxtest.New()
		f.On(pathMdutil, "-s", "/").Return(mdutilEnabled, 0)
		res := spotlightTask().Precheck(t.Context(), macEnv(f))
		if res == nil {
			t.Fatal("precheck sent a healthy index to the elevated batch")
		}
		assertOutcome(t, *res, taskresult.Unchanged, "无需修复")
	})
	for _, out := range []string{mdutilDisabled, mdutilOff, mdutilUnknown} {
		f := execxtest.New()
		f.On(pathMdutil, "-s", "/").Return(out, 0)
		if res := spotlightTask().Precheck(t.Context(), macEnv(f)); res != nil {
			t.Errorf("precheck stopped the task for %q: %+v", out, *res)
		}
	}
	t.Run("unknown output changes nothing", func(t *testing.T) {
		f := execxtest.New()
		f.On(pathMdutil, "-s", "/").Return("/:\n\tSomething new.\n", 0)
		res := spotlightTask().Precheck(t.Context(), macEnv(f))
		if res == nil {
			t.Fatal("precheck let an unparsed state through")
		}
		assertOutcome(t, *res, taskresult.Failed, "未做改动")
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
}

func TestSpotlightRun(t *testing.T) {
	t.Run("disabled is turned back on", func(t *testing.T) {
		f := execxtest.New()
		mdutilStatusSequence(f, mdutilDisabled, mdutilEnabled)
		f.On(pathMdutil, "-i", "on", "/")
		res := runSpotlight(t.Context(), macEnv(f))
		assertOutcome(t, res, taskresult.Applied, "重新开启")
		f.AssertCalled(t, pathMdutil, "-i", "on", "/")
		if f.Called(pathMdutil, "-E", "/") {
			t.Error("a disabled index was erased")
		}
	})
	t.Run("broken is enabled and rebuilt", func(t *testing.T) {
		f := execxtest.New()
		mdutilStatusSequence(f, mdutilUnknown, mdutilEnabled)
		f.On(pathMdutil, "-i", "on", "/")
		f.On(pathMdutil, "-E", "/")
		res := runSpotlight(t.Context(), macEnv(f))
		assertOutcome(t, res, taskresult.Applied, "重建")
		f.AssertCalled(t, pathMdutil, "-i", "on", "/")
		f.AssertCalled(t, pathMdutil, "-E", "/")
	})
	t.Run("healthy in the child does nothing", func(t *testing.T) {
		f := execxtest.New()
		mdutilStatusSequence(f, mdutilEnabled)
		res := runSpotlight(t.Context(), macEnv(f))
		assertOutcome(t, res, taskresult.Unchanged)
		if len(f.Calls()) != 1 {
			t.Errorf("ran %v, want only the status read", f.Calls())
		}
	})
	t.Run("still disabled afterwards fails", func(t *testing.T) {
		f := execxtest.New()
		mdutilStatusSequence(f, mdutilDisabled)
		f.On(pathMdutil, "-i", "on", "/")
		assertOutcome(t, runSpotlight(t.Context(), macEnv(f)), taskresult.Failed, "无法开启")
	})
	t.Run("mdutil -i fails", func(t *testing.T) {
		f := execxtest.New()
		mdutilStatusSequence(f, mdutilUnknown)
		f.On(pathMdutil, "-i", "on", "/").Return("", 1)
		res := runSpotlight(t.Context(), macEnv(f))
		assertOutcome(t, res, taskresult.Failed, "修复 Spotlight 索引失败")
		if f.Called(pathMdutil, "-E", "/") {
			t.Error("erased the index after mdutil -i failed")
		}
	})
}
