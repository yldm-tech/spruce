package advice

import (
	"cmp"
	"slices"
	"strings"
	"time"
	"unicode"

	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/devjunk"
)

// timeToken is where a reason's time goes; ReasonWith fills it in.
const timeToken = "{time}"

// smallBytes is the size under which a card's categories fold into 其他小项: 100 MB.
const smallBytes = 100_000_000

// picked is one recommended item and its category.
type picked struct {
	cat  *cleanup.Category
	item cleanup.Item
}

// card is a Recommendation being built.
type card struct {
	rec   Recommendation
	tmpl  *template
	cats  []*cleanup.Category
	items []picked
	// dirtyProjects counts the projects with uncommitted changes in the scan's build output (projects-idle).
	dirtyProjects int
}

func (c *card) add(cat *cleanup.Category, items []picked) {
	c.cats = append(c.cats, cat)
	c.items = append(c.items, items...)
	c.rec.Categories = append(c.rec.Categories, cat.ID)
}

func (c *card) bytes() int64 {
	var n int64
	for _, p := range c.items {
		n += p.item.Size
	}
	return n
}

// command reports whether one of the card's categories is cleaned by a command.
func (c *card) command() bool {
	return slices.ContainsFunc(c.cats, func(cat *cleanup.Category) bool { return cat.Command != "" })
}

// largest is the card's largest item.
func (c *card) largest() cleanup.Item {
	var it cleanup.Item
	for _, p := range c.items {
		if p.item.Size > it.Size || it.ID == "" {
			it = p.item
		}
	}
	return it
}

// finish fills in what every card has, then lets the template word it.
func (c *card) finish(in Inputs) Recommendation {
	r := &c.rec
	r.ID, r.Rule = c.tmpl.id, c.tmpl.rule
	first := c.cats[0]
	r.Source = sourceOf(*first)
	r.Icon = first.Icon
	r.Items = make([]ItemRef, 0, len(c.items))
	for _, p := range c.items {
		r.Items = append(r.Items, ItemRef{CategoryID: p.cat.ID, ItemID: p.item.ID})
		r.Bytes += p.item.Size
		if t := latest(p.item); t.After(r.Newest) {
			r.Newest = t
		}
		if r.Command == "" {
			r.Command = p.cat.Command
		}
	}
	r.Count = len(c.items)
	r.Action = actionOf(c.items)
	r.Score = Score(r.Bytes, r.Action)
	c.tmpl.build(c, in)
	return *r
}

// actionOf is ActionOf over picked items.
func actionOf(items []picked) Action {
	var res cleanup.ScanResult
	var refs []ItemRef
	seen := map[string]bool{}
	for _, p := range items {
		if !seen[p.cat.ID] {
			seen[p.cat.ID] = true
			res.Categories = append(res.Categories, *p.cat)
		}
		refs = append(refs, ItemRef{CategoryID: p.cat.ID, ItemID: p.item.ID})
	}
	return ActionOf(res, refs)
}

// latest is the later of an item's Newest and ModTime.
func latest(it cleanup.Item) time.Time {
	if it.Newest.After(it.ModTime) {
		return it.Newest
	}
	return it.ModTime
}

// days is how many whole days lie between t and now.
func days(now, t time.Time) int {
	if t.IsZero() || !now.After(t) {
		return 0
	}
	return int(now.Sub(t) / (24 * time.Hour))
}

// sourceOf is the source of a merged category: Category.Source, else the prefix of its ID.
func sourceOf(c cleanup.Category) string {
	if c.Source != "" {
		return c.Source
	}
	src, _, _ := strings.Cut(c.ID, ":")
	return src
}

// Derive builds the cards from a merged scan: only Selected items without Hold, grouped by the template table, small categories folded into "small", sorted by Score then Title. Categories no template covers (the Trash itself, caches of running apps, recent installers) make no card, nor does a card of zero bytes. A card under 100 MB folds into 其他小项 unless it runs a command, whose confirmation must name it; 其他小项 always comes last. Pure.
func Derive(res cleanup.ScanResult, in Inputs) []Recommendation {
	cards := map[string]*card{}
	var order []*card
	dirty := map[string]bool{}
	for i := range res.Categories {
		cat := &res.Categories[i]
		if strings.HasPrefix(cat.ID, artifactPrefix) {
			for _, it := range cat.Items {
				if it.Git == cleanup.GitDirty {
					dirty[projectOf(it)] = true
				}
			}
		}
		if cat.Kind == cleanup.KindTrash {
			continue
		}
		id, tmpl := templateFor(cat.ID)
		if tmpl == nil {
			continue
		}
		var items []picked
		for _, it := range cat.Items {
			if it.Selected && it.Hold == "" {
				items = append(items, picked{cat: cat, item: it})
			}
		}
		if len(items) == 0 {
			continue
		}
		c := cards[id]
		if c == nil {
			c = &card{tmpl: tmpl}
			cards[id] = c
			order = append(order, c)
		}
		c.add(cat, items)
	}
	var recs []Recommendation
	rest := &card{tmpl: small}
	for _, c := range order {
		switch n := c.bytes(); {
		case n <= 0:
		case n < smallBytes && !c.command():
			for _, cat := range c.cats {
				var items []picked
				for _, p := range c.items {
					if p.cat == cat {
						items = append(items, p)
					}
				}
				rest.add(cat, items)
			}
		default:
			if c.tmpl == projectsIdle {
				c.dirtyProjects = len(dirty)
			}
			recs = append(recs, c.finish(in))
		}
	}
	slices.SortStableFunc(recs, func(a, b Recommendation) int {
		return cmp.Or(cmp.Compare(b.Score, a.Score), cmp.Compare(a.Title, b.Title))
	})
	if len(rest.cats) > 0 {
		recs = append(recs, rest.finish(in))
	}
	if recs == nil {
		recs = []Recommendation{}
	}
	return recs
}

