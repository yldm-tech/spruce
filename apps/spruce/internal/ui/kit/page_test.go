package kit_test

import (
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/ui/kit"
	"spruce/apps/spruce/internal/ui/nav"
)

// pageApp shows a tall kit.Page on route key under a navigator, with a header and a footer, while shown is true, and the toast flag the footer rises for.
type pageApp struct {
	*nsgotest.App
	nav          *nav.Navigator
	shown, toast *core.Signal[bool]
	rows         *core.Signal[int]
}

func startPage(t *testing.T, key nav.Route, rows int) *pageApp {
	t.Helper()
	p := &pageApp{}
	p.App = nsgotest.Start(t, func(cx *core.Scope) core.Node {
		p.nav = nav.NewNavigator(cx, nav.Overview)
		nav.Key.Provide(cx, p.nav)
		p.shown, p.toast, p.rows = core.NewSignal(cx, true), core.NewSignal(cx, false), core.NewSignal(cx, rows)
		kit.ToastShownKey.Provide(cx, p.toast.Get)
		return core.View{Style: core.Style{Width: core.Pt(800), Height: core.Pt(600)}, Children: []core.Node{
			core.Show{When: p.shown.Get, Then: func(*core.Scope) core.Node {
				// The rows come as the page's data would, and may come later.
				list := core.Dynamic(func(*core.Scope) core.Node {
					children := make([]core.Node, p.rows.Get())
					for i := range children {
						children[i] = core.View{Style: core.Style{Height: core.Pt(100), FlexShrink: 0}}
					}
					return core.View{Style: core.Style{Gap: 18}, Children: children}
				})
				return kit.Page{TestID: "page", ScrollKey: key, Header: core.Text{Content: "头部", TestID: "head"}, Children: []core.Node{list},
					Footer: core.Text{Content: "底部", TestID: "foot"}}
			}},
		}}
	}, nsgotest.Size(800, 600))
	p.WaitFor("id:page", 5*time.Second)
	return p
}

func (p *pageApp) set(s *core.Signal[bool], on bool) {
	p.Post(func() { s.Set(on) })
	p.Settle()
}

func TestPageKeepsTheHeaderAndTheFooterInView(t *testing.T) {
	p := startPage(t, "", 30)
	if p.Find("id:page-header-rule").Style.Background != 0 {
		t.Error("the rule under the header shows at the top of the page")
	}
	p.Scroll("id:page", 2000)
	p.Advance(200 * time.Millisecond)
	head, foot, view := p.Find("id:head"), p.Find("id:foot"), p.Find("id:page")
	if head.Frame.Origin.Y < 0 || head.Frame.Origin.Y > view.Frame.Origin.Y {
		t.Errorf("the header at %v left the window after scrolling", head.Frame.Origin.Y)
	}
	footer := p.Find("id:page-footer")
	if footer.Frame.Origin.Y < view.Frame.Origin.Y+view.Frame.Size.Height-0.5 || footer.Frame.Origin.Y+footer.Frame.Size.Height > 600.5 {
		t.Errorf("the footer at %v is not under the scroll view (ends %v)", footer.Frame.Origin.Y, view.Frame.Origin.Y+view.Frame.Size.Height)
	}
	if foot.Frame.Size.Height == 0 {
		t.Error("the footer is empty")
	}
	if p.Find("id:page-header-rule").Style.Background != kit.BorderSubtle {
		t.Error("no rule under the header once scrolled")
	}

	before := p.Find("id:page-footer").Frame
	p.set(p.toast, true)
	if after := p.Find("id:page-footer").Frame; after.Origin.Y > before.Origin.Y-50 {
		t.Errorf("the footer did not rise above the toast: at %v, then %v", before.Origin.Y, after.Origin.Y)
	}
}

func TestPageLandsWhereItWasLeft(t *testing.T) {
	p := startPage(t, nav.Overview, 30)
	p.Scroll("id:page", 640)
	p.Advance(200 * time.Millisecond)
	if got := p.nav.ScrollOffset(nav.Overview); got != 640 {
		t.Fatalf("recorded offset %v, want 640", got)
	}
	p.set(p.shown, false)
	p.set(p.shown, true)
	p.WaitUntil(5*time.Second, func() bool { return p.Find("id:page").Offset.Y == 640 })

	// Back at the top, the next showing opens at the top.
	p.Scroll("id:page", -640)
	p.Advance(200 * time.Millisecond)
	p.set(p.shown, false)
	p.set(p.shown, true)
	p.Advance(2 * time.Second)
	if y := p.Find("id:page").Offset.Y; y != 0 {
		t.Errorf("offset %v after leaving the page at its top", y)
	}
}

func TestPageWaitsForItsContentToLand(t *testing.T) {
	p := startPage(t, nav.History, 30)
	p.Scroll("id:page", 1200)
	p.Advance(200 * time.Millisecond)
	p.set(p.shown, false)
	// The page comes back before its data: two rows, then all of them 300 ms later.
	p.Post(func() { p.rows.Set(2) })
	p.set(p.shown, true)
	p.Advance(300 * time.Millisecond)
	p.Post(func() { p.rows.Set(30) })
	p.Advance(time.Second)
	if y := p.Find("id:page").Offset.Y; y != 1200 {
		t.Errorf("offset %v once the rows came, want 1200", y)
	}
}

func TestPageWithoutAKeyOpensAtTheTop(t *testing.T) {
	p := startPage(t, "", 30)
	p.Scroll("id:page", 640)
	p.Advance(200 * time.Millisecond)
	p.set(p.shown, false)
	p.set(p.shown, true)
	p.Advance(2 * time.Second)
	if y := p.Find("id:page").Offset.Y; y != 0 {
		t.Errorf("offset %v without a ScrollKey", y)
	}
}
