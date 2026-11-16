package kit_test

import (
	"testing"

	"github.com/nsgo-dev/nsgo/core"

	"spruce/apps/spruce/internal/ui/kit"
)

// A Shift-press on a checkbox with OnShift goes there instead of OnChange; any other press changes it. nsgotest cannot press with a modifier (NSGo #349), so the checkbox's view is pressed directly.
func TestCheckboxShiftPress(t *testing.T) {
	var changed []bool
	shifted := 0
	c := kit.Checkbox{Checked: func() bool { return false }, OnChange: func(on bool) { changed = append(changed, on) }, OnShift: func() { shifted++ }}
	v, ok := c.Render(nil).(core.View)
	if !ok {
		t.Fatalf("a checkbox renders a %T", c.Render(nil))
	}
	v.OnPress(core.PressEvent{Modifiers: core.Modifiers{Shift: true}})
	if shifted != 1 || len(changed) != 0 {
		t.Errorf("a Shift-press: OnShift %d times, OnChange %v", shifted, changed)
	}
	v.OnPress(core.PressEvent{})
	if shifted != 1 || len(changed) != 1 || !changed[0] {
		t.Errorf("a press: OnShift %d times, OnChange %v", shifted, changed)
	}
}
