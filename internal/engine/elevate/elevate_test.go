package elevate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

const testExe = "/Applications/Spruce.app/Contents/MacOS/Spruce"

var testAllowed = []string{"dns", "spotlight", "icons", "launchservices"}

var channels = map[string]channel{"stdout": viaStdout, "file": viaFile}

// childLauncher plays the platform launcher and the elevated child inside the test: it runs serveChild with the arguments the parent built, the way main.go does in the relaunched executable, and behaves like the real launchers on a non-zero exit (osascript loses the output, ShellExecuteEx reports the status).
type childLauncher struct {
	t        *testing.T
	ch       channel
	tempDir  string
	handler  Handler
	notAdmin bool
	err      error

	launched bool
	gotExe   string
	gotArgs  []string
	code     int
}

func (l *childLauncher) Launch(ctx context.Context, exe string, args []string) ([]byte, error) {
	l.launched, l.gotExe, l.gotArgs = true, exe, args
	if l.err != nil {
		return nil, l.err
	}
	var stdout, stderr bytes.Buffer
	l.code = serveChild(ctx, args, testAllowed, l.handler, childEnv{
		channel:  l.ch,
		stdout:   &stdout,
		stderr:   &stderr,
		tempDir:  l.tempDir,
		elevated: func() bool { return !l.notAdmin },
	})
	if l.code != ExitOK {
		return nil, fmt.Errorf("child exited with status %d: %s", l.code, strings.TrimSpace(stderr.String()))
	}
	if l.ch == viaFile {
		return nil, nil
	}
	return stdout.Bytes(), nil
}

func echoHandler(_ context.Context, ids []string) []taskresult.Result {
	out := make([]taskresult.Result, len(ids))
	for i, id := range ids {
		out[i] = taskresult.Result{TaskID: id, Outcome: taskresult.Applied, Message: "已完成 " + id}
	}
	slices.Reverse(out) // the parent must restore the requested order
	return out
}

func newPair(t *testing.T, ch channel, h Handler) (*ProcessElevator, *childLauncher) {
	tmp := t.TempDir()
	l := &childLauncher{t: t, ch: ch, tempDir: tmp, handler: h}
	p := New(testExe, l)
	p.TempDir, p.channel = tmp, ch
	return p, l
}

func TestNewUsesPlatformChannel(t *testing.T) {
	if p := New(testExe, nil); p.channel != platformChannel {
		t.Fatalf("channel = %d, want %d", p.channel, platformChannel)
	}
}

func TestRunRoundTrip(t *testing.T) {
	for name, ch := range channels {
		t.Run(name, func(t *testing.T) {
			p, l := newPair(t, ch, echoHandler)
			got, err := p.Run(t.Context(), []string{"dns", "spotlight", "icons"})
			if err != nil {
				t.Fatal(err)
			}
			if l.code != ExitOK || l.gotExe != testExe {
				t.Fatalf("child exit code %d, exe %q", l.code, l.gotExe)
			}
			if want := []string{Flag, "--tasks", "dns,spotlight,icons"}; !slices.Equal(l.gotArgs[:3], want) {
				t.Errorf("args %q, want prefix %q", l.gotArgs, want)
			}
			if ch == viaFile && (len(l.gotArgs) != 5 || l.gotArgs[3] != "--out" || filepath.Dir(l.gotArgs[4]) != p.TempDir) {
				t.Errorf("file channel args %q", l.gotArgs)
			}
			if ch == viaStdout && len(l.gotArgs) != 3 {
				t.Errorf("stdout channel args %q", l.gotArgs)
			}
			var ids []string
			for _, r := range got {
				ids = append(ids, r.TaskID)
				if r.Outcome != taskresult.Applied || r.Message != "已完成 "+r.TaskID {
					t.Errorf("%s: %+v", r.TaskID, r)
				}
			}
			if !slices.Equal(ids, []string{"dns", "spotlight", "icons"}) {
				t.Errorf("results in order %q", ids)
			}
			if entries, _ := os.ReadDir(p.TempDir); len(entries) != 0 {
				t.Errorf("exchange directory left behind: %v", entries)
			}
		})
	}
}

