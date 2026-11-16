package kit_test

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"spruce/apps/spruce/internal/ui/kit"
)

// brightness is the brightness filter of the view selector matches, 0 without one.
func brightness(a *nsgotest.App, selector string) float32 {
	if fx := a.Find(selector).Style.Effects; fx != nil {
		for _, f := range fx.Filters {
			if f.Kind == core.FilterBrightness {
				return f.Amount
			}
		}
	}
	return 0
}

// The filled buttons answer the pointer as the ghost and link ones do: the colored ones light up, the white one dims; a disabled one stays as it is.
func TestFilledButtonsAnswerThePointer(t *testing.T) {
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Style: core.Style{Width: core.Pt(600), Padding: core.All(20), Gap: 10, AlignItems: core.AlignStart}, Children: []core.Node{
			kit.Button{Text: "完成", Variant: kit.ButtonLight, TestID: "light"},
			kit.Button{Text: "开始检查", Variant: kit.ButtonAccent, TestID: "accent"},
			kit.Button{Text: "删除", Variant: kit.ButtonDanger, TestID: "danger"},
			kit.Button{Text: "删除", Variant: kit.ButtonDanger, TestID: "disabled", Disabled: func() bool { return true }},
			core.View{TestID: "away", Style: core.Style{Width: core.Pt(200), Height: core.Pt(20)}},
		}}
	}, nsgotest.Size(640, 400), macDesktop)
	for _, c := range []struct {
		id      string
		lighter bool
	}{{"light", false}, {"accent", true}, {"danger", true}} {
		if got := brightness(a, "id:"+c.id); got != 0 {
			t.Errorf("%s: brightness %v before the pointer comes", c.id, got)
		}
		a.Hover("id:" + c.id)
		got := brightness(a, "id:"+c.id)
		if got == 0 || (got > 1) != c.lighter {
			t.Errorf("%s: brightness %v under the pointer", c.id, got)
		}
		a.Hover("id:away")
	}
	a.Hover("id:disabled")
	if got := brightness(a, "id:disabled"); got != 0 {
		t.Errorf("a disabled button answers the pointer: brightness %v", got)
	}
}

// An unselected tab takes a faint fill under the pointer; the selected one keeps its look.
func TestTabsAnswerThePointer(t *testing.T) {
	a := nsgotest.Start(t, func(cx *core.Scope) core.Node {
		return core.View{Style: core.Style{Width: core.Pt(600), Padding: core.All(20)}, Children: []core.Node{
			kit.Tabs[int]{Label: "范围", Value: func() int { return 1 }, Options: []kit.Option[int]{
				{Value: 1, Label: "24 小时", TestID: "tab-day"}, {Value: 7, Label: "7 天", TestID: "tab-week"}}},
		}}
	}, nsgotest.Size(640, 200), macDesktop)
	selected := a.Find("id:tab-day").Style.Background
	if got := a.Find("id:tab-week").Style.Background; got != 0 {
		t.Fatalf("an unselected tab has a fill: %v", got)
	}
	a.Hover("id:tab-week")
	if got := a.Find("id:tab-week").Style.Background; got == 0 {
		t.Error("an unselected tab does not answer the pointer")
	}
	a.Hover("id:tab-day")
	if got := a.Find("id:tab-day").Style.Background; got != selected {
		t.Errorf("the selected tab changes under the pointer: %v, was %v", got, selected)
	}
}
