package overview

import (
	"fmt"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"

	"spruce/apps/spruce/internal/app"
	"spruce/apps/spruce/internal/engine/advice"
	"spruce/apps/spruce/internal/engine/cleanup"
	"spruce/apps/spruce/internal/engine/growth"
	"spruce/apps/spruce/internal/engine/history"
	"spruce/apps/spruce/internal/engine/settings"
	"spruce/apps/spruce/internal/ui/cleanflow"
	"spruce/apps/spruce/internal/ui/format"
	"spruce/apps/spruce/internal/ui/store"
)

// Sources are the cleaners a check combines, in the order the Go side scans them (internal/engine/smartscan via wire.go). Each one is also a drill-down view.
var Sources = []app.CleanerID{app.CleanerSystemJunk, app.CleanerDevJunk, app.CleanerInstallers}

// SourceScope is what each source looks at, in a few words, for the progress rows.
var SourceScope = map[app.CleanerID]string{
	app.CleanerSystemJunk: "应用和浏览器缓存、日志、崩溃报告、未完成的下载",
	app.CleanerDevJunk:    "开发工具的缓存和项目里的构建产物",
	app.CleanerInstallers: "下载过的安装包和磁盘映像",
}

// trashCategory is the merged ID of the Trash in a check: the 废纸篓 row, never a card.
const trashCategory = "system-junk:trash"

// Toast times: a message alone stays 6 seconds, one with 撤销 or 查看 10.
const (
	toastShort = 6 * time.Second
	toastLong  = 10 * time.Second
)

// RowStatus is where a source is in the progress block: StatusWaiting before its first event, otherwise its cleanup.SourceStatus.
type RowStatus string

// StatusWaiting is a source the check has not reported yet.
const StatusWaiting RowStatus = "waiting"

// SourceRow is one row of the progress block.
type SourceRow struct {
	ID     app.CleanerID
	Status RowStatus
	// Fraction is the share done when Known; a source scanning without a total does not know it.
	Fraction float64
	Known    bool
	Bytes    int64
	Items    int
	Error    string
}

// SourceRows turns the latest progress event into one row per source; before the first event (the zero Progress) every source is waiting.
func SourceRows(p cleanup.Progress) []SourceRow {
	rows := make([]SourceRow, len(Sources))
	for i, id := range Sources {
		row := SourceRow{ID: id, Status: StatusWaiting}
		for _, st := range p.Sources {
			if st.ID != string(id) {
				continue
			}
			row = SourceRow{ID: id, Status: RowStatus(st.Status), Bytes: st.Bytes, Items: st.Items, Error: st.Error}
			switch {
			case st.Status != cleanup.SourceScanning:
				row.Fraction, row.Known = 1, true
			case st.Total > 0:
				row.Fraction, row.Known = min(1, float64(st.Done)/float64(st.Total)), true
			}
			break
		}
		rows[i] = row
	}
	return rows
}

// OverallFraction is how far the whole check got, from the event's Done and Total (which add up the sources' shares); not known before the first event.
func OverallFraction(p cleanup.Progress) (float64, bool) {
	if p.Total <= 0 {
		return 0, false
	}
	return min(1, float64(p.Done)/float64(p.Total)), true
}

// RowStatusText is a progress row's state in words.
func RowStatusText(row SourceRow) string {
	switch row.Status {
	case StatusWaiting:
		return "等待开始…"
	case RowStatus(cleanup.SourceScanning):
		return "正在检查… 已找到 " + format.Bytes(row.Bytes)
	case RowStatus(cleanup.SourceDone):
		if row.Items > 0 {
			return "完成，找到 " + strconv.Itoa(row.Items) + " 项，共 " + format.Bytes(row.Bytes)
		}
		return "完成，没有找到可以清理的内容"
	}
	if row.Error != "" {
		return row.Error
	}
	return "没能检查完成"
}

// ConclusionLabel is the line over the figure.
func ConclusionLabel(low bool) string {
	if low {
		return "空间不多了，可以放心释放"
	}
	return "可以放心释放"
}