func TestRunEmptyDoesNotLaunch(t *testing.T) {
	p, l := newPair(t, platformChannel, echoHandler)
	got, err := p.Run(t.Context(), nil)
	if err != nil || got != nil || l.launched {
		t.Fatalf("got %v, %v, launched %v", got, err, l.launched)
	}
}

func TestRunCancelledContextDoesNotLaunch(t *testing.T) {
	p, l := newPair(t, platformChannel, echoHandler)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	if _, err := p.Run(ctx, []string{"dns"}); !errors.Is(err, context.Canceled) || l.launched {
		t.Fatalf("err = %v, launched %v", err, l.launched)
	}
}

func TestRunCancelledPrompt(t *testing.T) {
	for name, ch := range channels {
		t.Run(name, func(t *testing.T) {
			p, l := newPair(t, ch, echoHandler)
			l.err = fmt.Errorf("wrapped: %w", ErrCancelled)
			if _, err := p.Run(t.Context(), []string{"dns"}); err != ErrCancelled {
				t.Fatalf("err = %v, want ErrCancelled itself", err)
			}
			if entries, _ := os.ReadDir(p.TempDir); len(entries) != 0 {
				t.Errorf("exchange directory left behind: %v", entries)
			}
		})
	}
}

func TestRunLaunchFailure(t *testing.T) {
	for name, ch := range channels {
		t.Run(name, func(t *testing.T) {
			p, l := newPair(t, ch, echoHandler)
			l.err = errors.New("osascript: no such file")
			_, err := p.Run(t.Context(), []string{"dns"})
			if err == nil || errors.Is(err, ErrCancelled) || !strings.Contains(err.Error(), "no such file") {
				t.Fatalf("err = %v", err)
			}
		})
	}
}

func TestRunRejectsInvalidIDs(t *testing.T) {
	for _, ids := range [][]string{{"DNS"}, {"dns", "dns"}, {"../x"}, {"dns,icons"}, {strings.Repeat("a", 65)}} {
		p, l := newPair(t, platformChannel, echoHandler)
		if _, err := p.Run(t.Context(), ids); err == nil {
			t.Errorf("Run(%q) succeeded", ids)
		}
		if l.launched {
			t.Errorf("Run(%q) launched", ids)
		}
	}
}

func TestRunRejectsBadResponses(t *testing.T) {
	cases := map[string]Handler{
		"missing result": func(_ context.Context, ids []string) []taskresult.Result {
			return []taskresult.Result{{TaskID: ids[0], Outcome: taskresult.Applied}}
		},
		"unrequested result": func(_ context.Context, ids []string) []taskresult.Result {
			return append(echoHandler(context.Background(), ids), taskresult.Result{TaskID: "other", Outcome: taskresult.Applied})
		},
		"duplicate result": func(_ context.Context, ids []string) []taskresult.Result {
			return append(echoHandler(context.Background(), ids), taskresult.Result{TaskID: ids[0], Outcome: taskresult.Failed})
		},
		"invalid outcome": func(_ context.Context, ids []string) []taskresult.Result {
			return []taskresult.Result{{TaskID: ids[0], Outcome: "done"}, {TaskID: ids[1], Outcome: taskresult.Applied}}
		},
	}
	for name, h := range cases {
		for chName, ch := range channels {
			t.Run(name+"/"+chName, func(t *testing.T) {
				p, _ := newPair(t, ch, h)
				if _, err := p.Run(t.Context(), []string{"dns", "icons"}); err == nil {
					t.Fatal("Run succeeded")
				}
			})
		}
	}
}

func TestRunRejectsMalformedStdout(t *testing.T) {
	cases := map[string]string{
		"empty":         "",
		"not json":      "Password:\n",
		"wrong version": `{"version":2,"results":[]}`,
		"refusal":       `{"version":1,"error":"nope"}`,
		"too large":     strings.Repeat(" ", maxResponseSize+1),
	}
	for name, out := range cases {
		t.Run(name, func(t *testing.T) {
			p := New(testExe, launcherFunc(func(context.Context, string, []string) ([]byte, error) { return []byte(out), nil }))
			p.channel = viaStdout
			if _, err := p.Run(t.Context(), []string{"dns"}); err == nil {
				t.Fatal("Run succeeded")
			}
		})
	}
}

