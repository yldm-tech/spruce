package shared

import (
	"strings"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// lastCleanLookback is how many recent runs are searched for a page's last clean.
const lastCleanLookback = 100

// LastCleanText describes a recorded clean in one line, e.g. "上次清理：3天前，释放 1.2 GB".
func LastCleanText(run history.Run, now time.Time) string {
	parts := []string{}
	if s := run.Summary; s != nil {
		if s.Freed > 0 {
			parts = append(parts, "释放 "+format.Bytes(s.Freed))
		}
		if s.Trashed > 0 {
			parts = append(parts, "移到"+format.TrashName()+" "+format.Bytes(s.Trashed))
		}
	}
	when := format.RelativeTime(run.EndedAt, now)
	if when == "" {
		when = format.RelativeTime(run.StartedAt, now)
	}
	return strings.Join(append([]string{"上次清理：" + when}, parts...), "，")
}

// LastClean returns a function reporting the newest recorded run of kind among the recent history, nil when there is none, read again whenever the history changes. Call it in a page's component; the reads stop when the page goes.
func LastClean(cx *core.Scope, kind history.Kind) func() *history.Run {
	h := store.Use(cx).History
	ctx := cx.Context()
	last := core.NewSignalFunc[*history.Run](cx, nil, nil)
	seq := 0
	cx.Effect(func() {
		h.Generation()
		seq++
		mine := seq
		h.Load(cx, ctx, lastCleanLookback, func(runs []history.Run, err error) {
			if mine != seq || err != nil {
				return
			}
			for _, r := range runs {
				if r.Kind == kind {
					last.Set(&r)
					return
				}
			}
			last.Set(nil)
		})
	})
	return last.Get
}
