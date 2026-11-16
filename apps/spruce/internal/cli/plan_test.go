package cli

import (
	"slices"
	"testing"
	"time"

	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
)

// planOf is buildPlan over the fixture and its cards.
func planOf(sel Selection) Plan {
	res := fixture()
	recs := advice.Derive(res, advice.Inputs{Now: now, Home: home})
	return buildPlan(res, recs, sel, now)
}

func ids(rels ...string) []string {
	out := make([]string, len(rels))
	for i, r := range rels {
		out[i] = cleanup.ItemID(path(r))
	}
	return out
}

func TestBuildPlanDefault(t *testing.T) {
	p := planOf(Selection{})
	if want := []string{"system-junk:app-caches", "system-junk:incomplete-downloads", "dev-junk:artifact-node_modules", "installers:disk-images"}; !slices.Equal(p.Request.CategoryIDs, want) {
		t.Fatalf("categories %q, want %q", p.Request.CategoryIDs, want)
	}
	if want := ids("Library/Caches/com.old", "Library/Caches/com.new", "Downloads/x.crdownload", "code/web/node_modules", "Downloads/Tool.dmg"); !slices.Equal(p.Request.ItemIDs, want) {
		t.Fatalf("items %q, want %q", p.Request.ItemIDs, want)
	}
	if p.Items != 5 || p.Bytes != 1_350*mb || p.Trash != 350*mb {
		t.Fatalf("items %d, bytes %d, trash %d", p.Items, p.Bytes, p.Trash)
	}
	// The cards leave held items out already, so the default names none.
	if len(p.Held) != 0 || len(p.Missing) != 0 || p.Request.Mode != cleanup.ModeDefault {
		t.Fatalf("held %v, missing %v, mode %q", p.Held, p.Missing, p.Request.Mode)
	}
}

func TestBuildPlanCards(t *testing.T) {
	p := planOf(Selection{Cards: []string{"tool-pnpm", "installers", "tool-pnpm"}})
	if !slices.Equal(p.Request.CategoryIDs, []string{"dev-junk:tool-pnpm", "installers:disk-images"}) {
		t.Fatalf("categories %q", p.Request.CategoryIDs)
	}
	if p.Categories[0].Command != "pnpm store prune" || p.Trash != 200*mb {
		t.Fatalf("plan %+v", p)
	}
	p = planOf(Selection{Cards: []string{"logs", "app-caches", "logs"}})
	if !slices.Equal(p.Missing, []string{"logs"}) || !slices.Equal(p.Request.CategoryIDs, []string{"system-junk:app-caches"}) {
		t.Fatalf("missing %q, categories %q", p.Missing, p.Request.CategoryIDs)
	}
}

func TestBuildPlanCategories(t *testing.T) {
	// Every item of a named category, the unselected ones too, but never a held one.
	res := fixture()
	res.Categories[0].Items[1].Selected = false
	held := ids("code/api/node_modules")[0]
	p := buildPlan(res, nil, Selection{Categories: []string{"system-junk:app-caches", "dev-junk:artifact-node_modules", "system-junk:trash", "installers:archives"}}, now)
	if want := ids("Library/Caches/com.old", "Library/Caches/com.new", "code/web/node_modules"); !slices.Equal(p.Request.ItemIDs, want) {
		t.Fatalf("items %q, want %q", p.Request.ItemIDs, want)
	}
	if slices.Contains(p.Request.ItemIDs, held) {
		t.Fatal("a held item is in the request")
	}
	if len(p.Held) != 1 || p.Held[0].ID != held || p.Held[0].Hold != "项目有未提交的改动" {
		t.Fatalf("held %+v", p.Held)
	}
	// The empty Trash and a category this check did not find name nothing.
	if !slices.Equal(p.Missing, []string{"system-junk:trash", "installers:archives"}) {
		t.Fatalf("missing %q", p.Missing)
	}
	if p.Trash != 0 {
		t.Fatalf("trash %d", p.Trash)
	}
}

func TestBuildPlanHoldOnACard(t *testing.T) {
	// A card never holds an item with Hold; were one to slip in, the plan keeps it out all the same.
	res := fixture()
	recs := []advice.Recommendation{{ID: "projects-idle", Items: []advice.ItemRef{{CategoryID: "dev-junk:artifact-node_modules", ItemID: ids("code/api/node_modules")[0]}}}}
	p := buildPlan(res, recs, Selection{Cards: []string{"projects-idle"}}, now)
	if !p.Empty() || len(p.Request.ItemIDs) != 0 || len(p.Held) != 1 {
		t.Fatalf("plan %+v", p)
	}
}

func TestBuildPlanOlderThan(t *testing.T) {
	// app-caches: 40 and 2 days; the download 9; build output 120 (its project's activity); the installer 30.
	p := planOf(Selection{OlderThan: 30 * 24 * time.Hour})
	if want := ids("Library/Caches/com.old", "code/web/node_modules", "Downloads/Tool.dmg"); !slices.Equal(p.Request.ItemIDs, want) {
		t.Fatalf("items %q, want %q", p.Request.ItemIDs, want)
	}
	// Newest, when known, wins over ModTime.
	res := fixture()
	res.Categories[0].Items[0].Newest = now.Add(-time.Hour)
	p = buildPlan(res, nil, Selection{Categories: []string{"system-junk:app-caches"}, OlderThan: 24 * time.Hour}, now)
	if want := ids("Library/Caches/com.new"); !slices.Equal(p.Request.ItemIDs, want) {
		t.Fatalf("items %q, want %q", p.Request.ItemIDs, want)
	}
	// An item whose age is unknown is not old enough.
	res = fixture()
	for i := range res.Categories[0].Items {
		res.Categories[0].Items[i].ModTime = time.Time{}
	}
	p = buildPlan(res, nil, Selection{Categories: []string{"system-junk:app-caches"}, OlderThan: time.Hour}, now)
	if !p.Empty() {
		t.Fatalf("items %q, want none", p.Request.ItemIDs)
	}
}