func TestRunFileChannelWithoutResponse(t *testing.T) {
	p := New(testExe, launcherFunc(func(context.Context, string, []string) ([]byte, error) { return nil, nil }))
	p.TempDir, p.channel = t.TempDir(), viaFile
	if _, err := p.Run(t.Context(), []string{"dns"}); err == nil || !strings.Contains(err.Error(), "no response") {
		t.Fatalf("err = %v", err)
	}
}

func TestRunReportsChildRefusal(t *testing.T) {
	for name, ch := range channels {
		t.Run(name, func(t *testing.T) {
			ran := false
			p, l := newPair(t, ch, func(context.Context, []string) []taskresult.Result { ran = true; return nil })
			l.notAdmin = true
			_, err := p.Run(t.Context(), []string{"dns"})
			if err == nil || !strings.Contains(err.Error(), "not running with administrator rights") {
				t.Fatalf("err = %v", err)
			}
			if ran || l.code != ExitNotElevated {
				t.Fatalf("ran %v, exit code %d", ran, l.code)
			}
		})
	}
}

type launcherFunc func(ctx context.Context, exe string, args []string) ([]byte, error)

func (f launcherFunc) Launch(ctx context.Context, exe string, args []string) ([]byte, error) {
	return f(ctx, exe, args)
}

// serve runs serveChild with a fake environment and reports the exit code, whether the handler ran, and what went to stdout.
func serve(t *testing.T, args []string, env childEnv) (code int, ran bool, stdout string) {
	t.Helper()
	var out bytes.Buffer
	env.stdout, env.stderr = &out, io.Discard
	if env.elevated == nil {
		env.elevated = func() bool { return true }
	}
	code = serveChild(t.Context(), args, testAllowed, func(ctx context.Context, ids []string) []taskresult.Result {
		ran = true
		return echoHandler(ctx, ids)
	}, env)
	return code, ran, out.String()
}

func TestServeChildStdout(t *testing.T) {
	code, ran, out := serve(t, []string{Flag, "--tasks", "icons,dns"}, childEnv{channel: viaStdout})
	if code != ExitOK || !ran {
		t.Fatalf("code %d, ran %v", code, ran)
	}
	resp, err := decodeResponse([]byte(out))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 2 || resp.Error != "" {
		t.Fatalf("response %+v", resp)
	}
	if !strings.HasSuffix(out, "}\n") || strings.Count(out, "\n") != 1 {
		t.Errorf("stdout is not one JSON line: %q", out)
	}
}

