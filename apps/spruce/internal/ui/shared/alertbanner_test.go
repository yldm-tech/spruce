package shared_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/engine/health"
	"spruce/apps/spruce/internal/ui/shared"
	"spruce/apps/spruce/internal/ui/store/storefake"
)

// The banner's close button hides the process it names for the session: the next alert shows, then none, and a later broadcast does not bring it back.
func TestAlertBannerIgnoresAProcess(t *testing.T) {
	const wait = 5 * time.Second
	fakes := storefake.New(t.TempDir())
	fakes.Alerts = []health.Alert{{PID: 812, Name: "Xcode", CPUPercent: 148, Duration: 12 * time.Minute}, {PID: 90, Name: "node", CPUPercent: 95, Duration: 3 * time.Minute}}
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		storefake.Provide(cx, fakes.Deps())
		return core.View{Style: core.Style{Width: core.Pt(800), Padding: core.All(20)}, Children: []core.Node{shared.AlertBanner{}}}
	}, nsgotest.Size(840, 300))
	a.WaitFor("id:alert-banner >> text:Xcode 已经持续占用 148% CPU 有 12 分钟", wait)
	a.Press("id:alert-ignore")
	a.WaitFor("id:alert-banner >> text:node 已经持续占用 95% CPU 有 3 分钟", wait)
	a.Press("id:alert-ignore")
	a.WaitForGone("id:alert-banner", wait)
	fakes.Alert([]health.Alert{{PID: 813, Name: "Xcode", CPUPercent: 150, Duration: 13 * time.Minute}})
	a.Advance(time.Second)
	if a.Exists("id:alert-banner") {
		t.Error("an ignored process came back")
	}
}
