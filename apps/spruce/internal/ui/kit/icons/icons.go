// Package icons holds the app's line icons: 24-unit SVGs with round caps, stroked in the text color (currentColor), as the React app's Glyph drew them. Names are page/<route>, category/<key> for the cleaner lists' Category.Icon keys, task/<icon> for the 维护 cards' maintenance.Icon, and the shared glyphs (check, alert, reveal, trash, ...).
package icons

import (
	"fmt"
	"slices"
	"strings"
	"sync"

	"github.com/nsgo-dev/nsgo/core"
)

// glyph is one icon: its shapes, and whether its corners join round (the 维护 cards' first six glyphs join square, as the design draws them).
type glyph struct {
	round bool
	paths string
}

// Fallback is the glyph a name without one draws: a category key the design does not know shows a folder.
const Fallback = "category/folder"

type key struct {
	name   string
	stroke float32
}

var (
	mu    sync.Mutex
	cache = map[key]*core.SVG{}
)

// SVG returns the parsed icon name stroked stroke units wide on the 24-unit grid (2 when 0), parsing it once. An unknown name gives Fallback's.
func SVG(name string, stroke float32) *core.SVG {
	if stroke <= 0 {
		stroke = 2
	}
	g, ok := glyphs[name]
	if !ok {
		name, g = Fallback, glyphs[Fallback]
	}
	k := key{name, stroke}
	mu.Lock()
	defer mu.Unlock()
	if svg, ok := cache[k]; ok {
		return svg
	}
	join := "miter"
	if g.round {
		join = "round"
	}
	doc := fmt.Sprintf(`<svg xmlns="http://www.w3.org/2000/svg" viewBox="0 0 24 24" fill="none" stroke="currentColor" stroke-width="%g" stroke-linecap="round" stroke-linejoin="%s">%s</svg>`, stroke, join, g.paths)
	svg := core.MustParseSVG([]byte(doc))
	cache[k] = svg
	return svg
}

// Has reports whether name has a glyph of its own.
func Has(name string) bool {
	_, ok := glyphs[name]
	return ok
}

// Names lists the glyphs whose names start with prefix ("category/", "task/", "page/" or "" for all), sorted.
func Names(prefix string) []string {
	var out []string
	for n := range glyphs {
		if strings.HasPrefix(n, prefix) {
			out = append(out, n)
		}
	}
	slices.Sort(out)
	return out
}