func TestServeChildRejectsArguments(t *testing.T) {
	dir := func(t *testing.T) string {
		d, err := os.MkdirTemp(t.TempDir(), exchangeDirPrefix)
		if err != nil {
			t.Fatal(err)
		}
		return d
	}
	stdoutCases := map[string][]string{
		"no tasks flag":       {Flag},
		"equals form":         {Flag, "--tasks=dns"},
		"empty list":          {Flag, "--tasks", ""},
		"trailing comma":      {Flag, "--tasks", "dns,"},
		"leading comma":       {Flag, "--tasks", ",dns"},
		"duplicate":           {Flag, "--tasks", "dns,dns"},
		"upper case":          {Flag, "--tasks", "DNS"},
		"shell syntax":        {Flag, "--tasks", "dns;reboot"},
		"space":               {Flag, "--tasks", "dns icons"},
		"not an admin task":   {Flag, "--tasks", "dns,quicklook"},
		"unknown task":        {Flag, "--tasks", "rm-rf"},
		"extra argument":      {Flag, "--tasks", "dns", "--verbose"},
		"out on stdout":       {Flag, "--tasks", "dns", "--out", "/tmp"},
		"flag not first":      {"--tasks", "dns", Flag},
		"command smuggled":    {Flag, "--tasks", "dns", "/bin/sh", "-c", "id"},
		"another first flag":  {"--other", "--tasks", "dns"},
		"tasks flag repeated": {Flag, "--tasks", "--tasks"},
	}
	for name, args := range stdoutCases {
		t.Run("stdout/"+name, func(t *testing.T) {
			code, ran, out := serve(t, args, childEnv{channel: viaStdout})
			if code != ExitBadRequest || ran || out != "" {
				t.Fatalf("code %d, ran %v, stdout %q", code, ran, out)
			}
		})
	}
	fileCases := map[string]func(d string) []string{
		"missing out":   func(string) []string { return []string{Flag, "--tasks", "dns"} },
		"other flag":    func(d string) []string { return []string{Flag, "--tasks", "dns", "--output", d} },
		"out first":     func(d string) []string { return []string{Flag, "--out", d, "--tasks", "dns"} },
		"extra":         func(d string) []string { return []string{Flag, "--tasks", "dns", "--out", d, "x"} },
		"not allowed":   func(d string) []string { return []string{Flag, "--tasks", "quicklook", "--out", d} },
		"bad id syntax": func(d string) []string { return []string{Flag, "--tasks", `..\x`, "--out", d} },
	}
	for name, mk := range fileCases {
		t.Run("file/"+name, func(t *testing.T) {
			d := dir(t)
			code, ran, _ := serve(t, mk(d), childEnv{channel: viaFile, tempDir: filepath.Dir(d)})
			if code != ExitBadRequest || ran {
				t.Fatalf("code %d, ran %v", code, ran)
			}
			if _, err := os.Lstat(filepath.Join(d, ResponseFile)); !errors.Is(err, os.ErrNotExist) {
				t.Fatalf("a response was written: %v", err)
			}
		})
	}
}

func TestServeChildRefusesWhenNotElevated(t *testing.T) {
	code, ran, out := serve(t, []string{Flag, "--tasks", "dns"}, childEnv{channel: viaStdout, elevated: func() bool { return false }})
	if code != ExitNotElevated || ran {
		t.Fatalf("code %d, ran %v", code, ran)
	}
	var resp Response
	if err := json.Unmarshal([]byte(out), &resp); err != nil || resp.Error == "" || resp.Results != nil {
		t.Fatalf("response %q: %v", out, err)
	}
}

func TestServeChildFileChannel(t *testing.T) {
	temp := t.TempDir()
	d, err := os.MkdirTemp(temp, exchangeDirPrefix)
	if err != nil {
		t.Fatal(err)
	}
	code, ran, out := serve(t, []string{Flag, "--tasks", "dns,icons", "--out", d}, childEnv{channel: viaFile, tempDir: temp})
	if code != ExitOK || !ran || out != "" {
		t.Fatalf("code %d, ran %v, stdout %q", code, ran, out)
	}
	resp, err := readResponseFile(filepath.Join(d, ResponseFile))
	if err != nil {
		t.Fatal(err)
	}
	if len(resp.Results) != 2 {
		t.Fatalf("response %+v", resp)
	}
}

