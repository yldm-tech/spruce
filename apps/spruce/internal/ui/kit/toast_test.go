package kit_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/ui/kit"
)

// toastApp shows a 6-second toast with one action until it closes, and counts its closes and presses.
func toastApp(t *testing.T, opts ...nsgotest.Option) (*nsgotest.App, *int, *int) {
	t.Helper()
	closes, presses := 0, 0
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		open := core.NewSignal(cx, true)
		return core.View{TestID: "root", Style: core.Style{Width: core.Pt(800), Height: core.Pt(400)}, Children: []core.Node{
			core.View{TestID: "elsewhere", Style: core.Style{Height: core.Pt(100)}},
			core.Show{When: open.Get, Then: func(*core.Scope) core.Node {
				return kit.Toast{Text: func() string { return "已移到废纸篓 3.1 GB" }, Duration: 6 * time.Second,
					Actions: []kit.ToastAction{{Text: "撤销", TestID: "toast-undo", OnPress: func() { presses++ }}},
					OnClose: func() {
						closes++
						open.Set(false)
					}}
			}},
		}}
	}, append([]nsgotest.Option{nsgotest.Size(800, 400)}, opts...)...)
	a.WaitFor("id:toast", 5*time.Second)
	return a, &closes, &presses
}

func TestToastClosesAfterItsTime(t *testing.T) {
	a, closes, presses := toastApp(t)
	if got := a.Find("id:toast-text").Text; got != "已移到废纸篓 3.1 GB" {
		t.Errorf("text %q", got)
	}
	a.Press("id:toast-undo")
	if *presses != 1 {
		t.Error("action not pressed")
	}
	a.Advance(5 * time.Second)
	if !a.Exists("id:toast") || *closes != 0 {
		t.Fatal("closed early")
	}
	a.Advance(2 * time.Second)
	if a.Exists("id:toast") || *closes != 1 {
		t.Errorf("still open after its time (closes %d)", *closes)
	}
}

func TestToastPausesWhileHovered(t *testing.T) {
	a, closes, _ := toastApp(t)
	a.Advance(4 * time.Second)
	a.Hover("id:toast")
	a.Advance(30 * time.Second)
	if !a.Exists("id:toast") || *closes != 0 {
		t.Fatal("closed while the pointer was over it")
	}
	a.Hover("id:elsewhere")
	a.Advance(1500 * time.Millisecond)
	if !a.Exists("id:toast") {
		t.Fatal("the pause lost the time left")
	}
	a.Advance(time.Second)
	if a.Exists("id:toast") || *closes != 1 {
		t.Error("did not close once the time left ran out")
	}
}

func TestToastCloseButton(t *testing.T) {
	a, closes, _ := toastApp(t)
	a.Press("id:toast-close")
	if a.Exists("id:toast") || *closes != 1 {
		t.Errorf("close button: closes %d", *closes)
	}
}

func TestToastPausesWithTheFocusAndEscapeCloses(t *testing.T) {
	a, closes, _ := toastApp(t, nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	a.Key("tab")
	a.Advance(30 * time.Second)
	if !a.Exists("id:toast") || *closes != 0 {
		t.Fatal("closed while the focus was in it")
	}
	a.Key("escape")
	if a.Exists("id:toast") || *closes != 1 {
		t.Errorf("Escape with the focus in it: closes %d", *closes)
	}
}

func TestToastEscapeWithoutFocusDoesNothing(t *testing.T) {
	a, closes, _ := toastApp(t, nsgotest.Environment(core.Environment{OS: core.OSMacOS, Idiom: core.IdiomDesktop}))
	a.Key("escape")
	if !a.Exists("id:toast") || *closes != 0 {
		t.Error("Escape closed a toast without the focus")
	}
}

// The time runs only while the app is active: a toast that comes while the user is in another app waits for them, and one they leave keeps what it had left.
func TestToastPausesWhileTheAppIsInactive(t *testing.T) {
	a, closes, _ := toastApp(t)
	a.Advance(2 * time.Second)
	a.SetEnvironment(core.Environment{Lifecycle: core.LifecycleInactive})
	a.Advance(30 * time.Second)
	if !a.Exists("id:toast") || *closes != 0 {
		t.Fatal("closed while the app was inactive")
	}
	a.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	a.Advance(3500 * time.Millisecond)
	if !a.Exists("id:toast") {
		t.Fatal("the pause lost the time left")
	}
	a.Advance(time.Second)
	if a.Exists("id:toast") || *closes != 1 {
		t.Errorf("did not close once the time left ran out (closes %d)", *closes)
	}
}

func TestToastShownInTheBackgroundWaitsForTheApp(t *testing.T) {
	a, closes, _ := toastApp(t, nsgotest.Environment(core.Environment{Lifecycle: core.LifecycleBackground}))
	a.Advance(time.Minute)
	if !a.Exists("id:toast") || *closes != 0 {
		t.Fatal("closed while the app was in the background")
	}
	a.SetEnvironment(core.Environment{Lifecycle: core.LifecycleActive})
	a.Advance(5 * time.Second)
	if !a.Exists("id:toast") {
		t.Fatal("did not get its whole time once the app came back")
	}
	a.Advance(2 * time.Second)
	if a.Exists("id:toast") || *closes != 1 {
		t.Errorf("did not close after its time (closes %d)", *closes)
	}
}