// ConclusionSub is the line under the figure: how many cards, and how much of them goes to the Trash first.
func ConclusionSub(recs []advice.Recommendation) string {
	var trashed int64
	for _, r := range recs {
		if r.Action == advice.ActionTrash {
			trashed += r.Bytes
		}
	}
	s := strconv.Itoa(len(recs)) + " 条建议"
	if trashed > 0 {
		s += "，其中 " + format.Bytes(trashed) + " 会先移到" + format.TrashName()
	}
	return s
}

// CheckedAtText is the time line: when the result was checked at, that it may be outdated (stale: Stale, or the last check failed) and that a check is updating it, with how far it is (progress) once it knows. The page's clock moves once a minute, so a result that has just arrived can be later than now; it reads 「刚刚」 rather than a date.
func CheckedAtText(at time.Time, stale, checking bool, progress cleanup.Progress, now time.Time) string {
	if now.Before(at) {
		now = at
	}
	s := "上次检查：" + format.RelativeTime(at, now)
	if stale && !checking {
		s += "，结果可能已经变化"
	}
	if checking {
		s += " · 正在更新…"
		if v, ok := OverallFraction(progress); ok {
			s += " " + strconv.Itoa(int(v*100+0.5)) + "%"
		}
	}
	return s
}

// ReasonText is a card's reason with its time written for the page.
func ReasonText(r advice.Recommendation, now time.Time) string {
	return r.ReasonWith(func(t time.Time, kind advice.TimeKind) string {
		if kind == advice.TimeDate {
			return format.MonthDay(t, now)
		}
		return format.RelativeTime(t, now)
	})
}

// ActionLabel is a card's button for the action of its selection; partial adds the selection's size.
func ActionLabel(a advice.Action, partial bool, bytes int64) string {
	var s string
	switch a {
	case advice.ActionDelete:
		s = "删除…"
	case advice.ActionCommand:
		s = "运行清理…"
	case advice.ActionMixed:
		s = "清理…"
	default:
		s = "移到" + format.TrashName()
	}
	if partial {
		s += " · " + format.Bytes(bytes)
	}
	return s
}

// CleanProgressText is a card's progress line while its clean of total bytes runs: 已处理 {done}/{total} over several items; one item reports nothing until it ends, so it reads how much is freed (or moved to the Trash, for a move) of total, which at least says how much is being handled.
func CleanProgressText(a advice.Action, pr cleanup.Progress, total int64) string {
	if pr.Total > 1 {
		return "已处理 " + strconv.Itoa(pr.Done) + "/" + strconv.Itoa(pr.Total)
	}
	verb := "已释放 "
	if a == advice.ActionTrash {
		verb = "已移到" + format.TrashName() + " "
	}
	return verb + format.Bytes(pr.Bytes) + " / " + format.Bytes(total)
}

// CleanProgressFraction is how far a card's clean of total bytes is, as CleanProgressText counts it: by items over several, by bytes for one.
func CleanProgressFraction(pr cleanup.Progress, total int64) float64 {
	switch {
	case pr.Total > 1:
		return min(1, float64(pr.Done)/float64(pr.Total))
	case total > 0:
		return min(1, float64(pr.Bytes)/float64(total))
	}
	return 0
}

// BusyLabel is a card's button while its action runs.
func BusyLabel(a advice.Action) string {
	switch a {
	case advice.ActionDelete:
		return "正在删除…"
	case advice.ActionCommand:
		return "正在运行…"
	case advice.ActionMixed:
		return "正在清理…"
	}
	return "正在移到" + format.TrashName() + "…"
}

// Badge is the badge of an action.
type Badge struct {
	Text, Glyph string
	Warning     bool
}

