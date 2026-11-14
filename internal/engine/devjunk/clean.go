package devjunk

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/execx"
)

// maxOutputInReason bounds how much of a failed command's output the page shows.
const maxOutputInReason = 160

// Clean deletes what req names from the last scan: the file categories through the Executor, then the command categories by running their commands. It deletes nothing and returns an error when the request does not match the last scan (cleanup.ErrStale), names nothing (cleanup.ErrNothingSelected) or another clean runs (cleanup.ErrBusy).
func (c *Cleaner) Clean(ctx context.Context, req cleanup.CleanRequest, progress func(cleanup.Progress)) (cleanup.CleanResult, error) {
	if !c.cleaning.TryLock() {
		return cleanup.CleanResult{}, cleanup.ErrBusy
	}
	defer c.cleaning.Unlock()
	if progress == nil {
		progress = func(cleanup.Progress) {}
	}
	c.mu.Lock()
	snap := c.last
	c.mu.Unlock()
	if snap == nil {
		return cleanup.CleanResult{}, cleanup.ErrNoScan
	}

	picked := map[string]bool{}
	for _, id := range req.CategoryIDs {
		if !slices.Contains(snap.order, id) {
			return cleanup.CleanResult{}, fmt.Errorf("%w: category %q", cleanup.ErrStale, id)
		}
		picked[id] = true
	}
	itemSet := map[string]bool{}
	for _, id := range req.ItemIDs {
		cat, ok := snap.items[id]
		if !ok || !picked[cat] {
			return cleanup.CleanResult{}, fmt.Errorf("%w: item %q", cleanup.ErrStale, id)
		}
		itemSet[id] = true
	}

	var jobs []*commandJob
	fileReq := cleanup.CleanRequest{}
	fileItems := 0
	for _, id := range snap.order {
		if !picked[id] {
			continue
		}
		if job := snap.commands[id]; job != nil {
			if len(itemSet) == 0 || itemSet[job.itemID] {
				jobs = append(jobs, job)
			}
			continue
		}
		n := 0
		for itemID, cat := range snap.items {
			if cat == id && (len(itemSet) == 0 || itemSet[itemID]) {
				n++
				if len(itemSet) > 0 {
					fileReq.ItemIDs = append(fileReq.ItemIDs, itemID)
				}
			}
		}
		if n > 0 {
			fileReq.CategoryIDs = append(fileReq.CategoryIDs, id)
			fileItems += n
		}
	}
	total := fileItems + len(jobs)
	if total == 0 {
		return cleanup.CleanResult{}, cleanup.ErrNothingSelected
	}

	started := c.opts.Now()
	res := cleanup.CleanResult{StartedAt: started, Failed: []cleanup.Failure{}}
	if fileItems > 0 {
		fileRes, err := c.exec.Clean(ctx, fileReq, func(p cleanup.Progress) {
			p.Total = total
			progress(p)
		})
		if err != nil {
			return cleanup.CleanResult{}, err
		}
		res = fileRes
		res.StartedAt = started
	}

	done := fileItems
	ran := map[string]bool{}
	for _, job := range jobs {
		if res.Interrupted || ctx.Err() != nil {
			res.Interrupted = true
			break
		}
		progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, CategoryID: job.categoryID, Path: job.path, Done: done, Total: total, Bytes: res.Freed + res.Trashed})
		out := cleanup.CategoryOutcome{CategoryID: job.categoryID, Title: job.title}
		// A clean command is never cut off half way: stopping takes effect before the next one.
		_, err := c.opts.Runner.Run(context.WithoutCancel(ctx), job.cmd)
		if err != nil {
			res.Failed = append(res.Failed, cleanup.Failure{ItemID: job.itemID, CategoryID: job.categoryID, Path: job.path, Reason: commandReason(job.cmd, err), Detail: err.Error()})
			out.Failed++
		} else {
			freed := max(0, job.size-job.measure(context.WithoutCancel(ctx)))
			out.Removed++
			out.Freed = freed
			res.Removed++
			res.Freed += freed
			c.forget(job.categoryID)
		}
		done++
		ran[job.categoryID] = true
		res.Categories = append(res.Categories, out)
	}
	for _, job := range jobs {
		if !ran[job.categoryID] {
			res.Categories = append(res.Categories, cleanup.CategoryOutcome{CategoryID: job.categoryID, Title: job.title})
		}
	}
	if len(jobs) > 0 {
		progress(cleanup.Progress{Phase: cleanup.PhaseCleaning, Done: done, Total: total, Bytes: res.Freed + res.Trashed})
	}

	// One outcome per requested category, in scan order.
	order := map[string]int{}
	for i, id := range snap.order {
		order[id] = i
	}
	slices.SortStableFunc(res.Categories, func(a, b cleanup.CategoryOutcome) int { return order[a.CategoryID] - order[b.CategoryID] })
	res.Partial = res.Interrupted || len(res.Failed) > 0
	res.Duration = time.Since(started)
	return res, nil
}

