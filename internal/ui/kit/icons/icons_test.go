package icons

import "testing"

func TestEveryGlyphParses(t *testing.T) {
	for _, name := range Names("") {
		if SVG(name, 2) == nil {
			t.Errorf("%s did not parse", name)
		}
	}
	if len(Names("page/")) != 10 {
		t.Errorf("page glyphs: %v", Names("page/"))
	}
	if len(Names("category/")) != 22 {
		t.Errorf("category glyphs: %d", len(Names("category/")))
	}
	if len(Names("task/")) != 13 {
		t.Errorf("task glyphs: %d", len(Names("task/")))
	}
}

func TestUnknownNameFallsBack(t *testing.T) {
	if SVG("category/no-such-key", 2) != SVG(Fallback, 2) {
		t.Error("unknown key does not fall back to the folder")
	}
	if SVG("check", 0) != SVG("check", 2) {
		t.Error("stroke 0 is not the default 2")
	}
}
