package nav

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/engine/history"
)

func newNav(t *testing.T, start Route) *Navigator {
	t.Helper()
	root := core.NewRoot(core.RootOptions{Post: func(fn func()) { fn() }, Report: func(err error) { t.Errorf("graph failed: %v", err) }})
	t.Cleanup(root.Dispose)
	return NewNavigator(root, start)
}

func TestPagesAndMeta(t *testing.T) {
	if len(Pages) != 7 || Pages[0] != Overview || Start != Overview {
		t.Fatalf("pages %v, start %q", Pages, Start)
	}
	for _, r := range []Route{SystemJunk, DevJunk, Installers} {
		m := r.Meta()
		if m.Parent != Overview || m.Section != "" || r.InSidebar() {
			t.Errorf("%s: %+v", r, m)
		}
	}
	if m := Overview.Meta(); m.Title != "概览" || m.Glyph != "page/overview" || m.Subtitle != "Spruce 会在后台定期检查，告诉你哪些空间可以放心释放" {
		t.Errorf("overview %+v", m)
	}
	if m := DevJunk.Meta(); m.Title != "开发者" || m.Subtitle != "开发工具的缓存和项目里的构建产物，大多会在需要时重新下载或生成" {
		t.Errorf("dev junk %+v", m)
	}
	for _, r := range Pages {
		if r.Meta().Parent != "" || r.Meta().Title == "" {
			t.Errorf("sidebar page %s: %+v", r, r.Meta())
		}
	}
}

func TestBackReturnsWhereTheDrillDownWasEntered(t *testing.T) {
	n := newNav(t, Overview)
	n.Go(SystemJunk)
	n.Back()
	if n.Current() != Overview {
		t.Errorf("back from 系统垃圾 entered from 概览: %s", n.Current())
	}
	// Entered from 操作历史 (a run's page link): back goes there.
	n.Go(History)
	n.Go(DevJunk)
	n.Back()
	if n.Current() != History {
		t.Errorf("back to the sidebar page it came from: %s", n.Current())
	}
	// Opened directly (a test's start, or from another drill-down view): back goes to the parent.
	n = newNav(t, Installers)
	n.Back()
	if n.Current() != Overview {
		t.Errorf("back without an origin: %s", n.Current())
	}
	n.Go(SystemJunk)
	n.Go(Installers)
	n.Back()
	if n.Current() != Overview {
		t.Errorf("back after a drill-down view: %s", n.Current())
	}
	// Back on a sidebar page does nothing.
	n.Go(Space)
	n.Back()
	if n.Current() != Space {
		t.Errorf("back on a sidebar page moved to %s", n.Current())
	}
}

func TestLinksAreTakenOnce(t *testing.T) {
	n := newNav(t, Overview)
	n.GoDrill(SystemJunk, "system-junk:logs")
	if n.Current() != SystemJunk || n.TakeReveal() != "system-junk:logs" || n.TakeReveal() != "" {
		t.Error("GoDrill")
	}
	n.Back()
	if n.Current() != Overview {
		t.Errorf("back after GoDrill: %s", n.Current())
	}

	asked := n.HistoryAsked()
	n.GoHistory(history.KindAuto, "run-1")
	if n.HistoryAsked() != asked+1 {
		t.Error("GoHistory was not counted")
	}
	if k, r := n.TakeHistoryFilter(); n.Current() != History || k != history.KindAuto || r != "run-1" {
		t.Errorf("GoHistory: %s %q %q", n.Current(), k, r)
	}
	if k, r := n.TakeHistoryFilter(); k != "" || r != "" {
		t.Error("history filter taken twice")
	}

	n.GoDevTab(DevTools)
	if n.Current() != DevJunk || n.TakeDevTab() != DevTools || n.TakeDevTab() != "" {
		t.Error("GoDevTab")
	}

	n.GoOverviewCard("app-caches")
	if n.Current() != Overview || n.TakeOverviewCard() != "app-caches" || n.TakeOverviewCard() != "" {
		t.Error("GoOverviewCard")
	}

	n.GoSpace("/Users/me/Library/Caches")
	if n.Current() != Space || n.TakeSpacePath() != "/Users/me/Library/Caches" || n.TakeSpacePath() != "" {
		t.Error("GoSpace")
	}

	asked = n.SettingsAsked()
	n.GoSettings(SettingsAuto)
	if n.Current() != Settings || n.TakeSettingsTab() != SettingsAuto || n.TakeSettingsTab() != "" {
		t.Error("GoSettings")
	}
	if n.SettingsAsked() != asked+1 {
		t.Error("GoSettings was not counted")
	}

	shown := n.Shown()
	n.WindowShown()
	if n.Shown() != shown+1 {
		t.Error("WindowShown was not counted")
	}
}

