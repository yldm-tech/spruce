package uninstall

import (
	"context"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"

	"howett.net/plist"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/fsx"
)

// fakeDeleter moves items into a folder of the test instead of the Trash, after the same root check as the real one. failing makes Trash fail for a path with the given error.
type fakeDeleter struct {
	t   *testing.T
	bin string

	mu      sync.Mutex
	trashed []string
	failing map[string]error
}

func newFakeDeleter(t *testing.T) *fakeDeleter {
	t.Helper()
	bin := filepath.Join(t.TempDir(), "Trash")
	if err := os.MkdirAll(bin, 0o755); err != nil {
		t.Fatal(err)
	}
	return &fakeDeleter{t: t, bin: bin, failing: make(map[string]error)}
}

func (d *fakeDeleter) Remove(context.Context, string, []string) (fsx.Removal, error) {
	d.t.Error("the uninstaller must never delete permanently")
	return fsx.Removal{}, errors.New("permanent deletion")
}

func (d *fakeDeleter) Trash(_ context.Context, path string, roots []string) (cleanup.Trashed, error) {
	if err := fsx.CheckTrashable(path, roots); err != nil {
		return cleanup.Trashed{}, err
	}
	d.mu.Lock()
	defer d.mu.Unlock()
	if err := d.failing[path]; err != nil {
		return cleanup.Trashed{}, err
	}
	to := filepath.Join(d.bin, fmt.Sprintf("%d-%s", len(d.trashed), filepath.Base(path)))
	if err := os.Rename(path, to); err != nil {
		return cleanup.Trashed{}, err
	}
	d.trashed = append(d.trashed, path)
	return cleanup.Trashed{Path: to}, nil
}

func (d *fakeDeleter) moved() []string {
	d.mu.Lock()
	defer d.mu.Unlock()
	return append([]string(nil), d.trashed...)
}

// writePlist writes v as an XML property list at path, creating its folder.
func writePlist(t *testing.T, path string, v any) {
	t.Helper()
	b, err := plist.Marshal(v, plist.XMLFormat)
	if err != nil {
		t.Fatal(err)
	}
	writeFile(t, path, string(b))
}

func writeFile(t *testing.T, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o644); err != nil {
		t.Fatal(err)
	}
}

// noProcesses is a Processes func with nothing running.
func noProcesses(context.Context) ([]string, error) { return nil, nil }
