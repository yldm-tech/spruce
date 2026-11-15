package space

import (
	"math"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/growth"
	"github.com/yldm-tech/spruce/internal/ui/format"
	"github.com/yldm-tech/spruce/internal/ui/store"
)

// EntryLabel is what the page calls an entry: its name, or 「其他 N 项」 and 「N 个小文件」 for the groups the scan folded.
func EntryLabel(e app.SpaceEntry) string {
	switch e.Kind {
	case app.SpaceOther:
		return "其他 " + strconv.FormatInt(e.Count, 10) + " 项"
	case app.SpaceFiles:
		return strconv.FormatInt(e.Count, 10) + " 个小文件"
	}
	return e.Name
}

// IsGroup reports whether an entry stands for several folded entries, which can be neither opened nor moved.
func IsGroup(e app.SpaceEntry) bool {
	return e.Kind == app.SpaceOther || e.Kind == app.SpaceFiles
}

// ShareOf is part's share of whole, 0 to 1.
func ShareOf(part, whole int64) float64 {
	if whole <= 0 {
		return 0
	}
	return min(1, max(0, float64(part)/float64(whole)))
}

// jsRound rounds as JavaScript's Math.round does: halves go up.
func jsRound(x float64) float64 { return math.Floor(x + 0.5) }

// FormatShare renders a share as a percent, e.g. "42%", "0.4%" or "<0.1%".
func FormatShare(share float64) string {
	percent := share * 100
	switch {
	case percent == 0:
		return "0%"
	case percent < 0.1:
		return "<0.1%"
	case percent < 10:
		return strings.TrimSuffix(strconv.FormatFloat(percent, 'f', 1, 64), ".0") + "%"
	}
	return strconv.FormatFloat(jsRound(percent), 'f', 0, 64) + "%"
}

// VolumeUsage describes a disk's capacity: what is used of the total, its share, and the line under its name.
type VolumeUsage struct {
	Used  int64
	Share float64
	Text  string
}

// UsageOf returns a disk's VolumeUsage.
func UsageOf(v app.SpaceVolume) VolumeUsage {
	used := max(0, v.Total-v.Free)
	return VolumeUsage{Used: used, Share: ShareOf(used, v.Total),
		Text: "可用 " + format.Bytes(v.Free) + "，共 " + format.Bytes(v.Total)}
}

// UsedText is the disk card's line for assistive technology, e.g. "已用 76%".
func UsedText(share float64) string {
	return "已用 " + strconv.FormatFloat(jsRound(share*100), 'f', 0, 64) + "%"
}

// FileCount renders how many files an entry holds, e.g. "1,204 个文件".
func FileCount(files int64) string { return format.Thousands(files) + " 个文件" }

// Cell is a rectangle of the treemap, in points from the top left of the map.
type Cell[T any] struct {
	Item                T
	X, Y, Width, Height float64
}

// TreemapGap is the gap between neighbouring cells, in points.
const TreemapGap = 3

// Cells at least this large show their name and size; smaller ones show them as a tooltip.
const (
	LabelMinWidth  = 72
	LabelMinHeight = 38
)

// CellShowsLabel reports whether a cell is large enough to be labelled.
func CellShowsLabel(width, height float64) bool {
	return width >= LabelMinWidth && height >= LabelMinHeight
}

// tile is a node of the layout: an item's value and its rectangle.
type tile struct {
	index          int
	value          float64
	x0, y0, x1, y1 float64
}

// phi is the aspect ratio squarify aims for, as d3-hierarchy's treemapSquarify.
var phi = (1 + math.Sqrt(5)) / 2

