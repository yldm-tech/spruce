package growth

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

// write makes a file of size bytes of data (not sparse, so it takes its blocks) at path, and its folders.
func write(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(strings.Repeat("x", size)), 0o600); err != nil {
		t.Fatal(err)
	}
}

func findLoc(s Snapshot, id string) (Location, bool) {
	for _, l := range s.Locations {
		if l.ID == id {
			return l, true
		}
	}
	return Location{}, false
}

func TestTakeMeasuresLocationsAndChildren(t *testing.T) {
	root := t.TempDir()
	caches := filepath.Join(root, "Caches")
	write(t, filepath.Join(caches, "big", "blob"), ChildMin+1<<20)
	write(t, filepath.Join(caches, "small", "a"), 4096)
	write(t, filepath.Join(caches, "loose.bin"), 8192)
	// A file as large as a child counts in Rest and never lists: 空间透视 only scans folders.
	write(t, filepath.Join(caches, "film.mov"), ChildMin+1<<20)
	// A symlink is not followed, neither as a child nor as a location.
	if err := os.Symlink(filepath.Join(caches, "big"), filepath.Join(caches, "link")); err != nil {
		t.Fatal(err)
	}
	music := filepath.Join(root, "Music")
	write(t, filepath.Join(music, "song"), 16384)
	if err := os.Symlink(music, filepath.Join(root, "MusicLink")); err != nil {
		t.Fatal(err)
	}
	var usage []string
	snap, err := Take(context.Background(), []Watch{
		{ID: "library-caches", Path: caches, Children: true},
		{ID: "music", Path: music},
		{ID: "missing", Path: filepath.Join(root, "nope")},
		{ID: "link", Path: filepath.Join(root, "MusicLink")},
	}, TakeOptions{Usage: func(p string) (int64, int64, error) { usage = append(usage, p); return 500 * gb, 40 * gb, nil }})
	if err != nil {
		t.Fatal(err)
	}
	if snap.Version != Version || snap.At.IsZero() || snap.Total != 500*gb || snap.Free != 40*gb || len(usage) != 1 || usage[0] != caches {
		t.Errorf("snapshot %+v, usage %v", snap, usage)
	}
	if len(snap.Locations) != 2 {
		t.Fatalf("locations %+v", snap.Locations)
	}
	c, _ := findLoc(snap, "library-caches")
	if len(c.Children) != 1 || c.Children[0].Name != "big" || c.Children[0].Bytes < ChildMin {
		t.Fatalf("children %+v", c.Children)
	}
	if c.Bytes <= c.Children[0].Bytes || c.Rest != c.Bytes-c.Children[0].Bytes || c.Rest < 4096+8192+ChildMin+1<<20 || c.Skipped || c.Denied != 0 {
		t.Errorf("caches %+v", c)
	}
	m, _ := findLoc(snap, "music")
	if m.Bytes < 16384 || m.Children != nil {
		t.Errorf("music %+v", m)
	}
}

func TestTakeHonoursTheReadGate(t *testing.T) {
	root := t.TempDir()
	downloads := filepath.Join(root, "Downloads")
	write(t, filepath.Join(downloads, "x.dmg"), 4096)
	code := filepath.Join(root, "code")
	write(t, filepath.Join(code, "app", "main.go"), 100)
	write(t, filepath.Join(code, "Private", "secret"), 100)
	containers := filepath.Join(root, "Containers")
	write(t, filepath.Join(containers, "com.example", "data"), 100)

	var asked []string
	gate := func(p string) bool {
		asked = append(asked, p)
		return p != downloads && p != filepath.Join(code, "Private")
	}
	watches := []Watch{
		{ID: "downloads", Path: downloads, Children: true},
		{ID: "dev-root:0", Path: code, Children: true},
		{ID: "library-containers", Path: containers, Children: true},
	}
	snap, err := Take(context.Background(), watches, TakeOptions{MayRead: gate})
	if err != nil {
		t.Fatal(err)
	}
	for _, id := range []string{"downloads", "dev-root:0", "library-containers"} {
		l, ok := findLoc(snap, id)
		if !ok || !l.Skipped || l.Bytes != 0 || l.Children != nil {
			t.Errorf("%s: %+v", id, l)
		}
	}
	for _, p := range asked {
		if strings.HasPrefix(p, containers) {
			t.Errorf("the gate was asked about %s without Full Disk Access", p)
		}
	}

	// With Full Disk Access the app data is read (through the gate as well).
	snap, err = Take(context.Background(), watches[2:], TakeOptions{MayRead: func(string) bool { return true }, FullDiskAccess: func() bool { return true }})
	if err != nil {
		t.Fatal(err)
	}
	if l, _ := findLoc(snap, "library-containers"); l.Skipped || l.Bytes == 0 {
		t.Errorf("containers with Full Disk Access: %+v", l)
	}
	if snap, _ := Take(context.Background(), watches[2:], TakeOptions{FullDiskAccess: func() bool { return false }}); !snap.Locations[0].Skipped {
		t.Errorf("containers without Full Disk Access: %+v", snap.Locations)
	}
}

func TestTakeCountsWhatItCannotRead(t *testing.T) {
	if os.Geteuid() == 0 {
		t.Skip("root reads everything")
	}
	root := t.TempDir()
	locked := filepath.Join(root, "Trash")
	write(t, filepath.Join(locked, "x"), 100)
	if err := os.Chmod(locked, 0); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(locked, 0o755) })
	snap, err := Take(context.Background(), []Watch{{ID: "trash", Path: locked}, {ID: "documents", Path: locked, Children: true}}, TakeOptions{})
	if err != nil {
		t.Fatal(err)
	}
	for _, l := range snap.Locations {
		if l.Bytes != 0 || l.Denied == 0 || l.Skipped {
			t.Errorf("%s: %+v", l.ID, l)
		}
	}
}

func TestTakePastItsBudgetSavesNothing(t *testing.T) {
	root := t.TempDir()
	write(t, filepath.Join(root, "a", "b"), 100)
	snap, err := Take(context.Background(), []Watch{{ID: "a", Path: filepath.Join(root, "a"), Children: true}}, TakeOptions{Budget: time.Nanosecond})
	if !errors.Is(err, ErrBudget) || len(snap.Locations) != 0 {
		t.Errorf("snapshot %+v, err %v", snap, err)
	}

	ctx, cancel := context.WithCancel(context.Background())
	cancel()
	if _, err := Take(ctx, []Watch{{ID: "a", Path: filepath.Join(root, "a")}}, TakeOptions{}); !errors.Is(err, context.Canceled) {
		t.Errorf("cancelled: %v", err)
	}
}
