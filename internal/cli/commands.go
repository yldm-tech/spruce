package cli

import (
	"errors"
	"flag"
	"fmt"
	"slices"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/advice"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/devjunk"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/rules"
	"github.com/yldm-tech/spruce/internal/ui/format"
)

// sourceTitles are the sources' names, as 概览's 按类别查看 shows them.
var sourceTitles = map[string]string{
	"system-junk": "系统垃圾",
	"dev-junk":    "开发者",
	"installers":  "安装包",
}

// cliDetail is the first Details line of a clean the command line ran.
const cliDetail = "命令行"

// listLimit is how many items of a category, or of a restore, the text output lists; --json lists them all.
const listLimit = 10

// flagsDone handles what parse returned: help for -h, a usage error otherwise. It reports whether the command should stop, with its exit code.
func (r *run) flagsDone(err error) (int, bool) {
	switch {
	case err == nil:
		return 0, false
	case errors.Is(err, flag.ErrHelp):
		return r.help([]string{r.command}), true
	}
	return r.usage(err, r.command), true
}

// noArgs refuses positional arguments of a command that takes none.
func (r *run) noArgs(pos []string) (int, bool) {
	if len(pos) == 0 {
		return 0, false
	}
	return r.usage(fmt.Errorf("多余的参数 %q", pos[0]), r.command), true
}

// check runs the merged scan with progress on stderr and says on stderr what the result misses. partial is set when a source failed.
func (r *run) check(eng Engine) (res cleanup.ScanResult, partial bool, err error) {
	p := r.newProgress()
	p.show("正在检查…")
	res, err = eng.Scan(r.ctx, func(ev cleanup.Progress) {
		p.show(fmt.Sprintf("正在检查… %d%%", percent(ev)))
	})
	p.clear()
	if err != nil {
		return res, false, err
	}
	for _, s := range res.Sources {
		if s.Status == cleanup.SourceFailed {
			partial = true
			r.note("%s没能检查完成：%s", sourceTitles[s.ID], s.Error)
		}
	}
	if res.Denied > 0 {
		r.note("有 %d 个位置没有读取权限，结果可能不完整", res.Denied)
	}
	return res, partial, nil
}

// cards derives the recommendation cards of res, their reasons written out.
func (r *run) cards(eng Engine, res cleanup.ScanResult) []advice.Recommendation {
	var apps []string
	if hasItems(res, "installers") {
		var err error
		if apps, err = eng.InstalledApps(r.ctx); err != nil {
			apps = nil
		}
	}
	now := r.d.Now()
	recs := advice.Derive(res, advice.Inputs{Now: now, InstalledApps: apps, Windows: r.d.GOOS == "windows", Home: r.d.Home, InstallerRoots: eng.Settings().Cleanup.InstallerRoots})
	for i := range recs {
		recs[i].Reason = recs[i].ReasonWith(func(t time.Time, kind advice.TimeKind) string {
			if kind == advice.TimeDate {
				return format.MonthDay(t, now)
			}
			return format.RelativeTime(t, now)
		})
	}
	return recs
}

// hasItems reports whether a category of source has items in res.
func hasItems(res cleanup.ScanResult, source string) bool {
	for _, c := range res.Categories {
		if len(c.Items) > 0 && (c.Source == source || strings.HasPrefix(c.ID, source+":")) {
			return true
		}
	}
	return false
}

// done is the exit code of a command that ran to its end: ExitInterrupted once ctx ended, ExitPartial when partial.
func (r *run) done(partial bool) int {
	switch {
	case r.ctx.Err() != nil:
		return ExitInterrupted
	case partial:
		return ExitPartial
	}
	return ExitOK
}

