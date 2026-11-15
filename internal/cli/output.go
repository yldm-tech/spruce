package cli

import (
	"encoding/json"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"golang.org/x/text/width"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/advice"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/devjunk"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/ui/format"
)

// jsonVersion is the version of the JSON output.
const jsonVersion = 1

// output is the one JSON object a command writes with --json. Fields a command does not produce are left out.
type output struct {
	Version         int                     `json:"version"`
	Command         string                  `json:"command,omitempty"`
	Error           string                  `json:"error,omitempty"`
	App             *versionInfo            `json:"app,omitempty"`
	Scan            *cleanup.ScanResult     `json:"scan,omitempty"`
	Recommendations []advice.Recommendation `json:"recommendations,omitzero"`
	Headline        *int64                  `json:"headline,omitempty"`
	Plan            *Plan                   `json:"plan,omitempty"`
	Result          *CleanResult            `json:"result,omitempty"`
	Projects        []devjunk.Project       `json:"projects,omitzero"`
	Runs            []HistoryRun            `json:"runs,omitzero"`
	Restore         *RestorePlan            `json:"restore,omitempty"`
	Restored        *app.RestoreOutcome     `json:"restored,omitempty"`
}

// versionInfo is the version command's output.
type versionInfo struct {
	Version     string `json:"version"`
	Development bool   `json:"development"`
}

// CleanResult is what a clean did, with the history run that records it.
type CleanResult struct {
	cleanup.CleanResult
	RunID string `json:"runId,omitempty"`
	// Undo is what 放回原处 can put back; nil when nothing.
	Undo *history.UndoInfo `json:"undo,omitempty"`
}

// HistoryRun is a recorded run and whether restore can still put it back.
type HistoryRun struct {
	history.Run
	Title      string `json:"title"`
	Restorable bool   `json:"restorable"`
}

// RestorePlan is what restore would put back.
type RestorePlan struct {
	RunID string        `json:"runId"`
	Items int           `json:"items"`
	Bytes int64         `json:"bytes"`
	Moves []RestoreItem `json:"moves"`
}

// RestoreItem is one item restore would put back.
type RestoreItem struct {
	Path string `json:"path"`
	Size int64  `json:"size"`
	Dir  bool   `json:"dir"`
}

// writeJSON writes o, stamped with the version and the command, as the command's output.
func (r *run) writeJSON(o output) {
	o.Version, o.Command = jsonVersion, r.command
	r.encode(o)
}

// writeError writes {"version":1,"error":…}.
func (r *run) writeError(err error) {
	r.encode(output{Version: jsonVersion, Error: err.Error()})
}

func (r *run) encode(o output) {
	enc := json.NewEncoder(r.stdout)
	enc.SetEscapeHTML(false)
	enc.SetIndent("", "  ")
	if err := enc.Encode(o); err != nil {
		fmt.Fprintf(r.stderr, "spruce：无法写出结果：%v\n", err)
	}
}

// table writes aligned columns, wide characters (Chinese) counting two cells.
type table struct {
	rows [][]string
	// right are the columns aligned right (sizes and counts).
	right map[int]bool
}

func (t *table) add(cells ...string) { t.rows = append(t.rows, cells) }

func (t *table) write(w io.Writer) {
	var widths []int
	for _, row := range t.rows {
		for i, c := range row {
			if i == len(widths) {
				widths = append(widths, 0)
			}
			widths[i] = max(widths[i], cells(c))
		}
	}
	for _, row := range t.rows {
		var b strings.Builder
		for i, c := range row {
			pad := strings.Repeat(" ", widths[i]-cells(c))
			last := i == len(row)-1
			switch {
			case t.right[i]:
				b.WriteString(pad + c)
			case last:
				b.WriteString(c)
			default:
				b.WriteString(c + pad)
			}
			if !last {
				b.WriteString("  ")
			}
		}
		fmt.Fprintln(w, strings.TrimRight(b.String(), " "))
	}
}

// cells is how many terminal cells s takes: two for East Asian wide and fullwidth characters, one for the rest.
func cells(s string) int {
	n := 0
	for _, r := range s {
		switch width.LookupRune(r).Kind() {
		case width.EastAsianWide, width.EastAsianFullwidth:
			n += 2
		default:
			n++
		}
	}
	return n
}

// short writes p with the home folder as "~".
func (r *run) short(p string) string {
	home := r.d.Home
	if home == "" {
		return p
	}
	if rel, err := filepath.Rel(home, p); err == nil && rel != ".." && !strings.HasPrefix(rel, ".."+string(filepath.Separator)) {
		if rel == "." {
			return "~"
		}
		return "~" + string(filepath.Separator) + rel
	}
	return p
}

// when writes t for a list: the date and time of day.
func when(t time.Time) string {
	if t.IsZero() {
		return ""
	}
	return t.Local().Format("2006-01-02 15:04")
}

// progress rewrites one status line on stderr while a scan or clean runs, only when stderr is a terminal.
type progress struct {
	w     io.Writer
	on    bool
	shown int
}

func (r *run) newProgress() *progress {
	return &progress{w: r.stderr, on: terminal(r.stderr)}
}

// show replaces the line with s.
func (p *progress) show(s string) {
	if !p.on {
		return
	}
	n := cells(s)
	fmt.Fprint(p.w, "\r"+s+strings.Repeat(" ", max(p.shown-n, 0)))
	p.shown = n
}

// clear removes the line.
func (p *progress) clear() {
	if !p.on || p.shown == 0 {
		return
	}
	fmt.Fprint(p.w, "\r"+strings.Repeat(" ", p.shown)+"\r")
	p.shown = 0
}

// terminal reports whether w is a terminal (a character device).
func terminal(w io.Writer) bool {
	f, ok := w.(*os.File)
	if !ok {
		return false
	}
	st, err := f.Stat()
	return err == nil && st.Mode()&os.ModeCharDevice != 0
}

// percent is a progress event's share done, 0 to 100.
func percent(p cleanup.Progress) int {
	if p.Total <= 0 {
		return 0
	}
	return min(p.Done*100/p.Total, 100)
}

// size writes a byte count as the file manager does.
func size(n int64) string { return format.Bytes(n) }
