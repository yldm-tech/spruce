package maintenance

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

const (
	statsSQL     = "PRAGMA page_count; PRAGMA freelist_count;"
	integritySQL = "PRAGMA integrity_check;"
	vacuumSQL    = "VACUUM;"
)

// dbFixture is a fake home directory with the three apps' databases and the task configuration pointing at it.
type dbFixture struct {
	home    string
	safari  string
	mail    string
	chat    string
	running map[string]bool
	free    uint64
	cfg     databaseConfig
}

func newDBFixture(t *testing.T) *dbFixture {
	t.Helper()
	home := t.TempDir()
	fx := &dbFixture{
		home:    home,
		safari:  filepath.Join(home, "Library", "Safari", "History.db"),
		mail:    filepath.Join(home, "Library", "Mail", "V10", "MailData", "Envelope Index"),
		chat:    filepath.Join(home, "Library", "Messages", "chat.db"),
		running: map[string]bool{},
		free:    100 << 30,
	}
	for _, p := range []string{fx.safari, fx.mail, fx.chat, filepath.Join(home, "Library", "Mail", "V9", "MailData", "Envelope Index")} {
		writeSQLiteFile(t, p)
	}
	fx.cfg = defaultDatabaseConfig()
	fx.cfg.running = func(context.Context) (map[string]bool, error) { return fx.running, nil }
	fx.cfg.freeBytes = func(string) (uint64, error) { return fx.free, nil }
	return fx
}