func (r *run) scan(args []string) int {
	fs := newFlags("scan")
	var sources list
	fs.Var(&sources, "source", "")
	fs.BoolVar(&r.json, "json", r.json, "")
	pos, err := parse(fs, args)
	if code, stop := r.flagsDone(err); stop {
		return code
	}
	if code, stop := r.noArgs(pos); stop {
		return code
	}
	if err := checkSources(sources); err != nil {
		return r.usage(err, r.command)
	}
	eng, err := r.engine(sourcesOrNil(sources))
	if err != nil {
		return r.fail(err)
	}
	res, partial, err := r.check(eng)
	if err != nil {
		return r.fail(err)
	}
	if r.json {
		r.writeJSON(output{Scan: &res})
		return r.done(partial)
	}
	t := &table{right: map[int]bool{2: true, 3: true}}
	t.add("类别", "名称", "大小", "项数")
	for _, c := range res.Categories {
		if len(c.Items) > 0 {
			t.add(c.ID, c.Title, size(c.Size), fmt.Sprint(len(c.Items)))
		}
	}
	if len(t.rows) == 1 {
		fmt.Fprintln(r.stdout, "没有找到可以清理的内容")
		return r.done(partial)
	}
	t.write(r.stdout)
	fmt.Fprintf(r.stdout, "\n共 %s，%d 项\n", size(res.Size), res.Items)
	return r.done(partial)
}

func (r *run) recommendations(args []string) int {
	fs := newFlags("recommendations")
	fs.BoolVar(&r.json, "json", r.json, "")
	pos, err := parse(fs, args)
	if code, stop := r.flagsDone(err); stop {
		return code
	}
	if code, stop := r.noArgs(pos); stop {
		return code
	}
	eng, err := r.engine(nil)
	if err != nil {
		return r.fail(err)
	}
	res, partial, err := r.check(eng)
	if err != nil {
		return r.fail(err)
	}
	recs := r.cards(eng, res)
	headline := advice.Headline(recs)
	if r.json {
		r.writeJSON(output{Scan: &res, Recommendations: recs, Headline: &headline})
		return r.done(partial)
	}
	if len(recs) == 0 {
		fmt.Fprintln(r.stdout, "没有需要清理的内容")
		return r.done(partial)
	}
	fmt.Fprintf(r.stdout, "可以放心释放 %s，%d 条建议\n\n", size(headline), len(recs))
	t := &table{right: map[int]bool{2: true}}
	t.add("建议", "标题", "大小", "动作", "理由")
	for _, rec := range recs {
		reason := rec.Reason
		if rec.Reason2 != "" {
			reason += "；" + rec.Reason2
		}
		t.add(rec.ID, rec.Title, size(rec.Bytes), actionText(rec), reason)
	}
	t.write(r.stdout)
	return r.done(partial)
}

// actionText is what acting on a card does.
func actionText(rec advice.Recommendation) string {
	switch rec.Action {
	case advice.ActionTrash:
		return "移到" + format.TrashName()
	case advice.ActionDelete:
		return "直接删除"
	case advice.ActionCommand:
		return "运行 " + rec.Command
	}
	return "部分无法撤销"
}

