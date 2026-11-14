package rules

import (
	"cmp"
	"path/filepath"
	"slices"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/advice"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

const (
	day  = 24 * time.Hour
	week = 7 * day
)

// MaxItems is the most items one rule takes in one run, largest first.
const MaxItems = 2000

// prefixMark ends an entry of Spec.Categories that names every merged category ID starting with what comes before it.
const prefixMark = "*"

// catalog is every rule, in the order the settings list them.
var catalog = []Spec{
	{
		ID: AppCaches, Title: "旧的应用缓存",
		Categories: []string{advice.SourceSystemJunk + ":app-caches"},
		MinAge:     30 * day, Every: week,
	},
	{
		ID: Installers, Title: "旧安装包",
		Categories: []string{advice.SourceInstallers + ":disk-images", advice.SourceInstallers + ":installer-packages"},
		MinAge:     7 * day, Every: day, InDownloads: true, NeedInstalled: true,
	},
	{
		ID: BuildOutput, Title: "闲置项目的构建产物",
		Categories: []string{advice.SourceDevJunk + ":artifact-" + prefixMark},
		MinAge:     90 * day, Every: week, NeedGit: true,
	},
	{
		ID: IncompleteDownloads, Title: "未完成的下载",
		Categories: []string{advice.SourceSystemJunk + ":incomplete-downloads"},
		MinAge:     7 * day, Every: day,
	},
}

// Catalog returns every rule, in the order the settings list them: 旧的应用缓存 (system-junk:app-caches, 30 days, weekly), 旧安装包 (installers:disk-images and installers:installer-packages in Downloads matching an installed app, 7 days, daily), 闲置项目的构建产物 (every dev-junk:artifact-* category, the Trash variants included, 90 days, weekly, clean or no repository only) and 未完成的下载 (system-junk:incomplete-downloads, 7 days, daily). An entry of Categories ending in "*" is a prefix.
func Catalog() []Spec {
	out := make([]Spec, len(catalog))
	for i, s := range catalog {
		s.Categories = slices.Clone(s.Categories)
		out[i] = s
	}
	return out
}

// Lookup returns the rule id names.
func Lookup(id ID) (Spec, bool) {
	for _, s := range Catalog() {
		if s.ID == id {
			return s, true
		}
	}
	return Spec{}, false
}

// Enabled returns the rules a turns on, in Catalog order.
func Enabled(a settings.AutomationSettings) []ID {
	on := map[ID]bool{AppCaches: a.AppCaches, Installers: a.Installers, BuildOutput: a.BuildOutput, IncompleteDownloads: a.IncompleteDownloads}
	var out []ID
	for _, s := range catalog {
		if on[s.ID] {
			out = append(out, s.ID)
		}
	}
	return out
}

// Due returns the rules of enabled that are due at now, in Catalog order: never run, or last run (runs, by ID) at least Every before now. A last run that lies in the future (the clock was set back) is not trusted, so the rule is due: what it takes still has to be MinAge old. Times compare as wall clocks. Pure.
func Due(enabled []ID, runs map[string]time.Time, now time.Time) []Spec {
	now = now.Round(0)
	var out []Spec
	for _, s := range Catalog() {
		if !slices.Contains(enabled, s.ID) {
			continue
		}
		last := runs[string(s.ID)].Round(0)
		if last.IsZero() || last.After(now) || now.Sub(last) >= s.Every {
			out = append(out, s)
		}
	}
	return out
}

// Age is how old an item counts as for MinAge: for build output (BuildOutput) the item's ModTime, which devjunk sets to the project's last activity (it never sets Newest on build output); for everything else the later of Newest and ModTime, so a recent sign of either kind keeps the item. An item without a time, or with one after now, is 0 old. Pure.
func Age(spec Spec, it cleanup.Item, now time.Time) time.Duration {
	t := it.ModTime
	if spec.ID != BuildOutput && it.Newest.After(t) {
		t = it.Newest
	}
	if t.IsZero() {
		return 0
	}
	return max(now.Round(0).Sub(t.Round(0)), 0)
}

// Plan picks each due rule's items from a fresh merged scan. An item qualifies only when its category matches the rule, is not a command category and not KindTrash, has no Warning (a scanner sets one when it could not tell running apps or attached images apart), the item is Selected (the scanners' own judgement on running apps, attached images and recent files stands), has no Hold, is not in in.Restored, Age(spec, it, now) ≥ MinAge, for NeedGit has Git GitClean or GitNone, for InDownloads lies inside in.Downloads, and for NeedInstalled names an installed app (and in.InstalledApps was read at all). Each rule takes at most MaxItems items, largest first, every one named in a ModeTrashOnly request; a rule with nothing to take is left out. Pure.
func Plan(res cleanup.ScanResult, due []Spec, in Inputs) []Planned {
	var out []Planned
	for _, spec := range due {
		if spec.NeedInstalled && in.InstalledApps == nil {
			continue
		}
		if spec.InDownloads && in.Downloads == "" {
			continue
		}
		type pick struct {
			cat  int
			item cleanup.Item
		}
		var picks []pick
		for ci, c := range res.Categories {
			if !matches(spec, c.ID) || c.Command != "" || c.Kind == cleanup.KindTrash || c.Warning != "" {
				continue
			}
			for _, it := range c.Items {
				if qualifies(spec, it, in) {
					picks = append(picks, pick{cat: ci, item: it})
				}
			}
		}
		if len(picks) == 0 {
			continue
		}
		slices.SortStableFunc(picks, func(a, b pick) int { return cmp.Compare(b.item.Size, a.item.Size) })
		if len(picks) > MaxItems {
			picks = picks[:MaxItems]
		}
		p := Planned{Rule: spec.ID, Req: cleanup.CleanRequest{Mode: cleanup.ModeTrashOnly}}
		used := map[int]bool{}
		for _, pk := range picks {
			used[pk.cat] = true
			p.Req.ItemIDs = append(p.Req.ItemIDs, pk.item.ID)
			p.Items++
			p.Bytes += pk.item.Size
		}
		// Categories in scan order.
		for ci, c := range res.Categories {
			if used[ci] {
				p.Req.CategoryIDs = append(p.Req.CategoryIDs, c.ID)
			}
		}
		out = append(out, p)
	}
	return out
}

// matches reports whether the merged category ID id is one of spec's categories.
func matches(spec Spec, id string) bool {
	for _, c := range spec.Categories {
		if prefix, ok := strings.CutSuffix(c, prefixMark); ok {
			if strings.HasPrefix(id, prefix) {
				return true
			}
		} else if id == c {
			return true
		}
	}
	return false
}

// qualifies is Plan's test of one item of a matching category.
func qualifies(spec Spec, it cleanup.Item, in Inputs) bool {
	if !it.Selected || it.Hold != "" || it.ID == "" {
		return false
	}
	if _, put := in.Restored[it.ID]; put {
		return false
	}
	if Age(spec, it, in.Now) < spec.MinAge {
		return false
	}
	if spec.NeedGit && it.Git != cleanup.GitClean && it.Git != cleanup.GitNone {
		return false
	}
	if spec.InDownloads && !inside(it.Path, in.Downloads) {
		return false
	}
	if spec.NeedInstalled && !advice.MatchInstalled(filepath.Base(it.Path), in.InstalledApps) {
		return false
	}
	return true
}

// inside reports whether path lies strictly inside dir, ignoring case as both platforms' default file systems do.
func inside(path, dir string) bool {
	if path == "" || dir == "" {
		return false
	}
	p, d := strings.ToLower(filepath.Clean(path)), strings.ToLower(filepath.Clean(dir))
	return strings.HasPrefix(p, strings.TrimSuffix(d, string(filepath.Separator))+string(filepath.Separator))
}

// Summarize adds up the KindAuto runs that started in [from, to), by rule, less what KindRestore runs (at any time) put back of them. ByRule lists the rules that still count anything, most bytes first. Pure.
func Summarize(runs []history.Run, from, to time.Time) Summary {
	from, to = from.Round(0), to.Round(0)
	var sum Summary
	totals := map[ID]*RuleTotal{}
	counted := map[string]ID{}
	for _, r := range runs {
		started := r.StartedAt.Round(0)
		if r.Kind != history.KindAuto || started.Before(from) || !started.Before(to) {
			continue
		}
		sum.Runs++
		counted[r.ID] = ID(r.Rule)
		t := totals[ID(r.Rule)]
		if t == nil {
			t = &RuleTotal{Rule: ID(r.Rule)}
			totals[t.Rule] = t
		}
		if r.Summary != nil {
			t.Items += r.Summary.Items
			t.Bytes += r.Summary.Trashed
		}
	}
	for _, r := range runs {
		rule, ok := counted[r.RestoreOf]
		if r.Kind != history.KindRestore || !ok || r.Summary == nil {
			continue
		}
		t := totals[rule]
		t.Items = max(t.Items-r.Summary.Items, 0)
		t.Bytes = max(t.Bytes-r.Summary.Trashed, 0)
	}
	for _, t := range totals {
		if t.Items == 0 && t.Bytes == 0 {
			continue
		}
		sum.Items += t.Items
		sum.Trashed += t.Bytes
		sum.ByRule = append(sum.ByRule, *t)
	}
	slices.SortFunc(sum.ByRule, func(a, b RuleTotal) int {
		if c := cmp.Compare(b.Bytes, a.Bytes); c != 0 {
			return c
		}
		return cmp.Compare(a.Rule, b.Rule)
	})
	return sum
}
