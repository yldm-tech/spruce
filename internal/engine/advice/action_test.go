package advice

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/cleanup"
)

func scanOf() cleanup.ScanResult {
	item := func(p string) cleanup.Item {
		return cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: 10, Selected: true}
	}
	return cleanup.NewResult([]cleanup.Category{
		{ID: "system-junk:app-caches", Deletion: cleanup.DeletePermanent, Items: []cleanup.Item{item("/u/Library/Caches/a"), item("/u/Library/Caches/b")}},
		{ID: "installers:disk-images", Deletion: cleanup.DeleteTrash, Items: []cleanup.Item{item("/u/Downloads/x.dmg")}},
		{ID: "dev-junk:tool-npm", Deletion: cleanup.DeletePermanent, Command: "npm cache clean --force", Items: []cleanup.Item{item("/u/.npm")}},
		{ID: "dev-junk:tool-pnpm", Deletion: cleanup.DeletePermanent, Command: "pnpm store prune", Items: []cleanup.Item{item("/u/pnpm")}},
	}, time.Now())
}

func ref(cat, path string) ItemRef { return ItemRef{CategoryID: cat, ItemID: cleanup.ItemID(path)} }

func TestActionOf(t *testing.T) {
	res := scanOf()
	cache, dmg := ref("system-junk:app-caches", "/u/Library/Caches/a"), ref("installers:disk-images", "/u/Downloads/x.dmg")
	npm, pnpm := ref("dev-junk:tool-npm", "/u/.npm"), ref("dev-junk:tool-pnpm", "/u/pnpm")
	cases := []struct {
		name string
		refs []ItemRef
		want Action
	}{
		{"trash", []ItemRef{dmg}, ActionTrash},
		{"delete", []ItemRef{cache, ref("system-junk:app-caches", "/u/Library/Caches/b")}, ActionDelete},
		{"command", []ItemRef{npm, pnpm}, ActionCommand},
		{"trash and delete", []ItemRef{dmg, cache}, ActionMixed},
		{"trash and command", []ItemRef{dmg, npm}, ActionMixed},
		{"delete and command", []ItemRef{cache, npm}, ActionMixed},
		{"unknown refs left out", []ItemRef{dmg, {CategoryID: "gone", ItemID: "x"}, {CategoryID: "system-junk:app-caches", ItemID: "x"}}, ActionTrash},
		{"nothing", nil, ""},
	}
	for _, c := range cases {
		if got := ActionOf(res, c.refs); got != c.want {
			t.Errorf("%s: %q, want %q", c.name, got, c.want)
		}
	}
}

func TestScore(t *testing.T) {
	for a, want := range map[Action]float64{ActionTrash: 1000, ActionDelete: 900, ActionMixed: 850, ActionCommand: 750, "": 0} {
		if got := Score(1000, a); got != want {
			t.Errorf("Score(1000, %q) = %v, want %v", a, got, want)
		}
	}
	// A trash card outranks a command card of the same size, and a much larger command card outranks a small trash one.
	if Score(100, ActionTrash) <= Score(100, ActionCommand) || Score(1000, ActionCommand) <= Score(100, ActionTrash) {
		t.Error("ordering")
	}
}

func TestCovering(t *testing.T) {
	res := scanOf()
	recs := []Recommendation{
		{ID: "installers", Items: []ItemRef{ref("installers:disk-images", "/u/Downloads/x.dmg")}},
		{ID: "app-caches", Items: []ItemRef{ref("system-junk:app-caches", "/u/Library/Caches/a")}},
		{ID: "also-caches", Items: []ItemRef{ref("system-junk:app-caches", "/u/Library/Caches/a")}},
		{ID: "stale", Items: []ItemRef{{CategoryID: "gone", ItemID: "x"}}},
	}
	cases := map[string]string{
		"/u/Library/Caches":          "app-caches", // a card's item lies inside the location
		"/u/Library/Caches/a":        "app-caches", // the item itself, first card wins
		"/u/Library/Caches/a/sub":    "app-caches", // the location lies inside an item
		"/u/Downloads/":              "installers",
		"/u/Library/Caches/b":        "", // in the scan but on no card
		"/u/Library/CachesElsewhere": "",
		"/u/Documents":               "",
		"":                           "",
	}
	for p, want := range cases {
		if got := Covering(recs, res, filepath.FromSlash(p)); got != want {
			t.Errorf("Covering(%q) = %q, want %q", p, got, want)
		}
	}
}
