package systemjunk

import (
	"path/filepath"
	"slices"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

// TestCleanTrashOnlyNeverEmptiesTheTrash: an automatic clean that names the Trash gets one failure for it, the Trash is not emptied, and the caches it names go to the Trash instead of being deleted.
func TestCleanTrashOnlyNeverEmptiesTheTrash(t *testing.T) {
	fx, c, res := cleanFixture(t)
	out, err := c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catAppCaches, catTrash}, Mode: cleanup.ModeTrashOnly}, nil)
	if err != nil {
		t.Fatal(err)
	}
	log := fx.rec.entries()
	if slices.Contains(log, "empty") || slices.ContainsFunc(log, func(e string) bool { return len(e) > 7 && e[:7] == "remove:" }) {
		t.Fatalf("trash-only emptied or deleted: %v", log)
	}
	caches := category(t, res, catAppCaches)
	if len(out.Moved) != len(caches.Items) || out.Removed != len(caches.Items) {
		t.Errorf("moved %+v, want every app cache", out.Moved)
	}
	if len(out.Failed) != 1 || out.Failed[0].CategoryID != catTrash || out.Failed[0].Reason != cleanup.ReasonNoEmptyTrash() || !out.Partial {
		t.Errorf("failures %+v", out.Failed)
	}
	if last := out.Categories[len(out.Categories)-1]; last.CategoryID != catTrash || last.Failed != 1 {
		t.Errorf("categories %+v", out.Categories)
	}
	// The Trash alone in trash-only is refused without emptying.
	out, err = c.Clean(t.Context(), cleanup.CleanRequest{CategoryIDs: []string{catTrash}, Mode: cleanup.ModeTrashOnly}, nil)
	if err != nil || len(out.Failed) != 1 || slices.Contains(fx.rec.entries(), "empty") {
		t.Errorf("trash alone: %+v, %v, log %v", out, err, fx.rec.entries())
	}
}

// TestScanSetsNewestOnFolders: a folder item carries the latest time inside it, a file item none.
func TestScanSetsNewestOnFolders(t *testing.T) {
	_, _, res := cleanFixture(t)
	app := category(t, res, catAppCaches)
	var folder *cleanup.Item
	for i := range app.Items {
		if filepath.Base(app.Items[i].Path) == "com.example.app" {
			folder = &app.Items[i]
		}
	}
	// The folder was made now and its file dated 3 days back: Newest is the latest of the two.
	if folder == nil || folder.Newest.IsZero() || time.Since(folder.Newest) > time.Hour {
		t.Errorf("folder item %+v, want Newest set to the folder's own recent time", folder)
	}
	for _, it := range category(t, res, catLogs).Items {
		if !it.Dir && !it.Newest.IsZero() {
			t.Errorf("file item %s has Newest %v", it.Path, it.Newest)
		}
	}
}