func TestScrollOffsetsAreKeptPerPageUntilALinkAsksForSomething(t *testing.T) {
	n := newNav(t, Overview)
	n.SetScrollOffset(Overview, 640)
	n.SetScrollOffset(History, 120)
	n.Go(Space)
	n.Go(Overview)
	if got := n.ScrollOffset(Overview); got != 640 {
		t.Errorf("概览 offset %v after going away and back, want 640", got)
	}
	if got := n.ScrollOffset(Space); got != 0 {
		t.Errorf("空间透视 offset %v, never scrolled", got)
	}
	n.SetScrollOffset(History, 0)
	if got := n.ScrollOffset(History); got != 0 {
		t.Errorf("history offset %v after scrolling back to the top", got)
	}

	// A link that asks the page to show something drops its offset: the page scrolls there instead.
	for name, c := range map[string]struct {
		r     Route
		visit func()
	}{
		"GoOverviewCard":  {Overview, func() { n.GoOverviewCard("app-caches") }},
		"GoDrill":         {SystemJunk, func() { n.GoDrill(SystemJunk, "logs") }},
		"GoDevTab":        {DevJunk, func() { n.GoDevTab(DevTools) }},
		"GoHistory":       {History, func() { n.GoHistory("", "run-1") }},
		"GoSettings(tab)": {Settings, func() { n.GoSettings(SettingsAuto) }},
		"GoSpace(path)":   {Space, func() { n.GoSpace("/tmp") }},
	} {
		n.SetScrollOffset(c.r, 300)
		c.visit()
		if got := n.ScrollOffset(c.r); got != 0 {
			t.Errorf("%s kept the offset %v", name, got)
		}
	}
	// A drill-down without a category to reveal keeps it.
	n.SetScrollOffset(SystemJunk, 300)
	n.GoDrill(SystemJunk, "")
	if got := n.ScrollOffset(SystemJunk); got != 300 {
		t.Errorf("GoDrill without a category dropped the offset (%v)", got)
	}
}

func TestCommandsFollowThePageThatRegisteredThem(t *testing.T) {
	root := core.NewRoot(core.RootOptions{Post: func(fn func()) { fn() }, Report: func(err error) { t.Errorf("graph failed: %v", err) }})
	t.Cleanup(root.Dispose)
	n := NewNavigator(root, Overview)
	Key.Provide(root, n)
	if c := n.Commands(); c.Refresh != nil || c.Find != nil {
		t.Fatal("commands before any page registered")
	}
	var did []string
	first := root.Child()
	UseCommands(first, Commands{Refresh: func() { did = append(did, "first") }})
	second := root.Child()
	UseCommands(second, Commands{Refresh: func() { did = append(did, "second") }, Find: func() {}})
	n.Commands().Refresh()
	if n.Commands().Find == nil {
		t.Error("the newest page's Find is missing")
	}
	second.Dispose()
	n.Commands().Refresh()
	if n.Commands().Find != nil {
		t.Error("Find outlived the page that registered it")
	}
	first.Dispose()
	if n.Commands().Refresh != nil {
		t.Error("Refresh outlived the page that registered it")
	}
	if len(did) != 2 || did[0] != "second" || did[1] != "first" {
		t.Errorf("ran %v, want [second first]", did)
	}
}