// BadgeFor is a card's badge for the action of its selection; canUndo is whether moves to the Trash can be undone (macOS).
func BadgeFor(a advice.Action, canUndo bool) Badge {
	switch a {
	case advice.ActionDelete:
		// Not every deletion comes back (logs, crash reports, temporary files); what every one shares is that it cannot be undone.
		return Badge{Text: "无法撤销", Glyph: "alert"}
	case advice.ActionCommand:
		return Badge{Text: "运行命令", Glyph: "alert", Warning: true}
	case advice.ActionMixed:
		return Badge{Text: "部分无法撤销", Glyph: "alert"}
	}
	if canUndo {
		return Badge{Text: "可撤销", Glyph: "check"}
	}
	return Badge{Text: "移到回收站", Glyph: "trash"}
}

// NeedsConfirm reports whether acting on a selection of action a asks first: everything but a move to the Trash that can be undone.
func NeedsConfirm(a advice.Action, canUndo bool) bool {
	return a != advice.ActionTrash || !canUndo
}

// Confirm is what a card's confirmation says.
type Confirm struct {
	Title        string
	Message      string
	ConfirmLabel string
	Danger       bool
	// Command, when set, is the command the message names, drawn in the monospaced font: Message is the text before it and After the text after it.
	Command string
	After   string
}

// DeleteConfirm asks before deleting n items of bytes for good.
func DeleteConfirm(r advice.Recommendation, n int, bytes int64) Confirm {
	rest := strings.TrimPrefix(r.Consequence, "直接删除，")
	if rest != "" && !strings.HasSuffix(rest, "。") {
		rest += "。"
	}
	return Confirm{Title: "删除" + r.Title + "？", ConfirmLabel: "删除", Danger: true,
		Message: format.Bytes(bytes) + "（" + strconv.Itoa(n) + " 项）会直接删除，不经过" + format.TrashName() + "，无法撤销。" + rest}
}

// commandEffect is what a command category's description says the command does: the part after 「：」, else after the 「运行 …，」 it starts with (advice words a command card's consequence the same way). The confirmation names the command itself, so the description's own 「运行 …」 would say it twice.
func commandEffect(desc string) string {
	if _, rest, ok := strings.Cut(desc, "："); ok {
		return rest
	}
	if strings.HasPrefix(desc, "运行") {
		if _, rest, ok := strings.Cut(desc, "，"); ok {
			return rest
		}
	}
	return desc
}

// CommandConfirm asks before running command, which cleans what its category's description says, about bytes.
func CommandConfirm(command, description string, bytes int64) Confirm {
	description = commandEffect(description)
	words := strings.Fields(command)
	if len(words) > 2 {
		words = words[:2]
	}
	return Confirm{Title: "运行 " + strings.Join(words, " ") + "？", ConfirmLabel: "运行", Danger: true, Command: command,
		Message: "将运行 ", After: "，" + description + "（约 " + format.Bytes(bytes) + "），无法撤销。停止后，正在运行的命令会先完成。"}
}

// RecycleConfirm asks before moving n items of bytes to the Recycle Bin, where nothing can be undone from Spruce (Windows).
func RecycleConfirm(r advice.Recommendation, n int, bytes int64) Confirm {
	return Confirm{Title: "把" + r.Title + "移到回收站？", ConfirmLabel: "移到回收站",
		Message: format.Bytes(bytes) + "（" + strconv.Itoa(n) + " 项）会移到回收站，需要时可以在回收站里还原。"}
}

