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

func TestAtLoginArg(t *testing.T) {
	for _, c := range []struct {
		args []string
		want bool
	}{
		{nil, false},
		{[]string{"-psn_0_12345"}, false},
		{[]string{"--login"}, true},
		{[]string{"--nsgo-dev", "--login"}, true},
		{[]string{"--login=1"}, false},
	} {
		if got := atLoginArg(c.args); got != c.want {
			t.Errorf("atLoginArg(%q) = %v, want %v", c.args, got, c.want)
		}
	}
}
