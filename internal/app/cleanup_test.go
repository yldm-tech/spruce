package app

import (
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/schedule"
)

type fakeScanner struct {
	scan    cleanup.ScanResult
	clean   cleanup.CleanResult
	scanErr error
	clnErr  error
	reqs    []cleanup.CleanRequest
}

func (f *fakeScanner) Scan(_ context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	progress(cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 1, Total: 1})
	return f.scan, f.scanErr
}

func (f *fakeScanner) Clean(_ context.Context, req cleanup.CleanRequest, _ func(cleanup.Progress)) (cleanup.CleanResult, error) {
	f.reqs = append(f.reqs, req)
	return f.clean, f.clnErr
}

func newTestCleanup(t *testing.T) (*Cleanup, *fakeStore, string) {
	t.Helper()
	store := &fakeStore{}
	home := filepath.Join(t.TempDir(), "home")
	return NewCleanup(store, home, slog.New(slog.DiscardHandler), schedule.NewActivity(nil)), store, home
}

func TestRegisterCleaner(t *testing.T) {
	c, _, _ := newTestCleanup(t)
	RegisterCleaner(c, CleanerSystemJunk, &fakeScanner{})
	RegisterCleaner(c, CleanerDevJunk, &fakeScanner{})
	if got := c.Available(); len(got) != 2 || got[0] != CleanerDevJunk {
		t.Errorf("Available = %v", got)
	}
	for name, fn := range map[string]func(){
		"twice":   func() { RegisterCleaner(c, CleanerSystemJunk, &fakeScanner{}) },
		"unknown": func() { RegisterCleaner(c, "mystery", &fakeScanner{}) },
		"nil":     func() { RegisterCleaner(c, CleanerInstallers, nil) },
	} {
		func() {
			defer func() {
				if recover() == nil {
					t.Errorf("%s did not panic", name)
				}
			}()
			fn()
		}()
	}
	if _, err := c.Scan(t.Context(), CleanerInstallers, nil); err == nil || err.Error() != "这项清理功能还没有接入" {
		t.Errorf("unregistered cleaner: %v", err)
	}
	// Every cleaner records under its own history kind.
	for id := range cleanerKinds {
		if cleanerKinds[id] == "" || cleanerKinds[id] == history.KindMaintenance {
			t.Errorf("cleaner %q has kind %q", id, cleanerKinds[id])
		}
	}
}

func TestCleanRecordsHistory(t *testing.T) {
	c, store, home := newTestCleanup(t)
	started := time.Date(2026, 11, 14, 2, 0, 0, 0, time.UTC)
	failures := []cleanup.Failure{{Path: filepath.Join(home, "Library", "Caches", "x"), Reason: "文件正在使用，已跳过"}}
	for i := range 25 {
		failures = append(failures, cleanup.Failure{Path: "/elsewhere/" + string(rune('a'+i)), Reason: "删除失败"})
	}
	s := &fakeScanner{clean: cleanup.CleanResult{
		Freed: 1 << 30, Trashed: 4096, Removed: 35, Failed: failures, Partial: true, StartedAt: started, Duration: time.Minute,
		Categories: []cleanup.CategoryOutcome{
			{CategoryID: "caches", Title: "系统缓存", Removed: 34, Freed: 1 << 30, Failed: 1},
			{CategoryID: "dmg", Title: "安装包", Removed: 1, Trashed: 4096, Failed: 25},
		},
	}}
	RegisterCleaner(c, CleanerSystemJunk, s)
	req := cleanup.CleanRequest{CategoryIDs: []string{"caches", "dmg"}}
	res, err := c.Clean(t.Context(), CleanerSystemJunk, req, nil)
	if err != nil || res.Removed != 35 {
		t.Fatalf("Clean = %+v, %v", res, err)
	}
	if len(s.reqs) != 1 || s.reqs[0].CategoryIDs[1] != "dmg" {
		t.Errorf("scanner got %+v", s.reqs)
	}
	if len(store.appended) != 1 {
		t.Fatalf("history %+v", store.appended)
	}
	r := store.appended[0]
	if r.Kind != history.KindSystemJunk || !r.StartedAt.Equal(started) || !r.EndedAt.Equal(started.Add(time.Minute)) ||
		*r.Summary != (history.Totals{Freed: 1 << 30, Trashed: 4096, Items: 35, Failed: 26}) || len(r.ID) != len("20261114T020000Z-3f9a0c1d") {
		t.Errorf("run %+v", r)
	}
	bin := binName()
	want := []string{
		"系统缓存：已清理 34 项，1 项未能处理",
		"安装包：已移到" + bin + " 1 项，25 项未能处理",
		filepath.Join("~", "Library", "Caches", "x") + "：文件正在使用，已跳过",
	}
	for i, w := range want {
		if r.Details[i] != w {
			t.Errorf("details[%d] = %q, want %q", i, r.Details[i], w)
		}
	}
	if last := r.Details[len(r.Details)-1]; last != "另有 6 项未能处理" || len(r.Details) != 2+maxDetailFailures+1 {
		t.Errorf("details end %q (%d lines)", last, len(r.Details))
	}

	// A clean that touched nothing is not recorded.
	s.clean = cleanup.CleanResult{Skipped: 3}
	if _, err := c.Clean(t.Context(), CleanerSystemJunk, req, nil); err != nil || len(store.appended) != 1 {
		t.Errorf("empty clean recorded: %v, %d runs", err, len(store.appended))
	}
}

