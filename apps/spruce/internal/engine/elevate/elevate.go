package elevate

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"slices"
	"strings"

	"spruce/apps/spruce/internal/engine/taskresult"
)

// Flag is the first argument that turns the app's executable into the elevated child.
const Flag = "--spruce-elevated"

// Child arguments after Flag. See ServeChild for the grammar.
const (
	tasksFlag = "--tasks"
	outFlag   = "--out"
)

// WireVersion is the version of Response. The parent and the child are the same executable, so a mismatch means a corrupted or foreign response.
const WireVersion = 1

// ResponseFile is the name of the response file inside the exchange directory, on platforms whose child answers through a file (Windows).
const ResponseFile = "response.json"

// exchangeDirPrefix is the name prefix of the exchange directory; os.MkdirTemp appends decimal digits.
const exchangeDirPrefix = "spruce-elevated-"

// maxResponseSize bounds the response read at the process boundary.
const maxResponseSize = 1 << 20

// ErrCancelled is returned when the user declined or dismissed the administrator prompt. The maintenance runner reports the admin tasks as skipped, not failed.
var ErrCancelled = errors.New("elevate: the administrator prompt was declined")

// Elevator runs admin tasks with administrator rights.
type Elevator interface {
	// Run runs the admin tasks named by taskIDs in one elevated batch, behind at most one system prompt, and returns one result per ID in the order of taskIDs. It returns ErrCancelled when the user declined the prompt, and another error when the batch could not run at all; a task that ran and failed is a Result with taskresult.Failed, not an error.
	Run(ctx context.Context, taskIDs []string) ([]taskresult.Result, error)
}

// Response is what the elevated child did.
type Response struct {
	Version int                 `json:"version"`
	Results []taskresult.Result `json:"results"`
	// Error is set when the child refused the request as a whole.
	Error string `json:"error,omitempty"`
}

// Launcher starts a program with administrator rights. NewLauncher returns the one of the running platform.
type Launcher interface {
	// Launch starts exe with args elevated, behind one system prompt, and waits for it to exit. It returns what the program wrote to standard output when the platform can capture it (macOS) and nil otherwise (Windows). The error is nil only when the program ran and exited with status 0; it is ErrCancelled when the user declined or dismissed the prompt.
	Launch(ctx context.Context, exe string, args []string) (stdout []byte, err error)
}

// Handler runs the requested admin tasks inside the elevated child and returns one result per ID, in order. maintenance.ElevatedHandler provides it.
type Handler func(ctx context.Context, taskIDs []string) []taskresult.Result

// channel is how the elevated child hands its Response back to the parent.
type channel int

const (
	// viaStdout: the child prints the Response on standard output, which the launcher captures (macOS, where osascript returns the output of "do shell script"). Nothing is written to disk by the elevated process.
	viaStdout channel = iota
	// viaFile: the child creates ResponseFile in an exchange directory the parent made with os.MkdirTemp (Windows, where ShellExecuteEx cannot capture the output of an elevated process).
	viaFile
)

// taskIDPattern is the syntax of task IDs accepted across the process boundary.
var taskIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidTaskID reports whether id has the syntax of a task ID: lower-case letters, digits and hyphens, at most 64 characters.
func ValidTaskID(id string) bool { return taskIDPattern.MatchString(id) }

// ProcessElevator implements Elevator by relaunching the app's executable through a Launcher.
type ProcessElevator struct {
	// Executable is the absolute path of the app's own executable (os.Executable, resolved through symlinks).
	Executable string
	// Launcher starts it elevated.
	Launcher Launcher
	// TempDir is where exchange directories are created when the platform answers through a file; empty means os.TempDir.
	TempDir string

	channel channel
}

var _ Elevator = (*ProcessElevator)(nil)

// New returns a ProcessElevator for executable that launches through l, using the response channel of the running platform.
func New(executable string, l Launcher) *ProcessElevator {
	return &ProcessElevator{Executable: executable, Launcher: l, channel: platformChannel}
}

