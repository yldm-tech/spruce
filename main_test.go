package main

import (
	"bufio"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"syscall"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/engine/elevate"
	"github.com/yldm-tech/spruce/internal/host/loginitem"
)

// TestMain hands the main thread to the platform under go tool nsgo test -target; headless it only runs the tests.
func TestMain(m *testing.M) { nsgotest.Main(m) }

// TestCommandLineDetection checks launchKindOf, which main switches on: the elevated child, then the command line, then the app. The command line must never take the elevated child's arguments nor any the app is started with (Finder, Explorer, the login item, NSGo's own flags, the single-instance hand-off passes none).
func TestCommandLineDetection(t *testing.T) {
	const (
		child   = launchChild
		command = launchCommand
		gui     = launchApp
	)
	for _, c := range []struct {
		args []string
		want launchKind
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
		if got := launchKindOf(c.args); got != c.want {
			t.Errorf("%q starts %d, want %d", c.args, got, c.want)
		}
	}
}

// interruptChildEnv makes TestInterruptChild act as the command line's process for TestInterruptContext.
const interruptChildEnv = "SPRUCE_TEST_INTERRUPT_CHILD"

// TestInterruptContext checks the command line's signals in a child process: the first SIGINT cancels the context without ending the process, a further one ends it.
func TestInterruptContext(t *testing.T) {
	if runtime.GOOS == "windows" || nsgotest.Target() != "" {
		t.Skip("sends POSIX signals to a child test process")
	}
	cmd := exec.Command(os.Args[0], "-test.run=^TestInterruptChild$", "-test.count=1")
	cmd.Env = append(os.Environ(), interruptChildEnv+"=1")
	out, err := cmd.StdoutPipe()
	if err != nil {
		t.Fatal(err)
	}
	if err := cmd.Start(); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = cmd.Process.Kill() })
	lines := bufio.NewScanner(out)
	expect := func(want string) {
		t.Helper()
		for lines.Scan() {
			if lines.Text() == want {
				return
			}
		}
		t.Fatalf("the child never printed %q", want)
	}
	expect("ready")
	if err := cmd.Process.Signal(syscall.SIGINT); err != nil {
		t.Fatal(err)
	}
	expect("cancelled")
	// The default behaviour comes back in a goroutine right after the cancel, so keep interrupting until the child ends.
	done := make(chan error, 1)
	go func() { done <- cmd.Wait() }()
	tick := time.NewTicker(50 * time.Millisecond)
	defer tick.Stop()
	for {
		select {
		case err := <-done:
			if cmd.ProcessState.ExitCode() != -1 {
				t.Fatalf("the child exited by itself (%v): a further SIGINT was swallowed", err)
			}
			return
		case <-tick.C:
			_ = cmd.Process.Signal(syscall.SIGINT)
		}
	}
}

// TestInterruptChild is the child process of TestInterruptContext and does nothing in a normal run.
func TestInterruptChild(t *testing.T) {
	if os.Getenv(interruptChildEnv) != "1" {
		t.Skip("child process of TestInterruptContext")
	}
	ctx, stop := interruptContext()
	defer stop()
	fmt.Println("ready")
	<-ctx.Done()
	fmt.Println("cancelled")
	time.Sleep(10 * time.Second)
}
