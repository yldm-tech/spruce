// Package maintenancefake is the 维护 page's Service for tests: the macOS and Windows catalogs of the design and a run the test scripts. Only tests import it.
package maintenancefake

import (
	"context"
	"sync"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/maintenance"
	"spruce/apps/spruce/internal/engine/taskresult"
)

// Task returns a catalog entry with a description derived from its title.
func Task(id, title string, icon maintenance.Icon, defaultOn bool, needsAdmin bool, seconds int) app.TaskView {
	return app.TaskView{ID: id, Title: title, Description: title + "的说明", Icon: icon, DefaultOn: defaultOn, NeedsAdmin: needsAdmin, EstimatedSeconds: seconds}
}

// MacCatalog is the design's six macOS cards.
func MacCatalog() []app.TaskView {
	db := Task("databases", "优化应用数据库", maintenance.IconDatabase, false, false, 45)
	db.Hint = "需要先退出 Safari 和邮件"
	return []app.TaskView{
		Task("dns", "刷新 DNS 缓存", maintenance.IconNetwork, true, true, 5),
		Task("spotlight", "校验 Spotlight 索引", maintenance.IconSearch, false, true, 30),
		Task("quicklook", "刷新 QuickLook 预览", maintenance.IconPreview, true, false, 5),
		Task("icons", "重建图标缓存", maintenance.IconIcons, true, true, 20),
		Task("launchservices", "修复「打开方式」", maintenance.IconOpenWith, true, false, 30),
		db,
	}
}

// WindowsCatalog is the Windows catalog's shape: five cards and no Full Disk Access task.
func WindowsCatalog() []app.TaskView {
	return []app.TaskView{
		Task("dns", "刷新 DNS 缓存", maintenance.IconNetwork, true, false, 10),
		Task("search", "检查 Windows 搜索", maintenance.IconSearch, false, true, 10),
		Task("thumbnails", "清除缩略图缓存", maintenance.IconPreview, true, false, 10),
		Task("icons", "刷新图标缓存", maintenance.IconIcons, true, false, 10),
		Task("associations", "清理失效的「打开方式」", maintenance.IconOpenWith, true, false, 10),
	}
}

// Result is a task's result.
func Result(id string, o taskresult.Outcome, msg string) taskresult.Result {
	return taskresult.Result{TaskID: id, Outcome: o, Message: msg, Duration: time.Millisecond}
}

// Summary counts results as app.Maintenance does.
func Summary(results ...taskresult.Result) app.RunSummary {
	s := app.RunSummary{RunID: "20261114T010412Z-3f9a0c1d", StartedAt: time.Date(2026, 11, 14, 1, 4, 12, 0, time.UTC),
		EndedAt: time.Date(2026, 11, 14, 1, 4, 40, 0, time.UTC), Results: results}
	for _, r := range results {
		switch r.Outcome {
		case taskresult.Applied:
			s.Applied++
		case taskresult.Unchanged:
			s.Unchanged++
		case taskresult.Skipped:
			s.Skipped++
		case taskresult.Unavailable:
			s.Unavailable++
		case taskresult.Failed:
			s.Failed++
		}
	}
	return s
}

// Service answers Catalog with Tasks and Run with OnRun (a run that waits for its context when nil). It records the IDs of every run.
type Service struct {
	Tasks []app.TaskView
	OnRun func(ctx context.Context, ids []string, progress func(app.ProgressEvent)) (app.RunSummary, error)

	mu   sync.Mutex
	runs [][]string
}

// Catalog implements maintenance.Service.
func (s *Service) Catalog() []app.TaskView { return s.Tasks }

// Run implements maintenance.Service.
func (s *Service) Run(ctx context.Context, ids []string, progress func(app.ProgressEvent)) (app.RunSummary, error) {
	s.mu.Lock()
	s.runs = append(s.runs, ids)
	fn := s.OnRun
	s.mu.Unlock()
	if fn == nil {
		<-ctx.Done()
		return app.RunSummary{Interrupted: true}, nil
	}
	return fn(ctx, ids, progress)
}

// Runs returns the IDs of every run so far.
func (s *Service) Runs() [][]string {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([][]string(nil), s.runs...)
}
