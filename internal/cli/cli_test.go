package cli

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"path/filepath"
	"slices"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/schedule"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/engine/undo"
)

const mb = 1_000_000

// now is the fixed clock of the tests.
var now = time.Date(2026, 11, 15, 10, 0, 0, 0, time.UTC)

// fakeEngine is an Engine over a fixed scan that records what the commands ask of it.
type fakeEngine struct {
	scan    cleanup.ScanResult
	scanErr error
	// cancel, when set, is called during the scan (Ctrl+C while checking).
	cancel func()
	apps   []string
	set    settings.AppSettings

	cleans   []cleanup.CleanRequest
	opts     []app.CleanOptions
	cleanRun app.CleanRun
	cleanErr error

	runs       []history.Run
	restorable map[string]bool
	entries    map[string]undo.Entry
	restores   []string
	restoreOut app.RestoreOutcome
	restoreErr error
}

func (f *fakeEngine) Scan(ctx context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error) {
	progress(cleanup.Progress{Phase: cleanup.PhaseScanning, Done: 500, Total: 1000})
	if f.cancel != nil {
		f.cancel()
		return cleanup.ScanResult{}, errors.New("已取消")
	}
	return f.scan, f.scanErr
}

func (f *fakeEngine) InstalledApps(context.Context) ([]string, error) { return f.apps, nil }

func (f *fakeEngine) Settings() settings.AppSettings { return f.set }

func (f *fakeEngine) Clean(_ context.Context, req cleanup.CleanRequest, opts app.CleanOptions, _ func(cleanup.Progress)) (app.CleanRun, error) {
	f.cleans = append(f.cleans, req)
	f.opts = append(f.opts, opts)
	return f.cleanRun, f.cleanErr
}

func (f *fakeEngine) History(limit int) ([]history.Run, error) {
	return f.runs[:min(limit, len(f.runs))], nil
}

func (f *fakeEngine) Restorable([]history.Run, time.Time) map[string]bool { return f.restorable }

func (f *fakeEngine) Entry(runID string) (undo.Entry, error) {
	e, ok := f.entries[runID]
	if !ok {
		return undo.Entry{}, undo.ErrNoEntry
	}
	return e, nil
}

func (f *fakeEngine) Restore(_ context.Context, runID string) (app.RestoreOutcome, error) {
	f.restores = append(f.restores, runID)
	return f.restoreOut, f.restoreErr
}

// home is the fake home folder of the scan's paths.
var home = filepath.FromSlash("/Users/u")

func path(rel string) string { return filepath.Join(home, filepath.FromSlash(rel)) }

// item is a scanned item: size in MB, last changed days ago.
func item(rel string, sizeMB int64, days int, selected bool) cleanup.Item {
	p := path(rel)
	return cleanup.Item{ID: cleanup.ItemID(p), Path: p, Size: sizeMB * mb, ModTime: now.Add(-time.Duration(days) * 24 * time.Hour), Selected: selected}
}

// fixture is a merged scan of the three sources: caches deleted for good, a download moved to the Trash, a command cache, build output of a clean and of a dirty project, and an installer.
func fixture() cleanup.ScanResult {
	cleanProject, dirtyProject := path("code/web"), path("code/api")
	built := item("code/web/node_modules", 500, 120, true)
	built.Project, built.Git, built.Note = cleanProject, cleanup.GitClean, "web"
	dirty := item("code/api/node_modules", 600, 150, false)
	dirty.Project, dirty.Git, dirty.Note, dirty.Hold = dirtyProject, cleanup.GitDirty, "api · 有未提交的改动", "项目有未提交的改动"
	oldCache, newCache := item("Library/Caches/com.old", 300, 40, true), item("Library/Caches/com.new", 200, 2, true)
	oldCache.Dir, newCache.Dir = true, true
	cats := []cleanup.Category{
		{ID: "system-junk:app-caches", Title: "应用缓存", Icon: "cache", Kind: cleanup.KindCache, Deletion: cleanup.DeletePermanent, Source: "system-junk", Items: []cleanup.Item{oldCache, newCache}},
		{ID: "system-junk:incomplete-downloads", Title: "未完成的下载", Icon: "downloads", Kind: cleanup.KindCache, Deletion: cleanup.DeleteTrash, Source: "system-junk", Items: []cleanup.Item{item("Downloads/x.crdownload", 150, 9, true)}},
		{ID: "system-junk:trash", Title: "废纸篓", Icon: "trash", Kind: cleanup.KindTrash, Deletion: cleanup.DeletePermanent, Source: "system-junk", Items: []cleanup.Item{}},
		{ID: "dev-junk:tool-pnpm", Title: "pnpm 存储", Icon: "node", Kind: cleanup.KindDevCache, Deletion: cleanup.DeletePermanent, Command: "pnpm store prune", Description: "运行 pnpm store prune：删除没有项目引用的包", Source: "dev-junk", Items: []cleanup.Item{item("Library/pnpm/store", 400, 5, true)}},
		{ID: "dev-junk:artifact-node_modules", Title: "node_modules", Icon: "node", Kind: cleanup.KindProjectArtifact, Deletion: cleanup.DeletePermanent, Source: "dev-junk", Items: []cleanup.Item{built, dirty}},
		{ID: "installers:disk-images", Title: "磁盘映像", Icon: "installer", Kind: cleanup.KindInstaller, Deletion: cleanup.DeleteTrash, Source: "installers", Items: []cleanup.Item{item("Downloads/Tool.dmg", 200, 30, true)}},
	}
	res := cleanup.NewResult(cats, now)
	res.Sources = []cleanup.SourceState{{ID: "system-junk", Status: cleanup.SourceDone}, {ID: "dev-junk", Status: cleanup.SourceDone}, {ID: "installers", Status: cleanup.SourceDone}}
	return res
}

