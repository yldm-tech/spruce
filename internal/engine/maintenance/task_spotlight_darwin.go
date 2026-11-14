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
	// spotlightSettlePoll and spotlightSettleTimeout bound the wait for a volume that is transitioning after mdutil -E.
	spotlightSettlePoll    = 2 * time.Second
	spotlightSettleTimeout = 30 * time.Second
)

// spotlightTask verifies the Spotlight index of the boot volume and rebuilds it only when mdutil says its state is unknown.
//
// Criteria, from the output of mdutil -s / (read as the user in Precheck and again as root in the child), classified line by line:
//
//   - "Indexing enabled." is healthy: the task ends Unchanged in Precheck, without an administrator prompt and without touching the index.
//   - "Indexing disabled." or "Indexing and searching disabled." is the user's choice (people turn indexing off for privacy, performance or another search tool): the task ends Skipped in Precheck and changes nothing. Turning indexing back on is not part of 校验.
//   - "Error: unexpected indexing state." (for example kMDConfigSearchLevelTransitioning) or a line about transitioning means the volume is changing state: the task ends Skipped and changes nothing.
//   - "Error: unknown indexing state." is broken: the child runs mdutil -E / to erase and rebuild the index, which keeps the volume's enabled setting. This is the only path to -E.
//   - Any other output, including other Error lines ("mdsManageCheckin failed", "could not create client port", "unable to perform operation"), is not trusted: the task fails without changing anything.
//
// After -E the child polls the state for up to spotlightSettleTimeout, since the volume is often transitioning right after the change.
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
		Run: func(ctx context.Context, env Env) taskresult.Result {
			return runSpotlight(ctx, env, sleepCtx)
		},
	}
}

// spotlightState is the indexing state of the boot volume.
type spotlightState int

const (
	// spotlightUnparsed is output the task does not trust, including Error lines other than the unknown state.
	spotlightUnparsed spotlightState = iota
	spotlightEnabled
	spotlightDisabled
	// spotlightTransient is a volume changing state; it settles by itself.
	spotlightTransient
	// spotlightBroken is the one state that is repaired: "Error: unknown indexing state.".
	spotlightBroken
)

// parseSpotlightStatus classifies the output of mdutil -s for one volume, e.g. "/:\n\tIndexing enabled. \n", line by line. A disabled volume stays disabled whatever else is printed; a transitioning one is never treated as broken; any Error line other than the unknown state makes the whole output untrusted.
func parseSpotlightStatus(out string) spotlightState {
	var disabled, enabled, transient, unknown, otherError bool
	for line := range strings.Lines(strings.ToLower(out)) {
		line = strings.TrimSpace(line)
		if strings.Contains(line, "indexing and searching disabled") || strings.Contains(line, "indexing disabled") {
			disabled = true
		}
		switch {
		case strings.Contains(line, "error: unknown indexing state"):
			unknown = true
		case strings.Contains(line, "unexpected indexing state"), strings.Contains(line, "transitioning"), strings.Contains(line, "already changing state"):
			transient = true
		case strings.Contains(line, "error"):
			otherError = true
		case strings.Contains(line, "indexing enabled"):
			enabled = true
		}
	}
	switch {
	case disabled:
		return spotlightDisabled
	case transient:
		return spotlightTransient
	case otherError:
		return spotlightUnparsed
	case unknown:
		return spotlightBroken
	case enabled:
		return spotlightEnabled
	}
	return spotlightUnparsed
}

