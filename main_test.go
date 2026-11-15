package main

import (
	"testing"

	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/cli"
	"github.com/yldm-tech/spruce/internal/engine/elevate"
	"github.com/yldm-tech/spruce/internal/host/loginitem"
)

// TestMain hands the main thread to the platform under go tool nsgo test -target; headless it only runs the tests.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// TestCommandLineDetection checks the order main tells its starts apart in: the elevated child, then the command line, then the app. The command line must never take the elevated child's arguments nor any the app is started with (Finder, Explorer, the login item, NSGo's own flags, the single-instance hand-off passes none).
func TestCommandLineDetection(t *testing.T) {
	type start int
	const (
		child start = iota
		command
		gui
	)
	route := func(args []string) start {
		switch {
		case elevate.IsChild(args):
			return child
		case cli.IsCommand(args):
			return command
		}
		return gui
	}
	for _, c := range []struct {
		args []string
		want start
	}{
		{nil, gui},
		{[]string{loginitem.LoginArg}, gui},
		{[]string{"--nsgo-dev"}, gui},
		{[]string{"--nsgo-dev", loginitem.LoginArg}, gui},
		{[]string{"-psn_0_4242"}, gui},
		{[]string{`C:\Users\u\Downloads\Spruce.exe`}, gui},
		{[]string{"--json"}, gui},
		{[]string{elevate.Flag, "--tasks", "dns"}, child},
		{[]string{elevate.Flag, "--tasks", "dns", "--out", "/tmp/x"}, child},
		{[]string{elevate.Flag, "scan"}, child},
		{[]string{"scan"}, command},
		{[]string{"recommendations", "--json"}, command},
		{[]string{"list"}, command},
		{[]string{"clean", "--dry-run"}, command},
		{[]string{"projects"}, command},
		{[]string{"history", "--limit", "5"}, command},
		{[]string{"restore", "20261115T090000Z-0001"}, command},
		{[]string{"help", "clean"}, command},
		{[]string{"version"}, command},
		{[]string{"--help"}, command},
		{[]string{"-h"}, command},
		{[]string{"scan", elevate.Flag}, command},
	} {
		if got := route(c.args); got != c.want {
			t.Errorf("%q starts %d, want %d", c.args, got, c.want)
		}
	}

}
