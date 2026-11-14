// Package history records what the app did, for the 操作历史 page and for support: maintenance runs (with one taskresult.Result per task) and the clean-ups of the cleaner pages (with Summary totals and Details lines).
//
// # Types (history.go)
//
// Kind, Run and Store are fixed here. The caller decides where the file lives: main.go passes a path inside the app's data folder (internal/host/datadir).
//
// # Store (store.go)
//
// The file-backed Store:
//
//	// Open returns a Store keeping its runs in the file at path (JSON Lines, one Run per line, appended). Open performs no I/O: the directory and the file are created on the first Append, and a missing file lists as empty.
//	func Open(path string) (Store, error)
//
// Requirements: Append, List, Prune and Clear are safe for concurrent use within the process; Append writes the whole line with one write and fsyncs; List tolerates (skips and logs) a truncated or corrupt line, such as the last line after a crash, instead of failing; the file is capped at MaxRuns runs by rewriting it through a temporary file and a rename when it grows past that. Prune(cutoff) drops runs that started before cutoff the same way (main.go prunes with the retention of the settings, RetentionCutoff, at start, on every settings change and daily); Clear deletes the file.
//
// # Watching (watch.go)
//
// The Store that Open returns is also a Watcher: Subscribe delivers each run once Append has written it, without ever blocking Append. The services layer forwards them to the page as the "history:appended" event.
//
// Fields added after the first release (Summary, Details, and in v2 Rule, Undo and RestoreOf) are optional in JSON, so files written before them read unchanged, maintenance runs carry none of them, and an older build reading a newer file ignores what it does not know.
package history
