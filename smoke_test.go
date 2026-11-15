package main

import (
	"log/slog"
	"os"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
	"github.com/nsgo-dev/nsgo/nsgotest"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/ui/shell"
)

// TestSmoke starts the app on the real engine with a temporary home and data folder, checks that it opens on 概览, goes to 维护 through the sidebar and finds the task catalog there. It never presses 开始检查 or 运行, so nothing is scanned, cleaned or run.
func TestSmoke(t *testing.T) {
	dir := t.TempDir()
	home, dataDir := dir+string(os.PathSeparator)+"home", dir+string(os.PathSeparator)+"data"
	for _, d := range []string{home, dataDir} {
		if err := os.MkdirAll(d, 0o700); err != nil {
			t.Fatal(err)
		}
	}
	exe, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	deps, err := wire(env{home: home, dataDir: dataDir, exe: exe, runner: execx.OSRunner{}, tasks: maintenance.PlatformTasks(),
		log: slog.New(slog.DiscardHandler)})
	if err != nil {
		t.Fatal(err)
	}
	// 概览's controller is wired, but its loop is background work and does not start: no check reads anything (the sources run real tools), nothing is scheduled and the CPU monitor stays off.
	if deps.Stores.Overview == nil {
		t.Fatal("概览's controller is not wired")
	}
	deps.Background = nil
	a := nsgotest.Start(t, core.AppRoot(shell.App(deps)), nsgotest.Size(1280, 820), wallNow)
	a.WaitFor("text:Spruce 会在后台定期检查，告诉你哪些空间可以放心释放", 10*time.Second)
	// Opening 概览 asks the controller for a check, which waits for the loop that is not running: 概览 shows it stopped and offers 开始检查.
	a.WaitFor("id:overview-page >> id:check-start", 10*time.Second)
	if got := len(a.Query("id:sidebar >> role:link")); got != 7 {
		t.Errorf("the sidebar has %d entries, want 7", got)
	}
	a.Screenshot("smoke-home")

	a.Press("id:nav-maintenance")
	a.WaitFor("text:给系统做一次轻量保养，不会动你的文档和照片", 10*time.Second)
	a.WaitUntil(10*time.Second, func() bool { return len(a.Query("role:switch")) > 0 })
	if got, want := len(a.Query("role:switch")), len(maintenance.PlatformTasks()); got != want {
		t.Errorf("维护 shows %d cards, want %d", got, want)
	}
	a.Find("id:run")
	a.Screenshot("smoke-maintenance")
}
