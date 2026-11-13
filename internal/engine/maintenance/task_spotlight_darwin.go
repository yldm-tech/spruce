package maintenance

import (
	"context"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// spotlightVolume is the boot volume; on an APFS volume group mdutil applies the setting to the whole group.
const spotlightVolume = "/"

const (
	spotlightStatusTimeout = 15 * time.Second
	spotlightChangeTimeout = 60 * time.Second
)

// spotlightTask verifies the Spotlight index of the boot volume and repairs it only when it is not working.
//
// Criteria, from the output of mdutil -s / (read as the user in Precheck and again as root in the child):
//
//   - "Indexing enabled." is healthy: the task ends Unchanged in Precheck, without an administrator prompt and without touching the index.
//   - "Indexing disabled." or "Indexing and searching disabled." is disabled: the child runs mdutil -i on / to turn indexing back on; macOS then indexes in the background.
//   - "unknown indexing state" or an "Error" line is broken: the child runs mdutil -i on / and then mdutil -E / to erase and rebuild the index.
//   - Any other output is not trusted: the task fails without changing anything.
//
// After a change the child reads the state again and reports Failed if indexing is still not enabled (for example when a configuration profile keeps it off).
func spotlightTask() TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "spotlight",
			Title:            "校验 Spotlight 索引",
			Description:      "搜索结果缺失或不准时修复",
			Icon:             IconSearch,
			DefaultOn:        false,
			NeedsAdmin:       true,
			EstimatedSeconds: 5,
		},
		Precheck: precheckSpotlight,
		Run:      runSpotlight,
	}
}

// spotlightState is the indexing state of the boot volume.
type spotlightState int

const (
	spotlightUnparsed spotlightState = iota
	spotlightEnabled
	spotlightDisabled
	spotlightBroken
)

// parseSpotlightStatus classifies the output of mdutil -s for one volume, e.g. "/:\n\tIndexing enabled. \n".
func parseSpotlightStatus(out string) spotlightState {
	s := strings.ToLower(out)
	switch {
	case strings.Contains(s, "unknown indexing state"), strings.Contains(s, "error"):
		return spotlightBroken
	case strings.Contains(s, "indexing and searching disabled"), strings.Contains(s, "indexing disabled"):
		return spotlightDisabled
	case strings.Contains(s, "indexing enabled"):
		return spotlightEnabled
	}
	return spotlightUnparsed
}

// readSpotlightState runs mdutil -s on the boot volume. A non-zero exit whose output still names a broken state counts as broken; any other failure is returned as a final Result.
func readSpotlightState(ctx context.Context, runner execx.Runner) (spotlightState, string, *taskresult.Result) {
	cmd := execx.Command{Name: pathMdutil, Args: []string{"-s", spotlightVolume}, Env: cLocale, Timeout: spotlightStatusTimeout}
	out, err := runner.Run(ctx, cmd)
	text := strings.TrimSpace(combinedOutput(out))
	state := parseSpotlightStatus(text)
	if err != nil && state != spotlightBroken {
		res := commandFailure("无法读取 Spotlight 索引状态", cmd, err)
		return spotlightUnparsed, text, &res
	}
	if state == spotlightUnparsed {
		return state, text, &taskresult.Result{Outcome: taskresult.Failed, Message: "无法识别 Spotlight 索引状态，未做改动", Detail: "mdutil -s /: " + text}
	}
	return state, text, nil
}

func precheckSpotlight(ctx context.Context, env Env) *taskresult.Result {
	state, text, res := readSpotlightState(ctx, env.Runner)
	if res != nil {
		return res
	}
	if state == spotlightEnabled {
		return &taskresult.Result{Outcome: taskresult.Unchanged, Message: "Spotlight 索引正常，无需修复", Detail: text}
	}
	return nil
}

// runSpotlight runs in the elevated child.
func runSpotlight(ctx context.Context, env Env) taskresult.Result {
	state, before, res := readSpotlightState(ctx, env.Runner)
	if res != nil {
		return *res
	}
	var msg string
	switch state {
	case spotlightEnabled:
		return taskresult.Result{Outcome: taskresult.Unchanged, Message: "Spotlight 索引正常，无需修复", Detail: before}
	case spotlightDisabled:
		if r := runMdutil(ctx, env.Runner, "-i", "on"); r != nil {
			return *r
		}
		msg = "已重新开启 Spotlight 索引，系统会在后台建立索引，期间搜索结果可能不全"
	case spotlightBroken:
		if r := runMdutil(ctx, env.Runner, "-i", "on"); r != nil {
			return *r
		}
		if r := runMdutil(ctx, env.Runner, "-E"); r != nil {
			return *r
		}
		msg = "已开始重建 Spotlight 索引，会在后台进行，可能需要一段时间"
	}

	after, afterText, res := readSpotlightState(ctx, env.Runner)
	if res != nil {
		return *res
	}
	if after != spotlightEnabled {
		return taskresult.Result{Outcome: taskresult.Failed, Message: "无法开启 Spotlight 索引，可能被描述文件或系统设置限制", Detail: joinDetail("before: "+before, "after: "+afterText)}
	}
	return taskresult.Result{Outcome: taskresult.Applied, Message: msg, Detail: joinDetail("before: "+before, "after: "+afterText)}
}

// runMdutil runs mdutil with args on the boot volume and returns a final Result when it fails.
func runMdutil(ctx context.Context, runner execx.Runner, args ...string) *taskresult.Result {
	cmd := execx.Command{Name: pathMdutil, Args: append(args, spotlightVolume), Env: cLocale, Timeout: spotlightChangeTimeout}
	if _, err := runner.Run(ctx, cmd); err != nil {
		res := commandFailure("修复 Spotlight 索引失败", cmd, err)
		return &res
	}
	return nil
}
