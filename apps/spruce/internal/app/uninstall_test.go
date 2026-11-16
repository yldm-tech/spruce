package app

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/schedule"
	"spruce/apps/spruce/internal/engine/uninstall"
)

type fakeUninstallEngine struct {
	apps      []uninstall.App
	appsErr   error
	plan      uninstall.Plan
	planErr   error
	result    uninstall.Result
	removeErr error
	runner    uninstall.UninstallerResult
	runErr    error
	reqs      []uninstall.Request
	started   bool
}

func (f *fakeUninstallEngine) Apps(context.Context) ([]uninstall.App, error) {
	return f.apps, f.appsErr
}

func (f *fakeUninstallEngine) Details(_ context.Context, emit func(uninstall.Details)) error {
	emit(uninstall.Details{ID: "a", HasSize: true, Size: 10})
	return nil
}

func (f *fakeUninstallEngine) Plan(context.Context, string) (uninstall.Plan, error) {
	return f.plan, f.planErr
}

func (f *fakeUninstallEngine) Remove(_ context.Context, req uninstall.Request, progress func(cleanup.Progress)) (uninstall.Result, error) {
	f.reqs = append(f.reqs, req)
	progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Done: 1, Total: 1})
	return f.result, f.removeErr
}

func (f *fakeUninstallEngine) RunUninstaller(_ context.Context, _ string, started func()) (uninstall.UninstallerResult, error) {
	if f.runErr == nil {
		started()
		f.started = true
	}
	return f.runner, f.runErr
}

func (f *fakeUninstallEngine) CheckRemoved(string) (uninstall.UninstallerResult, error) {
	return f.runner, f.runErr
}

func newTestUninstall(t *testing.T, eng *fakeUninstallEngine) (*Uninstall, *fakeStore, string) {
	t.Helper()
	store := &fakeStore{}
	home := filepath.Join(t.TempDir(), "home")
	return NewUninstall(eng, store, home, slog.New(slog.DiscardHandler), schedule.NewActivity(nil)), store, home
}

