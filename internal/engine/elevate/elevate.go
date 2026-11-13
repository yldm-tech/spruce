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

	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// Flag is the first argument that turns the app's executable into the elevated child.
const Flag = "--spruce-elevated"

// WireVersion is the version of Request and Response. The parent and the child are the same executable, so a mismatch means a corrupted or foreign file.
const WireVersion = 1

// File names inside the exchange directory.
const (
	RequestFile  = "request.json"
	ResponseFile = "response.json"
)

// Size limits for the files read at the process boundary.
const (
	maxRequestSize  = 64 << 10
	maxResponseSize = 1 << 20
)

// ErrCancelled is returned when the user declined or dismissed the administrator prompt. The maintenance runner reports the admin tasks as skipped, not failed.
var ErrCancelled = errors.New("elevate: the administrator prompt was declined")

// Elevator runs admin tasks with administrator rights.
type Elevator interface {
	// Run runs the admin tasks named by taskIDs in one elevated batch, behind at most one system prompt, and returns one result per ID in the order of taskIDs. It returns ErrCancelled when the user declined the prompt, and another error when the batch could not run at all; a task that ran and failed is a Result with taskresult.Failed, not an error.
	Run(ctx context.Context, taskIDs []string) ([]taskresult.Result, error)
}

// Request is what the parent asks the elevated child to do.
type Request struct {
	Version int      `json:"version"`
	TaskIDs []string `json:"taskIds"`
}

// Response is what the elevated child did.
type Response struct {
	Version int                 `json:"version"`
	Results []taskresult.Result `json:"results"`
	// Error is set when the child rejected the request as a whole.
	Error string `json:"error,omitempty"`
}

// Launcher starts a program with administrator rights. See the package documentation for the contract each platform implements.
type Launcher interface {
	// Launch starts exe with args elevated and waits for it to exit. It returns ErrCancelled when the user declined the prompt.
	Launch(ctx context.Context, exe string, args []string) error
}

// Handler runs the requested admin tasks inside the elevated child and returns one result per ID, in order. maintenance.ElevatedHandler provides it.
type Handler func(ctx context.Context, taskIDs []string) []taskresult.Result

// taskIDPattern is the syntax of task IDs accepted across the process boundary.
var taskIDPattern = regexp.MustCompile(`^[a-z0-9][a-z0-9-]{0,63}$`)

// ValidTaskID reports whether id has the syntax of a task ID: lower-case letters, digits and hyphens, at most 64 characters.
func ValidTaskID(id string) bool { return taskIDPattern.MatchString(id) }

// ProcessElevator implements Elevator by relaunching the app's executable through a Launcher.
type ProcessElevator struct {
	// Executable is the absolute path of the app's own executable (os.Executable).
	Executable string
	// Launcher starts it elevated.
	Launcher Launcher
	// TempDir is where exchange directories are created; empty means os.TempDir.
	TempDir string
}

var _ Elevator = (*ProcessElevator)(nil)

// New returns a ProcessElevator for executable that launches through l.
func New(executable string, l Launcher) *ProcessElevator {
	return &ProcessElevator{Executable: executable, Launcher: l}
}

// Run implements Elevator.
func (p *ProcessElevator) Run(ctx context.Context, taskIDs []string) ([]taskresult.Result, error) {
	if len(taskIDs) == 0 {
		return nil, nil
	}
	if err := validateIDs(taskIDs); err != nil {
		return nil, err
	}
	dir, err := os.MkdirTemp(p.TempDir, "spruce-elevated-")
	if err != nil {
		return nil, fmt.Errorf("elevate: create exchange directory: %w", err)
	}
	defer os.RemoveAll(dir)

	req, err := json.Marshal(Request{Version: WireVersion, TaskIDs: taskIDs})
	if err != nil {
		return nil, err
	}
	if err := os.WriteFile(filepath.Join(dir, RequestFile), req, 0o600); err != nil {
		return nil, fmt.Errorf("elevate: write request: %w", err)
	}
	if err := p.Launcher.Launch(ctx, p.Executable, []string{Flag, dir}); err != nil {
		if errors.Is(err, ErrCancelled) {
			return nil, ErrCancelled
		}
		return nil, fmt.Errorf("elevate: launch: %w", err)
	}
	resp, err := readResponse(filepath.Join(dir, ResponseFile))
	if err != nil {
		return nil, err
	}
	return matchResults(taskIDs, resp.Results)
}

