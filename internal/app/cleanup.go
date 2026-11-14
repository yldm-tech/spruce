package app

import (
	"cmp"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"time"

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

// cleanerTitles are the page names the history uses for the cleaners a combined clean (智能扫描) reports by source.
var cleanerTitles = map[CleanerID]string{
	CleanerSystemJunk: "系统垃圾",
	CleanerDevJunk:    "开发者垃圾",
	CleanerInstallers: "安装包",
}

// progressEvery bounds how often scan and clean progress reaches the page.
const progressEvery = 80 * time.Millisecond

// maxDetailFailures is how many failed items a history record lists by path.
const maxDetailFailures = 20

// Cleanup is the controller every cleaner page scans and cleans through, by CleanerID. It records each clean in the history.
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

// throttled returns fn throttled to one call per progressEvery, keeping the last (nil reports nothing).
func throttled(fn func(cleanup.Progress)) func(cleanup.Progress) {
	if fn == nil {
		return func(cleanup.Progress) {}
	}
	return cleanup.Throttle(fn, progressEvery)
}

// Scan runs the scan of cleaner, reports its progress (throttled; nil reports nothing) and returns what it found; the cleaner remembers the result for Clean. Cancelling ctx cancels the scan. Errors are sentences in Simplified Chinese.
func (c *Cleanup) Scan(ctx context.Context, cleaner CleanerID, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	s, err := c.scanner(cleaner)
	if err != nil {
		return cleanup.ScanResult{}, err
	}
	res, err := s.Scan(ctx, throttled(progress))
	if err != nil {
		return cleanup.ScanResult{}, c.userError(ctx, cleaner, "scan", err)
	}
	return res, nil
}

// Clean deletes what req names from cleaner's last scan, reports progress (throttled; nil reports nothing), records the clean in the history and returns what it did. It fails without deleting anything when the request does not match the last scan (the page then scans again). Cancelling ctx stops the clean after the current item; the result says it was interrupted.
func (c *Cleanup) Clean(ctx context.Context, cleaner CleanerID, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	s, err := c.scanner(cleaner)
	if err != nil {
		return cleanup.CleanResult{}, err
	}
	res, err := s.Clean(ctx, req, throttled(progress))
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

// details are the history lines of a clean: one per category (one per source for a combined clean, whose outcomes carry their source), then the items left in place.
func (c *Cleanup) details(res cleanup.CleanResult) []string {
	var lines []string
	for _, g := range groupOutcomes(res.Categories) {
		if line := outcomeLine(g); line != "" {
			lines = append(lines, line)
		}
	}
	for i, f := range res.Failed {
		if i == maxDetailFailures {
			lines = append(lines, fmt.Sprintf("另有 %d 项未能处理", len(res.Failed)-maxDetailFailures))
			break
		}
		if f.Path == "" {
			lines = append(lines, f.Reason)
			continue
		}
		lines = append(lines, c.short(f.Path)+"："+f.Reason)
	}
	if res.Interrupted {
		lines = append(lines, "清理在中途停止，其余项目没有改动")
	}
	return lines
}

// outcomeGroup is what one history line sums up: a category, or every category of one source.
type outcomeGroup struct {
	title                    string
	removed, trashed, failed int
}

// groupOutcomes sums outcomes per source, in order of first appearance, when they carry one, and keeps one group per category otherwise.
func groupOutcomes(outs []cleanup.CategoryOutcome) []outcomeGroup {
	var groups []outcomeGroup
	bySource := map[string]int{}
	for _, o := range outs {
		i := len(groups)
		if o.Source != "" {
			if j, ok := bySource[o.Source]; ok {
				i = j
			} else {
				bySource[o.Source] = i
			}
		}
		if i == len(groups) {
			title := o.Title
			if o.Source != "" {
				title = cmp.Or(cleanerTitles[CleanerID(o.Source)], o.Source)
			}
			groups = append(groups, outcomeGroup{title: title})
		}
		g := &groups[i]
		// A category whose items went to the Trash counts as moved there, as on its own page.
		if o.Trashed > 0 {
			g.trashed += o.Removed
		} else {
			g.removed += o.Removed
		}
		g.failed += o.Failed
	}
	return groups
}

// outcomeLine words a group, e.g. "系统缓存：已清理 34 项，1 项未能处理"; "" when nothing happened to it.
func outcomeLine(g outcomeGroup) string {
	var parts []string
	if g.removed > 0 {
		parts = append(parts, fmt.Sprintf("已清理 %d 项", g.removed))
	}
	if g.trashed > 0 {
		parts = append(parts, fmt.Sprintf("已移到%s %d 项", binName(), g.trashed))
	}
	if g.failed > 0 {
		parts = append(parts, fmt.Sprintf("%d 项未能处理", g.failed))
	}
	if len(parts) == 0 {
		return ""
	}
	return g.title + "：" + strings.Join(parts, "，")
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