func TestUninstallAppsAndLeftovers(t *testing.T) {
	installed := time.Date(2025, 3, 4, 0, 0, 0, 0, time.UTC)
	eng := &fakeUninstallEngine{
		apps: []uninstall.App{{ID: "a", Name: "Foo", Version: "1.2", BundleID: "com.example.foo", Path: "/Applications/Foo.app", AppStore: true, Running: true, InstallDate: installed}},
		plan: uninstall.Plan{
			AppID:     "a",
			Bundle:    &uninstall.Leftover{ID: "b", Path: "/Applications/Foo.app", Kind: uninstall.KindApp, Size: 100, Dir: true, Selected: true},
			Leftovers: []uninstall.Leftover{{ID: "c", Path: "/x/Caches/com.example.foo", Kind: uninstall.KindCaches, Size: 5, Selected: true, Note: "n"}},
			Denied:    2, Running: true, SharedWith: "/Users/x/Applications/Foo.app",
		},
	}
	u, _, _ := newTestUninstall(t, eng)
	apps, err := u.Apps(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	want := UninstallApp{ID: "a", Name: "Foo", Version: "1.2", BundleID: "com.example.foo", Path: "/Applications/Foo.app", AppStore: true, Running: true, InstallDate: installed}
	if len(apps) != 1 || apps[0] != want {
		t.Errorf("apps = %+v", apps)
	}
	plan, err := u.Leftovers(t.Context(), "a")
	if err != nil {
		t.Fatal(err)
	}
	if plan.Bundle == nil || plan.Bundle.Kind != UninstallKindApp || plan.Bundle.Size != 100 || len(plan.Leftovers) != 1 || plan.Leftovers[0].Kind != UninstallKindCaches || plan.Leftovers[0].Note != "n" || plan.Denied != 2 || !plan.Running || plan.SharedWith == "" {
		t.Errorf("plan = %+v", plan)
	}
	eng.plan = uninstall.Plan{AppID: "a"}
	if plan, err := u.Leftovers(t.Context(), "a"); err != nil || plan.Bundle != nil || plan.Leftovers == nil {
		t.Errorf("empty plan = %+v, %v", plan, err)
	}
	if err := u.Measure(t.Context(), nil); err != nil {
		t.Errorf("Measure: %v", err)
	}
}

func TestUninstallDetailsView(t *testing.T) {
	used := time.Date(2026, 1, 2, 3, 4, 5, 0, time.UTC)
	d := detailsView(uninstall.Details{ID: "a", Usage: uninstall.UsageUsed, LastUsed: used})
	if d.Size != nil || d.Usage != UninstallUsed || !d.LastUsed.Equal(used) {
		t.Errorf("usage view = %+v", d)
	}
	d = detailsView(uninstall.Details{ID: "a", HasSize: true, Size: 0, Icon: "data:image/png;base64,AA=="})
	if d.Size == nil || *d.Size != 0 || d.Usage != "" || d.Icon == "" {
		t.Errorf("size view = %+v", d)
	}
}

func TestUninstallRemoveRecordsHistory(t *testing.T) {
	eng := &fakeUninstallEngine{}
	u, store, home := newTestUninstall(t, eng)
	started := time.Date(2026, 11, 14, 2, 0, 0, 0, time.UTC)
	var failed []cleanup.Failure
	for i := range 22 {
		failed = append(failed, cleanup.Failure{Path: filepath.Join(home, "Library", "Caches", fmt.Sprint(i)), Reason: "没有权限删除"})
	}
	eng.result = uninstall.Result{
		App: "Foo", BundleRemoved: true,
		Clean: cleanup.CleanResult{
			Trashed: 300, Removed: 4, Failed: failed, Interrupted: true, StartedAt: started, Duration: time.Second,
			Categories: []cleanup.CategoryOutcome{
				{CategoryID: "app", Removed: 1, Trashed: 200},
				{CategoryID: "caches", Removed: 2, Trashed: 60, Failed: 22},
				{CategoryID: "support", Removed: 1, Trashed: 40},
			},
		},
	}
	out, err := u.Remove(t.Context(), UninstallRequest{AppID: "a", LeftoverIDs: []string{"x", "y"}}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if !out.BundleRemoved || out.App != "Foo" || out.Result.Trashed != 300 {
		t.Errorf("outcome = %+v", out)
	}
	if len(eng.reqs) != 1 || eng.reqs[0].AppID != "a" || !slices.Equal(eng.reqs[0].LeftoverIDs, []string{"x", "y"}) {
		t.Errorf("requests = %+v", eng.reqs)
	}
	if len(store.appended) != 1 {
		t.Fatalf("history = %+v", store.appended)
	}
	run := store.appended[0]
	if out.RunID != run.ID || out.Undoable {
		t.Errorf("outcome run %q undoable %v, recorded %q without a ledger", out.RunID, out.Undoable, run.ID)
	}
	if run.Kind != history.KindUninstall || !run.StartedAt.Equal(started) || !run.EndedAt.Equal(started.Add(time.Second)) {
		t.Errorf("run = %+v", run)
	}
	if run.Summary == nil || run.Summary.Trashed != 300 || run.Summary.Items != 4 || run.Summary.Failed != 22 {
		t.Errorf("summary = %+v", run.Summary)
	}
	bin := binName()
	wantStart := []string{
		"已将「Foo」移到" + bin,
		"「Foo」的相关文件：已移到" + bin + " 3 项",
		"「Foo」的相关文件：22 项未能处理",
		filepath.Join("~", "Library", "Caches", "0") + "：没有权限删除",
	}
	if !slices.Equal(run.Details[:4], wantStart) {
		t.Errorf("details start = %q", run.Details[:4])
	}
	if n := len(run.Details); run.Details[n-2] != "另有 2 项未能处理" || run.Details[n-1] != "卸载在中途停止，其余项目没有改动" {
		t.Errorf("details end = %q", run.Details[n-2:])
	}

	// An app that stayed is recorded with its reason, and nothing else.
	eng.result = uninstall.Result{App: "Foo", BundleFailure: "需要管理员权限才能把「Foo」移到废纸篓。", Clean: cleanup.CleanResult{StartedAt: started, Failed: []cleanup.Failure{{Path: "/Applications/Foo.app", Reason: "没有权限删除"}}}}
	out, err = u.Remove(t.Context(), UninstallRequest{AppID: "a"}, nil)
	if err != nil || out.BundleFailure == "" || out.Result.Categories == nil {
		t.Fatalf("outcome = %+v, %v", out, err)
	}
	if run := store.appended[1]; run.Details[0] != eng.result.BundleFailure {
		t.Errorf("failure details = %q", run.Details)
	}

	// A removal that did nothing is not recorded.
	eng.result = uninstall.Result{App: "Foo"}
	if out, err := u.Remove(t.Context(), UninstallRequest{AppID: "a"}, nil); err != nil || out.Result.Failed == nil || out.RunID != "" {
		t.Fatalf("empty outcome = %+v, %v", out, err)
	}
	if len(store.appended) != 2 {
		t.Errorf("an empty removal was recorded")
	}
}

func TestUninstallRunUninstallerRecordsHistory(t *testing.T) {
	for _, tc := range []struct {
		res   uninstall.UninstallerResult
		line  string
		items int
	}{
		{uninstall.UninstallerResult{App: "Foo", Removed: true, Waited: true}, "已运行「Foo」的卸载程序，它已从已安装的程序中移除", 1},
		{uninstall.UninstallerResult{App: "Foo", Waited: true}, "已运行「Foo」的卸载程序，但它仍在已安装的程序中", 0},
		{uninstall.UninstallerResult{App: "Foo"}, "已打开「Foo」的卸载程序，没有等它结束", 0},
	} {
		eng := &fakeUninstallEngine{runner: tc.res}
		u, store, _ := newTestUninstall(t, eng)
		out, err := u.RunUninstaller(t.Context(), "a", nil)
		if err != nil {
			t.Fatal(err)
		}
		if out.Removed != tc.res.Removed || out.Waited != tc.res.Waited || !eng.started {
			t.Errorf("outcome = %+v", out)
		}
		if len(store.appended) != 1 || store.appended[0].Kind != history.KindUninstall || store.appended[0].Details[0] != tc.line || store.appended[0].Summary.Items != tc.items {
			t.Errorf("history = %+v", store.appended)
		}
	}
	// A declined UAC prompt shows its sentence and records nothing.
	eng := &fakeUninstallEngine{runErr: cleanup.NewUserError("没有获得管理员授权，卸载程序没有运行", errors.New("cancelled"))}
	u, store, _ := newTestUninstall(t, eng)
	if _, err := u.RunUninstaller(t.Context(), "a", nil); err == nil || err.Error() != "没有获得管理员授权，卸载程序没有运行" {
		t.Errorf("err = %v", err)
	}
	if len(store.appended) != 0 {
		t.Error("a run that never started was recorded")
	}
	eng.runErr = nil
	eng.runner = uninstall.UninstallerResult{App: "Foo", Removed: true, Waited: true}
	if out, err := u.CheckRemoved(t.Context(), "a"); err != nil || !out.Removed {
		t.Errorf("CheckRemoved = %+v, %v", out, err)
	}
}

func TestUninstallErrors(t *testing.T) {
	cases := map[error]string{
		cleanup.NewUserError("请先退出「Foo」再卸载", nil): "请先退出「Foo」再卸载",
		uninstall.ErrBusy:       "已有卸载正在进行，请等它结束后再试",
		uninstall.ErrUnknownApp: "应用列表已更新，请刷新后重新选择",
		fmt.Errorf("%w: leftover x", uninstall.ErrStalePlan): "相关文件已过期，请重新选择应用",
		uninstall.ErrNothingSelected:                         "没有选择任何项目",
		uninstall.ErrNotUninstalled:                          "请先运行它的卸载程序，卸载完成后再清理相关文件",
		fmt.Errorf("uninstall: %w", fs.ErrNotExist):          "这个应用已经不存在了，请刷新列表",
		errors.New("disk on fire"):                           "卸载没能开始，详情已记录到日志",
	}
	if runtime.GOOS == "darwin" {
		cases[errors.ErrUnsupported] = "macOS 上的应用直接移到废纸篓即可，不需要卸载程序"
	}
	for in, want := range cases {
		eng := &fakeUninstallEngine{removeErr: in}
		u, _, _ := newTestUninstall(t, eng)
		if _, err := u.Remove(t.Context(), UninstallRequest{AppID: "a"}, nil); err == nil || err.Error() != want {
			t.Errorf("%v → %v, want %q", in, err, want)
		}
	}
	eng := &fakeUninstallEngine{appsErr: errors.New("boom")}
	u, _, _ := newTestUninstall(t, eng)
	if _, err := u.Apps(t.Context()); err == nil || !strings.Contains(err.Error(), "无法读取已安装的应用") {
		t.Errorf("Apps err = %v", err)
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	eng.appsErr = ctx.Err()
	if _, err := u.Apps(ctx); err == nil || err.Error() != "已取消" {
		t.Errorf("cancelled err = %v", err)
	}
}
