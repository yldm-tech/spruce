package loginitem

import "testing"

func TestDisabledInTaskManager(t *testing.T) {
	for _, c := range []struct {
		state []byte
		want  bool
	}{
		{nil, false},
		{[]byte{0x02, 0, 0}, false},
		{[]byte{0x06}, false},
		{[]byte{0x03, 0, 0}, true},
		{[]byte{0x01}, true},
	} {
		if got := disabledInTaskManager(c.state); got != c.want {
			t.Errorf("disabledInTaskManager(%v) = %v, want %v", c.state, got, c.want)
		}
	}
}