func readResponse(path string) (Response, error) {
	f, err := os.Open(path)
	if err != nil {
		return Response{}, fmt.Errorf("elevate: the elevated process wrote no response: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxResponseSize+1))
	if err != nil {
		return Response{}, fmt.Errorf("elevate: read response: %w", err)
	}
	if len(data) > maxResponseSize {
		return Response{}, errors.New("elevate: response too large")
	}
	var resp Response
	if err := json.Unmarshal(data, &resp); err != nil {
		return Response{}, fmt.Errorf("elevate: decode response: %w", err)
	}
	if resp.Version != WireVersion {
		return Response{}, fmt.Errorf("elevate: response version %d, want %d", resp.Version, WireVersion)
	}
	if resp.Error != "" {
		return Response{}, fmt.Errorf("elevate: the elevated process rejected the request: %s", resp.Error)
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

// IsChild reports whether args (os.Args[1:]) start the elevated child.
func IsChild(args []string) bool {
	return len(args) > 0 && args[0] == Flag
}

// Exit codes of ServeChild.
const (
	ExitOK         = 0
	ExitBadRequest = 2
	ExitIOError    = 3
)

// ServeChild is the whole life of the elevated child: it reads the request from the exchange directory named in args ([Flag, directory]), runs h, writes the response and returns the process exit code.
func ServeChild(ctx context.Context, args []string, h Handler) int {
	if len(args) != 2 || args[0] != Flag {
		fmt.Fprintf(os.Stderr, "usage: %s <exchange directory>\n", Flag)
		return ExitBadRequest
	}
	dir := args[1]
	if !filepath.IsAbs(dir) {
		fmt.Fprintln(os.Stderr, "elevate: the exchange directory must be absolute")
		return ExitBadRequest
	}
	fi, err := os.Lstat(dir)
	if err != nil || !fi.IsDir() {
		fmt.Fprintln(os.Stderr, "elevate: the exchange directory is not a directory")
		return ExitBadRequest
	}
	root, err := os.OpenRoot(dir)
	if err != nil {
		fmt.Fprintf(os.Stderr, "elevate: %v\n", err)
		return ExitIOError
	}
	defer root.Close()

	req, err := readRequest(root)
	if err != nil {
		if werr := writeResponse(root, Response{Version: WireVersion, Error: err.Error()}); werr != nil {
			fmt.Fprintf(os.Stderr, "elevate: %v\n", werr)
			return ExitIOError
		}
		return ExitBadRequest
	}
	results := h(ctx, req.TaskIDs)
	if err := writeResponse(root, Response{Version: WireVersion, Results: results}); err != nil {
		fmt.Fprintf(os.Stderr, "elevate: %v\n", err)
		return ExitIOError
	}
	return ExitOK
}

func readRequest(root *os.Root) (Request, error) {
	f, err := root.Open(RequestFile)
	if err != nil {
		return Request{}, fmt.Errorf("open request: %w", err)
	}
	defer f.Close()
	data, err := io.ReadAll(io.LimitReader(f, maxRequestSize+1))
	if err != nil {
		return Request{}, fmt.Errorf("read request: %w", err)
	}
	if len(data) > maxRequestSize {
		return Request{}, errors.New("request too large")
	}
	dec := json.NewDecoder(bytes.NewReader(data))
	dec.DisallowUnknownFields()
	var req Request
	if err := dec.Decode(&req); err != nil {
		return Request{}, fmt.Errorf("decode request: %w", err)
	}
	if req.Version != WireVersion {
		return Request{}, fmt.Errorf("request version %d, want %d", req.Version, WireVersion)
	}
	if len(req.TaskIDs) == 0 {
		return Request{}, errors.New("request names no tasks")
	}
	if err := validateIDs(req.TaskIDs); err != nil {
		return Request{}, err
	}
	return req, nil
}

// writeResponse writes resp to ResponseFile through a fresh temporary file renamed into place, all inside root, so neither a planted file nor a symlink can redirect the write. The file is world-readable because the child may run as root while the parent reads it as the user.
func writeResponse(root *os.Root, resp Response) error {
	data, err := json.Marshal(resp)
	if err != nil {
		return err
	}
	const tmp = ResponseFile + ".tmp"
	_ = root.Remove(tmp)
	f, err := root.OpenFile(tmp, os.O_WRONLY|os.O_CREATE|os.O_EXCL, 0o644)
	if err != nil {
		return fmt.Errorf("create response: %w", err)
	}
	if _, err := f.Write(data); err != nil {
		f.Close()
		return fmt.Errorf("write response: %w", err)
	}
	if err := f.Close(); err != nil {
		return fmt.Errorf("write response: %w", err)
	}
	if err := root.Rename(tmp, ResponseFile); err != nil {
		return fmt.Errorf("rename response: %w", err)
	}
	return nil
}
