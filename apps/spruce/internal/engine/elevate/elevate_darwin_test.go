package elevate

import (
	"context"
	"errors"
	"os"
	"slices"
	"strings"
	"testing"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/execx/execxtest"
	"spruce/apps/spruce/internal/engine/taskresult"
)

// newTestLauncher returns a launcher whose running executable is self and has not been replaced since startup.
func newTestLauncher(f *execxtest.Fake, self string) *osascriptLauncher {
	img := selfImage{path: self, unchanged: func() error { return nil }}
	return &osascriptLauncher{runner: f, self: func() (selfImage, error) { return img, nil }}
}

var childArgs = []string{Flag, "--tasks", "dns,icons"}

func TestLaunchRunsOsascriptWithArgv(t *testing.T) {
	f := execxtest.New()
	f.On(osascriptPath, osascriptArgs(testExe, childArgs)...).Return(`{"version":1,"results":[]}`+"\n\n", 0)
	out, err := newTestLauncher(f, testExe).Launch(t.Context(), testExe, childArgs)
	if err != nil {
		t.Fatal(err)
	}
	if string(out) != `{"version":1,"results":[]}`+"\n\n" {
		t.Errorf("stdout %q", out)
	}
	calls := f.Calls()
	if len(calls) != 1 {
		t.Fatalf("calls %v", calls)
	}
	args := calls[0].Args
	n := 2 * len(elevateScript)
	for i, line := range elevateScript {
		if args[2*i] != "-e" || args[2*i+1] != line {
			t.Fatalf("script line %d = %q %q", i, args[2*i], args[2*i+1])
		}
	}
	if want := append([]string{authPrompt, testExe}, childArgs...); !slices.Equal(args[n:], want) {
		t.Errorf("argv %q, want %q", args[n:], want)
	}
	if calls[0].Stdin != nil || calls[0].Env != nil {
		t.Errorf("unexpected stdin or env: %+v", calls[0])
	}
}

func TestScriptIsConstantAndAdministrative(t *testing.T) {
	script := strings.Join(elevateScript, "\n")
	for _, want := range []string{"quoted form of (item 2 of argv)", "quoted form of (item i of argv)", "with administrator privileges", "with prompt promptText", "without altering line endings"} {
		if !strings.Contains(script, want) {
			t.Errorf("script lacks %q", want)
		}
	}
	if strings.HasPrefix(authPrompt, "-") {
		t.Error("the prompt would be parsed as an osascript option")
	}
}

// TestHostileValuesStayArguments checks that an executable path or argument full of AppleScript and shell syntax reaches osascript as one argv element each and never appears in the script source.
func TestHostileValuesStayArguments(t *testing.T) {
	hostileExe := `/Users/a"b/x'$(id)` + "`id`" + `\" & (do shell script "id") & "/Evil.app/Contents/MacOS/Spruce`
	hostileArgs := []string{Flag, "--tasks", `dns"; do shell script "id`, "it's $(id) ; id"}
	f := execxtest.New()
	f.On(osascriptPath, osascriptArgs(hostileExe, hostileArgs)...).Return("{}", 0)
	if _, err := newTestLauncher(f, hostileExe).Launch(t.Context(), hostileExe, hostileArgs); err != nil {
		t.Fatal(err)
	}
	args := f.Calls()[0].Args
	n := 2 * len(elevateScript)
	if !slices.Equal(args[n:], append([]string{authPrompt, hostileExe}, hostileArgs...)) {
		t.Fatalf("argv %q", args[n:])
	}
	for _, a := range args[:n] {
		if a != "-e" && !slices.Contains(elevateScript, a) {
			t.Errorf("value spliced into the script: %q", a)
		}
	}
}

