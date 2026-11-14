package execx

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"os/exec"
	"slices"
	"strconv"
	"strings"
	"testing"
	"time"
)

// TestMain turns the test binary into a harmless helper program when SPRUCE_EXECX_HELPER is set, so OSRunner is tested against a real process on every platform without running any system tool.
func TestMain(m *testing.M) {
	switch os.Getenv("SPRUCE_EXECX_HELPER") {
	case "":
		os.Exit(m.Run())
	case "echo":
		in, _ := io.ReadAll(os.Stdin)
		fmt.Fprintf(os.Stdout, "out:%s:%s", in, os.Getenv("SPRUCE_EXTRA"))
		fmt.Fprint(os.Stderr, "err")
		os.Exit(0)
	case "exit":
		code, _ := strconv.Atoi(os.Args[len(os.Args)-1])
		fmt.Fprint(os.Stderr, "something broke")
		os.Exit(code)
	case "env":
		// Prints name=value for each argument after "--", "name!" for a variable that is not set.
		for _, name := range os.Args[slices.Index(os.Args, "--")+1:] {
			if v, ok := os.LookupEnv(name); ok {
				fmt.Fprintf(os.Stdout, "%s=%s\n", name, v)
			} else {
				fmt.Fprintf(os.Stdout, "%s!\n", name)
			}
		}
		os.Exit(0)
	case "sleep":
		time.Sleep(time.Minute)
		os.Exit(0)
	}
}

func helper(t *testing.T, mode string, args ...string) Command {
	t.Helper()
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	return Command{Name: exe, Args: args, Env: []string{"SPRUCE_EXECX_HELPER=" + mode}}
}

func TestOSRunnerCapturesOutput(t *testing.T) {
	cmd := helper(t, "echo")
	cmd.Stdin = []byte("in")
	cmd.Env = append(cmd.Env, "SPRUCE_EXTRA=x")
	out, err := OSRunner{}.Run(t.Context(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Stdout) != "out:in:x" || string(out.Stderr) != "err" || out.ExitCode != 0 {
		t.Fatalf("output %q %q %d", out.Stdout, out.Stderr, out.ExitCode)
	}
}

func TestOSRunnerExitError(t *testing.T) {
	out, err := OSRunner{}.Run(t.Context(), helper(t, "exit", "3"))
	var exitErr *ExitError
	if !errors.As(err, &exitErr) {
		t.Fatalf("err = %v", err)
	}
	if exitErr.ExitCode != 3 || out.ExitCode != 3 || exitErr.Stderr != "something broke" {
		t.Fatalf("exit error %+v, output code %d", exitErr, out.ExitCode)
	}
}

func TestOSRunnerTimeout(t *testing.T) {
	cmd := helper(t, "sleep")
	cmd.Timeout = 200 * time.Millisecond
	start := time.Now()
	out, err := OSRunner{}.Run(t.Context(), cmd)
	if !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("err = %v", err)
	}
	if out.ExitCode != -1 {
		t.Errorf("exit code %d", out.ExitCode)
	}
	if time.Since(start) > 30*time.Second {
		t.Errorf("timeout took %v", time.Since(start))
	}
}

func TestOSRunnerNotFound(t *testing.T) {
	_, err := OSRunner{}.Run(t.Context(), Command{Name: "spruce-no-such-program"})
	if !errors.Is(err, exec.ErrNotFound) {
		t.Fatalf("err = %v", err)
	}
}

// TestOSRunnerUnset: the program does not inherit the variables Unset names (a trailing "*" removes a prefix), Env still applies after them, and the rest of the environment is kept; Unset alone, with Env nil, builds the environment too.
func TestOSRunnerUnset(t *testing.T) {
	t.Setenv("GIT_DIR", "/elsewhere/.git")
	t.Setenv("GIT_CONFIG_KEY_0", "core.hooksPath")
	t.Setenv("GIT_CONFIG_KEY_1", "x")
	t.Setenv("GIT_CONFIGURE", "kept, no star")
	t.Setenv("SPRUCE_KEEP", "kept")
	names := []string{"GIT_DIR", "GIT_CONFIG_KEY_0", "GIT_CONFIG_KEY_1", "GIT_CONFIGURE", "SPRUCE_KEEP", "LC_ALL"}

	cmd := helper(t, "env", append([]string{"--"}, names...)...)
	cmd.Unset = []string{"GIT_DIR", "GIT_CONFIG_KEY_*", "SPRUCE_EXECX_HELPER"}
	cmd.Env = append(cmd.Env, "LC_ALL=C")
	out, err := OSRunner{}.Run(t.Context(), cmd)
	if err != nil {
		t.Fatal(err)
	}
	want := "GIT_DIR!\nGIT_CONFIG_KEY_0!\nGIT_CONFIG_KEY_1!\nGIT_CONFIGURE=kept, no star\nSPRUCE_KEEP=kept\nLC_ALL=C\n"
	if string(out.Stdout) != want {
		t.Errorf("environment:\n%s\nwant:\n%s", out.Stdout, want)
	}

	// Unset with Env nil: the helper switch itself comes from the parent environment here.
	t.Setenv("SPRUCE_EXECX_HELPER", "env")
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	out, err = OSRunner{}.Run(t.Context(), Command{Name: exe, Args: []string{"--", "GIT_DIR", "SPRUCE_KEEP"}, Unset: []string{"GIT_DIR"}})
	if err != nil {
		t.Fatal(err)
	}
	if string(out.Stdout) != "GIT_DIR!\nSPRUCE_KEEP=kept\n" {
		t.Errorf("Unset without Env: %q", out.Stdout)
	}
}

func TestWithoutVarsFoldsOnWindows(t *testing.T) {
	env := []string{"Git_Dir=x", "PATH=/bin", "=C:=C:\\", "git_config_key_3=y"}
	got := withoutVars(env, []string{"GIT_DIR", "GIT_CONFIG_KEY_*"}, true)
	if strings.Join(got, ";") != "PATH=/bin;=C:=C:\\" {
		t.Errorf("folded: %q", got)
	}
	if got := withoutVars(env, []string{"GIT_DIR"}, false); len(got) != len(env) {
		t.Errorf("case-sensitive removed %q", got)
	}
}