// Headline is the sum of the cards' Bytes: what 概览 says can be freed safely.
func Headline(recs []Recommendation) int64 {
	var n int64
	for _, r := range recs {
		n += r.Bytes
	}
	return n
}

// Drill returns the categories of one source of a merged scan, in scan order: what its drill-down view lists. Pure.
func Drill(res cleanup.ScanResult, source string) []cleanup.Category {
	out := []cleanup.Category{}
	for _, c := range res.Categories {
		if sourceOf(c) == source {
			out = append(out, c)
		}
	}
	return out
}

// KnownID reports whether id can name a card: a fixed ID of the template table, or "tool-<id>" for an id of devjunk.ToolIDs(). The command line refuses other IDs before scanning. Pure.
func KnownID(id string) bool {
	switch id {
	case IDInstallers, IDIncompleteDownloads, IDAppCaches, IDBrowserCaches, IDLogs, IDTempFiles, IDProjectsIdle, IDSmall:
		return true
	}
	if tool, ok := strings.CutPrefix(id, "tool-"); ok {
		return slices.Contains(devjunk.ToolIDs(), tool)
	}
	return false
}

// ReasonWith returns Reason with "{time}" replaced by format(ReasonTime, ReasonTimeKind); the page and the command line pass a function over internal/ui/format, which the engine may not import. Pure.
func (r Recommendation) ReasonWith(format func(t time.Time, kind TimeKind) string) string {
	if !strings.Contains(r.Reason, timeToken) || format == nil {
		return r.Reason
	}
	return strings.ReplaceAll(r.Reason, timeToken, format(r.ReasonTime, r.ReasonTimeKind))
}

// aliases are other names installers of an app go by, by normalized app name, and the other way round.
var aliases = map[string][]string{
	"visualstudiocode": {"vscode"},
	"vscode":           {"visualstudiocode"},
	"zoomus":           {"zoom"},
	"intellijidea":     {"ideaiu", "ideaic"},
	"微信":               {"wechat"},
	"wechat":           {"微信"},
}

// MatchInstalled reports whether an installer file name names one of apps: the normalized (lower case, letters and digits only) app name, or one of its aliases (e.g. "visual studio code" ↔ "vscode"), is a prefix of the normalized file name without its extension, ending where a word of the file name ends (followed by a digit, a separator, an upper-case letter after a lower-case one, or nothing), so that "Go" does not name "GoogleChrome.dmg". Pure.
func MatchInstalled(fileName string, apps []string) bool {
	stem := strings.TrimSuffix(fileName, extOf(fileName))
	for _, app := range apps {
		name := normalize(app)
		if name == "" {
			continue
		}
		for _, n := range append([]string{name}, aliases[name]...) {
			if prefixWord(stem, n) {
				return true
			}
		}
	}
	return false
}

// extOf is the extension of an installer file name, a double one such as .tar.gz included.
func extOf(name string) string {
	i := strings.LastIndex(name, ".")
	if i <= 0 {
		return ""
	}
	switch strings.ToLower(name[i:]) {
	case ".gz", ".bz2", ".xz":
		if j := strings.LastIndex(name[:i], "."); j > 0 && strings.EqualFold(name[j:i], ".tar") {
			return name[j:]
		}
	}
	return name[i:]
}

// normalize keeps the letters and digits of s, in lower case.
func normalize(s string) string {
	var b strings.Builder
	for _, r := range s {
		if unicode.IsLetter(r) || unicode.IsDigit(r) {
			b.WriteRune(unicode.ToLower(r))
		}
	}
	return b.String()
}

// prefixWord reports whether the letters and digits of stem start with want (normalized) and the match ends at a word boundary of stem.
func prefixWord(stem, want string) bool {
	rs, ws := []rune(stem), []rune(want)
	i, j := 0, 0
	for ; i < len(rs) && j < len(ws); i++ {
		r := rs[i]
		if !unicode.IsLetter(r) && !unicode.IsDigit(r) {
			continue
		}
		if unicode.ToLower(r) != ws[j] {
			return false
		}
		j++
	}
	if j < len(ws) {
		return false
	}
	if i == len(rs) {
		return true
	}
	next, prev := rs[i], rs[i-1]
	return !unicode.IsLetter(next) || (unicode.IsUpper(next) && unicode.IsLower(prev))
}
