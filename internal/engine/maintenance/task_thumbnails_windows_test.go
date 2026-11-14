package maintenance

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"strings"
	"testing"

	"golang.org/x/sys/windows/registry"

	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// thumbSystem returns a winSystem whose thumbnail handler answers with p and err, and records the volume it was asked to clean.
func thumbSystem(t *testing.T, p winCachePurge, err error) (*winSystem, *string) {
	t.Helper()
	sys := newTestWinSystem(t)
	sys.explorerCacheDir = func() (string, error) { return `D:\Users\mei\AppData\Local\Microsoft\Windows\Explorer`, nil }
	var volume string
	sys.purgeThumbnailCache = func(_ context.Context, v string) (winCachePurge, error) {
		volume = v
		return p, err
	}
	return sys, &volume
}

func TestThumbnailsUsesTheDiskCleanupHandler(t *testing.T) {
	sys, volume := thumbSystem(t, winCachePurge{used: 40 << 20, freed: 40 << 20}, nil)
	f := execxtest.New()
	res := winThumbnailsTask(sys).Run(context.Background(), testEnv(f))
	if res.Outcome != taskresult.Applied || res.Partial {
		t.Fatalf("result %+v", res)
	}
	if *volume != `D:\` {
		t.Errorf("cleaned volume %q, want the volume of the Explorer cache", *volume)
	}
	if calls := f.Calls(); len(calls) != 0 {
		t.Errorf("the thumbnail task must not run programs (such as taskkill explorer.exe): %v", calls)
	}
}

func TestThumbnailsOutcomes(t *testing.T) {
	cases := []struct {
		name string
		p    winCachePurge
		err  error
		want taskresult.Outcome
	}{
		{"nothing to clean", winCachePurge{empty: true}, nil, taskresult.Unchanged},
		{"handler not registered", winCachePurge{}, fmt.Errorf("open HKLM: %w", registry.ErrNotExist), taskresult.Unavailable},
		{"32-bit Windows", winCachePurge{}, fmt.Errorf("handler: %w", errors.ErrUnsupported), taskresult.Unavailable},
		{"handler failed", winCachePurge{}, errors.New("thumbnail cache handler Purge: HRESULT 0x80070005"), taskresult.Failed},
		{"handler could not be created", winCachePurge{}, errors.New("create the thumbnail cache handler: HRESULT 0x80040154"), taskresult.Failed},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			sys, _ := thumbSystem(t, c.p, c.err)
			res := winThumbnailsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
			if res.Outcome != c.want {
				t.Fatalf("outcome %q (%s; %s), want %q", res.Outcome, res.Message, res.Detail, c.want)
			}
			if c.err != nil && !strings.Contains(res.Detail, c.err.Error()) {
				t.Errorf("detail %q lacks the cause", res.Detail)
			}
			if strings.Contains(res.Message, "重新登录") {
				t.Errorf("message %q sends the user to sign in again, which does not release Explorer's cache", res.Message)
			}
		})
	}
}

func TestThumbnailsCancelled(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	sys, _ := thumbSystem(t, winCachePurge{used: 10}, fmt.Errorf("thumbnail cache handler Purge: %w", context.Canceled))
	res := winThumbnailsTask(sys).Run(ctx, testEnv(execxtest.New()))
	if res.Outcome != taskresult.Skipped {
		t.Fatalf("result %+v, want interrupted", res)
	}

	sys, _ = thumbSystem(t, winCachePurge{used: 10, freed: 4}, fmt.Errorf("thumbnail cache handler Purge: %w", context.Canceled))
	res = winThumbnailsTask(sys).Run(ctx, testEnv(execxtest.New()))
	if res.Outcome != taskresult.Applied || !res.Partial {
		t.Fatalf("result %+v, want a partial applied result", res)
	}
}

func TestThumbnailsMissingCacheDirectory(t *testing.T) {
	sys, _ := thumbSystem(t, winCachePurge{}, nil)
	sys.explorerCacheDir = func() (string, error) { return "", &fs.PathError{Op: "SHGetKnownFolderPath", Err: fs.ErrNotExist} }
	res := winThumbnailsTask(sys).Run(context.Background(), testEnv(execxtest.New()))
	if res.Outcome != taskresult.Failed {
		t.Fatalf("result %+v", res)
	}
}
