package maintenance

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"sync"
	"testing"

	"spruce/apps/spruce/internal/engine/elevate"
	"spruce/apps/spruce/internal/engine/taskresult"
)

// fakeElevator records the batches it is asked to run and answers with results or an error.
type fakeElevator struct {
	mu      sync.Mutex
	batches [][]string
	err     error
	outcome taskresult.Outcome
}

func (f *fakeElevator) Run(_ context.Context, ids []string) ([]taskresult.Result, error) {
	f.mu.Lock()
	f.batches = append(f.batches, slices.Clone(ids))
	f.mu.Unlock()
	if f.err != nil {
		return nil, f.err
	}
	out := make([]taskresult.Result, len(ids))
	for i, id := range ids {
		out[i] = taskresult.Result{TaskID: id, Outcome: f.outcome, Message: "admin " + id, Duration: 7}
	}
	return out, nil
}

// recorder collects the order in which user tasks ran.
type recorder struct {
	mu  sync.Mutex
	ran []string
}

func (r *recorder) task(id string, admin bool, outcome taskresult.Outcome) TaskImpl {
	return TaskImpl{
		Task: Task{ID: id, Title: id, Icon: IconNetwork, NeedsAdmin: admin},
		Run: func(context.Context, Env) taskresult.Result {
			r.mu.Lock()
			r.ran = append(r.ran, id)
			r.mu.Unlock()
			return taskresult.Result{Outcome: outcome, Message: "user " + id}
		},
	}
}

func newTestService(t *testing.T, tasks []TaskImpl, el elevate.Elevator) *Service {
	t.Helper()
	s, err := NewService(tasks, Env{}, el)
	if err != nil {
		t.Fatal(err)
	}
	return s
}

func TestNewServiceValidatesCatalog(t *testing.T) {
	run := func(context.Context, Env) taskresult.Result { return taskresult.Result{} }
	ok := Task{ID: "dns", Title: "DNS", Icon: IconNetwork}
	cases := map[string][]TaskImpl{
		"bad id":                 {{Task: Task{ID: "DNS", Title: "x", Icon: IconNetwork}, Run: run}},
		"duplicate":              {{Task: ok, Run: run}, {Task: ok, Run: run}},
		"no title":               {{Task: Task{ID: "dns", Icon: IconNetwork}, Run: run}},
		"no icon":                {{Task: Task{ID: "dns", Title: "x"}, Run: run}},
		"no run":                 {{Task: ok}},
		"admin without elevator": {{Task: Task{ID: "dns", Title: "x", Icon: IconNetwork, NeedsAdmin: true}, Run: run}},
	}
	for name, tasks := range cases {
		if _, err := NewService(tasks, Env{}, nil); err == nil {
			t.Errorf("%s: NewService succeeded", name)
		}
	}
}

func TestRunRejectsBadSelections(t *testing.T) {
	r := &recorder{}
	s := newTestService(t, []TaskImpl{r.task("dns", false, taskresult.Applied)}, nil)
	if _, err := s.Run(t.Context(), nil, nil); !errors.Is(err, ErrNoTasks) {
		t.Errorf("empty: %v", err)
	}
	if _, err := s.Run(t.Context(), []string{"dns", "nope"}, nil); !errors.Is(err, ErrUnknownTask) {
		t.Errorf("unknown: %v", err)
	}
	if len(r.ran) != 0 {
		t.Errorf("tasks ran: %v", r.ran)
	}
}