// CleanToast is the toast after acting on a card or emptying the Trash, from what the action was and what it did; canUndo is whether moves to the Trash can be undone (macOS). The items another program held open are told apart (cleanflow.SetInUseAside).
func CleanToast(a advice.Action, out app.OverviewOutcome, canUndo bool) store.Toast {
	res, inUse := cleanflow.SetInUseAside(out.Result)
	failed := len(res.Failed)
	var t store.Toast
	switch {
	case a == advice.ActionTrash && canUndo:
		t.Text = "已移到废纸篓 " + format.Bytes(res.Trashed)
		if out.NotUndoable > 0 {
			t.Text += "，其中 " + strconv.Itoa(out.NotUndoable) + " 项不能撤销"
		}
	case a == advice.ActionTrash:
		t.Text = "已移到回收站 " + format.Bytes(res.Trashed) + "，需要时可以在回收站里还原"
	default:
		t.Text = "已释放 " + format.Bytes(res.Freed)
		if res.Trashed > 0 {
			t.Text += "，移到" + format.TrashName() + " " + format.Bytes(res.Trashed)
		}
		if res.Trashed > 0 && canUndo && out.NotUndoable > 0 {
			t.Text += "，其中 " + strconv.Itoa(out.NotUndoable) + " 项不能撤销"
		}
	}
	if failed > 0 {
		t.Text += "，" + strconv.Itoa(failed) + " 项未能处理"
		t.ViewRun = out.RunID
	}
	if inUse > 0 {
		t.Text += "，" + strconv.Itoa(inUse) + " 项正在使用，已跳过"
	}
	if canUndo && out.Undoable {
		t.UndoRun = out.RunID
	}
	t.Duration = toastShort
	if t.UndoRun != "" || (a == advice.ActionTrash && canUndo) {
		t.Duration = toastLong
	}
	return t
}

// Pick is a card's selection, its part of the app's Choice (cleanflow.Kept), which the drill-down views share: Sel holds the checked items of the card by category, and Added counts the card's items a newer check added since the user last changed them, which start unchecked.
type Pick struct {
	Sel   cleanflow.Selection
	Added int
}

// refSet is the set of a card's items.
func refSet(r advice.Recommendation) map[advice.ItemRef]bool {
	m := make(map[advice.ItemRef]bool, len(r.Items))
	for _, ref := range r.Items {
		m[ref] = true
	}
	return m
}

// DefaultPick checks every item of r: a card holds only what starts checked.
func DefaultPick(r advice.Recommendation) Pick {
	sel := cleanflow.Selection{}
	for _, ref := range r.Items {
		if sel[ref.CategoryID] == nil {
			sel[ref.CategoryID] = map[string]struct{}{}
		}
		sel[ref.CategoryID][ref.ItemID] = struct{}{}
	}
	return Pick{Sel: sel}
}

// CardPick is the card r's part of ch over scan: what ch checks among r's items, and how many of them a newer check added to what the user changed.
func CardPick(r advice.Recommendation, ch cleanflow.Choice, scan cleanup.ScanResult) Pick {
	sel := cleanflow.Selection{}
	for _, ref := range r.Items {
		if ch.Sel.Has(ref.CategoryID, ref.ItemID) {
			if sel[ref.CategoryID] == nil {
				sel[ref.CategoryID] = map[string]struct{}{}
			}
			sel[ref.CategoryID][ref.ItemID] = struct{}{}
		}
	}
	return Pick{Sel: sel, Added: ch.Added(cardCategories(scan, r), r.Items)}
}

// ChangeCard is ch once the user checked sel on the card r over scan: r's items take sel's checks, every other check stays, and r's categories count as changed from r's items as they are now.
func ChangeCard(ch cleanflow.Choice, r advice.Recommendation, scan cleanup.ScanResult, sel cleanflow.Selection) cleanflow.Choice {
	return ch.Change(cleanflow.WithinRefs(ch.Sel, r.Items, sel), cardCategories(scan, r), "")
}

// Refs are the checked items, in the card's order.
func (p Pick) Refs(r advice.Recommendation) []advice.ItemRef {
	var out []advice.ItemRef
	for _, ref := range r.Items {
		if p.Sel.Has(ref.CategoryID, ref.ItemID) {
			out = append(out, ref)
		}
	}
	return out
}

// itemsOf returns the items refs names in scan, with their categories, in refs' order; refs the scan lacks are left out.
func itemsOf(scan cleanup.ScanResult, refs []advice.ItemRef) []cleanup.Item {
	byRef := make(map[advice.ItemRef]cleanup.Item, scan.Items)
	for _, c := range scan.Categories {
		for _, it := range c.Items {
			byRef[advice.ItemRef{CategoryID: c.ID, ItemID: it.ID}] = it
		}
	}
	var out []cleanup.Item
	for _, ref := range refs {
		if it, ok := byRef[ref]; ok {
			out = append(out, it)
		}
	}
	return out
}