// readSpotlightState runs mdutil -s on the boot volume. A non-zero exit whose output still names the unknown or a transient state counts as that state; any other failure is returned as a final Result.
func readSpotlightState(ctx context.Context, runner execx.Runner) (spotlightState, string, *taskresult.Result) {
	cmd := execx.Command{Name: pathMdutil, Args: []string{"-s", spotlightVolume}, Env: cLocale, Timeout: spotlightStatusTimeout}
	out, err := runner.Run(ctx, cmd)
	text := strings.TrimSpace(combinedOutput(out))
	state := parseSpotlightStatus(text)
	if err != nil && state != spotlightBroken && state != spotlightTransient {
		res := commandFailure("无法读取 Spotlight 索引状态", cmd, err)
		return spotlightUnparsed, text, &res
	}
	if state == spotlightUnparsed {
		return state, text, &taskresult.Result{Outcome: taskresult.Failed, Message: "无法识别 Spotlight 索引状态，未做改动", Detail: "mdutil -s /: " + text}
	}
	return state, text, nil
}

// spotlightVerdict is the final Result for a state that needs no change, or nil for the broken state.
func spotlightVerdict(state spotlightState, text string) *taskresult.Result {
	switch state {
	case spotlightEnabled:
		return &taskresult.Result{Outcome: taskresult.Unchanged, Message: "Spotlight 索引正常，无需修复", Detail: text}
	case spotlightDisabled:
		return &taskresult.Result{Outcome: taskresult.Skipped, Message: "Spotlight 索引已被关闭（可能是你手动关闭的），未做改动；需要时可在「系统设置 › Spotlight」里重新开启", Detail: text}
	case spotlightTransient:
		return &taskresult.Result{Outcome: taskresult.Skipped, Message: "Spotlight 索引状态正在变化，未做改动；稍后再运行一次", Detail: text}
	}
	return nil
}

func precheckSpotlight(ctx context.Context, env Env) *taskresult.Result {
	state, text, res := readSpotlightState(ctx, env.Runner)
	if res != nil {
		return res
	}
	return spotlightVerdict(state, text)
}

// runSpotlight runs in the elevated child. It re-reads the state, since it may have changed since the precheck, and rebuilds only a broken index. sleep waits between the reads after the rebuild.
func runSpotlight(ctx context.Context, env Env, sleep func(context.Context, time.Duration) error) taskresult.Result {
	state, before, res := readSpotlightState(ctx, env.Runner)
	if res != nil {
		return *res
	}
	if res := spotlightVerdict(state, before); res != nil {
		return *res
	}
	cmd := execx.Command{Name: pathMdutil, Args: []string{"-E", spotlightVolume}, Env: cLocale, Timeout: spotlightChangeTimeout}
	if _, err := env.Runner.Run(ctx, cmd); err != nil {
		return commandFailure("重建 Spotlight 索引失败", cmd, err)
	}

	const started = "已开始重建 Spotlight 索引，会在后台进行，可能需要一段时间"
	after, afterText := spotlightUnparsed, ""
	for waited := time.Duration(0); ; waited += spotlightSettlePoll {
		var res *taskresult.Result
		after, afterText, res = readSpotlightState(ctx, env.Runner)
		if res != nil {
			afterText = res.Detail
			break
		}
		if after != spotlightTransient || waited >= spotlightSettleTimeout || sleep(ctx, spotlightSettlePoll) != nil {
			break
		}
	}
	detail := joinDetail("before: "+before, "after: "+afterText)
	switch after {
	case spotlightEnabled:
		return taskresult.Result{Outcome: taskresult.Applied, Message: started, Detail: detail}
	case spotlightTransient:
		return taskresult.Result{Outcome: taskresult.Applied, Message: started + "；系统正在后台切换索引状态", Detail: detail}
	case spotlightDisabled:
		return taskresult.Result{Outcome: taskresult.Applied, Partial: true, Message: "已清除 Spotlight 索引，但索引目前处于关闭状态，未重新开启", Detail: detail}
	}
	// mdutil -E exited 0, so the rebuild was requested even though the state cannot be read back.
	return taskresult.Result{Outcome: taskresult.Applied, Partial: true, Message: started + "；暂时无法确认索引状态", Detail: detail}
}

// sleepCtx waits for d or until ctx ends, and returns ctx's error in that case.
func sleepCtx(ctx context.Context, d time.Duration) error {
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-t.C:
		return nil
	}
}
