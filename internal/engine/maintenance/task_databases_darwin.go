package maintenance

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"io/fs"
	"os"
	"path/filepath"
	"slices"
	"strconv"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/shirou/gopsutil/v4/process"
	"golang.org/x/sys/unix"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// The "databases" task runs PRAGMA integrity_check and then VACUUM on the SQLite databases of Safari, Mail and Messages, through Apple's /usr/bin/sqlite3.
//
// Why /usr/bin/sqlite3 and not a Go driver: these files are written by the SQLite library that ships with macOS, an Apple build with Apple's compile options and file-format handling (WAL, the -shm index, locking with the apps' background agents). The command-line shell of the same OS build links that same library, so integrity_check and VACUUM see the files exactly as Safari, Mail and Messages do. A pure-Go driver (modernc.org/sqlite) is a separate translation of upstream SQLite with its own version and options, and would add a large dependency for three statements. Every invocation passes -init /dev/null so a ~/.sqliterc cannot inject commands, and -bail so the first error stops it.

// sqliteHeader is the first 16 bytes of every SQLite 3 database file.
var sqliteHeader = []byte("SQLite format 3\x00")

const (
	sqliteBusyTimeoutMillis = 5000
	sqliteStatsTimeout      = 30 * time.Second
	sqliteIntegrityTimeout  = 2 * time.Minute
	sqliteVacuumTimeout     = 3 * time.Minute

	// compactPercent: a database whose free pages are below this share of its pages is already compact and is left alone.
	compactPercent = 5
	// maxDatabaseBytes: larger databases are not vacuumed, since VACUUM rewrites the whole file and would outlast the page's time estimate.
	maxDatabaseBytes = 512 << 20
	// vacuumSpareBytes is free space required on top of twice the database size (VACUUM writes a temporary copy and then the new content).
	vacuumSpareBytes = 1 << 30
)

// appDatabases names one app and where its databases are.
type appDatabases struct {
	// process is the executable name the kernel reports for the running app (p_comm), e.g. "Safari".
	process string
	// label is the app's name in the user's language, for messages.
	label string
	// files returns the absolute paths of the app's database files under home. Missing files are not an error; they are skipped later.
	files func(home string) ([]string, error)
}

// databaseConfig is what the task depends on, so that tests can replace the process list and the disk.
type databaseConfig struct {
	apps []appDatabases
	// running returns the executable names of the running processes.
	running func(ctx context.Context) (map[string]bool, error)
	// freeBytes returns the free space available to the user on the volume of dir.
	freeBytes func(dir string) (uint64, error)
	maxBytes  int64
}

func defaultDatabaseConfig() databaseConfig {
	return databaseConfig{
		apps: []appDatabases{
			{process: "Safari", label: "Safari", files: fixedFiles("Library/Safari/History.db")},
			{process: "Mail", label: "邮件", files: mailEnvelopeIndex},
			{process: "Messages", label: "信息", files: fixedFiles("Library/Messages/chat.db")},
		},
		running:   runningProcessNames,
		freeBytes: volumeFreeBytes,
		maxBytes:  maxDatabaseBytes,
	}
}

// fixedFiles returns database paths relative to the home directory.
func fixedFiles(rel ...string) func(home string) ([]string, error) {
	return func(home string) ([]string, error) {
		out := make([]string, len(rel))
		for i, r := range rel {
			out[i] = filepath.Join(home, filepath.FromSlash(r))
		}
		return out, nil
	}
}