func TestSmartCleanRecordsOneLinePerSource(t *testing.T) {
	c, store, _ := newTestCleanup(t)
	started := time.Date(2026, 11, 14, 3, 0, 0, 0, time.UTC)
	RegisterCleaner(c, CleanerSmartScan, &fakeScanner{clean: cleanup.CleanResult{
		Freed: 3000, Trashed: 500, Removed: 9, StartedAt: started, Duration: time.Second, Partial: true,
		Failed: []cleanup.Failure{{CategoryID: "installers:dmg", Reason: "「磁盘映像」没有清理：扫描结果已过期，请重新扫描"}},
		Categories: []cleanup.CategoryOutcome{
			{CategoryID: "system-junk:app-caches", Title: "应用缓存", Source: "system-junk", Removed: 4, Freed: 2000},
			{CategoryID: "system-junk:logs", Title: "日志", Source: "system-junk", Removed: 2, Freed: 500, Failed: 1},
			{CategoryID: "dev-junk:tool-npm", Title: "npm 缓存", Source: "dev-junk", Removed: 1, Freed: 500},
			{CategoryID: "dev-junk:tool-xcode-archives", Title: "Xcode 归档", Source: "dev-junk", Removed: 2, Trashed: 500},
			{CategoryID: "installers:dmg", Title: "磁盘映像", Source: "installers", Failed: 3},
		},
	}})
	if _, err := c.Clean(t.Context(), CleanerSmartScan, cleanup.CleanRequest{CategoryIDs: []string{"system-junk:app-caches"}}, nil); err != nil {
		t.Fatal(err)
	}
	if len(store.appended) != 1 {
		t.Fatalf("history %+v", store.appended)
	}
	r := store.appended[0]
	bin := binName()
	want := []string{
		"系统垃圾：已清理 6 项，1 项未能处理",
		"开发者垃圾：已清理 1 项，已移到" + bin + " 2 项",
		"安装包：3 项未能处理",
		"「磁盘映像」没有清理：扫描结果已过期，请重新扫描",
	}
	if r.Kind != history.KindSmartScan || *r.Summary != (history.Totals{Freed: 3000, Trashed: 500, Items: 9, Failed: 1}) || strings.Join(r.Details, "\n") != strings.Join(want, "\n") {
		t.Errorf("run %+v, details %q", r, r.Details)
	}
}

func TestCleanupErrorsAreChinese(t *testing.T) {
	c, store, _ := newTestCleanup(t)
	s := &fakeScanner{}
	RegisterCleaner(c, CleanerDevJunk, s)
	cases := []struct {
		scan bool
		err  error
		want string
	}{
		{true, errors.New("walk: boom"), "扫描没能完成，详情已记录到日志"},
		{true, cleanup.NewUserError("需要「完全磁盘访问权限」才能扫描", errors.New("EPERM")), "需要「完全磁盘访问权限」才能扫描"},
		{false, cleanup.ErrBusy, "已有清理正在进行，请等它结束后再试"},
		{false, cleanup.ErrStale, "扫描结果已过期，请重新扫描"},
		{false, cleanup.ErrNoScan, "扫描结果已过期，请重新扫描"},
		{false, cleanup.ErrNothingSelected, "没有选择任何项目"},
		{false, errors.New("odd"), "清理没能开始，详情已记录到日志"},
	}
	for _, tc := range cases {
		var err error
		if tc.scan {
			s.scanErr = tc.err
			_, err = c.Scan(t.Context(), CleanerDevJunk, nil)
		} else {
			s.clnErr = tc.err
			_, err = c.Clean(t.Context(), CleanerDevJunk, cleanup.CleanRequest{}, nil)
		}
		if err == nil || err.Error() != tc.want {
			t.Errorf("%v: got %v, want %q", tc.err, err, tc.want)
		}
	}
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	s.scanErr = ctx.Err()
	if _, err := c.Scan(ctx, CleanerDevJunk, nil); err == nil || err.Error() != "已取消" {
		t.Errorf("cancelled scan: %v", err)
	}
	if len(store.appended) != 0 {
		t.Errorf("failed cleans recorded: %+v", store.appended)
	}
}