// harness runs Main over a fake engine.
type harness struct {
	eng     *fakeEngine
	sources [][]string
	built   int
	d       Deps
}

func newHarness() *harness {
	h := &harness{eng: &fakeEngine{scan: fixture(), set: settings.Defaults(home)}}
	h.d = Deps{
		Engine: func(sources []string) (Engine, error) {
			h.built++
			h.sources = append(h.sources, sources)
			return h.eng, nil
		},
		Version:    "2.0.0",
		Home:       home,
		GOOS:       "darwin",
		CanRestore: true,
		Now:        func() time.Time { return now },
	}
	return h
}

// run runs args and returns the exit code, stdout and stderr.
func (h *harness) run(t *testing.T, args ...string) (int, string, string) {
	t.Helper()
	return h.runCtx(t, context.Background(), args...)
}

func (h *harness) runCtx(t *testing.T, ctx context.Context, args ...string) (int, string, string) {
	t.Helper()
	var out, errOut bytes.Buffer
	code := Main(ctx, args, h.d, &out, &errOut)
	return code, out.String(), errOut.String()
}

// decode reads the one JSON object a command wrote.
func decode(t *testing.T, s string) map[string]any {
	t.Helper()
	var m map[string]any
	dec := json.NewDecoder(strings.NewReader(s))
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("stdout is not one JSON object: %v\n%s", err, s)
	}
	if dec.More() {
		t.Fatalf("stdout holds more than one JSON value:\n%s", s)
	}
	if m["version"] != float64(1) {
		t.Fatalf("version = %v, want 1", m["version"])
	}
	return m
}

func keys(m map[string]any) []string {
	var out []string
	for k := range m {
		out = append(out, k)
	}
	slices.Sort(out)
	return out
}

func TestIsCommand(t *testing.T) {
	for _, args := range [][]string{{"scan"}, {"recommendations"}, {"list"}, {"clean", "--yes"}, {"projects"}, {"history"}, {"restore", "x"}, {"help"}, {"version"}, {"--help"}, {"-h"}} {
		if !IsCommand(args) {
			t.Errorf("IsCommand(%q) = false", args)
		}
	}
	for _, args := range [][]string{nil, {}, {""}, {"--login"}, {"--nsgo-dev"}, {"-psn_0_4242"}, {"--spruce-elevated", "--tasks", "dns"}, {"Scan"}, {"/Users/u/Downloads/a.dmg"}, {"--json"}, {"--version"}} {
		if IsCommand(args) {
			t.Errorf("IsCommand(%q) = true", args)
		}
	}
}

func TestUnknownCommand(t *testing.T) {
	h := newHarness()
	code, _, errOut := h.run(t, "frobnicate")
	if code != ExitUsage || !strings.Contains(errOut, "未知的命令") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if h.built != 0 {
		t.Fatal("a usage error built the engine")
	}
}