// Sum adds up the size of the items refs names in scan.
func Sum(scan cleanup.ScanResult, refs []advice.ItemRef) int64 {
	var n int64
	for _, it := range itemsOf(scan, refs) {
		n += it.Size
	}
	return n
}

// CleanRequest is the request for the checked items of a card: every category with a checked item, every checked item named (a card holds part of its categories).
func CleanRequest(r advice.Recommendation, p Pick) cleanup.CleanRequest {
	req := cleanup.CleanRequest{CategoryIDs: []string{}}
	for _, ref := range p.Refs(r) {
		if !slices.Contains(req.CategoryIDs, ref.CategoryID) {
			req.CategoryIDs = append(req.CategoryIDs, ref.CategoryID)
		}
		req.ItemIDs = append(req.ItemIDs, ref.ItemID)
	}
	return req
}

// cardCategories are the scan's categories of a card, in the card's order, each with only the card's items: what its expanded list and its plan show.
func cardCategories(scan cleanup.ScanResult, r advice.Recommendation) []cleanup.Category {
	refs := refSet(r)
	var out []cleanup.Category
	for _, id := range r.Categories {
		for _, c := range scan.Categories {
			if c.ID != id {
				continue
			}
			cat := c
			cat.Items = nil
			cat.Size = 0
			for _, it := range c.Items {
				if refs[advice.ItemRef{CategoryID: c.ID, ItemID: it.ID}] {
					cat.Items = append(cat.Items, it)
					cat.Size += it.Size
				}
			}
			out = append(out, cat)
			break
		}
	}
	return out
}

// Review is what starts unchecked in a check (the Trash aside): how many items, their size, and the sources they come from, in Sources order.
type Review struct {
	Count   int
	Bytes   int64
	Sources []string
}

// ReviewOf sums up the items a check leaves unchecked or holds, for 需要你判断.
func ReviewOf(scan cleanup.ScanResult) Review {
	var r Review
	seen := map[string]bool{}
	for _, c := range scan.Categories {
		if c.Kind == cleanup.KindTrash || c.ID == trashCategory {
			continue
		}
		for _, it := range c.Items {
			if (c.Selected || it.Selected) && it.Hold == "" {
				continue
			}
			r.Count++
			r.Bytes += it.Size
			seen[sourceOf(c)] = true
		}
	}
	for _, id := range Sources {
		if seen[string(id)] {
			r.Sources = append(r.Sources, string(id))
		}
	}
	return r
}

// ReviewText is 需要你判断's sentence.
func ReviewText(r Review) string {
	return "还有 " + strconv.Itoa(r.Count) + " 项默认不选（" + format.Bytes(r.Bytes) + "），比如正在运行的应用的缓存、最近下载的安装包、有未提交改动的项目，可能还要用"
}

// sourceOf is the source of a merged category: Category.Source, else the prefix of its ID.
func sourceOf(c cleanup.Category) string {
	if c.Source != "" {
		return c.Source
	}
	src, _, _ := strings.Cut(c.ID, ":")
	return src
}

// Browse is one source's card of 按类别查看.
type Browse struct {
	Source string
	// Bytes and Items are everything the source found.
	Bytes int64
	Items int
	// RecItems and RecBytes are the source's share of the cards.
	RecItems int
	RecBytes int64
	// Error is set when the source's check failed.
	Error string
}

