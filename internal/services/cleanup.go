package services

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"runtime"
	"slices"
	"strings"
	"sync"
	"time"

	"github.com/egoist/mygo"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
)

// CleanerID names a cleaner that the Cleanup service runs. Each cleaner page has one; its engine registers a cleanup.Scanner under it in main.go with RegisterCleaner.
type CleanerID string

const (
	// CleanerSmartScan is the 智能扫描 page.
	CleanerSmartScan CleanerID = "smart-scan"
	// CleanerSystemJunk is the 系统垃圾 page.
	CleanerSystemJunk CleanerID = "system-junk"
	// CleanerDevJunk is the 开发者垃圾 page.
	CleanerDevJunk CleanerID = "dev-junk"
	// CleanerInstallers is the 安装包 page.
	CleanerInstallers CleanerID = "installers"
	// CleanerUninstall is the leftovers of the 应用卸载 page.
	CleanerUninstall CleanerID = "uninstall"
	// CleanerSpace is the deletions of the 空间透视 page.
	CleanerSpace CleanerID = "space"
)

// cleanerKinds is the history kind each cleaner records its cleans as.
var cleanerKinds = map[CleanerID]history.Kind{
	CleanerSmartScan:  history.KindSmartScan,
	CleanerSystemJunk: history.KindSystemJunk,
	CleanerDevJunk:    history.KindDevJunk,
	CleanerInstallers: history.KindInstallers,
	CleanerUninstall:  history.KindUninstall,
	CleanerSpace:      history.KindSpace,
}

// progressEvery bounds how often scan and clean progress reaches the page.
const progressEvery = 80 * time.Millisecond

// maxDetailFailures is how many failed items a history record lists by path.
const maxDetailFailures = 20

// Cleanup is the bound service every cleaner page scans and cleans through, by CleanerID. It records each clean in the history.
type Cleanup struct {
	history history.Store
	home    string
	log     *slog.Logger

	mu       sync.RWMutex
	cleaners map[CleanerID]cleanup.Scanner
}

// NewCleanup returns the service with no cleaners; main.go adds them with RegisterCleaner. home shortens paths in the history to "~/…".
func NewCleanup(store history.Store, home string, log *slog.Logger) *Cleanup {
	return &Cleanup{history: store, home: home, log: log, cleaners: make(map[CleanerID]cleanup.Scanner)}
}