// Run implements Elevator.
func (p *ProcessElevator) Run(ctx context.Context, taskIDs []string) ([]taskresult.Result, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}
	if err := validateIDs(taskIDs); err != nil {
		return nil, err
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	args := []string{Flag, tasksFlag, strings.Join(taskIDs, ",")}

	var resp Response
	switch p.channel {
	case viaStdout:
		out, err := p.Launcher.Launch(ctx, p.Executable, args)
		if err != nil {
			return nil, launchError(err)
		}
		if resp, err = decodeResponse(out); err != nil {
			return nil, err
		}
	case viaFile:
		dir, err := os.MkdirTemp(p.TempDir, exchangeDirPrefix)
		if err != nil {
			return nil, fmt.Errorf("elevate: create exchange directory: %w", err)
		}
		defer os.RemoveAll(dir)
		// The child rejects a path with a symlink, junction or 8.3 short name in it (a temp directory such as C:\Users\RUNNER~1\… or macOS's /var/folders), so it gets the resolved form.
		if dir, err = filepath.EvalSymlinks(dir); err != nil {
			return nil, fmt.Errorf("elevate: resolve exchange directory: %w", err)
		}
		_, lerr := p.Launcher.Launch(ctx, p.Executable, append(args, outFlag, dir))
		if errors.Is(lerr, ErrCancelled) {
			return nil, ErrCancelled
		}
		resp, err = readResponseFile(filepath.Join(dir, ResponseFile))
		switch {
		case err == nil && lerr == nil:
		case err == nil && resp.Error != "":
			// The child refused the request and exited non-zero; its own explanation is the better error.
			return nil, fmt.Errorf("elevate: the elevated process refused the request: %s", resp.Error)
		case lerr != nil:
			return nil, launchError(lerr)
		default:
			return nil, err
		}
	default:
		return nil, fmt.Errorf("elevate: unknown response channel %d", p.channel)
	}
	if resp.Error != "" {
		return nil, fmt.Errorf("elevate: the elevated process refused the request: %s", resp.Error)
	}
	return matchResults(taskIDs, resp.Results)
}

func launchError(err error) error {
	if errors.Is(err, ErrCancelled) {
		return ErrCancelled
	}
	return fmt.Errorf("elevate: launch: %w", err)
}

func readResponseFile(path string) (Response, error) {
	f, err := os.Open(path)
	if err != nil {
		return Response{}, fmt.Errorf("elevate: the elevated process wrote no response: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxResponseSize+1))
	if err != nil {
		return Response{}, fmt.Errorf("elevate: read response: %w", err)
	}
	return decodeResponse(data)
}

// decodeResponse validates the bytes the child handed back: size, JSON and version. A refusal (Response.Error) is returned as data for the caller to report.
func decodeResponse(data []byte) (Response, error) {
	if len(data) > maxResponseSize {
		return Response{}, errors.New("elevate: response too large")
	}
	data = bytes.TrimSpace(data)
	if len(data) == 0 {
		return Response{}, errors.New("elevate: the elevated process returned no response")
	}
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return Response{}, fmt.Errorf("elevate: decode response: %w", err)
	}
	if resp.Version != WireVersion {
		return Response{}, fmt.Errorf("elevate: response version %d, want %d", resp.Version, WireVersion)
	}
	return resp, nil
}

// matchResults returns the results in the order of ids, rejecting results for IDs that were not requested, invalid outcomes and missing results.
func matchResults(ids []string, results []taskresult.Result) ([]taskresult.Result, error) {
	byID := make(map[string]taskresult.Result, len(results))
	for _, r := range results {
		if !slices.Contains(ids, r.TaskID) {
			return nil, fmt.Errorf("elevate: response has a result for %q, which was not requested", r.TaskID)
		}
		if !r.Outcome.Valid() {
			return nil, fmt.Errorf("elevate: response has outcome %q for %q", r.Outcome, r.TaskID)
		}
		if r.Partial && r.Outcome != taskresult.Applied {
			return nil, fmt.Errorf("elevate: response marks outcome %q for %q as partial", r.Outcome, r.TaskID)
		}
		if !r.Reason.Valid() {
			return nil, fmt.Errorf("elevate: response has reason %q for %q", r.Reason, r.TaskID)
		}
		if _, dup := byID[r.TaskID]; dup {
			return nil, fmt.Errorf("elevate: response has two results for %q", r.TaskID)
		}
		byID[r.TaskID] = r
	}
	out := make([]taskresult.Result, 0, len(ids))
	for _, id := range ids {
		r, ok := byID[id]
		if !ok {
			return nil, fmt.Errorf("elevate: response has no result for %q", id)
		}
		out = append(out, r)
	}
	return out, nil
}

func validateIDs(ids []string) error {
	seen := make(map[string]bool, len(ids))
	for _, id := range ids {
		if !ValidTaskID(id) {
			return fmt.Errorf("elevate: invalid task ID %q", id)
		}
		if seen[id] {
			return fmt.Errorf("elevate: task ID %q requested twice", id)
		}
		seen[id] = true
	}
	return nil
}

// selfImage is the running executable as found when the launcher was created at startup: its path with symlinks resolved, and a check that the file at that path is still the one that was there.
type selfImage struct {
	path string
	// unchanged returns an error when the file now at path is not the file that was there at startup.
	unchanged func() error
}

// snapshotSelf records the running executable. Launchers call it when they are created, which main.go does at startup, so a file put at that path later (a rename-and-replace of the running image) no longer matches.
func snapshotSelf() (selfImage, error) {
	exe, err := os.Executable()
	if err != nil {
		return selfImage{}, err
	}
	if exe, err = filepath.EvalSymlinks(exe); err != nil {
		return selfImage{}, err
	}
	return imageAt(exe)
}

// imageAt records the identity of the file at path (device and inode on macOS, volume serial and file index on Windows).
func imageAt(path string) (selfImage, error) {
	start, err := fileIdentity(path)
	if err != nil {
		return selfImage{}, err
	}
	return selfImage{path: path, unchanged: func() error {
		now, err := fileIdentity(path)
		if err != nil {
			return err
		}
		if !os.SameFile(start, now) {
			return fmt.Errorf("%q is no longer the file the app was started from", path)
		}
		return nil
	}}, nil
}

// fileIdentity stats path through an open handle. On Windows a FileInfo from os.Stat loads the file ID lazily, from the path, at the first os.SameFile call, which would compare whatever file is at the path then; File.Stat reads it from the handle right away.
func fileIdentity(path string) (os.FileInfo, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, err
	}
	defer f.Close()
	return f.Stat()
}