// LayoutTreemap lays items out as a squarified treemap filling width by height, one cell per item with a positive size, largest first, with TreemapGap between cells and edges rounded to whole points. Items of no size and an empty area give no cells. It is d3-hierarchy's treemap().tile(treemapSquarify).size([width, height]).paddingInner(TreemapGap).round(true), which the React page used, step for step.
func LayoutTreemap[T any](items []T, sizeOf func(T) int64, width, height float64) []Cell[T] {
	var nodes []tile
	total := 0.0
	for i, it := range items {
		if s := sizeOf(it); s > 0 {
			nodes = append(nodes, tile{index: i, value: float64(s)})
			total += float64(s)
		}
	}
	if len(nodes) == 0 || width <= 0 || height <= 0 {
		return nil
	}
	slices.SortStableFunc(nodes, func(a, b tile) int {
		switch {
		case a.value > b.value:
			return -1
		case a.value < b.value:
			return 1
		}
		return 0
	})
	// The root fills the area; its children tile it grown by half the gap on every side, and each child then gives that half back, so neighbours end a whole gap apart and the outer cells meet the edges.
	p := float64(TreemapGap) / 2
	x0, y0, x1, y1 := -p, -p, width+p, height+p
	squarify(nodes, total, x0, y0, x1, y1)
	cells := make([]Cell[T], 0, len(nodes))
	for _, n := range nodes {
		nx0, ny0, nx1, ny1 := n.x0+p, n.y0+p, n.x1-p, n.y1-p
		if nx1 < nx0 {
			nx0 = (nx0 + nx1) / 2
			nx1 = nx0
		}
		if ny1 < ny0 {
			ny0 = (ny0 + ny1) / 2
			ny1 = ny0
		}
		nx0, ny0, nx1, ny1 = jsRound(nx0), jsRound(ny0), jsRound(nx1), jsRound(ny1)
		w, h := nx1-nx0, ny1-ny0
		// A sliver that rounds away to nothing is not drawn; the list beside the map still shows it.
		if w <= 0 || h <= 0 {
			continue
		}
		cells = append(cells, Cell[T]{Item: items[n.index], X: nx0, Y: ny0, Width: w, Height: h})
	}
	return cells
}

// squarify is d3-hierarchy's squarifyRatio(phi, …): rows of nodes, each laid along the shorter side while adding a node keeps its worst aspect ratio from getting worse.
func squarify(nodes []tile, value, x0, y0, x1, y1 float64) {
	n := len(nodes)
	i0, i1 := 0, 0
	for i0 < n {
		dx, dy := x1-x0, y1-y0
		// Find the next non-empty node.
		var sum float64
		for {
			sum = nodes[i1].value
			i1++
			if sum != 0 || i1 >= n {
				break
			}
		}
		minV, maxV := sum, sum
		alpha := max(dy/dx, dx/dy) / (value * phi)
		beta := sum * sum * alpha
		minRatio := max(maxV/beta, beta/minV)
		// Keep adding nodes while the aspect ratio maintains or improves.
		for ; i1 < n; i1++ {
			v := nodes[i1].value
			sum += v
			minV, maxV = min(minV, v), max(maxV, v)
			beta = sum * sum * alpha
			ratio := max(maxV/beta, beta/minV)
			if ratio > minRatio {
				sum -= v
				break
			}
			minRatio = ratio
		}
		row := nodes[i0:i1]
		if dx < dy {
			top := y0
			bottom := y1
			if value != 0 {
				y0 += dy * sum / value
				bottom = y0
			}
			dice(row, sum, x0, top, x1, bottom)
		} else {
			left := x0
			right := x1
			if value != 0 {
				x0 += dx * sum / value
				right = x0
			}
			slice(row, sum, left, y0, right, y1)
		}
		value -= sum
		i0 = i1
	}
}

// dice lays a row's nodes left to right across x0..x1, each as wide as its share of value, all from y0 to y1.
func dice(row []tile, value, x0, y0, x1, y1 float64) {
	k := 0.0
	if value != 0 {
		k = (x1 - x0) / value
	}
	for i := range row {
		row[i].y0, row[i].y1, row[i].x0 = y0, y1, x0
		x0 += row[i].value * k
		row[i].x1 = x0
	}
}

