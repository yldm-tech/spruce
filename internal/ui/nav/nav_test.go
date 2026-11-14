package nav

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"

	"github.com/yldm-tech/spruce/internal/engine/history"
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

	n.GoSettings(SettingsAuto)
	if n.Current() != Settings || n.TakeSettingsTab() != SettingsAuto {
		t.Error("GoSettings")
	}
}
