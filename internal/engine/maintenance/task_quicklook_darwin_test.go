package maintenance

import (
	"io/fs"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

func TestQuickLookRun(t *testing.T) {
	t.Run("resets server and cache", func(t *testing.T) {
		f := execxtest.New()
		f.On(pathQlmanage, "-r").Return("qlmanage: resetting quicklookd\n", 0)
		f.On(pathQlmanage, "-r", "cache")
		res := runQuickLook(t.Context(), macEnv(f))
		assertOutcome(t, res, taskresult.Applied, "QuickLook")
		calls := f.Calls()
		if len(calls) != 2 || calls[0].String() != pathQlmanage+" -r" || calls[1].String() != pathQlmanage+" -r cache" {
			t.Errorf("ran %v", calls)
		}
	})
	t.Run("failure", func(t *testing.T) {
		f := execxtest.New()
		f.On(pathQlmanage, "-r").Return("", 1)
		assertOutcome(t, runQuickLook(t.Context(), macEnv(f)), taskresult.Failed, "失败")
		if f.Called(pathQlmanage, "-r", "cache") {
			t.Error("kept going after the first command failed")
		}
	})
	t.Run("missing", func(t *testing.T) {
		f := execxtest.New()
		f.On(pathQlmanage, "-r").Fail(&fs.PathError{Op: "fork/exec", Path: pathQlmanage, Err: fs.ErrNotExist})
		assertOutcome(t, runQuickLook(t.Context(), macEnv(f)), taskresult.Unavailable)
	})
}
