// Package cli is Spruce's command line: subcommands of the same executable that main detects (IsCommand) before the app starts, the way it detects the elevated child. It runs on the app's engine (the same merged scan, cards, clean controller, history and undo ledger), never takes the app's single-instance lock and shares only the clean lock with it, so the two never delete at the same time.
//
// Every command is read-only by default. clean deletes only with --yes, through app.Cleanup like a card of 概览, so every check of the engine applies: the scanned roots, the exclusions, the check of each item right before it is moved, Item.Hold and the second git check of build output. restore (macOS) puts back only with --yes, through app.Restore.
//
// Output is aligned text in Simplified Chinese on stdout, or with --json one JSON object ({"version":1,"command":…}; an error is {"version":1,"error":…}). Progress and notes go to stderr. Exit codes are the Exit constants.
//
// The package does not import NSGo; it may import internal/ui/format, which depends only on the standard library.
package cli

import (
	"context"
	"errors"
	"fmt"
	"io"
	"time"

	"github.com/yldm-tech/spruce/internal/app"
	"github.com/yldm-tech/spruce/internal/engine/cleanup"
	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/schedule"
	"github.com/yldm-tech/spruce/internal/engine/settings"
	"github.com/yldm-tech/spruce/internal/engine/undo"
)

// Exit codes of Main.
const (
	// ExitOK is success, including nothing to do and a dry run.
	ExitOK = 0
	// ExitFailed is a failure.
	ExitFailed = 1
	// ExitUsage is a usage error: an unknown command, flag or ID, or flags that do not go together.
	ExitUsage = 2
	// ExitPartial is a command that did part of its work: an item failed or was held, or a source of the check failed.
	ExitPartial = 3
	// ExitBusy is another clean running, in another Spruce process or this one.
	ExitBusy = 4
	// ExitInterrupted is a command stopped by Ctrl+C or SIGTERM.
	ExitInterrupted = 130
)

// Engine is what the commands run on: main implements it over the app's engine, tests over fakes.
type Engine interface {
	// Scan runs the merged foreground scan (app.CleanerSmartScan) and remembers it for Clean.
	Scan(ctx context.Context, progress func(cleanup.Progress)) (cleanup.ScanResult, error)
	// InstalledApps names the installed apps, for the installers card's reason.
	InstalledApps(ctx context.Context) ([]string, error)
	// Settings are the user's current settings.
	Settings() settings.AppSettings
	// Clean cleans req from the last Scan through app.Cleanup.CleanWith, recorded as opts says.
	Clean(ctx context.Context, req cleanup.CleanRequest, opts app.CleanOptions, progress func(cleanup.Progress)) (app.CleanRun, error)
	// History lists up to limit recorded runs, newest first.
	History(limit int) ([]history.Run, error)
	// Restorable is app.History.Restorable: the runs 放回原处 can still put back.
	Restorable(runs []history.Run, now time.Time) map[string]bool
	// Entry is the undo ledger's entry of a run.
	Entry(runID string) (undo.Entry, error)
	// Restore is app.Restore.Run.
	Restore(ctx context.Context, runID string) (app.RestoreOutcome, error)
}

// Deps is what Main needs besides the arguments.
type Deps struct {
	// Engine builds the engine over the given scan sources (nil: all three), once per command; help and version never call it.
	Engine func(sources []string) (Engine, error)
	// Version is the app's version, "" for a development build.
	Version string
	// Home is the user's home folder: paths under it print as "~/…".
	Home string
	// GOOS is runtime.GOOS (tests pass the other platform).
	GOOS string
	// CanRestore is trash.CanRestore(): whether restore works on this platform.
	CanRestore bool
	// Now is the clock; nil is time.Now.
	Now func() time.Time
}

// commands are the first arguments IsCommand recognises, with the command each runs.
var commands = map[string]string{
	"scan":            "scan",
	"recommendations": "recommendations",
	"list":            "recommendations",
	"clean":           "clean",
	"projects":        "projects",
	"history":         "history",
	"restore":         "restore",
	"help":            "help",
	"--help":          "help",
	"-h":              "help",
	"version":         "version",
}

// IsCommand reports whether args (os.Args[1:]) start a command-line run rather than the app. Finder, Explorer, the login item and the single-instance hand-off never pass these words, and none of them is the elevated child's flag.
func IsCommand(args []string) bool {
	if len(args) == 0 {
		return false
	}
	_, ok := commands[args[0]]
	return ok
}

// Main runs the command args names (os.Args[1:], args[0] a word IsCommand accepts), writing its output to stdout and progress and notes to stderr, and returns the exit code. Cancelling ctx (Ctrl+C) stops a scan, or a clean after its current item, and returns ExitInterrupted.
func Main(ctx context.Context, args []string, d Deps, stdout, stderr io.Writer) int {
	if d.Now == nil {
		d.Now = time.Now
	}
	r := &run{ctx: ctx, d: d, stdout: stdout, stderr: stderr, json: wantsJSON(args)}
	if len(args) == 0 {
		return r.usage(errors.New("缺少命令"), "")
	}
	name, ok := commands[args[0]]
	if !ok {
		return r.usage(fmt.Errorf("未知的命令 %q", args[0]), "")
	}
	r.command = name
	rest := args[1:]
	switch name {
	case "scan":
		return r.scan(rest)
	case "recommendations":
		return r.recommendations(rest)
	case "clean":
		return r.clean(rest)
	case "projects":
		return r.projects(rest)
	case "history":
		return r.history(rest)
	case "restore":
		return r.restore(rest)
	case "version":
		return r.version(rest)
	}
	return r.help(rest)
}

// run is one command's state: where it writes and how.
type run struct {
	ctx            context.Context
	d              Deps
	stdout, stderr io.Writer
	command        string
	// json is --json: one JSON object on stdout.
	json bool
}

// usage reports a usage error and returns ExitUsage; command names the help to point at ("" for the general one).
func (r *run) usage(err error, command string) int {
	if r.json {
		r.writeError(err)
		return ExitUsage
	}
	hint := "spruce help"
	if command != "" {
		hint += " " + command
	}
	fmt.Fprintf(r.stderr, "spruce：%v\n运行 %s 查看用法。\n", err, hint)
	return ExitUsage
}

// fail reports err and returns the exit code it stands for: ExitInterrupted once ctx ended, ExitBusy for another clean, ExitFailed otherwise.
func (r *run) fail(err error) int {
	code := ExitFailed
	switch {
	case r.ctx.Err() != nil:
		code, err = ExitInterrupted, errors.New("已中断")
	case errors.Is(err, schedule.ErrBusyElsewhere), errors.Is(err, schedule.ErrBusy), errors.Is(err, cleanup.ErrBusy):
		code, err = ExitBusy, errors.New("Spruce 正在清理，请稍后再试")
	}
	if r.json {
		r.writeError(err)
	} else {
		fmt.Fprintf(r.stderr, "spruce：%v\n", err)
	}
	return code
}

// engine builds the engine over sources.
func (r *run) engine(sources []string) (Engine, error) {
	if r.d.Engine == nil {
		return nil, errors.New("命令行没有接入引擎")
	}
	return r.d.Engine(sources)
}

// note writes a line for the user to stderr, in text and JSON mode alike.
func (r *run) note(format string, a ...any) {
	fmt.Fprintf(r.stderr, format+"\n", a...)
}
