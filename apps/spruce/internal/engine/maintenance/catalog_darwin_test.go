package maintenance

import (
	"log/slog"
	"strings"
	"testing"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/execx/execxtest"
	"spruce/apps/spruce/internal/engine/taskresult"
)

// macEnv is the app-process Env used by the macOS task tests.
func macEnv(r execx.Runner) Env {
	return Env{Runner: r, Log: slog.New(slog.DiscardHandler)}
}

// macPermissions is a PermissionChecker with a fixed answer.
type macPermissions struct {
	state PermissionState
	err   error
}

func (p macPermissions) FullDiskAccess() (PermissionState, error) { return p.state, p.err }

// assertOutcome fails t unless res has the outcome and its message contains every fragment.
func assertOutcome(t *testing.T, res taskresult.Result, want taskresult.Outcome, fragments ...string) {
	t.Helper()
	if res.Outcome != want {
		t.Fatalf("outcome = %q, want %q (message %q, detail %q)", res.Outcome, want, res.Message, res.Detail)
	}
	if res.Message == "" {
		t.Errorf("result has no message")
	}
	for _, f := range fragments {
		if !strings.Contains(res.Message, f) {
			t.Errorf("message %q does not contain %q", res.Message, f)
		}
	}
}

func TestPlatformTasksMatchTheDesign(t *testing.T) {
	tasks := PlatformTasks()
	want := []struct {
		id         string
		title      string
		defaultOn  bool
		needsAdmin bool
	}{
		{"dns", "刷新 DNS 缓存", true, true},
		{"spotlight", "校验 Spotlight 索引", false, true},
		{"quicklook", "刷新 QuickLook 预览", true, false},
		{"icons", "重建图标缓存", true, false},
		{"launchservices", "修复「打开方式」", true, false},
		{"databases", "优化应用数据库", false, false},
	}
	if len(tasks) != len(want) {
		t.Fatalf("got %d tasks, want %d", len(tasks), len(want))
	}
	total := 0
	for i, w := range want {
		got := tasks[i]
		if got.ID != w.id || got.Title != w.title || got.DefaultOn != w.defaultOn || got.NeedsAdmin != w.needsAdmin {
			t.Errorf("task %d = {%q %q on=%v admin=%v}, want {%q %q on=%v admin=%v}", i, got.ID, got.Title, got.DefaultOn, got.NeedsAdmin, w.id, w.title, w.defaultOn, w.needsAdmin)
		}
		if got.Description == "" {
			t.Errorf("task %q has no description", got.ID)
		}
		if got.EstimatedSeconds <= 0 {
			t.Errorf("task %q has no time estimate", got.ID)
		}
		total += got.EstimatedSeconds
	}
	if hint := tasks[5].Hint; hint != "需要先退出 Safari 和邮件" {
		t.Errorf("databases hint = %q", hint)
	}
	// The design promises "约 1 分钟" for the whole catalog.
	if total < 30 || total > 90 {
		t.Errorf("estimated seconds sum to %d, want about a minute", total)
	}
	if _, err := NewService(tasks, macEnv(execxtest.New()), &fakeElevator{}); err != nil {
		t.Fatalf("NewService rejected the macOS catalog: %v", err)
	}
}

func TestElevatedHandlerRunsOnlyMacAdminTasks(t *testing.T) {
	f := execxtest.New()
	h := ElevatedHandler(PlatformTasks(), macEnv(f))
	res := h(t.Context(), []string{"quicklook", "launchservices", "databases", "icons"})
	for _, r := range res {
		if r.Outcome != taskresult.Failed {
			t.Errorf("%s ran in the elevated child: %+v", r.TaskID, r)
		}
	}
	if calls := f.Calls(); len(calls) != 0 {
		t.Errorf("the child ran user tasks: %v", calls)
	}
}