func (r *run) clean(args []string) int {
	fs := newFlags("clean")
	var sources, categories list
	var older age
	var yes, dryRun bool
	fs.Var(&categories, "category", "")
	fs.Var(&sources, "source", "")
	fs.Var(&older, "older-than", "")
	fs.BoolVar(&yes, "yes", false, "")
	fs.BoolVar(&dryRun, "dry-run", false, "")
	fs.BoolVar(&r.json, "json", r.json, "")
	pos, err := parse(fs, args)
	if code, stop := r.flagsDone(err); stop {
		return code
	}
	if yes && dryRun {
		return r.usage(errors.New("--yes 和 --dry-run 不能同时使用"), r.command)
	}
	if err := checkSources(sources); err != nil {
		return r.usage(err, r.command)
	}
	for _, id := range pos {
		if !advice.KnownID(id) {
			return r.usage(fmt.Errorf("未知的建议 %q，运行 spruce recommendations 可以看到这次的建议", id), r.command)
		}
	}
	for _, id := range categories {
		src, rest, ok := strings.Cut(id, ":")
		if !ok || rest == "" || !slices.Contains(sourceIDs, src) {
			return r.usage(fmt.Errorf("未知的类别 %q，类别写成 来源:类别，运行 spruce scan 可以看到", id), r.command)
		}
		if len(sources) > 0 && !slices.Contains(sources, src) {
			return r.usage(fmt.Errorf("类别 %q 不在 --source 指定的来源里", id), r.command)
		}
	}
	sel := Selection{Cards: pos, Categories: categories, OlderThan: time.Duration(older)}

	eng, err := r.engine(sourcesOrNil(sources))
	if err != nil {
		return r.fail(err)
	}
	res, partial, err := r.check(eng)
	if err != nil {
		return r.fail(err)
	}
	recs := r.cards(eng, res)
	plan := buildPlan(res, recs, sel, r.d.Now())
	if len(plan.Missing) > 0 {
		r.note("这次检查没有找到：%s", strings.Join(plan.Missing, "、"))
	}
	held := len(plan.Held) > 0
	if !yes || plan.Empty() {
		if r.json {
			r.writeJSON(output{Scan: &res, Recommendations: recs, Plan: &plan})
		} else {
			r.printPlan(plan)
		}
		if !yes && !dryRun && !plan.Empty() {
			r.note("没有清理：加 --yes 才会执行")
		}
		// Nothing was cleaned: a dry run succeeds whatever it would hold back; --yes with only held items did part of nothing.
		return r.done(partial || (yes && held))
	}

	p := r.newProgress()
	p.show("正在清理…")
	run, err := eng.Clean(r.ctx, plan.Request, app.CleanOptions{Detail: cliDetail}, func(ev cleanup.Progress) {
		if ev.Total > 0 {
			p.show(fmt.Sprintf("正在清理… 已处理 %d/%d", ev.Done, ev.Total))
		}
	})
	p.clear()
	if err != nil {
		return r.fail(err)
	}
	out := CleanResult{CleanResult: run.Result, RunID: run.RunID, Undo: run.Undo}
	if r.json {
		r.writeJSON(output{Scan: &res, Recommendations: recs, Plan: &plan, Result: &out})
	} else {
		if held {
			r.printHeld(plan.Held)
		}
		r.printResult(out)
	}
	return r.done(partial || held || len(run.Result.Failed) > 0 || run.Result.Interrupted)
}

// deletionText is how a plan's category is cleaned.
func deletionText(c PlanCategory) string {
	switch {
	case c.Command != "":
		return "运行 " + c.Command
	case c.Kind == cleanup.KindTrash:
		return "清空" + format.TrashName() + "，无法恢复"
	case c.Deletion == cleanup.DeleteTrash:
		return "移到" + format.TrashName()
	}
	return "直接删除，无法撤销"
}

// printPlan writes what a clean would do.
func (r *run) printPlan(plan Plan) {
	if plan.Empty() {
		fmt.Fprintln(r.stdout, "没有需要清理的内容")
		if len(plan.Held) > 0 {
			r.printHeld(plan.Held)
		}
		return
	}
	head := fmt.Sprintf("将清理 %d 项，共 %s", plan.Items, size(plan.Bytes))
	if plan.Trash > 0 && plan.Trash < plan.Bytes {
		head += fmt.Sprintf("，其中 %s 移到%s", size(plan.Trash), format.TrashName())
	}
	fmt.Fprintln(r.stdout, head)
	for _, c := range plan.Categories {
		fmt.Fprintf(r.stdout, "\n%s（%s）· %s · %d 项 · %s\n", c.Title, c.ID, deletionText(c), len(c.Items), size(c.Bytes))
		t := &table{right: map[int]bool{0: true}}
		for i, it := range c.Items {
			if i == listLimit {
				break
			}
			t.add("  "+size(it.Size), r.short(it.Path))
		}
		t.write(r.stdout)
		if n := len(c.Items) - listLimit; n > 0 {
			fmt.Fprintf(r.stdout, "  另有 %d 项\n", n)
		}
	}
	if len(plan.Held) > 0 {
		fmt.Fprintln(r.stdout)
		r.printHeld(plan.Held)
	}
}

