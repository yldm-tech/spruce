// Package history records what the app did, for the 操作历史 page and for support. This milestone records maintenance runs only.
//
// # Types (history.go)
//
// Kind, Run and Store are fixed here. The caller decides where the file lives: main.go passes a path inside mygo.App.Path(mygo.PathUserData).
//
// # Store (store.go)
//
// The file-backed Store:
//
//	// Open returns a Store keeping its runs in the file at path (JSON Lines, one Run per line, appended). Open performs no I/O: the directory and the file are created on the first Append, and a missing file lists as empty. This keeps `mygo generate`, which runs main up to App.Run, from touching the user's data.
//	func Open(path string) (Store, error)
//
// Requirements: Append and List are safe for concurrent use within the process; Append writes the whole line with one write and fsyncs; List tolerates (skips and logs) a truncated or corrupt line, such as the last line after a crash, instead of failing; the file is capped at MaxRuns runs by rewriting it through a temporary file and a rename when it grows past that.
package history
