package trash

import (
	"context"
	"errors"
	"io/fs"
	"testing"
)

// fakeMover records what it was asked to move and fails the paths in fail.
type fakeMover struct {
	moved []string
	fail  map[string]error
	// cancel, when set, is called after the first move.
	cancel func()
}

func (f *fakeMover) move(path string) (string, error) {
	if f.cancel != nil {
		defer f.cancel()
	}
	if err := f.fail[path]; err != nil {
		return "", err
	}
	f.moved = append(f.moved, path)
	return path + ".trashed", nil
}

func TestMoveAllKeepsGoingAndReportsEach(t *testing.T) {
	m := &fakeMover{fail: map[string]error{"/b": fs.ErrNotExist}}
	out, err := moveAll(t.Context(), m, []string{"/a", "/b", "relative", "/c/../d"})
	if err != nil {
		t.Fatal(err)
	}
	if len(out) != 4 || out[0].Err != nil || out[0].TrashedPath != "/a.trashed" {
		t.Fatalf("outcomes %+v", out)
	}
	if !errors.Is(out[1].Err, fs.ErrNotExist) || out[2].Err == nil || out[3].Err != nil {
		t.Errorf("outcomes %+v", out)
	}
	if len(m.moved) != 2 || m.moved[1] != "/d" {
		t.Errorf("moved %v; want /a and the cleaned /d", m.moved)
	}
	if bad := Failed(out); len(bad) != 2 {
		t.Errorf("Failed = %+v", bad)
	}
}

func TestMoveAllStopsOnCancel(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	m := &fakeMover{cancel: cancel}
	out, err := moveAll(ctx, m, []string{"/a", "/b", "/c"})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err %v", err)
	}
	if len(m.moved) != 1 || out[0].Err != nil || !errors.Is(out[1].Err, context.Canceled) || !errors.Is(out[2].Err, context.Canceled) {
		t.Errorf("moved %v, outcomes %+v", m.moved, out)
	}
}

func TestErrNoAccessIsPermission(t *testing.T) {
	if !errors.Is(ErrNoAccess, fs.ErrPermission) {
		t.Error("ErrNoAccess does not match fs.ErrPermission")
	}
}