func writeSQLiteFile(t *testing.T, path string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	data := append([]byte("SQLite format 3\x00"), make([]byte, 4080)...)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

func (fx *dbFixture) env(r execx.Runner) Env {
	env := macEnv(r)
	env.HomeDir = fx.home
	return env
}

func (fx *dbFixture) run(t *testing.T, f *execxtest.Fake) taskresult.Result {
	t.Helper()
	return databasesTask(fx.cfg).Run(t.Context(), fx.env(f))
}

func onSQLite(f *execxtest.Fake, db, sql string) *execxtest.Expectation {
	return f.On(pathSqlite3, sqliteArgs(db, sql)...)
}

// scriptHealthy answers every query for db as a healthy database with 20% free pages.
func scriptHealthy(f *execxtest.Fake, db string) {
	onSQLite(f, db, statsSQL).Return("100\n20\n", 0)
	onSQLite(f, db, integritySQL).Return("ok\n", 0)
	onSQLite(f, db, vacuumSQL)
}

func TestDatabasesVacuumsHealthyDatabases(t *testing.T) {
	fx := newDBFixture(t)
	f := execxtest.New()
	for _, db := range []string{fx.safari, fx.mail, fx.chat} {
		scriptHealthy(f, db)
	}
	res := fx.run(t, f)
	assertOutcome(t, res, taskresult.Applied, "已优化 3 个")
	for _, db := range []string{fx.safari, fx.mail, fx.chat} {
		f.AssertCalled(t, pathSqlite3, sqliteArgs(db, integritySQL)...)
		f.AssertCalled(t, pathSqlite3, sqliteArgs(db, vacuumSQL)...)
	}
	for _, c := range f.Calls() {
		if strings.Contains(strings.Join(c.Args, " "), "V9") {
			t.Errorf("touched the stale Mail data version: %s", c.String())
		}
		if c.Args[0] != "-init" || c.Args[1] != "/dev/null" {
			t.Errorf("sqlite3 may read ~/.sqliterc: %s", c.String())
		}
	}
}

func TestDatabasesIntegrityCheckComesBeforeVacuum(t *testing.T) {
	fx := newDBFixture(t)
	f := execxtest.New()
	for _, db := range []string{fx.safari, fx.mail, fx.chat} {
		scriptHealthy(f, db)
	}
	fx.run(t, f)
	var order []string
	for _, c := range f.Calls() {
		if c.Args[len(c.Args)-2] == fx.chat {
			order = append(order, c.Args[len(c.Args)-1])
		}
	}
	want := []string{statsSQL, integritySQL, vacuumSQL}
	if strings.Join(order, "|") != strings.Join(want, "|") {
		t.Errorf("queries on chat.db = %q, want %q", order, want)
	}
}

func TestDatabasesSkipsRunningApps(t *testing.T) {
	fx := newDBFixture(t)
	fx.running = map[string]bool{"Safari": true, "Mail": true}
	f := execxtest.New()
	scriptHealthy(f, fx.chat)
	res := fx.run(t, f)
	assertOutcome(t, res, taskresult.Skipped, "已优化 1 个", "Safari、邮件仍在运行", "退出后再运行一次")
	for _, c := range f.Calls() {
		if db := c.Args[len(c.Args)-2]; db == fx.safari || db == fx.mail {
			t.Errorf("queried the database of a running app: %s", c.String())
		}
	}
}

func TestDatabasesRunningLatinNameIsSpaced(t *testing.T) {
	fx := newDBFixture(t)
	fx.running = map[string]bool{"Safari": true}
	f := execxtest.New()
	scriptHealthy(f, fx.mail)
	scriptHealthy(f, fx.chat)
	assertOutcome(t, fx.run(t, f), taskresult.Skipped, "Safari 仍在运行")
}

func TestDatabasesCompactIsUnchanged(t *testing.T) {
	fx := newDBFixture(t)
	f := execxtest.New()
	for _, db := range []string{fx.safari, fx.mail, fx.chat} {
		onSQLite(f, db, statsSQL).Return("1000\n10\n", 0)
	}
	assertOutcome(t, fx.run(t, f), taskresult.Unchanged, "无需优化")
	for _, c := range f.Calls() {
		if c.Args[len(c.Args)-1] != statsSQL {
			t.Errorf("ran more than the page count on a compact database: %s", c.String())
		}
	}
}

func TestDatabasesCorruptIsNeverVacuumed(t *testing.T) {
	fx := newDBFixture(t)
	f := execxtest.New()
	scriptHealthy(f, fx.mail)
	scriptHealthy(f, fx.chat)
	onSQLite(f, fx.safari, statsSQL).Return("100\n20\n", 0)
	onSQLite(f, fx.safari, integritySQL).Return("*** in database main ***\nPage 5: btreeInitPage() returns error code 11\n", 0)
	res := fx.run(t, f)
	assertOutcome(t, res, taskresult.Failed, "Safari 的数据库校验未通过", "未做改动")
	if f.Called(pathSqlite3, sqliteArgs(fx.safari, vacuumSQL)...) {
		t.Error("vacuumed a database that failed its integrity check")
	}
}

func TestDatabasesLockedIsSkipped(t *testing.T) {
	fx := newDBFixture(t)
	f := execxtest.New()
	scriptHealthy(f, fx.safari)
	scriptHealthy(f, fx.mail)
	onSQLite(f, fx.chat, statsSQL).Return("100\n20\n", 0)
	onSQLite(f, fx.chat, integritySQL).Return("ok\n", 0)
	onSQLite(f, fx.chat, vacuumSQL).ReturnOutput(execx.Output{Stderr: []byte("Runtime error near line 1: database is locked (5)\n"), ExitCode: 1})
	assertOutcome(t, fx.run(t, f), taskresult.Skipped, "信息的数据库正被占用")
}

func TestDatabasesVacuumErrorFails(t *testing.T) {
	fx := newDBFixture(t)
	f := execxtest.New()
	scriptHealthy(f, fx.safari)
	scriptHealthy(f, fx.chat)
	onSQLite(f, fx.mail, statsSQL).Return("100\n20\n", 0)
	onSQLite(f, fx.mail, integritySQL).Return("ok\n", 0)
	onSQLite(f, fx.mail, vacuumSQL).ReturnOutput(execx.Output{Stderr: []byte("Runtime error: disk I/O error (10)\n"), ExitCode: 1})
	assertOutcome(t, fx.run(t, f), taskresult.Failed, "优化 邮件的数据库失败")
}

func TestDatabasesSizeAndSpaceLimits(t *testing.T) {
	t.Run("too large", func(t *testing.T) {
		fx := newDBFixture(t)
		fx.cfg.maxBytes = 16
		f := execxtest.New()
		assertOutcome(t, fx.run(t, f), taskresult.Unchanged, "较大")
		if len(f.Calls()) != 0 {
			t.Errorf("queried databases over the size limit: %v", f.Calls())
		}
	})
	t.Run("low disk space", func(t *testing.T) {
		fx := newDBFixture(t)
		fx.free = 1 << 20
		f := execxtest.New()
		for _, db := range []string{fx.safari, fx.mail, fx.chat} {
			onSQLite(f, db, statsSQL).Return("100\n20\n", 0)
		}
		assertOutcome(t, fx.run(t, f), taskresult.Skipped, "磁盘空间不足")
		for _, c := range f.Calls() {
			if c.Args[len(c.Args)-1] != statsSQL {
				t.Errorf("ran %s with too little free space", c.String())
			}
		}
	})
}

func TestDatabasesWithoutFullDiskAccess(t *testing.T) {
	t.Run("precheck denied", func(t *testing.T) {
		env := macEnv(execxtest.New())
		env.Permissions = macPermissions{state: PermissionDenied}
		res := databasesTask(defaultDatabaseConfig()).Precheck(t.Context(), env)
		if res == nil {
			t.Fatal("precheck let the task run without Full Disk Access")
		}
		assertOutcome(t, *res, taskresult.Skipped, "完全磁盘访问权限")
	})
	for _, p := range []PermissionChecker{nil, macPermissions{state: PermissionGranted}, macPermissions{state: PermissionNotRequired}, macPermissions{state: PermissionUnknown}, macPermissions{err: errors.New("probe failed")}} {
		env := macEnv(execxtest.New())
		env.Permissions = p
		if res := databasesTask(defaultDatabaseConfig()).Precheck(t.Context(), env); res != nil {
			t.Errorf("precheck with %#v stopped the task: %+v", p, *res)
		}
	}
	t.Run("unreadable file", func(t *testing.T) {
		if os.Geteuid() == 0 {
			t.Skip("root reads files regardless of their mode")
		}
		fx := newDBFixture(t)
		for _, db := range []string{fx.safari, fx.mail, fx.chat} {
			if err := os.Chmod(db, 0); err != nil {
				t.Fatal(err)
			}
		}
		f := execxtest.New()
		assertOutcome(t, fx.run(t, f), taskresult.Skipped, "完全磁盘访问权限")
		if len(f.Calls()) != 0 {
			t.Errorf("ran sqlite3 on unreadable files: %v", f.Calls())
		}
	})
}

func TestDatabasesIgnoresMissingAndForeignFiles(t *testing.T) {
	fx := newDBFixture(t)
	if err := os.Remove(fx.safari); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(fx.chat, []byte("not a database"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Remove(fx.mail); err != nil {
		t.Fatal(err)
	}
	if err := os.Symlink(fx.chat, fx.mail); err != nil {
		t.Fatal(err)
	}
	f := execxtest.New()
	assertOutcome(t, fx.run(t, f), taskresult.Unchanged, "没有找到")
	if len(f.Calls()) != 0 {
		t.Errorf("ran sqlite3 on files that are not databases: %v", f.Calls())
	}
}

func TestDatabasesProcessListFailure(t *testing.T) {
	fx := newDBFixture(t)
	fx.cfg.running = func(context.Context) (map[string]bool, error) { return nil, errors.New("sysctl failed") }
	f := execxtest.New()
	assertOutcome(t, fx.run(t, f), taskresult.Failed, "未做改动")
	if len(f.Calls()) != 0 {
		t.Errorf("ran %v without knowing which apps run", f.Calls())
	}
}

func TestDatabasesCancelled(t *testing.T) {
	fx := newDBFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	res := databasesTask(fx.cfg).Run(ctx, fx.env(execxtest.New()))
	assertOutcome(t, res, taskresult.Skipped, "中断")
}

func TestMailEnvelopeIndexPicksNewestVersion(t *testing.T) {
	home := t.TempDir()
	for _, v := range []string{"V2", "V9", "V10", "Vx", "Bundles"} {
		mustMkdir(t, filepath.Join(home, "Library", "Mail", v, "MailData"))
	}
	got, err := mailEnvelopeIndex(home)
	if err != nil {
		t.Fatal(err)
	}
	want := filepath.Join(home, "Library", "Mail", "V10", "MailData", "Envelope Index")
	if len(got) != 1 || got[0] != want {
		t.Errorf("mailEnvelopeIndex = %v, want [%s]", got, want)
	}
	if got, err := mailEnvelopeIndex(t.TempDir()); err != nil || len(got) != 0 {
		t.Errorf("without Mail data: %v, %v", got, err)
	}
}

func TestParsePageCounts(t *testing.T) {
	tests := []struct {
		in          string
		pages, free int64
		ok          bool
	}{
		{"100\n20\n", 100, 20, true},
		{"0\n0\n", 0, 0, true},
		{"100\n", 0, 0, false},
		{"x\n1\n", 0, 0, false},
		{"10\n20\n", 0, 0, false},
		{"-1\n0\n", 0, 0, false},
	}
	for _, tt := range tests {
		pages, free, ok := parsePageCounts(tt.in)
		if pages != tt.pages || free != tt.free || ok != tt.ok {
			t.Errorf("parsePageCounts(%q) = %d, %d, %v", tt.in, pages, free, ok)
		}
	}
}

func TestRunningProcessNamesListsSystemProcesses(t *testing.T) {
	names, err := runningProcessNames(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if !names["launchd"] {
		t.Errorf("process list of %d names has no launchd", len(names))
	}
}
