package kit_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/ui/kit"
)

// lit reports whether the bar's band of light shows.
func lit(a *nsgotest.App, bar string) bool {
	return a.Exists("id:" + bar + " >> id:sweep-light")
}

// A working bar sweeps a band of light across it while Active says so, also while its fraction stands still, and says it is busy.
func TestMeterSweepsWhileActive(t *testing.T) {
	var active *core.Signal[bool]
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		active = core.NewSignal(cx, true)
		return core.View{Style: core.Style{Width: core.Pt(300), Padding: core.All(20)}, Children: []core.Node{
			kit.Meter{Value: func() float64 { return 0 }, Active: active.Get, Height: 4, Label: "清理进度", TestID: "bar"},
		}}
	}, nsgotest.Size(400, 200))
	a.Settle()
	if !lit(a, "bar") {
		t.Fatal("no light sweeps across an active bar")
	}
	if !a.Find("id:bar").Props.Busy {
		t.Error("an active bar does not say it is busy")
	}
	a.Post(func() { active.Set(false) })
	a.Settle()
	if lit(a, "bar") {
		t.Error("the light still sweeps once the work ended")
	}
}

// While the user asks for reduced motion the band stays hidden.
func TestMeterKeepsStillUnderReducedMotion(t *testing.T) {
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Style: core.Style{Width: core.Pt(300), Padding: core.All(20)}, Children: []core.Node{
			kit.Meter{Value: func() float64 { return 0.3 }, Active: func() bool { return true }, Height: 4, TestID: "bar"},
		}}
	}, nsgotest.Size(400, 200))
	a.SetEnvironment(core.Environment{ReduceMotion: true})
	a.Settle()
	if lit(a, "bar") {
		t.Error("the light sweeps although the user asked for reduced motion")
	}
}

// A toast told to leave plays its exit, then reports it is gone, once, and takes no presses meanwhile.
func TestToastLeavesWithItsExit(t *testing.T) {
	var leaving *core.Signal[bool]
	gone, presses := 0, 0
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		leaving = core.NewSignal(cx, false)
		return core.View{Style: core.Style{Width: core.Pt(800), Height: core.Pt(400)}, Children: []core.Node{
			kit.Toast{Text: func() string { return "已移到废纸篓 3.1 GB" }, Duration: 6 * time.Second, Leaving: leaving.Get,
				Actions: []kit.ToastAction{{Text: "撤销", TestID: "toast-undo", OnPress: func() { presses++ }}},
				OnClose: func() { leaving.Set(true) }, OnGone: func() { gone++ }},
		}}
	}, nsgotest.Size(800, 400))
	a.WaitFor("id:toast", 5*time.Second)
	a.Post(func() { leaving.Set(true) })
	a.Settle()
	if gone != 0 {
		t.Fatal("the toast was gone before its exit played")
	}
	a.Advance(400 * time.Millisecond)
	if gone != 1 {
		t.Fatalf("gone %d times after its exit", gone)
	}
	if presses != 0 {
		t.Errorf("presses %d", presses)
	}
}