// RegisterCleaner makes s the cleaner for id. It is a function rather than a method so the page cannot call it. It panics on an unknown or repeated id, so a wiring mistake shows at startup.
func RegisterCleaner(c *Cleanup, id CleanerID, s cleanup.Scanner) {
	if _, ok := cleanerKinds[id]; !ok {
		panic(fmt.Sprintf("services: unknown cleaner %q", id))
	}
	if s == nil {
		panic(fmt.Sprintf("services: nil scanner for %q", id))
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if _, dup := c.cleaners[id]; dup {
		panic(fmt.Sprintf("services: cleaner %q registered twice", id))
	}
	c.cleaners[id] = s
}

// Available lists the cleaners that are wired up, sorted.
func (c *Cleanup) Available() []CleanerID {
	c.mu.RLock()
	defer c.mu.RUnlock()
	ids := make([]CleanerID, 0, len(c.cleaners))
	for id := range c.cleaners {
		ids = append(ids, id)
	}
	slices.Sort(ids)
	return ids
}

func (c *Cleanup) scanner(id CleanerID) (cleanup.Scanner, error) {
	c.mu.RLock()
	defer c.mu.RUnlock()
	s, ok := c.cleaners[id]
	if !ok {
		return nil, errors.New("这项清理功能还没有接入")
	}
	return s, nil
}

// sender returns a throttled progress function that streams to ch (nil sends nothing).
func sender(ch *mygo.Channel[cleanup.Progress]) func(cleanup.Progress) {
	if ch == nil {
		return func(cleanup.Progress) {}
	}
	// A page that stopped listening also cancels ctx, which ends the work; the send error adds nothing.
	return cleanup.Throttle(func(p cleanup.Progress) { _ = ch.Send(p) }, progressEvery)
}

// Scan runs the scan of cleaner, streams its progress and returns what it found; the cleaner remembers the result for Clean. Closing the channel cancels the scan. Errors are sentences in Simplified Chinese.
func (c *Cleanup) Scan(ctx context.Context, cleaner CleanerID, progress *mygo.Channel[cleanup.Progress]) (cleanup.ScanResult, error) {
	s, err := c.scanner(cleaner)
	if err != nil {
		return cleanup.ScanResult{}, err
	}
	res, err := s.Scan(ctx, sender(progress))
	if err != nil {
		return cleanup.ScanResult{}, c.userError(ctx, cleaner, "scan", err)
	}
	return res, nil
}

// Clean deletes what req names from cleaner's last scan, streams progress, records the clean in the history and returns what it did. It fails without deleting anything when the request does not match the last scan (the page then scans again). Closing the channel stops the clean after the current item; the result says it was interrupted.
func (c *Cleanup) Clean(ctx context.Context, cleaner CleanerID, req cleanup.CleanRequest, progress *mygo.Channel[cleanup.Progress]) (cleanup.CleanResult, error) {
	s, err := c.scanner(cleaner)
	if err != nil {
		return cleanup.CleanResult{}, err
	}
	res, err := s.Clean(ctx, req, sender(progress))
	if err != nil {
		return cleanup.CleanResult{}, c.userError(ctx, cleaner, "clean", err)
	}
	if res.Removed+len(res.Failed) > 0 {
		run := history.Run{
			ID:        newRunID(res.StartedAt),
			Kind:      cleanerKinds[cleaner],
			StartedAt: res.StartedAt,
			EndedAt:   res.StartedAt.Add(res.Duration),
			Summary:   &history.Totals{Freed: res.Freed, Trashed: res.Trashed, Items: res.Removed, Failed: len(res.Failed)},
			Details:   c.details(res),
		}
		if err := c.history.Append(run); err != nil {
			// The items are already gone; failing the call would tell the user they were not.
			c.log.Error("record clean in history", "cleaner", cleaner, "err", err)
		}
	}
	for _, f := range res.Failed {
		c.log.Info("clean left an item", "cleaner", cleaner, "path", f.Path, "reason", f.Reason, "detail", f.Detail)
	}
	return res, nil
}

// details are the history lines of a clean: one per category, then the items left in place.
func (c *Cleanup) details(res cleanup.CleanResult) []string {
	bin := "废纸篓"
	if runtime.GOOS == "windows" {
		bin = "回收站"
	}
	var lines []string
	for _, cat := range res.Categories {
		var parts []string
		if cat.Removed > 0 {
			if cat.Trashed > 0 {
				parts = append(parts, fmt.Sprintf("已移到%s %d 项", bin, cat.Removed))
			} else {
				parts = append(parts, fmt.Sprintf("已清理 %d 项", cat.Removed))
			}
		}
		if cat.Failed > 0 {
			parts = append(parts, fmt.Sprintf("%d 项未能处理", cat.Failed))
		}
		if len(parts) > 0 {
			lines = append(lines, cat.Title+"："+strings.Join(parts, "，"))
		}
	}
	for i, f := range res.Failed {
		if i == maxDetailFailures {
			lines = append(lines, fmt.Sprintf("另有 %d 项未能处理", len(res.Failed)-maxDetailFailures))
			break
		}
		lines = append(lines, c.short(f.Path)+"："+f.Reason)
	}
	if res.Interrupted {
		lines = append(lines, "清理在中途停止，其余项目没有改动")
	}
	return lines
}

// short replaces the home folder at the start of p with "~".
func (c *Cleanup) short(p string) string {
	if c.home == "" {
		return p
	}
	if rel, err := filepath.Rel(c.home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(os.PathSeparator)) {
		return filepath.Join("~", rel)
	}
	return p
}

// userError turns an error of a cleaner into the sentence the page shows; the cleaner's own error goes to the log.
func (c *Cleanup) userError(ctx context.Context, cleaner CleanerID, op string, err error) error {
	if ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return errors.New("已取消")
	}
	c.log.Warn("cleaner failed", "cleaner", cleaner, "op", op, "err", err)
	var ue *cleanup.UserError
	switch {
	case errors.As(err, &ue):
		return errors.New(ue.Message)
	case errors.Is(err, cleanup.ErrBusy):
		return errors.New("已有清理正在进行，请等它结束后再试")
	case errors.Is(err, cleanup.ErrNoScan), errors.Is(err, cleanup.ErrStale):
		return errors.New("扫描结果已过期，请重新扫描")
	case errors.Is(err, cleanup.ErrNothingSelected):
		return errors.New("没有选择任何项目")
	case op == "scan":
		return errors.New("扫描没能完成，详情已记录到日志")
	}
	return errors.New("清理没能开始，详情已记录到日志")
}