func TestScanText(t *testing.T) {
	h := newHarness()
	code, out, errOut := h.run(t, "scan")
	if code != ExitOK {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if len(h.sources) != 1 || h.sources[0] != nil {
		t.Fatalf("sources %q, want all", h.sources)
	}
	for _, want := range []string{"类别", "system-junk:app-caches", "应用缓存", "500 MB", "dev-junk:artifact-node_modules", "1.1 GB", "共 2.4 GB，7 项"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	// The empty Trash category is not listed.
	if strings.Contains(out, "system-junk:trash") {
		t.Errorf("an empty category is listed:\n%s", out)
	}
	// Progress goes to a terminal only.
	if errOut != "" {
		t.Errorf("stderr %q, want nothing", errOut)
	}
}

func TestScanColumnsAlign(t *testing.T) {
	h := newHarness()
	_, out, _ := h.run(t, "scan")
	lines := strings.Split(strings.TrimSpace(out), "\n")
	// The size column ends at the same cell on every row of the table, wide characters counting two.
	var ends []int
	for _, l := range lines[1:] {
		if l == "" {
			break
		}
		f := strings.Fields(l)
		cut := strings.LastIndex(l, f[len(f)-1])
		ends = append(ends, cells(strings.TrimRight(l[:cut], " ")))
	}
	for _, e := range ends {
		if e != ends[0] {
			t.Fatalf("size column ends at %v:\n%s", ends, out)
		}
	}
}

func TestScanSources(t *testing.T) {
	h := newHarness()
	if code, _, _ := h.run(t, "scan", "--source", "installers", "--source=dev-junk,installers"); code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if want := []string{"dev-junk", "installers"}; !slices.Equal(h.sources[0], want) {
		t.Fatalf("sources %q, want %q", h.sources[0], want)
	}
	code, _, errOut := h.run(t, "scan", "--source", "photos")
	if code != ExitUsage || !strings.Contains(errOut, "未知的来源") {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if h.built != 1 {
		t.Fatal("a usage error built the engine")
	}
}

func TestScanJSON(t *testing.T) {
	h := newHarness()
	code, out, _ := h.run(t, "scan", "--json")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	m := decode(t, out)
	if got := keys(m); !slices.Equal(got, []string{"command", "scan", "version"}) {
		t.Fatalf("keys %q", got)
	}
	if m["command"] != "scan" {
		t.Fatalf("command %v", m["command"])
	}
	scan := m["scan"].(map[string]any)
	if n := len(scan["categories"].([]any)); n != 6 {
		t.Fatalf("%d categories", n)
	}
}

func TestScanFailures(t *testing.T) {
	t.Run("a source failed", func(t *testing.T) {
		h := newHarness()
		h.eng.scan.Sources[2] = cleanup.SourceState{ID: "installers", Status: cleanup.SourceFailed, Error: "无法读取下载文件夹"}
		h.eng.scan.Denied = 2
		code, _, errOut := h.run(t, "scan")
		if code != ExitPartial {
			t.Fatalf("code %d", code)
		}
		for _, want := range []string{"安装包没能检查完成：无法读取下载文件夹", "有 2 个位置没有读取权限"} {
			if !strings.Contains(errOut, want) {
				t.Errorf("stderr lacks %q: %q", want, errOut)
			}
		}
	})
	t.Run("every source failed", func(t *testing.T) {
		h := newHarness()
		h.eng.scanErr = errors.New("扫描没能完成，详情已记录到日志")
		code, out, errOut := h.run(t, "scan")
		if code != ExitFailed || out != "" || !strings.Contains(errOut, "扫描没能完成") {
			t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
		}
		code, out, _ = h.run(t, "scan", "--json")
		if code != ExitFailed {
			t.Fatalf("code %d", code)
		}
		m := decode(t, out)
		if got := keys(m); !slices.Equal(got, []string{"error", "version"}) || m["error"] != "扫描没能完成，详情已记录到日志" {
			t.Fatalf("error object %v", m)
		}
	})
	t.Run("interrupted", func(t *testing.T) {
		h := newHarness()
		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()
		h.eng.cancel = cancel
		code, _, errOut := h.runCtx(t, ctx, "scan")
		if code != ExitInterrupted || !strings.Contains(errOut, "已中断") {
			t.Fatalf("code %d, stderr %q", code, errOut)
		}
	})
}

func TestRecommendations(t *testing.T) {
	h := newHarness()
	code, out, _ := h.run(t, "list")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"可以放心释放", "条建议", "app-caches", "应用缓存", "直接删除", "projects-idle", "tool-pnpm", "运行 pnpm store prune", "installers", "移到废纸篓", "1 个有未提交改动的项目没有选入"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(out, "{time}") {
		t.Errorf("a reason keeps its time token:\n%s", out)
	}

	code, out, _ = h.run(t, "recommendations", "--json")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	m := decode(t, out)
	if got := keys(m); !slices.Equal(got, []string{"command", "headline", "recommendations", "scan", "version"}) {
		t.Fatalf("keys %q", got)
	}
	if m["command"] != "recommendations" {
		t.Fatalf("command %v", m["command"])
	}
	for _, r := range m["recommendations"].([]any) {
		if reason := r.(map[string]any)["reason"].(string); strings.Contains(reason, "{time}") {
			t.Errorf("JSON reason %q keeps its time token", reason)
		}
	}
}

func TestRecommendationsEmpty(t *testing.T) {
	h := newHarness()
	h.eng.scan = cleanup.NewResult(nil, now)
	code, out, _ := h.run(t, "recommendations", "--json")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if recs, ok := decode(t, out)["recommendations"].([]any); !ok || len(recs) != 0 {
		t.Fatalf("recommendations %v, want []", recs)
	}
}

func TestCleanWithoutYesIsADryRun(t *testing.T) {
	h := newHarness()
	code, out, errOut := h.run(t, "clean")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if len(h.eng.cleans) != 0 {
		t.Fatal("clean without --yes cleaned")
	}
	if !strings.Contains(errOut, "没有清理：加 --yes 才会执行") {
		t.Errorf("stderr %q", errOut)
	}
	for _, want := range []string{"将清理", "应用缓存（system-junk:app-caches）· 直接删除，无法撤销", "~/Library/Caches/com.old", "未完成的下载（system-junk:incomplete-downloads）· 移到废纸篓", "~/code/web/node_modules"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	// The default runs no command and never takes a held item.
	for _, not := range []string{"pnpm", "code/api"} {
		if strings.Contains(out, not) {
			t.Errorf("stdout has %q:\n%s", not, out)
		}
	}

	code, _, errOut = h.run(t, "clean", "--dry-run")
	if code != ExitOK || strings.Contains(errOut, "--yes") || len(h.eng.cleans) != 0 {
		t.Fatalf("--dry-run: code %d, stderr %q, cleans %d", code, errOut, len(h.eng.cleans))
	}
}

func TestCleanUsage(t *testing.T) {
	for name, args := range map[string][]string{
		"yes and dry run":        {"clean", "--yes", "--dry-run"},
		"unknown card":           {"clean", "everything"},
		"unknown category":       {"clean", "--category", "app-caches"},
		"unknown source of cat":  {"clean", "--category", "photos:all"},
		"category outside":       {"clean", "--source", "installers", "--category", "system-junk:logs"},
		"bad age":                {"clean", "--older-than", "30"},
		"zero age":               {"clean", "--older-than", "0d"},
		"unknown flag":           {"clean", "--force"},
		"flag without its value": {"clean", "--category"},
	} {
		t.Run(name, func(t *testing.T) {
			h := newHarness()
			code, _, errOut := h.run(t, args...)
			if code != ExitUsage {
				t.Fatalf("code %d, stderr %q", code, errOut)
			}
			if h.built != 0 {
				t.Fatal("a usage error built the engine")
			}
			if !strings.Contains(errOut, "spruce help clean") {
				t.Errorf("stderr %q does not point at the help", errOut)
			}
		})
	}
}

func TestCleanUsageJSON(t *testing.T) {
	// A usage error is a JSON error even when the bad flag comes before --json.
	for _, args := range [][]string{{"clean", "--json", "--yes", "--dry-run"}, {"clean", "--force", "--json"}, {"history", "--limit", "0", "--json"}} {
		h := newHarness()
		code, out, errOut := h.run(t, args...)
		if code != ExitUsage || errOut != "" {
			t.Fatalf("%q: code %d, stderr %q", args, code, errOut)
		}
		if m := decode(t, out); !slices.Equal(keys(m), []string{"error", "version"}) {
			t.Fatalf("%q: error object %v", args, m)
		}
	}
}

func TestCleanYes(t *testing.T) {
	h := newHarness()
	h.eng.cleanRun = app.CleanRun{
		Result: cleanup.CleanResult{Removed: 5, Freed: 800 * mb, Trashed: 350 * mb, Moved: []cleanup.Moved{{Path: path("Downloads/Tool.dmg")}, {Path: path("Downloads/x.crdownload")}}},
		RunID:  "20261115T100000Z-abcd",
		Undo:   &history.UndoInfo{Items: 2, Bytes: 350 * mb},
	}
	code, out, errOut := h.run(t, "clean", "--yes")
	if code != ExitOK {
		t.Fatalf("code %d, stderr %q", code, errOut)
	}
	if len(h.eng.cleans) != 1 {
		t.Fatalf("%d cleans", len(h.eng.cleans))
	}
	req := h.eng.cleans[0]
	if want := []string{"system-junk:app-caches", "system-junk:incomplete-downloads", "dev-junk:artifact-node_modules", "installers:disk-images"}; !slices.Equal(req.CategoryIDs, want) {
		t.Fatalf("categories %q, want %q", req.CategoryIDs, want)
	}
	if want := []string{cleanup.ItemID(path("Library/Caches/com.old")), cleanup.ItemID(path("Library/Caches/com.new")), cleanup.ItemID(path("Downloads/x.crdownload")), cleanup.ItemID(path("code/web/node_modules")), cleanup.ItemID(path("Downloads/Tool.dmg"))}; !slices.Equal(req.ItemIDs, want) {
		t.Fatalf("items %q, want %q", req.ItemIDs, want)
	}
	if req.Mode != cleanup.ModeDefault {
		t.Fatalf("mode %q", req.Mode)
	}
	if opts := h.eng.opts[0]; opts.Detail != "命令行" || opts.Kind != "" || opts.Rule != "" {
		t.Fatalf("options %+v", opts)
	}
	for _, want := range []string{"已清理 5 项，释放 800 MB，移到废纸篓 350 MB", "已记入操作历史：20261115T100000Z-abcd", "spruce restore 20261115T100000Z-abcd"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	if strings.Contains(errOut, "--yes") {
		t.Errorf("stderr %q", errOut)
	}
	// The plan, with how each category goes, is printed before the result.
	planAt, doneAt := strings.Index(out, "将清理 5 项"), strings.Index(out, "已清理 5 项")
	if planAt < 0 || doneAt < planAt || !strings.Contains(out, "移到废纸篓") || !strings.Contains(out, "~/Downloads/Tool.dmg") {
		t.Errorf("stdout lacks the plan before the result:\n%s", out)
	}
}

func TestCleanNamed(t *testing.T) {
	t.Run("a command card runs only when named", func(t *testing.T) {
		h := newHarness()
		if code, _, _ := h.run(t, "clean", "tool-pnpm", "--yes"); code != ExitOK {
			t.Fatalf("code %d", code)
		}
		if req := h.eng.cleans[0]; !slices.Equal(req.CategoryIDs, []string{"dev-junk:tool-pnpm"}) || len(req.ItemIDs) != 1 {
			t.Fatalf("request %+v", req)
		}
	})
	t.Run("flags after the card", func(t *testing.T) {
		h := newHarness()
		if code, _, _ := h.run(t, "clean", "app-caches", "--yes", "--source", "system-junk"); code != ExitOK {
			t.Fatalf("code %d", code)
		}
		if !slices.Equal(h.sources[0], []string{"system-junk"}) || !slices.Equal(h.eng.cleans[0].CategoryIDs, []string{"system-junk:app-caches"}) {
			t.Fatalf("sources %q, request %+v", h.sources[0], h.eng.cleans[0])
		}
	})
	t.Run("a category with a held item", func(t *testing.T) {
		h := newHarness()
		h.eng.cleanRun = app.CleanRun{Result: cleanup.CleanResult{Removed: 1, Freed: 500 * mb}, RunID: "r1"}
		code, out, _ := h.run(t, "clean", "--category", "dev-junk:artifact-node_modules", "--yes")
		if code != ExitPartial {
			t.Fatalf("code %d", code)
		}
		req := h.eng.cleans[0]
		if !slices.Equal(req.ItemIDs, []string{cleanup.ItemID(path("code/web/node_modules"))}) {
			t.Fatalf("items %q: the held item was asked for", req.ItemIDs)
		}
		if strings.Count(out, "1 项不会清理") != 1 || !strings.Contains(out, "~/code/api/node_modules：项目有未提交的改动") {
			t.Errorf("stdout:\n%s", out)
		}
	})
	t.Run("only held items", func(t *testing.T) {
		h := newHarness()
		cat := &h.eng.scan.Categories[4]
		cat.Items = slices.DeleteFunc(cat.Items, func(it cleanup.Item) bool { return it.Hold == "" })
		code, out, _ := h.run(t, "clean", "--category", "dev-junk:artifact-node_modules", "--yes")
		if code != ExitPartial || len(h.eng.cleans) != 0 || !strings.Contains(out, "没有需要清理的内容") {
			t.Fatalf("code %d, cleans %d, stdout:\n%s", code, len(h.eng.cleans), out)
		}
		// As a dry run it succeeds.
		if code, _, _ := h.run(t, "clean", "--category", "dev-junk:artifact-node_modules"); code != ExitOK {
			t.Fatalf("dry run code %d", code)
		}
	})
	t.Run("older than", func(t *testing.T) {
		h := newHarness()
		if code, _, _ := h.run(t, "clean", "--category", "system-junk:app-caches", "--older-than", "30d", "--yes"); code != ExitOK {
			t.Fatalf("code %d", code)
		}
		if req := h.eng.cleans[0]; !slices.Equal(req.ItemIDs, []string{cleanup.ItemID(path("Library/Caches/com.old"))}) {
			t.Fatalf("items %q", req.ItemIDs)
		}
	})
	t.Run("a card this check did not find", func(t *testing.T) {
		h := newHarness()
		code, out, errOut := h.run(t, "clean", "logs", "--yes")
		if code != ExitOK || len(h.eng.cleans) != 0 {
			t.Fatalf("code %d, cleans %d", code, len(h.eng.cleans))
		}
		if !strings.Contains(errOut, "这次检查没有找到：logs") || !strings.Contains(out, "没有需要清理的内容") {
			t.Fatalf("stdout %q, stderr %q", out, errOut)
		}
	})
}

func TestCleanOutcomes(t *testing.T) {
	t.Run("failed items", func(t *testing.T) {
		h := newHarness()
		h.eng.cleanRun = app.CleanRun{Result: cleanup.CleanResult{Removed: 4, Failed: []cleanup.Failure{{Path: path("Library/Caches/com.new"), Reason: "文件正在使用，已跳过"}}, Partial: true}, RunID: "r2"}
		code, out, _ := h.run(t, "clean", "--yes")
		if code != ExitPartial || !strings.Contains(out, "1 项未能处理") || !strings.Contains(out, "~/Library/Caches/com.new：文件正在使用，已跳过") {
			t.Fatalf("code %d, stdout:\n%s", code, out)
		}
	})
	t.Run("busy", func(t *testing.T) {
		for _, cause := range []error{schedule.ErrBusyElsewhere, cleanup.ErrBusy} {
			h := newHarness()
			h.eng.cleanErr = fmt.Errorf("另一个 Spruce 正在清理，请稍后再试: %w", cause)
			code, _, errOut := h.run(t, "clean", "--yes")
			if code != ExitBusy || !strings.Contains(errOut, "Spruce 正在清理，请稍后再试") {
				t.Fatalf("%v: code %d, stderr %q", cause, code, errOut)
			}
		}
	})
	t.Run("refused", func(t *testing.T) {
		h := newHarness()
		h.eng.cleanErr = errors.New("扫描结果已过期，请重新扫描")
		if code, _, _ := h.run(t, "clean", "--yes"); code != ExitFailed {
			t.Fatalf("code %d", code)
		}
	})
	t.Run("interrupted", func(t *testing.T) {
		h := newHarness()
		ctx, cancel := context.WithCancel(context.Background())
		cancel()
		h.eng.cleanRun = app.CleanRun{Result: cleanup.CleanResult{Removed: 1, Interrupted: true, Partial: true}, RunID: "r3"}
		code, out, _ := h.runCtx(t, ctx, "clean", "--yes")
		if code != ExitInterrupted || !strings.Contains(out, "清理在中途停止") {
			t.Fatalf("code %d, stdout:\n%s", code, out)
		}
	})
	t.Run("nothing to clean", func(t *testing.T) {
		h := newHarness()
		h.eng.scan = cleanup.NewResult(nil, now)
		code, out, errOut := h.run(t, "clean", "--yes")
		if code != ExitOK || len(h.eng.cleans) != 0 || !strings.Contains(out, "没有需要清理的内容") || errOut != "" {
			t.Fatalf("code %d, stdout %q, stderr %q", code, out, errOut)
		}
	})
}

func TestCleanJSON(t *testing.T) {
	h := newHarness()
	code, out, _ := h.run(t, "clean", "--json")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	m := decode(t, out)
	if got := keys(m); !slices.Equal(got, []string{"command", "plan", "recommendations", "scan", "version"}) {
		t.Fatalf("dry run keys %q", got)
	}
	plan := m["plan"].(map[string]any)
	if plan["items"] != float64(5) || plan["trash"] != float64(350*mb) {
		t.Fatalf("plan %v", plan)
	}

	h.eng.cleanRun = app.CleanRun{Result: cleanup.CleanResult{Removed: 5, Freed: 800 * mb, Trashed: 350 * mb}, RunID: "r4", Undo: &history.UndoInfo{Items: 2, Bytes: 350 * mb}}
	code, out, _ = h.run(t, "clean", "--yes", "--json")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	m = decode(t, out)
	if got := keys(m); !slices.Equal(got, []string{"command", "plan", "recommendations", "result", "scan", "version"}) {
		t.Fatalf("keys %q", got)
	}
	result := m["result"].(map[string]any)
	if result["runId"] != "r4" || result["removed"] != float64(5) || result["undo"].(map[string]any)["items"] != float64(2) {
		t.Fatalf("result %v", result)
	}
}

func TestProjects(t *testing.T) {
	h := newHarness()
	code, out, _ := h.run(t, "projects")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if !slices.Equal(h.sources[0], []string{"dev-junk"}) {
		t.Fatalf("sources %q, want only dev-junk", h.sources[0])
	}
	for _, want := range []string{"项目", "api", "有未提交的改动", "web", "干净", "600 MB", "node_modules"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}
	code, out, _ = h.run(t, "projects", "--json")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	m := decode(t, out)
	projects := m["projects"].([]any)
	if len(projects) != 2 || projects[0].(map[string]any)["git"] != "dirty" {
		t.Fatalf("projects %v", projects)
	}
}

func TestProjectsEmpty(t *testing.T) {
	h := newHarness()
	h.eng.scan = cleanup.NewResult(nil, now)
	h.eng.set.Cleanup.DevProjectRoots = nil
	_, out, _ := h.run(t, "projects")
	if !strings.Contains(out, "还没有设置项目文件夹") {
		t.Fatalf("stdout %q", out)
	}
	h.eng.set.Cleanup.DevProjectRoots = []string{path("code")}
	_, out, _ = h.run(t, "projects")
	if !strings.Contains(out, "没有找到构建产物") {
		t.Fatalf("stdout %q", out)
	}
	code, out, _ := h.run(t, "projects", "--json")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	if p, ok := decode(t, out)["projects"].([]any); !ok || len(p) != 0 {
		t.Fatalf("projects %v, want []", p)
	}
}

// runs is a history: a card's clean that can be put back, a maintenance run, an automatic run.
func runs() []history.Run {
	return []history.Run{
		{ID: "20261115T090000Z-0001", Kind: history.KindSmartScan, StartedAt: now.Add(-time.Hour), EndedAt: now.Add(-time.Hour), Summary: &history.Totals{Freed: 800 * mb, Trashed: 350 * mb, Items: 5}, Undo: &history.UndoInfo{Items: 2, Bytes: 350 * mb}},
		{ID: "20261114T090000Z-0002", Kind: history.KindAuto, Rule: "app-caches", StartedAt: now.Add(-25 * time.Hour), EndedAt: now.Add(-25 * time.Hour), Summary: &history.Totals{Trashed: 2_100 * mb, Items: 12, Failed: 1}},
		{ID: "20261113T090000Z-0003", Kind: history.KindMaintenance, StartedAt: now.Add(-49 * time.Hour), EndedAt: now.Add(-49 * time.Hour)},
		{ID: "20261112T090000Z-0004", Kind: history.KindRestore, StartedAt: now.Add(-73 * time.Hour), Summary: &history.Totals{Items: 3, Trashed: 30 * mb}, RestoreOf: "x"},
	}
}

func TestHistory(t *testing.T) {
	h := newHarness()
	h.eng.runs = runs()
	h.eng.restorable = map[string]bool{"20261115T090000Z-0001": true}
	code, out, _ := h.run(t, "history")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	for _, want := range []string{"放回原处", "20261115T090000Z-0001", "清理建议", "释放 800 MB，移到废纸篓 350 MB，5 项", "可以", "旧的应用缓存", "1 项未能处理", "0 项维护任务", "放回 3 项（30 MB）"} {
		if !strings.Contains(out, want) {
			t.Errorf("stdout lacks %q:\n%s", want, out)
		}
	}

	code, out, _ = h.run(t, "history", "--limit", "2", "--json")
	if code != ExitOK {
		t.Fatalf("code %d", code)
	}
	list := decode(t, out)["runs"].([]any)
	if len(list) != 2 {
		t.Fatalf("%d runs, want 2", len(list))
	}
	first := list[0].(map[string]any)
	if first["restorable"] != true || first["title"] != "清理建议" || first["id"] != "20261115T090000Z-0001" {
		t.Fatalf("first run %v", first)
	}
	if list[1].(map[string]any)["restorable"] != false {
		t.Fatalf("second run %v", list[1])
	}

	for _, bad := range []string{"0", "501", "x"} {
		if code, _, _ := h.run(t, "history", "--limit", bad); code != ExitUsage {
			t.Errorf("--limit %s: code %d", bad, code)
		}
	}
}

func TestHistoryWindows(t *testing.T) {
	h := newHarness()
	h.d.GOOS, h.d.CanRestore = "windows", false
	h.eng.runs = runs()
	_, out, _ := h.run(t, "history")
	if strings.Contains(out, "放回原处\n") || strings.Contains(strings.SplitN(out, "\n", 2)[0], "放回原处") {
		t.Fatalf("Windows shows the 放回原处 column:\n%s", out)
	}
}

func entry() undo.Entry {
	return undo.Entry{Version: undo.Version, RunID: "20261115T090000Z-0001", At: now.Add(-time.Hour), Moves: []undo.Move{
		{Moved: cleanup.Moved{Path: path("Downloads/Tool.dmg"), Size: 200 * mb}},
		{Moved: cleanup.Moved{Path: path("Downloads/x.crdownload"), Size: 150 * mb}},
		{Moved: cleanup.Moved{Path: path("Downloads/back.dmg"), Size: 10 * mb}, Restored: true},
	}}
}

func TestRestoreDryRun(t *testing.T) {
	h := newHarness()
	h.eng.runs = runs()
	h.eng.restorable = map[string]bool{"20261115T090000Z-0001": true}
	h.eng.entries = map[string]undo.Entry{"20261115T090000Z-0001": entry()}
	code, out, errOut := h.run(t, "restore", "20261115T090000Z-0001")
	if code != ExitOK || len(h.eng.restores) != 0 {
		t.Fatalf("code %d, restores %d", code, len(h.eng.restores))
	}
	if !strings.Contains(out, "将放回原处 2 项，共 350 MB") || !strings.Contains(out, "~/Downloads/Tool.dmg") || strings.Contains(out, "back.dmg") {
		t.Fatalf("stdout:\n%s", out)
	}
	if !strings.Contains(errOut, "没有放回：加 --yes 才会执行") {
		t.Fatalf("stderr %q", errOut)
	}
	_, out, _ = h.run(t, "restore", "--json", "20261115T090000Z-0001")
	restore := decode(t, out)["restore"].(map[string]any)
	if restore["items"] != float64(2) || len(restore["moves"].([]any)) != 2 {
		t.Fatalf("restore %v", restore)
	}
}

func TestRestoreRefusals(t *testing.T) {
	h := newHarness()
	h.eng.runs = runs()
	h.eng.restorable = map[string]bool{}
	if code, _, _ := h.run(t, "restore"); code != ExitUsage {
		t.Fatalf("no ID: code %d", code)
	}
	if code, _, _ := h.run(t, "restore", "a", "b"); code != ExitUsage {
		t.Fatalf("two IDs: code %d", code)
	}
	if code, _, errOut := h.run(t, "restore", "nope"); code != ExitFailed || !strings.Contains(errOut, "没有找到记录") {
		t.Fatalf("unknown run: code %d, %q", code, errOut)
	}
	if code, _, errOut := h.run(t, "restore", "20261114T090000Z-0002"); code != ExitFailed || !strings.Contains(errOut, "已经不能放回原处") {
		t.Fatalf("not restorable: code %d, %q", code, errOut)
	}
	h.d.CanRestore = false
	if code, _, errOut := h.run(t, "restore", "20261115T090000Z-0001", "--yes"); code != ExitFailed || !strings.Contains(errOut, "不能放回原处") || len(h.eng.restores) != 0 {
		t.Fatalf("Windows: code %d, %q", code, errOut)
	}
}

func TestRestoreYes(t *testing.T) {
	h := newHarness()
	h.eng.restoreOut = app.RestoreOutcome{RunID: "r9", Restored: 2, Bytes: 350 * mb}
	code, out, _ := h.run(t, "restore", "20261115T090000Z-0001", "--yes")
	if code != ExitOK || !slices.Equal(h.eng.restores, []string{"20261115T090000Z-0001"}) || !strings.Contains(out, "已放回原处 2 项（350 MB）") {
		t.Fatalf("code %d, restores %q, stdout:\n%s", code, h.eng.restores, out)
	}

	h.eng.restoreOut = app.RestoreOutcome{RunID: "r10", Restored: 1, Failed: 1, Reasons: []string{"~/Downloads/x.crdownload：原位置已有同名项目"}}
	code, out, _ = h.run(t, "restore", "20261115T090000Z-0001", "--yes")
	if code != ExitPartial || !strings.Contains(out, "已放回 1 项，1 项没能放回") || !strings.Contains(out, "原位置已有同名项目") {
		t.Fatalf("code %d, stdout:\n%s", code, out)
	}

	h.eng.restoreErr = fmt.Errorf("另一个 Spruce 正在清理，请稍后再试: %w", schedule.ErrBusyElsewhere)
	if code, _, _ = h.run(t, "restore", "20261115T090000Z-0001", "--yes"); code != ExitBusy {
		t.Fatalf("busy: code %d", code)
	}

	h.eng.restoreErr = nil
	h.eng.restoreOut = app.RestoreOutcome{RunID: "r11", Restored: 2, Bytes: 350 * mb}
	code, out, _ = h.run(t, "restore", "20261115T090000Z-0001", "--yes", "--json")
	if code != ExitOK || decode(t, out)["restored"].(map[string]any)["restored"] != float64(2) {
		t.Fatalf("code %d, stdout:\n%s", code, out)
	}
}

func TestVersionAndHelp(t *testing.T) {
	h := newHarness()
	if code, out, _ := h.run(t, "version"); code != ExitOK || out != "Spruce 2.0.0\n" {
		t.Fatalf("code %d, stdout %q", code, out)
	}
	h.d.Version = ""
	if _, out, _ := h.run(t, "version"); out != "Spruce 开发版本\n" {
		t.Fatalf("stdout %q", out)
	}
	if _, out, _ := h.run(t, "version", "--json"); decode(t, out)["app"].(map[string]any)["development"] != true {
		t.Fatalf("stdout %q", out)
	}
	for _, args := range [][]string{{"help"}, {"--help"}, {"-h"}} {
		if code, out, _ := h.run(t, args...); code != ExitOK || !strings.Contains(out, "用法：spruce <命令>") {
			t.Fatalf("%q: code %d, stdout %q", args, code, out)
		}
	}
	for _, args := range [][]string{{"help", "clean"}, {"clean", "--help"}, {"clean", "-h"}} {
		if code, out, _ := h.run(t, args...); code != ExitOK || !strings.Contains(out, "用法：spruce clean") {
			t.Fatalf("%q: code %d, stdout %q", args, code, out)
		}
	}
	if code, out, _ := h.run(t, "help", "list"); code != ExitOK || !strings.Contains(out, "用法：spruce recommendations") {
		t.Fatalf("help list: code %d, stdout %q", code, out)
	}
	if code, _, _ := h.run(t, "help", "frobnicate"); code != ExitUsage {
		t.Fatalf("help of an unknown command: code %d", code)
	}
	if h.built != 0 {
		t.Fatal("help or version built the engine")
	}
}