// mailEnvelopeIndex returns Mail's message index of the newest data version, ~/Library/Mail/V<n>/MailData/Envelope Index; older V<n> directories left behind by upgrades are not used by Mail and are ignored.
func mailEnvelopeIndex(home string) ([]string, error) {
	mailDir := filepath.Join(home, "Library", "Mail")
	entries, err := os.ReadDir(mailDir)
	if errors.Is(err, fs.ErrNotExist) {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	best, bestVersion := "", -1
	for _, e := range entries {
		v, ok := strings.CutPrefix(e.Name(), "V")
		n, err := strconv.Atoi(v)
		if !ok || err != nil || !e.IsDir() || n <= bestVersion {
			continue
		}
		best, bestVersion = e.Name(), n
	}
	if best == "" {
		return nil, nil
	}
	return []string{filepath.Join(mailDir, best, "MailData", "Envelope Index")}, nil
}

// runningProcessNames lists the names of all processes through gopsutil. A process that exits while being listed is left out.
func runningProcessNames(ctx context.Context) (map[string]bool, error) {
	procs, err := process.ProcessesWithContext(ctx)
	if err != nil {
		return nil, err
	}
	names := make(map[string]bool, len(procs))
	for _, p := range procs {
		if name, err := p.NameWithContext(ctx); err == nil {
			names[name] = true
		}
	}
	return names, nil
}

func volumeFreeBytes(dir string) (uint64, error) {
	var st unix.Statfs_t
	if err := unix.Statfs(dir, &st); err != nil {
		return 0, err
	}
	return st.Bavail * uint64(st.Bsize), nil
}

func databasesTask(cfg databaseConfig) TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "databases",
			Title:            "优化应用数据库",
			Description:      "整理 Safari、邮件和信息的数据库，让它们更快",
			Hint:             "需要先退出 Safari 和邮件",
			Icon:             IconDatabase,
			DefaultOn:        false,
			NeedsAdmin:       false,
			EstimatedSeconds: 30,
		},
		Precheck: precheckDatabases,
		Run: func(ctx context.Context, env Env) taskresult.Result {
			return runDatabases(ctx, env, cfg)
		},
	}
}

const fullDiskAccessMessage = "需要为 Spruce 开启「完全磁盘访问权限」，开启后再运行一次"

// precheckDatabases stops the task when Full Disk Access is known to be missing. An unknown state lets it run: reading the files then tells, and a permission error ends the same way.
func precheckDatabases(_ context.Context, env Env) *taskresult.Result {
	if env.Permissions == nil {
		return nil
	}
	state, err := env.Permissions.FullDiskAccess()
	if err == nil && state == PermissionDenied {
		return &taskresult.Result{Outcome: taskresult.Skipped, Reason: taskresult.ReasonFullDiskAccess, Message: fullDiskAccessMessage, Detail: "full disk access: denied"}
	}
	if err != nil {
		env.Log.Warn("full disk access check failed", "err", err)
	}
	return nil
}

// dbStatus is what happened to one database file.
type dbStatus int

const (
	dbMissing dbStatus = iota
	dbVacuumed
	dbCompact
	dbTooLarge
	dbLowSpace
	dbBusy
	dbCorrupt
	dbFailed
	dbNoAccess
)

// dbTally collects per-file statuses and the apps they belong to.
type dbTally struct {
	counts  map[dbStatus]int
	apps    map[dbStatus][]string
	running []string
	details []string
}

func (t *dbTally) add(st dbStatus, app, detail string) {
	t.counts[st]++
	if !slices.Contains(t.apps[st], app) {
		t.apps[st] = append(t.apps[st], app)
	}
	if detail != "" {
		t.details = append(t.details, detail)
	}
}

func runDatabases(ctx context.Context, env Env, cfg databaseConfig) taskresult.Result {
	running, err := cfg.running(ctx)
	if err != nil {
		return taskresult.Result{Outcome: taskresult.Failed, Message: "无法确认 Safari、邮件和信息是否在运行，未做改动", Detail: err.Error()}
	}
	t := &dbTally{counts: map[dbStatus]int{}, apps: map[dbStatus][]string{}}
	for _, app := range cfg.apps {
		if running[app.process] {
			t.running = append(t.running, app.label)
			continue
		}
		files, err := app.files(env.HomeDir)
		if errors.Is(err, fs.ErrPermission) {
			t.add(dbNoAccess, app.label, err.Error())
			continue
		}
		if err != nil {
			t.add(dbFailed, app.label, err.Error())
			continue
		}
		for _, f := range files {
			if ctx.Err() != nil {
				return t.interruptedResult()
			}
			st, detail := optimizeDatabase(ctx, env, cfg, f)
			if st != dbMissing {
				t.add(st, app.label, detail)
			}
		}
	}
	if ctx.Err() != nil {
		return t.interruptedResult()
	}
	return t.result()
}

