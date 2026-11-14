// Package execx runs external commands for the engine behind a small Runner interface, so that every task that shells out (dscacheutil, mdutil, qlmanage, lsregister, ipconfig, sfc …) can be tested with a fake instead of touching the real system.
//
// Contract for callers:
//
//   - Build a Command with the program Name (a bare name resolved through PATH, or an absolute path, which is preferred for system tools such as /usr/bin/qlmanage) and its Args. Arguments are passed directly to the program, never through a shell.
//   - Runner.Run always returns the captured Output, even together with an error, so Detail strings can include stderr.
//   - err is nil only when the program started and exited with status 0. A non-zero exit returns an *ExitError (match it with errors.As); a program that is not installed returns an error matching exec.ErrNotFound or fs.ErrNotExist; a Command.Timeout or a cancelled context returns an error matching context.DeadlineExceeded or context.Canceled.
//
// OSRunner is the real implementation over os/exec. Package execxtest provides Fake for tests.
package execx