func TestRunBatchesAdminTasksOnce(t *testing.T) {
	r := &recorder{}
	el := &fakeElevator{outcome: taskresult.Applied}
	s := newTestService(t, []TaskImpl{
		r.task("dns", true, ""),
		r.task("quicklook", false, taskresult.Applied),
		r.task("icons", true, ""),
		r.task("databases", false, taskresult.Unchanged),
	}, el)

	var events []string
	got, err := s.Run(t.Context(), []string{"databases", "icons", "dns", "quicklook", "dns"}, func(ev Event) {
		events = append(events, fmt.Sprintf("%s:%s:%d/%d", ev.Kind, ev.TaskID, ev.Done, ev.Total))
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(el.batches) != 1 || !slices.Equal(el.batches[0], []string{"dns", "icons"}) {
		t.Errorf("elevated batches %v", el.batches)
	}
	if !slices.Equal(r.ran, []string{"quicklook", "databases"}) {
		t.Errorf("user tasks ran %v", r.ran)
	}
	var ids []string
	for _, res := range got {
		ids = append(ids, res.TaskID+"="+string(res.Outcome))
	}
	if want := []string{"dns=applied", "quicklook=applied", "icons=applied", "databases=unchanged"}; !slices.Equal(ids, want) {
		t.Errorf("results %v, want %v", ids, want)
	}
	if got[0].Duration != 7 {
		t.Errorf("elevated duration overwritten: %v", got[0].Duration)
	}
	want := []string{
		"started:dns:0/4", "started:icons:0/4",
		"finished:dns:1/4", "finished:icons:2/4",
		"started:quicklook:2/4", "finished:quicklook:3/4",
		"started:databases:3/4", "finished:databases:4/4",
	}
	if !slices.Equal(events, want) {
		t.Errorf("events\n got %v\nwant %v", events, want)
	}
}

func TestRunDeclinedPromptSkipsAdminTasks(t *testing.T) {
	r := &recorder{}
	el := &fakeElevator{err: elevate.ErrCancelled}
	s := newTestService(t, []TaskImpl{r.task("dns", true, ""), r.task("quicklook", false, taskresult.Applied)}, el)
	got, err := s.Run(t.Context(), []string{"dns", "quicklook"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Outcome != taskresult.Skipped || got[1].Outcome != taskresult.Applied {
		t.Errorf("results %+v", got)
	}
}

func TestRunElevationFailureFailsAdminTasks(t *testing.T) {
	el := &fakeElevator{err: errors.New("launch failed")}
	s := newTestService(t, []TaskImpl{(&recorder{}).task("dns", true, "")}, el)
	got, err := s.Run(t.Context(), []string{"dns"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Outcome != taskresult.Failed || got[0].Detail == "" {
		t.Errorf("result %+v", got[0])
	}
}

func TestRunPrecheckStopsTask(t *testing.T) {
	r := &recorder{}
	el := &fakeElevator{outcome: taskresult.Applied}
	admin := r.task("dns", true, "")
	admin.Precheck = func(context.Context, Env) *taskresult.Result {
		return &taskresult.Result{Outcome: taskresult.Unavailable, Message: "系统缺少该工具"}
	}
	user := r.task("databases", false, taskresult.Applied)
	user.Precheck = func(context.Context, Env) *taskresult.Result {
		return &taskresult.Result{Outcome: taskresult.Skipped, Message: "Safari 仍在运行"}
	}
	s := newTestService(t, []TaskImpl{admin, user}, el)
	got, err := s.Run(t.Context(), []string{"dns", "databases"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if len(el.batches) != 0 || len(r.ran) != 0 {
		t.Errorf("ran despite precheck: batches %v, user %v", el.batches, r.ran)
	}
	if got[0].Outcome != taskresult.Unavailable || got[1].Outcome != taskresult.Skipped || got[1].TaskID != "databases" {
		t.Errorf("results %+v", got)
	}
}

func TestRunInvalidOutcomeBecomesFailed(t *testing.T) {
	r := &recorder{}
	s := newTestService(t, []TaskImpl{r.task("dns", false, "ok")}, nil)
	got, err := s.Run(t.Context(), []string{"dns"}, nil)
	if err != nil {
		t.Fatal(err)
	}
	if got[0].Outcome != taskresult.Failed {
		t.Errorf("outcome %q", got[0].Outcome)
	}
}

func TestRunCancelledContextSkipsRemaining(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	first := TaskImpl{Task: Task{ID: "a", Title: "a", Icon: IconNetwork}, Run: func(context.Context, Env) taskresult.Result {
		cancel()
		return taskresult.Result{Outcome: taskresult.Applied}
	}}
	r := &recorder{}
	s := newTestService(t, []TaskImpl{first, r.task("b", false, taskresult.Applied)}, nil)
	got, err := s.Run(ctx, []string{"a", "b"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if len(got) != 2 || got[0].Outcome != taskresult.Applied || got[1].Outcome != taskresult.Skipped || len(r.ran) != 0 {
		t.Errorf("results %+v, ran %v", got, r.ran)
	}
}

func TestRunBusy(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	slow := TaskImpl{Task: Task{ID: "slow", Title: "slow", Icon: IconNetwork}, Run: func(context.Context, Env) taskresult.Result {
		close(entered)
		<-release
		return taskresult.Result{Outcome: taskresult.Applied}
	}}
	s := newTestService(t, []TaskImpl{slow}, nil)
	done := make(chan error)
	go func() {
		_, err := s.Run(t.Context(), []string{"slow"}, nil)
		done <- err
	}()
	<-entered
	if _, err := s.Run(t.Context(), []string{"slow"}, nil); !errors.Is(err, ErrBusy) {
		t.Errorf("second run: %v", err)
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}

func TestElevatedHandlerRunsOnlyAdminTasks(t *testing.T) {
	var gotEnv Env
	admin := TaskImpl{Task: Task{ID: "dns", Title: "dns", Icon: IconNetwork, NeedsAdmin: true}, Run: func(_ context.Context, env Env) taskresult.Result {
		gotEnv = env
		return taskresult.Result{Outcome: taskresult.Applied}
	}}
	r := &recorder{}
	h := ElevatedHandler([]TaskImpl{admin, r.task("quicklook", false, taskresult.Applied)}, Env{HomeDir: "/Users/someone", Permissions: fakePermissions{}})
	got := h(t.Context(), []string{"dns", "quicklook", "unknown"})
	if len(got) != 3 || got[0].Outcome != taskresult.Applied || got[1].Outcome != taskresult.Failed || got[2].Outcome != taskresult.Failed {
		t.Fatalf("results %+v", got)
	}
	if got[2].TaskID != "unknown" || len(r.ran) != 0 {
		t.Errorf("results %+v, user tasks ran %v", got, r.ran)
	}
	if !gotEnv.Elevated || gotEnv.HomeDir != "" || gotEnv.Permissions != nil || gotEnv.Log == nil {
		t.Errorf("elevated env %+v", gotEnv)
	}
}

type fakePermissions struct{}

func (fakePermissions) FullDiskAccess() (PermissionState, error) { return PermissionGranted, nil }

// TestRunKeepsTheResultsOfABatchThatFinishedAsItWasStopped stops the run while the elevated child runs, and the child answers anyway: its results are recorded as it reported them, and the user tasks after it stop.
func TestRunKeepsTheResultsOfABatchThatFinishedAsItWasStopped(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	el := elevatorFunc(func(_ context.Context, ids []string) ([]taskresult.Result, error) {
		cancel()
		out := make([]taskresult.Result, len(ids))
		for i, id := range ids {
			out[i] = taskresult.Result{TaskID: id, Outcome: taskresult.Applied, Message: "已刷新 DNS 缓存"}
		}
		return out, nil
	})
	r := &recorder{}
	s := newTestService(t, []TaskImpl{r.task("dns", true, ""), r.task("quicklook", false, taskresult.Applied)}, el)
	got, err := s.Run(ctx, []string{"dns", "quicklook"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if got[0].Outcome != taskresult.Applied || got[0].Message != "已刷新 DNS 缓存" {
		t.Errorf("admin result %+v, want the child's own", got[0])
	}
	if got[1].Outcome != taskresult.Skipped || len(r.ran) != 0 {
		t.Errorf("user result %+v, ran %v", got[1], r.ran)
	}
}

// TestStoppingEndsTheElevatedBatch stops the run while the elevated batch waits (on macOS, for the user to answer the system's prompt): the batch sees the cancellation, so the launcher kills osascript and the prompt closes, and its tasks are recorded as stopped without knowing whether they ran.
func TestStoppingEndsTheElevatedBatch(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	el := elevatorFunc(func(bctx context.Context, ids []string) ([]taskresult.Result, error) {
		cancel()
		<-bctx.Done()
		return nil, bctx.Err()
	})
	r := &recorder{}
	s := newTestService(t, []TaskImpl{r.task("dns", true, ""), r.task("quicklook", false, taskresult.Applied)}, el)
	got, err := s.Run(ctx, []string{"dns", "quicklook"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	if got[0].Outcome != taskresult.Skipped || !MayStillRun(got[0]) {
		t.Errorf("admin result %+v", got[0])
	}
	if got[1].Outcome != taskresult.Skipped || len(r.ran) != 0 {
		t.Errorf("user result %+v, ran %v", got[1], r.ran)
	}
}

// TestRunCancelledPrecheckIsInterrupted cancels during a precheck whose command then fails: the result must say the run was interrupted, not the precheck's own verdict about the failed command.
func TestRunCancelledPrecheckIsInterrupted(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	el := &fakeElevator{outcome: taskresult.Applied}
	r := &recorder{}
	dns := r.task("dns", true, "")
	dns.Precheck = func(context.Context, Env) *taskresult.Result {
		cancel()
		return &taskresult.Result{Outcome: taskresult.Skipped, Message: "无法确认 VPN 是否已连接，为避免断线已跳过"}
	}
	laterChecked := false
	later := r.task("spotlight", true, "")
	later.Precheck = func(context.Context, Env) *taskresult.Result {
		laterChecked = true
		return nil
	}
	s := newTestService(t, []TaskImpl{dns, later}, el)
	got, err := s.Run(ctx, []string{"dns", "spotlight"}, nil)
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("err = %v", err)
	}
	for _, res := range got {
		if res != (taskresult.Result{TaskID: res.TaskID, Outcome: taskresult.Skipped, Message: "维护已中断，未执行", Detail: context.Canceled.Error()}) {
			t.Errorf("result %+v, want interrupted", res)
		}
	}
	if laterChecked || len(el.batches) != 0 {
		t.Errorf("ran after the cancel: precheck %v, batches %v", laterChecked, el.batches)
	}
}

type elevatorFunc func(ctx context.Context, ids []string) ([]taskresult.Result, error)

func (f elevatorFunc) Run(ctx context.Context, ids []string) ([]taskresult.Result, error) {
	return f(ctx, ids)
}