// printHeld writes the items a selection named that are never cleaned.
func (r *run) printHeld(items []PlanItem) {
	fmt.Fprintf(r.stdout, "%d 项不会清理：\n", len(items))
	for i, it := range items {
		if i == listLimit {
			fmt.Fprintf(r.stdout, "  另有 %d 项\n", len(items)-listLimit)
			break
		}
		fmt.Fprintf(r.stdout, "  %s：%s\n", r.short(it.Path), it.Hold)
	}
}

// printResult writes what a clean did.
func (r *run) printResult(out CleanResult) {
	res := out.CleanResult
	parts := []string{fmt.Sprintf("已清理 %d 项", res.Removed)}
	if res.Freed > 0 {
		parts = append(parts, "释放 "+size(res.Freed))
	}
	if res.Trashed > 0 {
		parts = append(parts, fmt.Sprintf("移到%s %s", format.TrashName(), size(res.Trashed)))
	}
	if len(res.Failed) > 0 {
		parts = append(parts, fmt.Sprintf("%d 项未能处理", len(res.Failed)))
	}
	fmt.Fprintln(r.stdout, strings.Join(parts, "，"))
	for i, f := range res.Failed {
		if i == listLimit {
			fmt.Fprintf(r.stdout, "  另有 %d 项未能处理\n", len(res.Failed)-listLimit)
			break
		}
		if f.Path == "" {
			fmt.Fprintf(r.stdout, "  %s\n", f.Reason)
			continue
		}
		fmt.Fprintf(r.stdout, "  %s：%s\n", r.short(f.Path), f.Reason)
	}
	if res.Interrupted {
		fmt.Fprintln(r.stdout, "清理在中途停止，其余项目没有改动")
	}
	if out.RunID != "" {
		fmt.Fprintf(r.stdout, "已记入操作历史：%s\n", out.RunID)
	}
	if out.Undo != nil && out.RunID != "" {
		fmt.Fprintf(r.stdout, "需要时可以运行 spruce restore %s 放回原处（%d 项）\n", out.RunID, out.Undo.Items)
	}
}

func (r *run) projects(args []string) int {
	fs := newFlags("projects")
	fs.BoolVar(&r.json, "json", r.json, "")
	pos, err := parse(fs, args)
	if code, stop := r.flagsDone(err); stop {
		return code
	}
	if code, stop := r.noArgs(pos); stop {
		return code
	}
	eng, err := r.engine([]string{"dev-junk"})
	if err != nil {
		return r.fail(err)
	}
	res, partial, err := r.check(eng)
	if err != nil {
		return r.fail(err)
	}
	projects := devjunk.GroupProjects(res)
	if projects == nil {
		projects = []devjunk.Project{}
	}
	if r.json {
		r.writeJSON(output{Projects: projects})
		return r.done(partial)
	}
	if len(projects) == 0 {
		if len(eng.Settings().Cleanup.DevProjectRoots) == 0 {
			fmt.Fprintln(r.stdout, "还没有设置项目文件夹，可以在 Spruce 的「设置 → 清理」中添加")
		} else {
			fmt.Fprintln(r.stdout, "项目文件夹里没有找到构建产物")
		}
		return r.done(partial)
	}
	now := r.d.Now()
	t := &table{right: map[int]bool{3: true}}
	t.add("项目", "最后活动", "Git", "大小", "构建产物", "说明")
	for _, p := range projects {
		// A dirty project's hold says what its Git column says already.
		note := p.Hold
		if p.Git == cleanup.GitDirty {
			note = ""
		}
		t.add(p.Label, format.RelativeTime(p.LastActive, now), gitText(p.Git), size(p.Size), strings.Join(p.Kinds, " · "), note)
	}
	t.write(r.stdout)
	return r.done(partial)
}

// gitText is what git says about a project.
func gitText(g cleanup.GitState) string {
	switch g {
	case cleanup.GitClean:
		return "干净"
	case cleanup.GitDirty:
		return "有未提交的改动"
	case cleanup.GitNone:
		return "不是 Git 仓库"
	}
	return "无法确定"
}

