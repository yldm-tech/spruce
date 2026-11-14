package smartscan

import (
	"strconv"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/cleanflow"
	"github.com/yldm-tech/spruce/internal/ui/format"
)

// Sources are the cleaners 智能扫描 combines, in the order the Go side scans and cleans them (internal/engine/smartscan via wire.go). Each one is also a page, which the source's card links to.
var Sources = []app.CleanerID{app.CleanerSystemJunk, app.CleanerDevJunk, app.CleanerInstallers}

// SourceScope is what each source looks at, in a few words, for the scanning rows.
var SourceScope = map[app.CleanerID]string{
	app.CleanerSystemJunk: "应用和浏览器缓存、日志、崩溃报告、未完成的下载",
	app.CleanerDevJunk:    "开发工具的缓存和项目里的构建产物",
	app.CleanerInstallers: "下载过的安装包和磁盘映像",
}

// SourceSummary is one source's share of a scan, for its summary card and its section of the list.
type SourceSummary struct {
	ID app.CleanerID
	// State is the source's state, nil when the scan did not report it.
	State      *cleanup.SourceState
	Categories []cleanup.Category
	// Bytes and Items are everything the source found.
	Bytes int64
	Items int
	// Recommended is what starts checked.
	Recommended cleanflow.Totals
}

// Failed reports whether the source's scan failed.
func (s SourceSummary) Failed() bool { return s.State != nil && s.State.Status == cleanup.SourceFailed }

// Summarize splits a merged scan by source, in Sources order, with each source's totals and its default selection's totals.
func Summarize(scan cleanup.ScanResult) []SourceSummary {
	out := make([]SourceSummary, len(Sources))
	for i, id := range Sources {
		s := SourceSummary{ID: id, Categories: []cleanup.Category{}}
		for _, c := range scan.Categories {
			if c.Source == string(id) {
				s.Categories = append(s.Categories, c)
				s.Bytes += c.Size
				s.Items += len(c.Items)
			}
		}
		for j := range scan.Sources {
			if scan.Sources[j].ID == string(id) {
				st := scan.Sources[j]
				s.State = &st
				break
			}
		}
		s.Recommended = cleanflow.SelectedTotals(s.Categories, cleanflow.DefaultSelection(s.Categories))
		out[i] = s
	}
	return out
}

// RecommendedTotals is the count and size of everything the scan checks by default: the 「可清理」 figure.
func RecommendedTotals(scan cleanup.ScanResult) cleanflow.Totals {
	return cleanflow.SelectedTotals(scan.Categories, cleanflow.DefaultSelection(scan.Categories))
}

// RowStatus is where a source is in the scanning view: StatusWaiting before its first event, otherwise its cleanup.SourceStatus.
type RowStatus string

// StatusWaiting is a source the scan has not reported yet.
const StatusWaiting RowStatus = "waiting"

// SourceRow is one row of the scanning view.
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

// SourceRows turns the latest scanning event into one row per source; before the first event (the zero Progress) every source is waiting.
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

// OverallFraction is how far the whole scan got, from the event's Done and Total (which add up the sources' shares); not known before the first event.
func OverallFraction(p cleanup.Progress) (float64, bool) {
	if p.Total <= 0 {
		return 0, false
	}
	return min(1, float64(p.Done)/float64(p.Total)), true
}

// RowStatusText is a scanning row's state in words.
func RowStatusText(row SourceRow) string {
	switch row.Status {
	case StatusWaiting:
		return "等待开始…"
	case RowStatus(cleanup.SourceScanning):
		return "正在扫描… 已找到 " + format.Bytes(row.Bytes)
	case RowStatus(cleanup.SourceDone):
		if row.Items > 0 {
			return "完成，找到 " + strconv.Itoa(row.Items) + " 项，共 " + format.Bytes(row.Bytes)
		}
		return "完成，没有找到可以清理的内容"
	}
	if row.Error != "" {
		return row.Error
	}
	return "没能扫描完成"
}

// SourceNote is a source card's line under its size.
func SourceNote(s SourceSummary) string {
	if s.Items == 0 {
		return "没有找到可以清理的内容"
	}
	return strconv.Itoa(s.Items) + " 项，推荐清理 " + strconv.Itoa(s.Recommended.Count) + " 项（" + format.Bytes(s.Recommended.Bytes) + "）"
}

// TotalSub is the line under the 「可清理」 figure.
func TotalSub(scan cleanup.ScanResult) string {
	r := RecommendedTotals(scan)
	return "已按推荐勾选 " + strconv.Itoa(r.Count) + " 项。一共找到 " + strconv.Itoa(scan.Items) + " 项（" + format.Bytes(scan.Size) + "），默认不选的可能还要用，可以在下面逐项查看。"
}

// MaintenanceDueDays is the age past which a 维护 run earns a 「去维护」.
const MaintenanceDueDays = 30

// Below lowFreeShare of free space, or below lowFreeBytes, the disk card warns.
const (
	lowFreeShare       = 0.1
	lowFreeBytes int64 = 20e9
)

// DiskLow reports whether a volume is running out of space; a volume without a size is not.
func DiskLow(v app.SpaceVolume) bool {
	if v.Total <= 0 {
		return false
	}
	return float64(v.Free)/float64(v.Total) < lowFreeShare || v.Free < lowFreeBytes
}

// UsedFraction is the share of a volume in use, 0 for a volume without a size.
func UsedFraction(v app.SpaceVolume) float64 {
	if v.Total <= 0 {
		return 0
	}
	return min(1, max(0, 1-float64(v.Free)/float64(v.Total)))
}

// SystemVolume is the disk the system runs from, or the first disk, or nil without disks.
func SystemVolume(l app.SpaceLocations) *app.SpaceVolume {
	for i := range l.Volumes {
		if l.Volumes[i].Startup {
			return &l.Volumes[i]
		}
	}
	if len(l.Volumes) > 0 {
		return &l.Volumes[0]
	}
	return nil
}

// DiskNote is the disk card's line under the free space, e.g. "Macintosh HD 共 1 TB".
func DiskNote(v app.SpaceVolume) string {
	name := v.Name
	if name == "" {
		name = v.Path
	}
	s := name + " 共 " + format.Bytes(v.Total)
	if DiskLow(v) {
		s += "，空间不多了"
	}
	return s
}

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