// BrowseOf splits a result by source, in Sources order.
func BrowseOf(r app.OverviewResult) []Browse {
	sourceOfCat := map[string]string{}
	out := make([]Browse, len(Sources))
	for i, id := range Sources {
		b := Browse{Source: string(id)}
		for _, c := range r.Scan.Categories {
			src := sourceOf(c)
			sourceOfCat[c.ID] = src
			if src == b.Source {
				b.Bytes += c.Size
				b.Items += len(c.Items)
			}
		}
		for _, s := range r.Scan.Sources {
			if s.ID == b.Source && s.Status == cleanup.SourceFailed {
				b.Error = s.Error
				if b.Error == "" {
					b.Error = "没能检查完成"
				}
			}
		}
		out[i] = b
	}
	byRef := map[advice.ItemRef]int64{}
	for _, c := range r.Scan.Categories {
		for _, it := range c.Items {
			byRef[advice.ItemRef{CategoryID: c.ID, ItemID: it.ID}] = it.Size
		}
	}
	for _, rec := range r.Recommendations {
		for _, ref := range rec.Items {
			for i := range out {
				if out[i].Source == sourceOfCat[ref.CategoryID] {
					out[i].RecItems++
					out[i].RecBytes += byRef[ref]
				}
			}
		}
	}
	return out
}

// BrowseNote is a browse card's line under its size.
func BrowseNote(b Browse) string {
	if b.Error != "" {
		return b.Error
	}
	if b.Items == 0 {
		return "没有找到可以清理的内容"
	}
	return strconv.Itoa(b.Items) + " 项，建议清理 " + strconv.Itoa(b.RecItems) + " 项（" + format.Bytes(b.RecBytes) + "）"
}

// SkippedNames names the prompting folders a background check did not read, in order and once each: 下载, 桌面, 文稿, iCloud 云盘, a cloud storage provider by its name, 外接磁盘.
func SkippedNames(home string, paths []string) []string {
	var out []string
	add := func(s string) {
		if !slices.Contains(out, s) {
			out = append(out, s)
		}
	}
	for _, p := range paths {
		p = filepath.Clean(p)
		switch {
		case p == filepath.Join(home, "Downloads"):
			add("下载")
		case p == filepath.Join(home, "Desktop"):
			add("桌面")
		case p == filepath.Join(home, "Documents"):
			add("文稿")
		case p == filepath.Join(home, "Library", "Mobile Documents"):
			add("iCloud 云盘")
		case filepath.Dir(p) == filepath.Join(home, "Library", "CloudStorage"):
			name, _, _ := strings.Cut(filepath.Base(p), "-")
			add(name)
		case strings.HasPrefix(p, "/Volumes/"):
			add("外接磁盘")
		default:
			add(filepath.Base(p))
		}
	}
	return out
}

// SkippedText is the notice about them.
func SkippedText(names []string) string {
	return "后台检查没有读取" + strings.Join(names, "、") + "，这些位置要在你使用 Spruce 时才能检查。"
}

// UsedFraction is the share of a volume in use, 0 for a volume without a size.
func UsedFraction(d app.DiskState) float64 {
	if d.Total <= 0 {
		return 0
	}
	return min(1, max(0, 1-float64(d.Free)/float64(d.Total)))
}

// DiskText is the disk row: 「Macintosh HD · 可用 120 GB，共 1 TB」.
func DiskText(d app.DiskState) string {
	return d.Volume + " · 可用 " + format.Bytes(d.Free) + "，共 " + format.Bytes(d.Total)
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

// GrowthSentence is a growth row's sentence: 「从周二到现在涨了 6 GB」.
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
		if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
			p = "~"
			if rel != "." {
				p += string(filepath.Separator) + rel
			}
		}
	}
	return format.TruncateMiddle(p, 48)
}

// Rule is an automatic rule as 概览 offers it under a card (以后自动处理): its label and what it does (设置 → 自动清理 says the same).
type Rule struct {
	Label, Description string
}