func (r *run) history(args []string) int {
	fs := newFlags("history")
	limit := fs.Int("limit", 20, "")
	fs.BoolVar(&r.json, "json", r.json, "")
	pos, err := parse(fs, args)
	if code, stop := r.flagsDone(err); stop {
		return code
	}
	if code, stop := r.noArgs(pos); stop {
		return code
	}
	if *limit < 1 || *limit > history.MaxRuns {
		return r.usage(fmt.Errorf("--limit 要在 1 到 %d 之间", history.MaxRuns), r.command)
	}
	eng, err := r.engine(nil)
	if err != nil {
		return r.fail(err)
	}
	all, err := eng.History(history.MaxRuns)
	if err != nil {
		return r.fail(err)
	}
	// Whether a run can be put back depends on the 放回原处 runs after it, so the whole history is read and the newest limit are shown.
	restorable := eng.Restorable(all, r.d.Now())
	runs := make([]HistoryRun, 0, min(len(all), *limit))
	for _, run := range all[:min(len(all), *limit)] {
		runs = append(runs, HistoryRun{Run: run, Title: kindTitle(run), Restorable: restorable[run.ID]})
	}
	if r.json {
		r.writeJSON(output{Runs: runs})
		return ExitOK
	}
	if len(runs) == 0 {
		fmt.Fprintln(r.stdout, "还没有操作记录")
		return ExitOK
	}
	t := &table{}
	head := []string{"记录", "时间", "类型", "摘要"}
	if r.d.CanRestore {
		head = append(head, "放回原处")
	}
	t.add(head...)
	for _, run := range runs {
		row := []string{run.ID, when(run.StartedAt), run.Title, summaryText(run.Run)}
		if r.d.CanRestore {
			can := ""
			if run.Restorable {
				can = "可以"
			}
			row = append(row, can)
		}
		t.add(row...)
	}
	t.write(r.stdout)
	return ExitOK
}

// kindTitles are the history kinds' titles, as 操作历史 shows them.
var kindTitles = map[history.Kind]string{
	history.KindSmartScan:   "清理建议",
	history.KindAuto:        "自动清理",
	history.KindSystemJunk:  "系统垃圾",
	history.KindDevJunk:     "开发者",
	history.KindInstallers:  "安装包",
	history.KindUninstall:   "应用卸载",
	history.KindSpace:       "空间透视",
	history.KindMaintenance: "维护",
	history.KindRestore:     "放回原处",
}

// kindTitle is a run's title: its kind's, an automatic run's rule label.
func kindTitle(run history.Run) string {
	if run.Kind == history.KindAuto && run.Rule != "" {
		if spec, ok := rules.Lookup(rules.ID(run.Rule)); ok {
			return spec.Title
		}
	}
	if t, ok := kindTitles[run.Kind]; ok {
		return t
	}
	return "其他操作"
}

// summaryText sums up a run in one line.
func summaryText(run history.Run) string {
	if run.Kind == history.KindMaintenance {
		return fmt.Sprintf("%d 项维护任务", len(run.Results))
	}
	s := run.Summary
	if s == nil {
		return ""
	}
	if run.Kind == history.KindRestore {
		line := fmt.Sprintf("放回 %d 项（%s）", s.Items, size(s.Trashed))
		if s.Failed > 0 {
			line += fmt.Sprintf("，%d 项没能放回", s.Failed)
		}
		return line
	}
	var parts []string
	if s.Freed > 0 {
		parts = append(parts, "释放 "+size(s.Freed))
	}
	if s.Trashed > 0 {
		parts = append(parts, fmt.Sprintf("移到%s %s", format.TrashName(), size(s.Trashed)))
	}
	parts = append(parts, fmt.Sprintf("%d 项", s.Items))
	if s.Failed > 0 {
		parts = append(parts, fmt.Sprintf("%d 项未能处理", s.Failed))
	}
	return strings.Join(parts, "，")
}

