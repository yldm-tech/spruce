package growth

import (
	"cmp"
	"path/filepath"
	"slices"
	"time"
)

// MaxAge is the oldest a snapshot is kept.
const MaxAge = 90 * 24 * time.Hour

// What Diff counts as growth: at least MinGrowth for a location and MinChildGrowth for a child, and at least MinShare of what it was.
const (
	MinGrowth      = 1 << 30
	MinChildGrowth = 500 << 20
	MinShare       = 0.10
)

// DefaultRetention keeps one snapshot a day for 14 days, one a week for 12 weeks, at most 40 files and 8 MB.
func DefaultRetention() Retention {
	return Retention{Daily: 14, Weekly: 12, MaxFiles: 40, MaxBytes: 8 << 20}
}

// Keep returns the times of times to keep at now, oldest first: the newest of each of the last r.Daily days and of each of the last r.Weekly ISO weeks (days and weeks in now's time zone), and the newest of all, none older than MaxAge and at most r.MaxFiles, the newest ones. Pure.
func Keep(times []time.Time, now time.Time, r Retention) []time.Time {
	sorted := slices.Clone(times)
	slices.SortFunc(sorted, func(a, b time.Time) int { return b.Compare(a) })
	sorted = slices.Compact(sorted)
	loc := now.Location()
	today := day(now, loc)
	nowYear, nowWeek := now.ISOWeek()
	days := map[time.Time]bool{}
	weeks := map[[2]int]bool{}
	var keep []time.Time
	for i, t := range sorted {
		if now.Sub(t) > MaxAge {
			continue
		}
		keepIt := i == 0
		d := day(t, loc)
		if age := int(today.Sub(d).Hours()/24 + 0.5); age < r.Daily && !days[d] {
			days[d] = true
			keepIt = true
		}
		y, w := t.In(loc).ISOWeek()
		if weeksBetween(y, w, nowYear, nowWeek) < r.Weekly && !weeks[[2]int{y, w}] {
			weeks[[2]int{y, w}] = true
			keepIt = true
		}
		if keepIt {
			keep = append(keep, t)
		}
	}
	if r.MaxFiles > 0 && len(keep) > r.MaxFiles {
		keep = keep[:r.MaxFiles]
	}
	slices.Reverse(keep)
	return keep
}

// day is the midnight starting t's day in loc.
func day(t time.Time, loc *time.Location) time.Time {
	y, m, d := t.In(loc).Date()
	return time.Date(y, m, d, 0, 0, 0, 0, loc)
}

// weeksBetween counts the ISO weeks from (y1, w1) to (y2, w2) through their Mondays.
func weeksBetween(y1, w1, y2, w2 int) int {
	return int(isoMonday(y2, w2).Sub(isoMonday(y1, w1)).Hours()/(24*7) + 0.5)
}

// isoMonday is the Monday of ISO week w of year y, in UTC.
func isoMonday(y, w int) time.Time {
	jan4 := time.Date(y, 1, 4, 0, 0, 0, 0, time.UTC)
	offset := (int(jan4.Weekday()) + 6) % 7
	return jan4.AddDate(0, 0, -offset+7*(w-1))
}

// Baseline is the snapshot to compare with for a window of ago: the newest of times at or before now-ago, else the oldest; false when times is empty. Pure.
func Baseline(times []time.Time, now time.Time, ago time.Duration) (time.Time, bool) {
	if len(times) == 0 {
		return time.Time{}, false
	}
	limit := now.Add(-ago)
	var best, oldest time.Time
	for _, t := range times {
		if oldest.IsZero() || t.Before(oldest) {
			oldest = t
		}
		if !t.After(limit) && (best.IsZero() || t.After(best)) {
			best = t
		}
	}
	if best.IsZero() {
		return oldest, true
	}
	return best, true
}