// checkSelf refuses to elevate any path other than the one the process started from: exe must be absolute, already resolved through symlinks, equal to that path, and still the same file (device and inode, or volume serial and file index) that was there when the launcher was created. It compares paths and file identity, not code signatures, so it does not catch a binary overwritten in place under the same path; see Known limitations in the package documentation.
func checkSelf(exe string, self func() (selfImage, error)) error {
	if !filepath.IsAbs(exe) || filepath.Clean(exe) != exe {
		return fmt.Errorf("elevate: %q is not a clean absolute path", exe)
	}
	own, err := self()
	if err != nil {
		return fmt.Errorf("elevate: locate the running executable: %w", err)
	}
	if !samePath(exe, own.path) {
		return fmt.Errorf("elevate: refusing to elevate %q, which is not the running executable %q", exe, own.path)
	}
	if err := own.unchanged(); err != nil {
		return fmt.Errorf("elevate: refusing to elevate: %w", err)
	}
	return nil
}

// IsChild reports whether args (os.Args[1:]) start the elevated child.
func IsChild(args []string) bool {
	return len(args) > 0 && args[0] == Flag
}

// Exit codes of ServeChild.
const (
	ExitOK          = 0
	ExitBadRequest  = 2
	ExitIOError     = 3
	ExitNotElevated = 4
)

// childEnv is what serveChild needs from the process, so that tests can play the elevated child without being elevated.
type childEnv struct {
	channel channel
	// stdout receives the Response with viaStdout.
	stdout io.Writer
	// stderr receives diagnostics.
	stderr io.Writer
	// checkDir is, with viaFile, the platform's check that the exchange directory may receive the response (on Windows: it is owned by an unprivileged account).
	checkDir func(dir string) error
	// elevated reports whether the process runs with administrator rights.
	elevated func() bool
}

// ServeChild is the whole life of the elevated child. args is os.Args[1:], which must be exactly
//
//	--spruce-elevated --tasks <id>[,<id>...]                (macOS)
//	--spruce-elevated --tasks <id>[,<id>...] --out <dir>    (Windows)
//
// where every ID is in allowed (the admin task IDs of the compiled-in catalog) and named once, and <dir> is an exchange directory: a plain directory named spruce-elevated-<digits>, reached without symlinks or junctions and owned by an unprivileged account. It refuses to run anything unless the process is elevated (euid 0 on macOS, an elevated token on Windows), runs h, writes the Response (to stdout on macOS, to <dir>/response.json created exclusively on Windows) and returns the process exit code.
func ServeChild(ctx context.Context, args []string, allowed []string, h Handler) int {
	return serveChild(ctx, args, allowed, h, platformChildEnv())
}