func (r *run) restore(args []string) int {
	fs := newFlags("restore")
	var yes bool
	fs.BoolVar(&yes, "yes", false, "")
	fs.BoolVar(&r.json, "json", r.json, "")
	pos, err := parse(fs, args)
	if code, stop := r.flagsDone(err); stop {
		return code
	}
	switch len(pos) {
	case 0:
		return r.usage(errors.New("缺少记录 ID，运行 spruce history 可以看到"), r.command)
	case 1:
	default:
		return r.usage(fmt.Errorf("多余的参数 %q", pos[1]), r.command)
	}
	runID := pos[0]
	if !r.d.CanRestore {
		return r.fail(fmt.Errorf("这台电脑上不能放回原处，可以在%s里手动还原", format.TrashName()))
	}
	eng, err := r.engine(nil)
	if err != nil {
		return r.fail(err)
	}
	if yes {
		return r.putBack(eng, runID)
	}
	runs, err := eng.History(history.MaxRuns)
	if err != nil {
		return r.fail(err)
	}
	if !slices.ContainsFunc(runs, func(run history.Run) bool { return run.ID == runID }) {
		return r.fail(fmt.Errorf("没有找到记录 %s", runID))
	}
	if !eng.Restorable(runs, r.d.Now())[runID] {
		return r.fail(errors.New("这条记录已经不能放回原处了"))
	}
	entry, err := eng.Entry(runID)
	if err != nil {
		return r.fail(errors.New("这条记录已经不能放回原处了"))
	}
	plan := RestorePlan{RunID: runID, Moves: []RestoreItem{}}
	for _, m := range entry.Moves {
		if m.Restored || m.Tried {
			continue
		}
		plan.Items++
		plan.Bytes += m.Size
		plan.Moves = append(plan.Moves, RestoreItem{Path: m.Path, Size: m.Size, Dir: m.Dir})
	}
	if r.json {
		r.writeJSON(output{Restore: &plan})
	} else {
		fmt.Fprintf(r.stdout, "将放回原处 %d 项，共 %s\n", plan.Items, size(plan.Bytes))
		t := &table{right: map[int]bool{0: true}}
		for i, m := range plan.Moves {
			if i == listLimit {
				break
			}
			t.add("  "+size(m.Size), r.short(m.Path))
		}
		t.write(r.stdout)
		if n := len(plan.Moves) - listLimit; n > 0 {
			fmt.Fprintf(r.stdout, "  另有 %d 项\n", n)
		}
	}
	r.note("没有放回：加 --yes 才会执行")
	return ExitOK
}

// putBack runs restore --yes.
func (r *run) putBack(eng Engine, runID string) int {
	out, err := eng.Restore(r.ctx, runID)
	if err != nil {
		return r.fail(err)
	}
	if r.json {
		r.writeJSON(output{Restored: &out})
		return r.done(out.Failed > 0)
	}
	if out.Failed == 0 {
		fmt.Fprintf(r.stdout, "已放回原处 %d 项（%s）\n", out.Restored, size(out.Bytes))
	} else {
		fmt.Fprintf(r.stdout, "已放回 %d 项，%d 项没能放回\n", out.Restored, out.Failed)
		for _, reason := range out.Reasons {
			fmt.Fprintf(r.stdout, "  %s\n", reason)
		}
	}
	if out.RunID != "" {
		fmt.Fprintf(r.stdout, "已记入操作历史：%s\n", out.RunID)
	}
	return r.done(out.Failed > 0)
}

func (r *run) version(args []string) int {
	fs := newFlags("version")
	fs.BoolVar(&r.json, "json", r.json, "")
	pos, err := parse(fs, args)
	if code, stop := r.flagsDone(err); stop {
		return code
	}
	if code, stop := r.noArgs(pos); stop {
		return code
	}
	if r.json {
		r.writeJSON(output{App: &versionInfo{Version: r.d.Version, Development: r.d.Version == ""}})
		return ExitOK
	}
	if r.d.Version == "" {
		fmt.Fprintln(r.stdout, "Spruce 开发版本")
	} else {
		fmt.Fprintln(r.stdout, "Spruce "+r.d.Version)
	}
	return ExitOK
}
