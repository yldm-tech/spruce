package elevate

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// inProcessLauncher plays the elevated child inside the test: it checks the argument contract and calls ServeChild, as main.go does in the relaunched executable.
type inProcessLauncher struct {
	t       *testing.T
	handler Handler
	err     error
	code    int
	gotArgs []string
}

func (l *inProcessLauncher) Launch(ctx context.Context, exe string, args []string) error {
	l.gotArgs = args
	if l.err != nil {
		return l.err
	}
	if exe != "/Applications/Spruce.app/Contents/MacOS/Spruce" {
		l.t.Errorf("exe = %q", exe)
	}
	if !IsChild(args) {
		l.t.Errorf("args %q do not start the child", args)
	}
	l.code = ServeChild(ctx, args, l.handler)
	return nil
}

func echoHandler(ctx context.Context, ids []string) []taskresult.Result {
	out := make([]taskresult.Result, len(ids))
	for i, id := range ids {
		out[i] = taskresult.Result{TaskID: id, Outcome: taskresult.Applied, Message: "已完成 " + id}
	}
	slices.Reverse(out) // the parent must restore the requested order
	return out
}

func newElevator(t *testing.T, l Launcher) *ProcessElevator {
	p := New("/Applications/Spruce.app/Contents/MacOS/Spruce", l)
	p.TempDir = t.TempDir()
	return p
}

func TestRunRoundTrip(t *testing.T) {
	l := &inProcessLauncher{t: t, handler: echoHandler}
	p := newElevator(t, l)
	got, err := p.Run(t.Context(), []string{"dns", "spotlight", "icons"})
	if err != nil {
		t.Fatal(err)
	}
	if l.code != ExitOK {
		t.Fatalf("child exit code %d", l.code)
	}
	var ids []string
	for _, r := range got {
		ids = append(ids, r.TaskID)
		if r.Outcome != taskresult.Applied {
			t.Errorf("%s: outcome %q", r.TaskID, r.Outcome)
		}
	}
	if !slices.Equal(ids, []string{"dns", "spotlight", "icons"}) {
		t.Errorf("results in order %q", ids)
	}
	if entries, _ := os.ReadDir(p.TempDir); len(entries) != 0 {
		t.Errorf("exchange directory left behind: %v", entries)
	}
}

func TestRunEmptyDoesNotLaunch(t *testing.T) {
	l := &inProcessLauncher{t: t, handler: echoHandler}
	got, err := newElevator(t, l).Run(t.Context(), nil)
	if err != nil || got != nil || l.gotArgs != nil {
		t.Fatalf("got %v, %v, launched %v", got, err, l.gotArgs)
	}
}

func TestRunCancelledPrompt(t *testing.T) {
	l := &inProcessLauncher{t: t, err: ErrCancelled}
	if _, err := newElevator(t, l).Run(t.Context(), []string{"dns"}); !errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v, want ErrCancelled", err)
	}
}

func TestRunLaunchFailure(t *testing.T) {
	l := &inProcessLauncher{t: t, err: errors.New("osascript: no such file")}
	_, err := newElevator(t, l).Run(t.Context(), []string{"dns"})
	if err == nil || errors.Is(err, ErrCancelled) {
		t.Fatalf("err = %v", err)
	}
}

func TestRunRejectsInvalidIDs(t *testing.T) {
	l := &inProcessLauncher{t: t, handler: echoHandler}
	for _, ids := range [][]string{{"DNS"}, {"dns", "dns"}, {"../x"}, {strings.Repeat("a", 65)}} {
		if _, err := newElevator(t, l).Run(t.Context(), ids); err == nil {
			t.Errorf("Run(%q) succeeded", ids)
		}
	}
	if l.gotArgs != nil {
		t.Errorf("launched with invalid IDs")
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
		"invalid outcome": func(_ context.Context, ids []string) []taskresult.Result {
			return []taskresult.Result{{TaskID: ids[0], Outcome: "done"}, {TaskID: ids[1], Outcome: taskresult.Applied}}
		},
	}
	for name, h := range cases {
		t.Run(name, func(t *testing.T) {
			l := &inProcessLauncher{t: t, handler: h}
			if _, err := newElevator(t, l).Run(t.Context(), []string{"dns", "icons"}); err == nil {
				t.Fatal("Run succeeded")
			}
		})
	}
}

func TestRunNoResponse(t *testing.T) {
	p := newElevator(t, launcherFunc(func(context.Context, string, []string) error { return nil }))
	if _, err := p.Run(t.Context(), []string{"dns"}); err == nil {
		t.Fatal("Run succeeded without a response")
	}
}

type launcherFunc func(ctx context.Context, exe string, args []string) error

func (f launcherFunc) Launch(ctx context.Context, exe string, args []string) error {
	return f(ctx, exe, args)
}

func TestServeChildRejectsBadRequests(t *testing.T) {
	cases := map[string]string{
		"unknown field": `{"version":1,"taskIds":["dns"],"command":"rm -rf /"}`,
		"wrong version": `{"version":2,"taskIds":["dns"]}`,
		"no tasks":      `{"version":1,"taskIds":[]}`,
		"bad id":        `{"version":1,"taskIds":["dns; reboot"]}`,
		"not json":      `dns`,
	}
	for name, body := range cases {
		t.Run(name, func(t *testing.T) {
			dir := t.TempDir()
			if err := os.WriteFile(filepath.Join(dir, RequestFile), []byte(body), 0o600); err != nil {
				t.Fatal(err)
			}
			ran := false
			code := ServeChild(t.Context(), []string{Flag, dir}, func(context.Context, []string) []taskresult.Result {
				ran = true
				return nil
			})
			if code != ExitBadRequest || ran {
				t.Fatalf("code %d, ran %v", code, ran)
			}
			if _, err := readResponse(filepath.Join(dir, ResponseFile)); err == nil || !strings.Contains(err.Error(), "rejected") {
				t.Fatalf("response error = %v", err)
			}
		})
	}
}

func TestServeChildArguments(t *testing.T) {
	h := func(context.Context, []string) []taskresult.Result { return nil }
	for _, args := range [][]string{{Flag}, {Flag, "relative/dir"}, {Flag, filepath.Join(t.TempDir(), "missing")}, {"--other", t.TempDir()}} {
		if code := ServeChild(t.Context(), args, h); code != ExitBadRequest {
			t.Errorf("ServeChild(%q) = %d", args, code)
		}
	}
}

func TestServeChildReplacesPlantedSymlink(t *testing.T) {
	dir := t.TempDir()
	victim := filepath.Join(t.TempDir(), "victim")
	if err := os.WriteFile(victim, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(victim, filepath.Join(dir, ResponseFile)); err != nil {
		t.Skipf("symlinks unavailable: %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, RequestFile), []byte(`{"version":1,"taskIds":["dns"]}`), 0o600); err != nil {
		t.Fatal(err)
	}
	if code := ServeChild(t.Context(), []string{Flag, dir}, echoHandler); code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if b, _ := os.ReadFile(victim); string(b) != "keep" {
		t.Fatalf("the symlink target was overwritten: %q", b)
	}
	if fi, err := os.Lstat(filepath.Join(dir, ResponseFile)); err != nil || fi.Mode()&os.ModeSymlink != 0 {
		t.Fatalf("response is not a regular file: %v %v", fi, err)
	}
}
