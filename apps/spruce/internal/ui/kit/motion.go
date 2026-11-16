package kit

import (
	"slices"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// Entrance is how a view comes in the first time it is made: what appears (a page, a card, a panel, a toast, a badge) moves into place instead of popping up. Every entrance lands where it ends at once while the user asks for reduced motion (core.Motion.Play).
type Entrance uint8

// The entrances.
const (
	// EnterRise fades in rising 12 points: pages, cards, panels.
	EnterRise Entrance = iota
	// EnterFade only fades in: content that replaces content in place (another folder, another app).
	EnterFade
	// EnterZoom fades in from 96%: dialogs and overlays.
	EnterZoom
	// EnterPop springs in from half its size, overshooting a little: a badge that marks a result.
	EnterPop
	// EnterSlide fades in rising 24 points: what comes up from the bottom edge (the toast).
	EnterSlide
)

// Stagger is the delay between the entrances of a list's rows, and StaggerMax the most a row waits, so a long list does not trickle in.
const (
	Stagger    = 45 * time.Millisecond
	StaggerMax = 360 * time.Millisecond
)

// StaggerDelay is the entrance delay of a list's row i.
func StaggerDelay(i int) time.Duration { return min(time.Duration(i)*Stagger, StaggerMax) }

// spec is the motion of e after delay: NSH's preset with Spruce's distances and durations, which move less and settle sooner than the presets, since the app's views are large.
func (e Entrance) spec(delay time.Duration) core.MotionSpec {
	preset, dist, d := core.FadeInUp, 12.0, 320*time.Millisecond
	switch e {
	case EnterFade:
		preset, d = core.FadeIn, 220*time.Millisecond
	case EnterZoom:
		preset, d = core.ZoomIn, 220*time.Millisecond
	case EnterPop:
		preset, d = core.PopIn, 460*time.Millisecond
	case EnterSlide:
		dist, d = 24, 300*time.Millisecond
	}
	s := preset.Spec()
	tracks := make([]core.MotionTrack, len(s.Tracks))
	for i, tr := range s.Tracks {
		stops := slices.Clone(tr.Stops)
		switch tr.Channel {
		case core.MotionTranslateX, core.MotionTranslateY:
			for j := range stops {
				stops[j].Value *= dist / 24
			}
		case core.MotionScale:
			if e == EnterZoom {
				// 0.9 → 0.96: a dialog grows a little, it does not jump at the user.
				for j := range stops {
					stops[j].Value = 1 - (1-stops[j].Value)*0.4
				}
			}
		}
		tracks[i] = core.MotionTrack{Channel: tr.Channel, Stops: stops}
	}
	s.Tracks = tracks
	s.Timing.Duration, s.Timing.Delay = d, delay
	return s
}

// leaveSpec is the motion of a view that goes: it fades out sinking 12 points in 200 ms.
func leaveSpec() core.MotionSpec {
	s := core.FadeOutDown.Spec()
	tracks := make([]core.MotionTrack, len(s.Tracks))
	for i, tr := range s.Tracks {
		stops := slices.Clone(tr.Stops)
		if tr.Channel == core.MotionTranslateY {
			for j := range stops {
				stops[j].Value /= 2
			}
		}
		tracks[i] = core.MotionTrack{Channel: tr.Channel, Stops: stops}
	}
	s.Tracks = tracks
	s.Timing.Duration = 200 * time.Millisecond
	return s
}

// Enter returns what makes a view come in with e after delay, for its Style.Animated: call it once in the component that makes the view (it starts at once), not in a StyleFunc.
func Enter(cx *core.Scope, e Entrance, delay time.Duration) *core.AnimatedStyle {
	spec := e.spec(delay)
	return core.NewMotion(cx, &spec).Style()
}

// Appear is a view whose Children come in with Entrance after Delay the first time it is made, laid out by Style. Use it where the content has no view of its own to animate; else set Enter on that view.
type Appear struct {
	Entrance Entrance
	Delay    time.Duration
	Style    core.Style
	Children []core.Node
	TestID   string
}

// Render implements core.Node.
func (a Appear) Render(cx *core.Scope) core.Node {
	st := a.Style
	st.Animated = Enter(cx, a.Entrance, a.Delay)
	return core.View{TestID: a.TestID, Style: st, Children: a.Children}
}

// Breathing is a loop for a view while its work goes on (the icon of a card that cleans): it grows 5% and dims a little and back every 1.8 s; still while the user asks for reduced motion. It returns the view's Style.Animated; call it once in the component that makes the view.
func Breathing(cx *core.Scope, on func() bool) *core.AnimatedStyle {
	m := core.NewMotion(cx, nil)
	spec := core.Breathe.Spec()
	var run *core.AnimationRun
	stop := func() {
		if run != nil {
			run.Stop()
			run = nil
		}
	}
	cx.Effect(func() {
		active := on() && !cx.Env().ReduceMotion()
		cx.Untrack(func() {
			stop()
			m.Value(core.MotionScale).Set(1)
			m.Value(core.MotionOpacity).Set(1)
			if active {
				run = m.Play(spec, nil)
			}
		})
	})
	cx.Cleanup(stop)
	return m.Style()
}

// pressTransition is how a control's look follows the pointer and its state: background, border, shadow, opacity and transform change in 150 ms.
var pressTransition = core.Transition{Duration: 150 * time.Millisecond, Easing: core.Ease,
	Properties: core.TransitionBackground | core.TransitionBorder | core.TransitionShadow | core.TransitionOpacity | core.TransitionTransform}

// HoverEntry is the one look an unselected entry of a list of places takes under the pointer (the sidebar's, a tab list's): a faint fill and the full text color. Give it to the unselected entries only, since Hover merges over Selected; a disabled entry shows none (Disabled replaces it).
func HoverEntry() *core.Style { return &core.Style{Background: SurfaceRowHover, Color: ColorText} }

// hoverFilled is the look a filled button takes under the pointer: its brightness times f, above 1 to light a colored one, below 1 to dim the white one, which cannot get brighter.
func hoverFilled(f float32) *core.Style {
	return &core.Style{Filters: []core.Filter{core.Brightness(f)}}
}

// Pressable returns st with the feel of a press: while the pointer holds it, it shrinks to scale (0.97 for a button, less for a small round one), and its look changes smoothly between its states. A disabled view keeps still (Disabled replaces Active).
func Pressable(st core.Style, scale float32) core.Style {
	st.Transition = pressTransition
	active := core.Style{Transform: core.Transform{Scale: scale}}
	if st.Active != nil {
		active = st.Active.Merge(active)
	}
	st.Active = &active
	return st
}

// FadeOnChange returns what fades a view in again each time key changes (another folder opened, another app picked), for its Style.Animated, so content that replaces content in place does not just flip; the first value shows as it is. Call it once in the component that makes the view.
func FadeOnChange[K comparable](cx *core.Scope, key func() K) *core.AnimatedStyle {
	m := core.NewMotion(cx, nil)
	spec := EnterFade.spec(0)
	first, last := true, *new(K)
	cx.Effect(func() {
		k := key()
		if first || k == last {
			first, last = false, k
			return
		}
		last = k
		cx.Untrack(func() { m.Play(spec, nil) })
	})
	return m.Style()
}

// SettleLayout has the next change of cx's window move into place instead of jumping: rows that go leave a gap that closes, rows that come grow in, and what stays slides (an item moved to the Trash, an app uninstalled). Call it just before the change it is for; it does nothing while the user asks for reduced motion.
func SettleLayout(cx *core.Scope) {
	if cx.Env().ReduceMotion() {
		return
	}
	cx.AnimateLayout(core.LayoutAnimation{Create: &settleFade, Update: &settleMove, Delete: &settleFade})
}

var (
	settleMove = core.LayoutTransition{Curve: core.LayoutEaseInEaseOut, Duration: 260 * time.Millisecond}
	settleFade = core.LayoutTransition{Curve: core.LayoutEaseInEaseOut, Duration: 200 * time.Millisecond}
)

// Chevron is the expand button's chevron, Size points (16 when 0): it points down and turns over as Open says the section opened.
type Chevron struct {
	Open func() bool
	Size core.Points
}

// Render implements core.Node.
func (c Chevron) Render(*core.Scope) core.Node {
	size := c.Size
	if size == 0 {
		size = 16
	}
	return core.View{StyleFunc: func() core.Style {
		st := core.Style{Transition: core.Transition{Duration: 200 * time.Millisecond, Easing: core.Ease, Properties: core.TransitionTransform}}
		if c.Open != nil && c.Open() {
			st.Transform = core.Transform{Rotate: 180}
		}
		return st
	}, Children: []core.Node{Glyph{Name: "chevron-down", Size: size, Color: core.CurrentColor}}}
}
