package shell

import (
	"slices"
	"testing"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/nav"
)

// fakeHost is windowHost recording what the shell asks of the platform, in order.
type fakeHost struct {
	calls []string
	main  *fakeWindow
	// closing is the handler OnClosing registered on the window open now.
	closing func(cancel func())
}

type fakeWindow struct{ host *fakeHost }

func (w *fakeWindow) Hide()  { w.host.calls = append(w.host.calls, "hide") }
func (w *fakeWindow) Focus() { w.host.calls = append(w.host.calls, "focus") }

func (h *fakeHost) Main() mainWindow {
	if h.main == nil {
		return nil
	}
	return h.main
}

func (h *fakeHost) SetMainView(view func(cx *core.Scope) core.Node) {
	if view == nil {
		panic("SetMainView without a view")
	}
	h.calls = append(h.calls, "open")
	h.main = &fakeWindow{host: h}
}

func (h *fakeHost) SetDockVisible(on bool) {
	if on {
		h.calls = append(h.calls, "dock")
	} else {
		h.calls = append(h.calls, "no-dock")
	}
}

func (h *fakeHost) OnClosing(fn func(cancel func())) {
	h.calls = append(h.calls, "on-closing")
	h.closing = fn
}

// close has the user close the window; it reports whether the close went ahead, which closes the window for good.
func (h *fakeHost) close() bool {
	canceled := false
	h.closing(func() { canceled = true })
	if !canceled {
		h.main, h.closing = nil, nil
	}
	return !canceled
}

func (h *fakeHost) take() []string {
	c := h.calls
	h.calls = nil
	return c
}

func newWindows(t *testing.T, start nav.Route, keep bool) (*windows, *fakeHost) {
	t.Helper()
	root := core.NewRoot(core.RootOptions{Post: func(fn func()) { fn() }, Report: func(err error) { t.Errorf("graph failed: %v", err) }})
	t.Cleanup(root.Dispose)
	h := &fakeHost{}
	w := &windows{host: h, nav: nav.NewNavigator(root, start), keepInTray: func() bool { return keep },
		view: func(*core.Scope) core.Node { return core.View{} }}
	return w, h
}

func TestShowOpensTheWindowOnceAndFocusesIt(t *testing.T) {
	w, h := newWindows(t, nav.Overview, true)
	w.show("")
	if got, want := h.take(), []string{"dock", "open", "on-closing", "focus"}; !slices.Equal(got, want) {
		t.Fatalf("first show %v, want %v", got, want)
	}
	// A window that is there is only brought to the front, on the page asked for.
	w.show(nav.Space)
	if got, want := h.take(), []string{"dock", "focus"}; !slices.Equal(got, want) {
		t.Errorf("second show %v, want %v", got, want)
	}
	if got := w.nav.Current(); got != nav.Space {
		t.Errorf("page %q, want space", got)
	}
}

func TestClosingHidesAndKeepsThePage(t *testing.T) {
	w, h := newWindows(t, nav.Maintenance, true)
	w.show("")
	h.take()
	if h.close() {
		t.Fatal("the window closed with keepInTray on")
	}
	if got, want := h.take(), []string{"hide", "no-dock"}; !slices.Equal(got, want) {
		t.Errorf("closing %v, want %v", got, want)
	}
	// The page area would mount the page again on another route, ending 维护's run.
	if got := w.nav.Current(); got != nav.Maintenance {
		t.Errorf("page %q after hiding, want maintenance", got)
	}
	// Opening again shows the same window, without a new one.
	w.show("")
	if got, want := h.take(), []string{"dock", "focus"}; !slices.Equal(got, want) {
		t.Errorf("show after hiding %v, want %v", got, want)
	}
}

func TestClosingOnHealthGoesBackToOverview(t *testing.T) {
	w, h := newWindows(t, nav.Health, true)
	w.show("")
	h.take()
	h.close()
	if got := w.nav.Current(); got != nav.Overview {
		t.Errorf("page %q after hiding 健康监测, want overview", got)
	}
}

func TestClosingWithoutKeepInTrayCloses(t *testing.T) {
	w, h := newWindows(t, nav.Health, false)
	w.show("")
	h.take()
	if !h.close() {
		t.Fatal("the window stayed with keepInTray off")
	}
	if got := h.take(); len(got) != 0 {
		t.Errorf("closing did %v", got)
	}
	if got := w.nav.Current(); got != nav.Health {
		t.Errorf("page %q, want health", got)
	}
	// With the window gone (ShutdownOnExplicit keeps the app up when it is set), showing opens a new one with its own close handler.
	w.show("")
	if got, want := h.take(), []string{"dock", "open", "on-closing", "focus"}; !slices.Equal(got, want) {
		t.Errorf("show after closing %v, want %v", got, want)
	}
}

func TestNotificationPressRoutes(t *testing.T) {
	for _, c := range []struct {
		id      string
		recs    bool
		want    nav.Route
		history bool
	}{
		{"low-disk", true, nav.Overview, false},
		{"low-disk", false, nav.Space, false},
		{"auto-summary:2026-W46", true, nav.History, true},
		{"something-else", true, nav.Maintenance, false},
	} {
		w, h := newWindows(t, nav.Maintenance, true)
		w.pressed(c.id, c.recs)
		if got := w.nav.Current(); got != c.want {
			t.Errorf("%s (recs %v): page %q, want %q", c.id, c.recs, got, c.want)
		}
		kind, run := w.nav.TakeHistoryFilter()
		if c.history != (kind == history.KindAuto) || run != "" {
			t.Errorf("%s: history filter %q %q", c.id, kind, run)
		}
		if calls := h.take(); !slices.Contains(calls, "focus") || !slices.Contains(calls, "dock") {
			t.Errorf("%s: the window was not shown: %v", c.id, calls)
		}
	}
}