// TestQuotingRoundTripThroughRealOsascript runs the launcher's script with the administrator clause removed (so no prompt appears and nothing runs as root) through the real osascript and /bin/sh, with /usr/bin/printf standing in for the executable, and checks that hostile arguments arrive byte for byte and nothing in them is executed.
func TestQuotingRoundTripThroughRealOsascript(t *testing.T) {
	if _, err := os.Stat(osascriptPath); err != nil {
		t.Skipf("osascript unavailable: %v", err)
	}
	hostile := []string{
		"it's",
		`"; printf PWNED; echo "`,
		"$(printf PWNED)",
		"`printf PWNED`",
		`\`,
		`' & quoted form of "x" & '`,
		"two\nlines",
		"--spruce-elevated",
		"-e",
		"已完成 dns",
		`C:\Temp\spruce-elevated-1`,
	}
	args := osascriptArgs("/usr/bin/printf", append([]string{"%s\n"}, hostile...))
	const adminClause = " with prompt promptText with administrator privileges"
	replaced := false
	for i, a := range args {
		if strings.Contains(a, adminClause) {
			args[i] = strings.Replace(a, adminClause, "", 1)
			replaced = true
		}
	}
	if !replaced {
		t.Fatal("the script no longer contains the administrator clause this test removes")
	}
	out, err := execx.OSRunner{}.Run(t.Context(), execx.Command{Name: osascriptPath, Args: args})
	if err != nil {
		t.Fatalf("osascript: %v (stderr %q)", err, out.Stderr)
	}
	got := strings.TrimSuffix(string(out.Stdout), "\n")
	if want := strings.Join(hostile, "\n") + "\n"; got != want {
		t.Fatalf("round trip\n got %q\nwant %q", got, want)
	}
}

func TestLaunchMapsUserCancel(t *testing.T) {
	for _, stderr := range []string{
		"0:312: execution error: User canceled. (-128)\n",
		"0:312: execution error: ユーザによってキャンセルされました。 (-128)",
		"execution error: 用户已取消。 (-128)\r\n",
	} {
		f := execxtest.New()
		f.On(osascriptPath, osascriptArgs(testExe, childArgs)...).ReturnOutput(execx.Output{Stderr: []byte(stderr), ExitCode: 1})
		if _, err := newTestLauncher(f, testExe).Launch(t.Context(), testExe, childArgs); err != ErrCancelled {
			t.Errorf("stderr %q: err = %v, want ErrCancelled", stderr, err)
		}
	}
}

func TestLaunchOtherFailuresAreNotCancel(t *testing.T) {
	for _, stderr := range []string{
		"0:312: execution error: elevate: usage (2)\n",
		"execution error: The administrator user name or password was incorrect. (-60007)",
		"execution error: something (-1280)",
		"execution error: (128)",
	} {
		f := execxtest.New()
		f.On(osascriptPath, osascriptArgs(testExe, childArgs)...).ReturnOutput(execx.Output{Stderr: []byte(stderr), ExitCode: 1})
		_, err := newTestLauncher(f, testExe).Launch(t.Context(), testExe, childArgs)
		if err == nil || errors.Is(err, ErrCancelled) {
			t.Errorf("stderr %q: err = %v", stderr, err)
		}
	}
	f := execxtest.New()
	f.On(osascriptPath, osascriptArgs(testExe, childArgs)...).Fail(context.DeadlineExceeded)
	if _, err := newTestLauncher(f, testExe).Launch(t.Context(), testExe, childArgs); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("err = %v", err)
	}
}

func TestLaunchRefusesForeignExecutables(t *testing.T) {
	cases := map[string]struct{ exe, self string }{
		"not the running executable": {"/bin/sh", testExe},
		"relative":                   {"Spruce", "Spruce"},
		"not clean":                  {"/Applications/Spruce.app/Contents/MacOS/../MacOS/Spruce", "/Applications/Spruce.app/Contents/MacOS/../MacOS/Spruce"},
		"outside a bundle":           {"/usr/local/bin/spruce", "/usr/local/bin/spruce"},
		"bundle resources":           {"/Applications/Spruce.app/Contents/Resources/helper", "/Applications/Spruce.app/Contents/Resources/helper"},
		"not an .app":                {"/Applications/Spruce/Contents/MacOS/Spruce", "/Applications/Spruce/Contents/MacOS/Spruce"},
	}
	for name, c := range cases {
		t.Run(name, func(t *testing.T) {
			f := execxtest.New()
			if _, err := newTestLauncher(f, c.self).Launch(t.Context(), c.exe, childArgs); err == nil {
				t.Fatal("Launch accepted")
			}
			f.AssertNotCalled(t, osascriptPath)
		})
	}
	f := execxtest.New()
	l := &osascriptLauncher{runner: f, self: func() (selfImage, error) { return selfImage{}, errors.New("no executable") }}
	if _, err := l.Launch(t.Context(), testExe, childArgs); err == nil {
		t.Fatal("Launch accepted without knowing the running executable")
	}
	f.AssertNotCalled(t, osascriptPath)

	replaced := selfImage{path: testExe, unchanged: func() error { return errors.New("replaced") }}
	l = &osascriptLauncher{runner: f, self: func() (selfImage, error) { return replaced, nil }}
	if _, err := l.Launch(t.Context(), testExe, childArgs); err == nil {
		t.Fatal("Launch accepted an executable replaced since startup")
	}
	f.AssertNotCalled(t, osascriptPath)
}

func TestInAppBundle(t *testing.T) {
	for exe, want := range map[string]bool{
		testExe: true,
		"/Users/me/proj/.nsgo/dev/macos/Spruce.app/Contents/MacOS/Spruce": true,
		"/Applications/Spruce.app/Contents/MacOS":                         false,
		"/Applications/Spruce.app/Contents/Frameworks/x":                  false,
		"/tmp/go-build123/b001/exe/spruce":                                false,
	} {
		if got := inAppBundle(exe); got != want {
			t.Errorf("inAppBundle(%q) = %v", exe, got)
		}
	}
}

func TestPlatformChildEnv(t *testing.T) {
	env := platformChildEnv()
	if env.channel != viaStdout || env.stdout != os.Stdout {
		t.Fatalf("env %+v", env)
	}
	if env.elevated() != (os.Geteuid() == 0) {
		t.Error("elevated does not follow the effective user ID")
	}
}

// TestServeChildRefusesUnelevatedProcess runs the real ServeChild, which must refuse in a test process that is not root.
func TestServeChildRefusesUnelevatedProcess(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("running as root")
	}
	ran := false
	code := ServeChild(t.Context(), []string{Flag, "--tasks", "dns"}, []string{"dns"}, func(context.Context, []string) []taskresult.Result {
		ran = true
		return nil
	})
	if code != ExitNotElevated || ran {
		t.Fatalf("code %d, ran %v", code, ran)
	}
}