// Diff lists what grew from old to cur, most first, at most n (n ≤ 0 for all). A location counts from MinGrowth and a child from MinChildGrowth, both only when they grew by MinShare of what they were. A location that grew lists as the child that accounts for most of its growth, when one does; a location that did not grow enough lists its children that did. A row inside another row that accounts for most of the other's growth replaces it, and a path lists once. Locations skipped or not read in either snapshot, locations read more fully now than before (fewer denied entries) and locations new in cur are left out, so nothing appears to grow because Spruce could read more. Pure.
func Diff(old, cur Snapshot, n int) []Growth {
	before := map[string]Location{}
	for _, l := range old.Locations {
		before[l.ID] = l
	}
	var rows []Growth
	for _, c := range cur.Locations {
		o, ok := before[c.ID]
		if !ok || o.Path != c.Path || o.Skipped || c.Skipped || unread(o) || unread(c) || c.Denied < o.Denied {
			continue
		}
		rows = append(rows, locationRows(o, c, old.At)...)
	}
	rows = nested(rows)
	if n > 0 && len(rows) > n {
		rows = rows[:n]
	}
	return rows
}

// unread reports whether a location's folder itself could not be read: nothing measured, something denied.
func unread(l Location) bool { return l.Bytes == 0 && l.Denied > 0 }

// grew reports whether before → after is growth of at least least and MinShare.
func grew(before, after, least int64) bool {
	d := after - before
	return d >= least && float64(d) >= MinShare*float64(before)
}

// locationRows are the rows location o → c gives.
func locationRows(o, c Location, since time.Time) []Growth {
	var kids []Growth
	if len(c.Children) > 0 {
		prev := map[string]int64{}
		for _, e := range o.Children {
			prev[e.Name] = e.Bytes
		}
		// A child the older snapshot did not list was below ChildMin, or below its smallest child when it listed MaxChildren.
		bound := int64(ChildMin)
		if len(o.Children) >= MaxChildren {
			bound = max(bound, o.Children[len(o.Children)-1].Bytes)
		}
		for _, e := range c.Children {
			b, listed := prev[e.Name]
			if !listed {
				b = min(bound, e.Bytes)
			}
			if !grew(b, e.Bytes, MinChildGrowth) {
				continue
			}
			label, icon := describe(c.ID, e.Name)
			kids = append(kids, Growth{Path: filepath.Join(c.Path, e.Name), Label: label, Icon: icon, Before: b, After: e.Bytes, Since: since, AtLeast: !listed})
		}
	}
	if !grew(o.Bytes, c.Bytes, MinGrowth) {
		return kids
	}
	d := c.Bytes - o.Bytes
	for _, k := range kids {
		if 2*(k.After-k.Before) > d {
			return []Growth{k}
		}
	}
	label, icon := describe(c.ID, "")
	return []Growth{{Path: c.Path, Label: label, Icon: icon, Before: o.Bytes, After: c.Bytes, Since: since}}
}

// unnamed is 1 for a row without a label, else 0.
func unnamed(g Growth) int {
	if g.Label == "" {
		return 1
	}
	return 0
}

// nested sorts rows by growth and drops a row when another inside it accounts for most of its growth, and a path already listed.
func nested(rows []Growth) []Growth {
	slices.SortStableFunc(rows, func(a, b Growth) int {
		if c := cmp.Compare(b.After-b.Before, a.After-a.Before); c != 0 {
			return c
		}
		if c := cmp.Compare(a.Path, b.Path); c != 0 {
			return c
		}
		// Of two rows of one path, the named one lists.
		return cmp.Compare(unnamed(a), unnamed(b))
	})
	var out []Growth
	seen := map[string]bool{}
	for i, r := range rows {
		if seen[r.Path] {
			continue
		}
		replaced := false
		for j, inner := range rows {
			if j != i && inner.Path != r.Path && within(inner.Path, r.Path) && 2*(inner.After-inner.Before) > r.After-r.Before {
				replaced = true
				break
			}
		}
		if replaced {
			continue
		}
		seen[r.Path] = true
		out = append(out, r)
	}
	return out
}