func serveChild(ctx context.Context, args []string, allowed []string, h Handler, env childEnv) int {
	ids, dir, err := parseChildArgs(args, allowed, env.channel)
	if err != nil {
		fmt.Fprintf(env.stderr, "elevate: %v\n", err)
		return ExitBadRequest
	}

	// The output is opened before anything runs, so an unusable exchange directory stops the child early.
	out, closeOut := env.stdout, func() error { return nil }
	if env.channel == viaFile {
		f, err := createResponseFile(dir, env.checkDir)
		if err != nil {
			fmt.Fprintf(env.stderr, "elevate: %v\n", err)
			return ExitIOError
		}
		out, closeOut = f, f.Close
	}

	resp, code := Response{Version: WireVersion}, ExitOK
	if env.elevated() {
		resp.Results = h(ctx, ids)
	} else {
		resp.Error = "the process is not running with administrator rights"
		fmt.Fprintf(env.stderr, "elevate: %s\n", resp.Error)
		code = ExitNotElevated
	}
	err = writeResponse(out, resp)
	if cerr := closeOut(); err == nil {
		err = cerr
	}
	if err != nil {
		fmt.Fprintf(env.stderr, "elevate: write response: %v\n", err)
		return ExitIOError
	}
	return code
}

// parseChildArgs checks args against the grammar of ServeChild and returns the task IDs and, with viaFile, the exchange directory.
func parseChildArgs(args []string, allowed []string, ch channel) (ids []string, dir string, err error) {
	want := 3
	if ch == viaFile {
		want = 5
	}
	if len(args) != want || args[0] != Flag || args[1] != tasksFlag {
		usage := Flag + " " + tasksFlag + " <id>[,<id>...]"
		if ch == viaFile {
			usage += " " + outFlag + " <dir>"
		}
		return nil, "", errors.New("usage: " + usage)
	}
	if ch == viaFile && args[3] != outFlag {
		return nil, "", fmt.Errorf("unexpected argument %q", args[3])
	}
	ids = strings.Split(args[2], ",")
	if err := validateIDs(ids); err != nil {
		return nil, "", err
	}
	for _, id := range ids {
		if !slices.Contains(allowed, id) {
			return nil, "", fmt.Errorf("task %q is not an administrator task of this build", id)
		}
	}
	if ch == viaFile {
		dir = args[4]
	}
	return ids, dir, nil
}

// createResponseFile creates ResponseFile inside dir exclusively. dir must be a clean absolute path named like an exchange directory, with no symlink or junction anywhere in it, naming a plain directory that checkDir accepts. The file is created through an os.Root with O_EXCL, so an existing file or a planted symlink at that name makes it fail instead of being followed or overwritten.
//
// The parent directory is deliberately not compared with the child's own temp directory: an elevated process gets its environment from the elevated account's profile, so with over-the-shoulder elevation (a standard user typing an administrator's credentials) the child's %TEMP% is the administrator's while the parent made the directory in the standard user's.
func createResponseFile(dir string, checkDir func(string) error) (*os.File, error) {
	if !filepath.IsAbs(dir) || filepath.Clean(dir) != dir {
		return nil, fmt.Errorf("the exchange directory %q is not a clean absolute path", dir)
	}
	name := filepath.Base(dir)
	suffix, ok := strings.CutPrefix(name, exchangeDirPrefix)
	if !ok || suffix == "" || strings.Trim(suffix, "0123456789") != "" {
		return nil, fmt.Errorf("the exchange directory %q is not named %s<digits>", dir, exchangeDirPrefix)
	}
	resolved, err := filepath.EvalSymlinks(dir)
	if err != nil {
		return nil, fmt.Errorf("resolve the exchange directory: %w", err)
	}
	if !samePath(resolved, dir) {
		return nil, fmt.Errorf("the exchange directory %q is reached through a symlink or junction (%q)", dir, resolved)
	}
	fi, err := os.Lstat(dir)
	if err != nil {
		return nil, fmt.Errorf("the exchange directory: %w", err)
	}
	if fi.Mode().Type() != os.ModeDir {
		return nil, fmt.Errorf("the exchange directory %q is not a plain directory", dir)
	}
	if err := checkDir(dir); err != nil {
		return nil, fmt.Errorf("the exchange directory %q: %w", dir, err)
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		return nil, err
	}
	defer root.Close()
	f, err := root.OpenFile(ResponseFile, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return nil, fmt.Errorf("create response: %w", err)
	}
	return f, nil
}

func writeResponse(w io.Writer, resp Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	_, err = w.Write(append(data, '\n'))
	return err
}