// optimizeDatabase checks one file and vacuums it when it is a healthy SQLite database with enough free pages to be worth it.
func optimizeDatabase(ctx context.Context, env Env, cfg databaseConfig, path string) (dbStatus, string) {
	fi, err := os.Lstat(path)
	switch {
	case errors.Is(err, fs.ErrNotExist):
		return dbMissing, ""
	case errors.Is(err, fs.ErrPermission):
		return dbNoAccess, err.Error()
	case err != nil:
		return dbFailed, err.Error()
	case !fi.Mode().IsRegular():
		// A symlink or anything else is not the app's own database file; leave it alone.
		return dbMissing, ""
	}
	isDB, err := hasSQLiteHeader(path)
	switch {
	case errors.Is(err, fs.ErrPermission):
		return dbNoAccess, err.Error()
	case err != nil:
		return dbFailed, err.Error()
	case !isDB:
		return dbMissing, ""
	}
	if fi.Size() > cfg.maxBytes {
		return dbTooLarge, fmt.Sprintf("%s: %d bytes, over the %d byte limit", path, fi.Size(), cfg.maxBytes)
	}

	stats, st, detail := sqliteQuery(ctx, env.Runner, path, "PRAGMA page_count; PRAGMA freelist_count;", sqliteStatsTimeout)
	if st != dbMissing {
		return st, detail
	}
	pages, free, ok := parsePageCounts(stats)
	if !ok {
		return dbFailed, fmt.Sprintf("%s: unexpected page counts %q", path, stats)
	}
	if pages == 0 || free*100 < pages*compactPercent {
		return dbCompact, ""
	}

	avail, err := cfg.freeBytes(filepath.Dir(path))
	if err != nil {
		return dbFailed, err.Error()
	}
	if need := uint64(2*fi.Size()) + vacuumSpareBytes; avail < need {
		return dbLowSpace, fmt.Sprintf("%s: %d bytes free, %d needed", path, avail, need)
	}

	check, st, detail := sqliteQuery(ctx, env.Runner, path, "PRAGMA integrity_check;", sqliteIntegrityTimeout)
	if st != dbMissing {
		return st, detail
	}
	if strings.TrimSpace(check) != "ok" {
		return dbCorrupt, fmt.Sprintf("%s: integrity_check: %s", path, execx.Tail([]byte(check), 512))
	}

	if _, st, detail := sqliteQuery(ctx, env.Runner, path, "VACUUM;", sqliteVacuumTimeout); st != dbMissing {
		return st, detail
	}
	env.Log.Debug("database vacuumed", "path", path, "pages", pages, "freePages", free)
	return dbVacuumed, ""
}

// hasSQLiteHeader reports whether the file starts with the SQLite 3 header.
func hasSQLiteHeader(path string) (bool, error) {
	f, err := os.Open(path)
	if err != nil {
		return false, err
	}
	defer f.Close()
	buf := make([]byte, len(sqliteHeader))
	if _, err := io.ReadFull(f, buf); err != nil {
		if errors.Is(err, io.EOF) || errors.Is(err, io.ErrUnexpectedEOF) {
			return false, nil
		}
		return false, err
	}
	return bytes.Equal(buf, sqliteHeader), nil
}

// sqliteQuery runs sql on the database at path with /usr/bin/sqlite3 and returns its standard output. The status is dbMissing (meaning no problem) on success, dbBusy when the database stayed locked by another process past the busy timeout, and dbFailed otherwise.
func sqliteQuery(ctx context.Context, runner execx.Runner, path, sql string, timeout time.Duration) (string, dbStatus, string) {
	cmd := execx.Command{
		Name:    pathSqlite3,
		Args:    sqliteArgs(path, sql),
		Env:     cLocale,
		Timeout: timeout,
	}
	out, err := runner.Run(ctx, cmd)
	if err == nil {
		return string(out.Stdout), dbMissing, ""
	}
	msg := strings.ToLower(combinedOutput(out) + err.Error())
	if strings.Contains(msg, "database is locked") || strings.Contains(msg, "database is busy") {
		return "", dbBusy, err.Error()
	}
	return "", dbFailed, err.Error()
}

// sqliteArgs builds the sqlite3 command line: no init file, batch mode, stop at the first error, and wait up to the busy timeout for another process's lock.
func sqliteArgs(path, sql string) []string {
	return []string{"-init", "/dev/null", "-batch", "-bail", "-cmd", ".timeout " + strconv.Itoa(sqliteBusyTimeoutMillis), path, sql}
}

