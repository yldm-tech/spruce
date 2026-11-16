package kit

import (
	"math"
	"time"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/nav"
)

// Page is a page's column (.page): Header pinned at the top, where it drags the window, with a hairline under it once the page is scrolled; Children scrolling under it, 18 points apart inside 32-point margins; and Footer pinned at the bottom, rising above the shell's toast while it shows (ToastShownKey). Without a Header or a Footer it is the scroll view alone. Either way the scroll view takes the main area's height, so an ActionBar among the children sits at its bottom. Ref names the scroll view, for ScrollTo, and TestID is its ID. With a ScrollKey the shell's Navigator keeps where the page was scrolled to, and the page lands there again the next time it is shown.
type Page struct {
	Header    core.Node
	Children  []core.Node
	Footer    core.Node
	ScrollKey nav.Route
	Ref       *core.Ref
	TestID    string
}

// ToastShownKey provides whether the shell's toast shows at the bottom of the page area (reading it subscribes), so a Page's Footer rises above it. The shell provides it; without it the footer never rises.
var ToastShownKey = core.NewKey[func() bool]("spruce.toast-shown", nil)

// Page spacing: the margins, the gap between the children, and the bottom of the content or of the footer.
const (
	pageMargin = 32
	pageGap    = 18
	pageBottom = 24
	// footerRaise is what the shell's toast takes at the bottom of the page area: 24 points below it, at least 44 for itself and 12 to the footer.
	footerRaise = 24 + 44 + 12
)

// Restoring an offset follows the content as it grows, as a page's data can arrive a moment after it mounts: it measures every restoreWait, restoreTries times at most, and each time scrolls as far toward the offset as the content allows.
const (
	restoreWait  = 100 * time.Millisecond
	restoreTries = 10
	// scrollThrottle is how often the scroll view reports where it is.
	scrollThrottle = 100 * time.Millisecond
)

// Render implements core.Node.
func (p Page) Render(cx *core.Scope) core.Node {
	ref := p.Ref
	if ref == nil {
		ref = new(core.Ref)
	}
	scrolled := core.NewSignal(cx, false)
	pad := core.Edges{Left: pageMargin, Right: pageMargin, Bottom: pageBottom}
	if p.Header != nil {
		pad.Top = 4
	}
	if p.Footer != nil {
		pad.Bottom = 12
	}
	record, laidOut := keepOffset(cx, nav.Use(cx), p.ScrollKey, ref, pad.Bottom)
	scroll := core.Scroll{Ref: ref, TestID: p.TestID, ScrollEventThrottle: scrollThrottle,
		OnScroll: func(e core.ScrollEvent) {
			scrolled.Set(e.Offset.Y > 0)
			record(e.Offset.Y)
		},
		OnLayout: func(core.Bounds) { laidOut() },
		Style:    core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0), Gap: pageGap, Padding: pad},
		Children: p.Children}
	if p.Header == nil && p.Footer == nil {
		return scroll
	}
	var children []core.Node
	if p.Header != nil {
		children = append(children,
			core.View{Desktop: core.DesktopViewProps{WindowDrag: core.WindowDragOn}, TestID: "page-header",
				Style:    core.Style{FlexShrink: 0, Padding: core.Edges{Left: pageMargin, Right: pageMargin, Bottom: pageGap - 4}},
				Children: []core.Node{p.Header}},
			core.View{TestID: "page-header-rule", StyleFunc: func() core.Style {
				st := core.Style{Height: core.Pt(1), FlexShrink: 0}
				if scrolled.Get() {
					st.Background = BorderSubtle
				}
				return st
			}})
	}
	children = append(children, scroll)
	if p.Footer != nil {
		raised := ToastShownKey.Use(cx)
		children = append(children, core.View{TestID: "page-footer", StyleFunc: func() core.Style {
			bottom := core.Points(pageBottom)
			if raised != nil && raised() {
				bottom = footerRaise
			}
			return core.Style{FlexShrink: 0, Padding: core.Edges{Left: pageMargin, Right: pageMargin, Bottom: bottom}}
		}, Children: []core.Node{p.Footer}})
	}
	return core.View{Style: core.Style{FlexGrow: 1, FlexShrink: 1, MinHeight: core.Pt(0)}, Children: children}
}

// keepOffset lands the scroll view ref, whose content ends bottom points above its end, where navigator last recorded page key, starting at its first layout (laidOut), and returns what records the offset as it scrolls. Without a key or a navigator it does nothing. Each measurement scrolls as far toward the offset as the content allows, so content that came back shorter (sections now closed, items gone) lands at its end at once, in the page's entrance, rather than after the wait; content still growing is followed until it is tall enough (restoreTries at most). The user scrolling first, or the page going, cancels it, and nothing is recorded until then, so a page that goes before it lands keeps the offset it had.
func keepOffset(cx *core.Scope, navigator *nav.Navigator, key nav.Route, ref *core.Ref, bottom core.Points) (record func(y core.Points), laidOut func()) {
	if key == "" || navigator == nil {
		return func(core.Points) {}, func() {}
	}
	saved := navigator.ScrollOffset(key)
	restoring := saved > 0
	cx.Cleanup(func() { restoring = false })
	// landed is where restoring scrolled to last; a scroll elsewhere is the user's.
	var landed core.Points
	var try func(left int)
	try = func(left int) {
		ref.Measure(func(frame, page core.Bounds, ok bool) {
			if !restoring || !ok {
				return
			}
			content, _ := ref.ContentBounds()
			room := content.Origin.Y + content.Size.Height + bottom - page.Origin.Y - frame.Size.Height
			if y := min(saved, room); y > landed {
				landed = y
				ref.ScrollTo(core.Point{Y: y}, false)
			}
			if room >= saved || left == 0 {
				restoring = false
				return
			}
			cx.AfterFunc(restoreWait, func() { try(left - 1) })
		})
	}
	started := false
	laidOut = func() {
		if restoring && !started {
			started = true
			try(restoreTries)
		}
	}
	record = func(y core.Points) {
		if restoring && math.Abs(float64(y-landed)) > 1 {
			restoring = false
		}
		if !restoring {
			navigator.SetScrollOffset(key, y)
		}
	}
	return record, laidOut
}