// forget drops a command category that ran from the last scan, so it is not run again without a new scan.
func (c *Cleaner) forget(categoryID string) {
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.last == nil {
		return
	}
	delete(c.last.commands, categoryID)
	c.last.order = slices.DeleteFunc(slices.Clone(c.last.order), func(id string) bool { return id == categoryID })
	for id, cat := range c.last.items {
		if cat == categoryID {
			delete(c.last.items, id)
		}
	}
}

// commandReason is the sentence the page shows for a command that failed, with the end of what it printed.
func commandReason(cmd execx.Command, err error) string {
	name := commandLabel(cmd)
	var exitErr *execx.ExitError
	switch {
	case errors.As(err, &exitErr):
		if msg := lastLine(exitErr.Stderr); msg != "" {
			return fmt.Sprintf("%s 没有成功：%s", name, msg)
		}
		return fmt.Sprintf("%s 没有成功（退出码 %d）", name, exitErr.ExitCode)
	case errors.Is(err, context.DeadlineExceeded):
		return fmt.Sprintf("%s 运行超时，已停止", name)
	}
	return fmt.Sprintf("无法运行 %s", name)
}

// lastLine is the last non-empty line of s, shortened for the page.
func lastLine(s string) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	line := strings.TrimSpace(lines[len(lines)-1])
	if r := []rune(line); len(r) > maxOutputInReason {
		line = string(r[:maxOutputInReason]) + "…"
	}
	return line
}

// commandLabel is the command as the user would type it: the binary's name without its folder or extension, and the arguments.
func commandLabel(cmd execx.Command) string {
	name := cmd.Name
	if i := strings.LastIndexAny(name, `/\`); i >= 0 {
		name = name[i+1:]
	}
	for _, ext := range []string{".exe", ".cmd"} {
		name = strings.TrimSuffix(name, ext)
	}
	return strings.Join(append([]string{name}, cmd.Args...), " ")
}

// Command is a category of the last scan that is cleaned by running a command.
type Command struct {
	CategoryID string
	// Command is what runs, as typed in a terminal, e.g. "brew cleanup -s --prune=all".
	Command string
}

// Details is what the page needs about the last scan beyond the cleanup model.
type Details struct {
	// DefaultItems are the IDs of the items that start checked.
	DefaultItems []string
	// Commands are the categories cleaned by running a command, in scan order.
	Commands []Command
}

// Details reports the last scan's default selection and command categories; empty before any scan.
func (c *Cleaner) Details() Details {
	c.mu.Lock()
	defer c.mu.Unlock()
	d := Details{DefaultItems: []string{}, Commands: []Command{}}
	if c.last == nil {
		return d
	}
	d.DefaultItems = append(d.DefaultItems, c.last.defaults...)
	for _, id := range c.last.order {
		if job := c.last.commands[id]; job != nil {
			d.Commands = append(d.Commands, Command{CategoryID: id, Command: commandLabel(job.cmd)})
		}
	}
	return d
}
