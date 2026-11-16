// Package execxtest provides Fake, a scripted execx.Runner for tests. It is imported only from _test.go files; production code never links it.
//
// Usage:
//
//	f := execxtest.New()
//	f.On("/usr/bin/dscacheutil", "-flushcache").Return("", 0)
//	f.On("/usr/bin/mdutil", "-s", "/").Return("Indexing enabled.\n", 0)
//	f.On("/usr/bin/qlmanage", "-r", "cache").Fail(errors.New("boom"))
//	// … run the code under test with f as its execx.Runner …
//	f.AssertCalled(t, "/usr/bin/dscacheutil", "-flushcache")
//
// A command that matches no expectation fails the run with ErrUnexpected, so a test notices a task that runs something it did not script.
package execxtest

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"spruce/apps/spruce/internal/engine/execx"
)

// ErrUnexpected is returned for a command no expectation matches.
var ErrUnexpected = errors.New("execxtest: unexpected command")

// Fake is a scripted execx.Runner. It is safe for concurrent use.
type Fake struct {
	mu    sync.Mutex
	exps  []*Expectation
	calls []execx.Command
}

var _ execx.Runner = (*Fake)(nil)

// New returns a Fake with no expectations.
func New() *Fake { return &Fake{} }

// Expectation is the scripted answer for one program and argument list.
type Expectation struct {
	name string
	args []string
	do   func(ctx context.Context, cmd execx.Command) (execx.Output, error)
}

// On adds an expectation for name with exactly args. Later expectations for the same command replace earlier ones. The default answer is success with no output.
func (f *Fake) On(name string, args ...string) *Expectation {
	e := &Expectation{name: name, args: args, do: func(context.Context, execx.Command) (execx.Output, error) {
		return execx.Output{}, nil
	}}
	f.mu.Lock()
	f.exps = append(f.exps, e)
	f.mu.Unlock()
	return e
}

// Return answers with stdout and exitCode; a non-zero exitCode produces an *execx.ExitError, as OSRunner does.
func (e *Expectation) Return(stdout string, exitCode int) *Expectation {
	return e.ReturnOutput(execx.Output{Stdout: []byte(stdout), ExitCode: exitCode})
}

// ReturnOutput answers with out; a non-zero out.ExitCode produces an *execx.ExitError, as OSRunner does.
func (e *Expectation) ReturnOutput(out execx.Output) *Expectation {
	e.do = func(_ context.Context, cmd execx.Command) (execx.Output, error) {
		if out.ExitCode != 0 {
			return out, &execx.ExitError{Command: cmd.String(), ExitCode: out.ExitCode, Stderr: execx.Tail(out.Stderr, 2048)}
		}
		return out, nil
	}
	return e
}

// Fail answers with err and an exit code of -1, like a program that could not start.
func (e *Expectation) Fail(err error) *Expectation {
	e.do = func(context.Context, execx.Command) (execx.Output, error) {
		return execx.Output{ExitCode: -1}, err
	}
	return e
}

// Do answers with fn, for tests that inspect Stdin or Env, block on the context, or count calls.
func (e *Expectation) Do(fn func(ctx context.Context, cmd execx.Command) (execx.Output, error)) *Expectation {
	e.do = fn
	return e
}

// Run implements execx.Runner.
func (f *Fake) Run(ctx context.Context, cmd execx.Command) (execx.Output, error) {
	f.mu.Lock()
	f.calls = append(f.calls, cmd)
	var match *Expectation
	for _, e := range slices.Backward(f.exps) {
		if e.name == cmd.Name && slices.Equal(e.args, cmd.Args) {
			match = e
			break
		}
	}
	f.mu.Unlock()
	if match == nil {
		return execx.Output{ExitCode: -1}, fmt.Errorf("%w: %s", ErrUnexpected, cmd.String())
	}
	return match.do(ctx, cmd)
}

// Calls returns every command run so far, in order, as given to Run (Env, Unset, Dir and Timeout included), so a test can check how a command was set up.
func (f *Fake) Calls() []execx.Command {
	f.mu.Lock()
	defer f.mu.Unlock()
	return slices.Clone(f.calls)
}

// Called reports whether name was run with exactly args.
func (f *Fake) Called(name string, args ...string) bool {
	for _, c := range f.Calls() {
		if c.Name == name && slices.Equal(c.Args, args) {
			return true
		}
	}
	return false
}

// AssertCalled fails t unless name was run with exactly args.
func (f *Fake) AssertCalled(t testing.TB, name string, args ...string) {
	t.Helper()
	if !f.Called(name, args...) {
		t.Errorf("command %q %q was not run; ran %v", name, args, f.Calls())
	}
}

// AssertNotCalled fails t if name was run with any arguments.
func (f *Fake) AssertNotCalled(t testing.TB, name string) {
	t.Helper()
	for _, c := range f.Calls() {
		if c.Name == name {
			t.Errorf("command %q ran unexpectedly: %s", name, c.String())
		}
	}
}