// parsePageCounts parses the two lines printed for "PRAGMA page_count; PRAGMA freelist_count;".
func parsePageCounts(out string) (pages, free int64, ok bool) {
	fields := strings.Fields(out)
	if len(fields) != 2 {
		return 0, 0, false
	}
	pages, err1 := strconv.ParseInt(fields[0], 10, 64)
	free, err2 := strconv.ParseInt(fields[1], 10, 64)
	if err1 != nil || err2 != nil || pages < 0 || free < 0 || free > pages {
		return 0, 0, false
	}
	return pages, free, true
}

// interruptedResult is the Result when the run was cancelled part-way: interrupted() when nothing was vacuumed yet, otherwise the databases already done. The rest of the tally is not used, since a file whose sqlite3 call the cancellation killed was counted as failed or busy.
func (t *dbTally) interruptedResult() taskresult.Result {
	done := t.counts[dbVacuumed]
	if done == 0 {
		return interrupted()
	}
	return taskresult.Result{
		Outcome: taskresult.Applied,
		Partial: true,
		Message: fmt.Sprintf("已优化 %d 个数据库，维护已中断，其余未执行", done),
		Detail:  joinDetail(strings.Join(t.details, "; "), context.Canceled.Error()),
	}
}

// result turns the tally into the task's Result. Precedence: missing Full Disk Access, then errors, then apps the user has to quit or that were busy, then work done, then the reasons nothing was done.
func (t *dbTally) result() taskresult.Result {
	detail := strings.Join(t.details, "; ")
	done := t.counts[dbVacuumed]
	prefix := ""
	if done > 0 {
		prefix = fmt.Sprintf("已优化 %d 个数据库；", done)
	}
	switch {
	case t.counts[dbNoAccess] > 0:
		return taskresult.Result{Outcome: taskresult.Skipped, Reason: taskresult.ReasonFullDiskAccess, Message: prefix + fullDiskAccessMessage, Detail: detail}
	case t.counts[dbCorrupt] > 0:
		return taskresult.Result{Outcome: taskresult.Failed, Message: prefix + joinApps(t.apps[dbCorrupt]) + "的数据库校验未通过，未做改动", Detail: detail}
	case t.counts[dbFailed] > 0:
		return taskresult.Result{Outcome: taskresult.Failed, Message: prefix + "优化 " + joinApps(t.apps[dbFailed]) + "的数据库失败", Detail: detail}
	case len(t.running) > 0:
		return taskresult.Result{Outcome: taskresult.Skipped, Message: prefix + joinApps(t.running) + "仍在运行，已跳过；退出后再运行一次", Detail: detail}
	case t.counts[dbBusy] > 0:
		return taskresult.Result{Outcome: taskresult.Skipped, Message: prefix + joinApps(t.apps[dbBusy]) + "的数据库正被占用，已跳过；稍后再运行一次", Detail: detail}
	case t.counts[dbLowSpace] > 0:
		return taskresult.Result{Outcome: taskresult.Skipped, Message: prefix + "磁盘空间不足，未优化 " + joinApps(t.apps[dbLowSpace]) + "的数据库", Detail: detail}
	case done > 0 && t.counts[dbTooLarge] > 0:
		return taskresult.Result{Outcome: taskresult.Applied, Partial: true, Message: prefix + joinApps(t.apps[dbTooLarge]) + "的数据库较大，为安全起见未压缩", Detail: detail}
	case done > 0:
		return taskresult.Result{Outcome: taskresult.Applied, Message: fmt.Sprintf("已优化 %d 个应用数据库", done), Detail: detail}
	case t.counts[dbTooLarge] > 0:
		return taskresult.Result{Outcome: taskresult.Unchanged, Message: joinApps(t.apps[dbTooLarge]) + "的数据库较大，为安全起见未压缩", Detail: detail}
	case t.counts[dbCompact] > 0:
		return taskresult.Result{Outcome: taskresult.Unchanged, Message: "应用数据库已经很紧凑，无需优化", Detail: detail}
	}
	return taskresult.Result{Outcome: taskresult.Unchanged, Message: "没有找到需要优化的应用数据库", Detail: detail}
}

// joinApps lists app names for a message, e.g. "Safari、邮件", followed by a space when the list ends in a Latin name so that the Chinese text after it is spaced as in the design copy ("Safari 仍在运行", "邮件仍在运行").
func joinApps(apps []string) string {
	s := strings.Join(apps, "、")
	if s != "" && s[len(s)-1] < utf8.RuneSelf {
		s += " "
	}
	return s
}