// Rules are the automatic rules by rules.ID.
var Rules = map[string]Rule{
	"app-caches":           {Label: "旧的应用缓存", Description: "每周把 30 天没有改动的应用缓存移到废纸篓。正在运行的应用的缓存不会处理。"},
	"installers":           {Label: "旧安装包", Description: "每天把「下载」里超过 7 天、对应的应用已经装好的安装包和磁盘映像（.dmg、.pkg）移到废纸篓。压缩包、系统映像和其他文件夹里的不会处理，正在挂载或无法确认是否挂载的也不会处理。"},
	"build-output":         {Label: "闲置项目的构建产物", Description: "每周把 90 天没有改动的项目里的 node_modules、target 等构建产物移到废纸篓。有未提交改动、无法确定 Git 状态，或构建产物文件夹里有纳入版本控制的文件的项目不会处理；不是 Git 仓库的项目按最后改动时间判断。"},
	"incomplete-downloads": {Label: "未完成的下载", Description: "每天把超过 7 天没有变化的未完成下载移到废纸篓。"},
}

// RuleOn reports whether the automatic rule id is on in a.
func RuleOn(a settings.AutomationSettings, id string) bool {
	switch id {
	case "app-caches":
		return a.AppCaches
	case "installers":
		return a.Installers
	case "build-output":
		return a.BuildOutput
	case "incomplete-downloads":
		return a.IncompleteDownloads
	}
	return false
}

// WithRule is a with the automatic rule id turned on.
func WithRule(a settings.AutomationSettings, id string) settings.AutomationSettings {
	switch id {
	case "app-caches":
		a.AppCaches = true
	case "installers":
		a.Installers = true
	case "build-output":
		a.BuildOutput = true
	case "incomplete-downloads":
		a.IncompleteDownloads = true
	}
	return a
}

// AnyRuleOn reports whether an automatic rule is on.
func AnyRuleOn(a settings.AutomationSettings) bool {
	return a.AppCaches || a.Installers || a.BuildOutput || a.IncompleteDownloads
}

// AutoJustRanText is the conclusion's note about the last automatic run.
func AutoJustRanText(trashed int64) string {
	return " · 自动清理刚刚移走了 " + format.Bytes(trashed)
}

// autoRecent is how long after an automatic run 概览 still mentions it.
const autoRecent = 24 * time.Hour

// MaintenanceDueDays is the age past which a 维护 run earns a 「去维护」.
const MaintenanceDueDays = 30

// LastMaintenance is the newest 维护 run among runs (newest first), or nil.
func LastMaintenance(runs []history.Run) *history.Run {
	for i := range runs {
		if runs[i].Kind == history.KindMaintenance {
			r := runs[i]
			return &r
		}
	}
	return nil
}

// MaintenanceDue reports whether 维护 should be offered: it never ran, its end is unknown, or it ended more than MaintenanceDueDays ago.
func MaintenanceDue(run *history.Run, now time.Time) bool {
	if run == nil || run.EndedAt.IsZero() {
		return true
	}
	return now.Sub(run.EndedAt) > MaintenanceDueDays*24*time.Hour
}

// MaintenanceValue is the 维护 card's state: when it last ran, or that it never did.
func MaintenanceValue(run *history.Run, now time.Time) string {
	if run == nil {
		return "还没有做过维护"
	}
	when := format.RelativeTime(run.EndedAt, now)
	if when == "" {
		when = format.RelativeTime(run.StartedAt, now)
	}
	return "上次维护：" + when
}

// MaintenanceNote is the 维护 card's line under its state.
func MaintenanceNote(run *history.Run, due bool) string {
	switch {
	case run == nil:
		return "做一次轻量保养，不会动你的文档和照片"
	case due:
		return "已经超过 " + strconv.Itoa(MaintenanceDueDays) + " 天没有维护了，可以再做一次"
	}
	return "最近做过维护"
}

// FullDiskAccessNote is what Full Disk Access unlocks in Spruce, as the engines use it (systemjunk's Trash and Safari caches, the 维护 database task, 空间透视's protected folders).
func FullDiskAccessNote() string {
	return "开启后才能读取和清空" + format.TrashName() + "、清理 Safari 缓存、优化 Safari 和邮件的数据库，空间透视也能算进受保护的文件夹"
}

// countText is 「{n} 项」.
func countText(n int) string { return fmt.Sprintf("%d 项", n) }
