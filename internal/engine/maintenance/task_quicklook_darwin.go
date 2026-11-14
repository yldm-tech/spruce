package maintenance

import (
	"context"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

const quickLookTimeout = 30 * time.Second

// quickLookTask resets the Quick Look server and its generators (qlmanage -r) and the thumbnail disk cache (qlmanage -r cache). Both act on the current user's Quick Look only and need no administrator rights; previews are regenerated on demand.
func quickLookTask() TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "quicklook",
			Title:            "刷新 QuickLook 预览",
			Description:      "空格预览一直显示旧内容时有用",
			Icon:             IconPreview,
			DefaultOn:        true,
			NeedsAdmin:       false,
			EstimatedSeconds: 2,
		},
		Run: runQuickLook,
	}
}

func runQuickLook(ctx context.Context, env Env) taskresult.Result {
	for _, args := range [][]string{{"-r"}, {"-r", "cache"}} {
		cmd := execx.Command{Name: pathQlmanage, Args: args, Timeout: quickLookTimeout}
		if _, err := env.Runner.Run(ctx, cmd); err != nil {
			return commandFailure("刷新 QuickLook 预览失败", cmd, err)
		}
	}
	return taskresult.Result{Outcome: taskresult.Applied, Message: "已刷新 QuickLook 预览，下次按空格会重新生成"}
}