// TestServeChildResponseFileSafety covers the rules for the exchange directory the Windows child writes into: it must be a plain directory named spruce-elevated-<digits> directly inside the system temp directory, and the response file must not exist yet.
func TestServeChildResponseFileSafety(t *testing.T) {
	type setup func(t *testing.T, temp string) (dir string, check func(t *testing.T))
	mkdir := func(t *testing.T, parent, name string) string {
		d := filepath.Join(parent, name)
		if err := os.Mkdir(d, 0o700); err != nil {
			t.Fatal(err)
		}
		return d
	}
	symlink := func(t *testing.T, target, link string) {
		if err := os.Symlink(target, link); err != nil {
			t.Skipf("symlinks unavailable: %v", err)
		}
	}
	cases := map[string]setup{
		"outside the temp dir": func(t *testing.T, _ string) (string, func(*testing.T)) {
			return mkdir(t, t.TempDir(), exchangeDirPrefix+"1"), nil
		},
		"nested below the temp dir": func(t *testing.T, temp string) (string, func(*testing.T)) {
			return mkdir(t, mkdir(t, temp, "sub"), exchangeDirPrefix+"1"), nil
		},
		"the temp dir itself": func(t *testing.T, temp string) (string, func(*testing.T)) {
			return temp, nil
		},
		"wrong name": func(t *testing.T, temp string) (string, func(*testing.T)) {
			return mkdir(t, temp, "System32"), nil
		},
		"prefix without digits": func(t *testing.T, temp string) (string, func(*testing.T)) {
			return mkdir(t, temp, exchangeDirPrefix), nil
		},
		"non-digit suffix": func(t *testing.T, temp string) (string, func(*testing.T)) {
			return mkdir(t, temp, exchangeDirPrefix+"1x"), nil
		},
		"relative": func(t *testing.T, temp string) (string, func(*testing.T)) {
			mkdir(t, temp, exchangeDirPrefix+"1")
			return exchangeDirPrefix + "1", nil
		},
		"not clean": func(t *testing.T, temp string) (string, func(*testing.T)) {
			mkdir(t, temp, exchangeDirPrefix+"1")
			return temp + string(filepath.Separator) + "." + string(filepath.Separator) + exchangeDirPrefix + "1", nil
		},
		"missing": func(t *testing.T, temp string) (string, func(*testing.T)) {
			return filepath.Join(temp, exchangeDirPrefix+"1"), nil
		},
		"directory is a symlink": func(t *testing.T, temp string) (string, func(*testing.T)) {
			elsewhere := t.TempDir()
			link := filepath.Join(temp, exchangeDirPrefix+"1")
			symlink(t, elsewhere, link)
			return link, func(t *testing.T) {
				if _, err := os.Lstat(filepath.Join(elsewhere, ResponseFile)); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("wrote through the symlinked directory: %v", err)
				}
			}
		},
		"response already exists": func(t *testing.T, temp string) (string, func(*testing.T)) {
			d := mkdir(t, temp, exchangeDirPrefix+"1")
			planted := filepath.Join(d, ResponseFile)
			if err := os.WriteFile(planted, []byte("planted"), 0o600); err != nil {
				t.Fatal(err)
			}
			return d, func(t *testing.T) {
				if b, _ := os.ReadFile(planted); string(b) != "planted" {
					t.Errorf("planted response overwritten: %q", b)
				}
			}
		},
		"response is a symlink to a file": func(t *testing.T, temp string) (string, func(*testing.T)) {
			d := mkdir(t, temp, exchangeDirPrefix+"1")
			victim := filepath.Join(t.TempDir(), "victim")
			if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
				t.Fatal(err)
			}
			symlink(t, victim, filepath.Join(d, ResponseFile))
			return d, func(t *testing.T) {
				if b, _ := os.ReadFile(victim); string(b) != "keep" {
					t.Errorf("symlink target overwritten: %q", b)
				}
			}
		},
		"response is a dangling symlink": func(t *testing.T, temp string) (string, func(*testing.T)) {
			d := mkdir(t, temp, exchangeDirPrefix+"1")
			victim := filepath.Join(t.TempDir(), "created-by-root")
			symlink(t, victim, filepath.Join(d, ResponseFile))
			return d, func(t *testing.T) {
				if _, err := os.Lstat(victim); !errors.Is(err, os.ErrNotExist) {
					t.Errorf("symlink target created: %v", err)
				}
			}
		},
	}
	for name, mk := range cases {
		t.Run(name, func(t *testing.T) {
			temp := t.TempDir()
			dir, check := mk(t, temp)
			code, ran, _ := serve(t, []string{Flag, "--tasks", "dns", "--out", dir}, childEnv{channel: viaFile, tempDir: temp})
			if code != ExitIOError || ran {
				t.Fatalf("code %d, ran %v", code, ran)
			}
			if check != nil {
				check(t)
			}
		})
	}
}