// slice lays a row's nodes top to bottom across y0..y1, each as tall as its share of value, all from x0 to x1.
func slice(row []tile, value, x0, y0, x1, y1 float64) {
	k := 0.0
	if value != 0 {
		k = (y1 - y0) / value
	}
	for i := range row {
		row[i].x0, row[i].x1, row[i].y0 = x0, x1, y0
		y0 += row[i].value * k
		row[i].y1 = y0
	}
}

// Toast times: a message alone stays 6 seconds, one with 撤销 10 (as the shell's).
const (
	toastShort = 6 * time.Second
	toastLong  = 10 * time.Second
)

// TrashToast is the toast after a move to the Trash that moved something. With undo (macOS, where Spruce can put moves back) it offers 撤销 for what the undo ledger kept, says what it could not keep and what was left, and offers 查看 when something was left; without it (Windows) it says the Recycle Bin can restore the items.
func TrashToast(res app.SpaceTrashResult, undo bool) store.Toast {
	trash := format.TrashName()
	text := "已移到" + trash + " " + format.Bytes(res.Trashed)
	failed := len(res.Failed)
	if !undo {
		t := store.Toast{Text: text + "，需要时可以在" + trash + "里还原", Duration: toastShort}
		if failed > 0 {
			t.ViewRun = res.RunID
		}
		return t
	}
	t := store.Toast{Duration: toastLong}
	switch {
	case failed > 0:
		text += "，" + strconv.Itoa(failed) + " 项未能处理"
		t.ViewRun = res.RunID
	case res.NotUndoable > 0:
		text += "，其中 " + strconv.Itoa(res.NotUndoable) + " 项不能撤销"
	}
	if res.Undoable && res.RunID != "" {
		t.UndoRun = res.RunID
	}
	t.Text = text
	return t
}

// weekdays are 星期 by time.Weekday.
var weekdays = [...]string{"周日", "周一", "周二", "周三", "周四", "周五", "周六"}

// GrowthWhen is when a growth started: hours within a day, the weekday within a week, else the date.
func GrowthWhen(since, now time.Time) string {
	d := now.Sub(since)
	switch {
	case d < 24*time.Hour:
		return strconv.Itoa(max(1, int(d/time.Hour))) + " 小时前"
	case d < 7*24*time.Hour:
		return weekdays[since.In(now.Location()).Weekday()]
	}
	return format.MonthDay(since, now)
}

// GrowthSentence is a growth row's sentence: 「从周二到现在涨了 6 GB」, 「至少涨了」 for a folder that was too small to be listed before.
func GrowthSentence(g growth.Growth, now time.Time) string {
	verb := "涨了 "
	if g.AtLeast {
		verb = "至少涨了 "
	}
	return "从" + GrowthWhen(g.Since, now) + "到现在" + verb + format.Bytes(g.After-g.Before)
}

// GrowthName is a growth row's name: its label, else its path with the home folder written ~, shortened in the middle.
func GrowthName(g growth.Growth, home string) string {
	if g.Label != "" {
		return g.Label
	}
	p := g.Path
	if home != "" {
		if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) && !filepath.IsAbs(rel) {
			p = "~"
			if rel != "." {
				p += string(filepath.Separator) + rel
			}
		}
	}
	return format.TruncateMiddle(p, 48)
}

// GrowthWindow is a tab of 最近变化.
type GrowthWindow time.Duration

// The tabs of 最近变化, 7 天 first shown.
const (
	GrowthDay   = GrowthWindow(24 * time.Hour)
	GrowthWeek  = GrowthWindow(7 * 24 * time.Hour)
	GrowthMonth = GrowthWindow(30 * 24 * time.Hour)
)

// Label is the tab's name: 24 小时, 7 天, 30 天.
func (w GrowthWindow) Label() string {
	switch w {
	case GrowthDay:
		return "24 小时"
	case GrowthMonth:
		return "30 天"
	}
	return "7 天"
}

// GrowthNone is what 最近变化 says when nothing grew enough over w.
func GrowthNone(w GrowthWindow) string {
	return "最近 " + w.Label() + "没有明显变大的位置。"
}
