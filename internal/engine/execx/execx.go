package execx

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"runtime"
	"strings"
	"time"
)

// Command describes one program invocation.
type Command struct {
	// Name is the program: an absolute path, or a bare name looked up in PATH.
	Name string
	// Args are the arguments after the program name, passed without a shell.
	Args []string
	// Stdin, when non-nil, is written to the program's standard input.
	Stdin []byte
	// Env, when non-nil, is appended to the current process environment as KEY=VALUE entries (later entries win). Nil inherits the environment unchanged.
	Env []string
	// Unset names variables of the current process environment the program must not inherit, removed before Env is appended; a name ending in "*" removes every variable starting with the rest (GIT_CONFIG_KEY_*). Names compare without case on Windows.
	Unset []string
	// Dir is the working directory; empty means the current one.
	Dir string
	// Timeout, when positive, bounds this command in addition to the context passed to Run.
	Timeout time.Duration
}

// String renders the command for logs and Result.Detail, e.g. "/usr/bin/qlmanage -r cache".
func (c Command) String() string {
	if len(c.Args) == 0 {
		return c.Name
	}
	return c.Name + " " + strings.Join(c.Args, " ")
}

// Output is what a command printed and how it exited.
type Output struct {
	Stdout []byte
	Stderr []byte
	// ExitCode is the program's exit status, or -1 when it did not start or was killed by a signal (including a timeout).
	ExitCode int
}

// ExitError reports that the program ran and exited with a non-zero status.
type ExitError struct {
	Command  string
	ExitCode int
	// Stderr is the trimmed tail of the program's standard error, for diagnostics.
	Stderr string
}

func (e *ExitError) Error() string {
	if e.Stderr == "" {
		return fmt.Sprintf("%s: exit status %d", e.Command, e.ExitCode)
	}
	return fmt.Sprintf("%s: exit status %d: %s", e.Command, e.ExitCode, e.Stderr)
}

// Runner runs external commands. Implementations must be safe for concurrent use.
type Runner interface {
	// Run starts cmd, waits for it to finish and returns what it printed. See the package documentation for the error contract.
	Run(ctx context.Context, cmd Command) (Output, error)
}

// OSRunner runs commands with os/exec. On Windows it starts them without a console window, since the app is a GUI process.
type OSRunner struct{}

// waitDelay bounds how long Run waits for the output pipes after the program exits or is killed, so that a grandchild holding the pipes open cannot hang the caller.
const waitDelay = 5 * time.Second

// stderrTail is how much of stderr an ExitError carries.
const stderrTail = 2048

// Run implements Runner.
func (OSRunner) Run(ctx context.Context, c Command) (Output, error) {
	if c.Timeout > 0 {
		var cancel context.CancelFunc
		ctx, cancel = context.WithTimeout(ctx, c.Timeout)
		defer cancel()
	}
	cmd := exec.CommandContext(ctx, c.Name, c.Args...)
	cmd.Dir = c.Dir
	cmd.WaitDelay = waitDelay
	if c.Env != nil || len(c.Unset) > 0 {
		cmd.Env = append(withoutVars(os.Environ(), c.Unset, runtime.GOOS == "windows"), c.Env...)
	}
	if c.Stdin != nil {
		cmd.Stdin = bytes.NewReader(c.Stdin)
	}
	var stdout, stderr bytes.Buffer
	cmd.Stdout = &stdout
	cmd.Stderr = &stderr
	configure(cmd)

	err := cmd.Run()
	out := Output{Stdout: stdout.Bytes(), Stderr: stderr.Bytes(), ExitCode: -1}
	if cmd.ProcessState != nil {
		out.ExitCode = cmd.ProcessState.ExitCode()
	}
	if err == nil {
		return out, nil
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return out, fmt.Errorf("%s: %w", c.String(), ctxErr)
	}
	var exitErr *exec.ExitError
	if errors.As(err, &exitErr) && out.ExitCode > 0 {
		return out, &ExitError{Command: c.String(), ExitCode: out.ExitCode, Stderr: Tail(out.Stderr, stderrTail)}
	}
	return out, fmt.Errorf("%s: %w", c.String(), err)
}

// withoutVars returns env (KEY=VALUE entries) without the variables unset names; fold compares names without case, as Windows does.
func withoutVars(env, unset []string, fold bool) []string {
	if len(unset) == 0 {
		return env
	}
	out := make([]string, 0, len(env))
	for _, kv := range env {
		name, _, _ := strings.Cut(kv, "=")
		if !unsets(name, unset, fold) {
			out = append(out, kv)
		}
	}
	return out
}

// unsets reports whether one of unset names the variable name.
func unsets(name string, unset []string, fold bool) bool {
	if fold {
		name = strings.ToUpper(name)
	}
	for _, u := range unset {
		if fold {
			u = strings.ToUpper(u)
		}
		if prefix, ok := strings.CutSuffix(u, "*"); ok {
			if strings.HasPrefix(name, prefix) {
				return true
			}
		} else if name == u {
			return true
		}
	}
	return false
}

// Tail returns at most n bytes from the end of b as trimmed text, for putting command output into Result.Detail.
func Tail(b []byte, n int) string {
	if len(b) > n {
		b = b[len(b)-n:]
	}
	return strings.TrimSpace(string(b))
}
