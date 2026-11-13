package maintenance

import (
	"context"
	"errors"
	"fmt"
	"time"

	"golang.org/x/sys/windows"
	"golang.org/x/sys/windows/svc"
	"golang.org/x/sys/windows/svc/mgr"

	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// winSearchService is the service name of Windows Search, the indexer behind Start menu and File Explorer search.
const winSearchService = "WSearch"

// winDelayedStartWindow is how long after boot a delayed automatic service may legitimately not have started yet: the SCM starts delayed services about 120 s after the last automatic service has started, and a slow disk stretches boot itself.
const winDelayedStartWindow = 10 * time.Minute

// errServiceMissing is returned by winServiceManager when the service is not installed (ERROR_SERVICE_DOES_NOT_EXIST).
var errServiceMissing = errors.New("service is not installed")

// winServiceInfo is the part of a service's status and configuration the search task looks at.
type winServiceInfo struct {
	State svc.State
	// StartType is mgr.StartAutomatic, mgr.StartManual or mgr.StartDisabled (boot and system start do not apply to WSearch).
	StartType        uint32
	DelayedAutoStart bool
	// Win32ExitCode is the error the service last stopped with; 0 for a clean stop.
	Win32ExitCode uint32
}

// winServiceManager queries and starts services. Query needs only read access, which a standard user has; Start needs administrator rights for WSearch.
type winServiceManager interface {
	Query(name string) (winServiceInfo, error)
	Start(name string) error
}

// winSearchTask checks that Windows Search is running and starts it when it is stopped although its start type is automatic. The check runs as Precheck in the app process, so a healthy service is reported without a UAC prompt; only a stopped service reaches Run in the elevated child. A disabled service is left alone: users disable it on purpose.
func winSearchTask(sys *winSystem) TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "search",
			Title:            "检查 Windows 搜索",
			Description:      "开始菜单搜不到文件时有用；仅在搜索服务意外停止时重新启动它，不会重建索引",
			Icon:             IconSearch,
			DefaultOn:        false,
			NeedsAdmin:       true,
			EstimatedSeconds: 5,
		},
		Precheck: func(_ context.Context, _ Env) *taskresult.Result {
			info, res := winQuerySearch(sys)
			if res != nil {
				return res
			}
			return winSearchVerdict(info, sys.uptime())
		},
		Run: func(ctx context.Context, env Env) taskresult.Result {
			info, res := winQuerySearch(sys)
			if res != nil {
				return *res
			}
			// The state may have changed between the precheck and the elevated batch.
			if res := winSearchVerdict(info, sys.uptime()); res != nil {
				return *res
			}
			before := fmt.Sprintf("WSearch was stopped (start type %s, last exit code %d)", winStartTypeName(info), info.Win32ExitCode)
			env.Log.Info("starting Windows Search", "exitCode", info.Win32ExitCode)
			if err := sys.services.Start(winSearchService); err != nil && !errors.Is(err, windows.ERROR_SERVICE_ALREADY_RUNNING) {
				return taskresult.Result{Outcome: taskresult.Failed, Message: "未能启动 Windows 搜索服务", Detail: before + "; start: " + err.Error()}
			}
			return winWaitForSearch(ctx, sys, before)
		},
	}
}

// winQuerySearch reads the WSearch service, or returns the Result that ends the task when it cannot.
func winQuerySearch(sys *winSystem) (winServiceInfo, *taskresult.Result) {
	info, err := sys.services.Query(winSearchService)
	switch {
	case errors.Is(err, errServiceMissing):
		return info, &taskresult.Result{Outcome: taskresult.Unavailable, Message: "此系统没有安装 Windows 搜索服务", Detail: err.Error()}
	case err != nil:
		return info, &taskresult.Result{Outcome: taskresult.Failed, Message: "无法读取 Windows 搜索服务的状态", Detail: err.Error()}
	}
	return info, nil
}

// winSearchVerdict returns the final Result for a service that needs no action, or nil when it is stopped although it should run automatically. WSearch is "Automatic (Delayed Start)" by default, so for a couple of minutes after boot it is legitimately stopped and has never run (ERROR_SERVICE_NEVER_STARTED); that window, judged by uptime, needs no action either. A delayed service that still has not started long after boot, or one that stopped with any exit code, is acted on.
func winSearchVerdict(info winServiceInfo, uptime time.Duration) *taskresult.Result {
	detail := fmt.Sprintf("WSearch state %d, start type %s", info.State, winStartTypeName(info))
	switch {
	case info.StartType == mgr.StartDisabled:
		return &taskresult.Result{Outcome: taskresult.Skipped, Message: "Windows 搜索服务已被禁用，未做改动；需要时可在「服务」里把它设为自动启动", Detail: detail}
	case info.State == svc.Running:
		return &taskresult.Result{Outcome: taskresult.Unchanged, Message: "Windows 搜索服务运行正常", Detail: detail}
	case info.State == svc.StartPending || info.State == svc.ContinuePending:
		return &taskresult.Result{Outcome: taskresult.Unchanged, Message: "Windows 搜索服务正在启动", Detail: detail}
	case info.State == svc.StopPending:
		return &taskresult.Result{Outcome: taskresult.Skipped, Message: "Windows 搜索服务正在停止，请稍后再运行一次", Detail: detail}
	case info.State != svc.Stopped:
		return &taskresult.Result{Outcome: taskresult.Unchanged, Message: "Windows 搜索服务已暂停，未做改动", Detail: detail}
	case info.StartType != mgr.StartAutomatic:
		return &taskresult.Result{Outcome: taskresult.Unchanged, Message: "Windows 搜索服务设为手动启动，当前未运行，未做改动", Detail: detail}
	case info.DelayedAutoStart && info.Win32ExitCode == uint32(windows.ERROR_SERVICE_NEVER_STARTED) && uptime < winDelayedStartWindow:
		return &taskresult.Result{Outcome: taskresult.Unchanged, Message: "Windows 搜索服务设为延迟启动，开机后还没到启动时间，未做改动", Detail: fmt.Sprintf("%s, never started, %s since boot", detail, uptime.Round(time.Second))}
	}
	return nil
}

// winWaitForSearch polls WSearch until it runs, stops again, sys.startTimeout passes or ctx ends.
func winWaitForSearch(ctx context.Context, sys *winSystem, before string) taskresult.Result {
	deadline := time.NewTimer(sys.startTimeout)
	defer deadline.Stop()
	tick := time.NewTicker(sys.pollInterval)
	defer tick.Stop()
	for {
		info, err := sys.services.Query(winSearchService)
		switch {
		case err != nil:
			return taskresult.Result{Outcome: taskresult.Failed, Message: "已尝试启动 Windows 搜索服务，但无法确认它的状态", Detail: before + "; query: " + err.Error()}
		case info.State == svc.Running:
			return taskresult.Result{Outcome: taskresult.Applied, Message: "已重新启动 Windows 搜索服务，索引会在后台继续更新", Detail: before + "; started"}
		case info.State == svc.Stopped:
			return taskresult.Result{Outcome: taskresult.Failed, Message: "Windows 搜索服务启动后又停止了", Detail: fmt.Sprintf("%s; stopped again with exit code %d", before, info.Win32ExitCode)}
		}
		select {
		case <-ctx.Done():
			return taskresult.Result{Outcome: taskresult.Skipped, Message: "维护已中断，Windows 搜索服务可能仍在启动", Detail: before + "; " + ctx.Err().Error()}
		case <-deadline.C:
			return taskresult.Result{Outcome: taskresult.Failed, Message: "Windows 搜索服务没有在预期时间内启动", Detail: fmt.Sprintf("%s; still in state %d after %s", before, info.State, sys.startTimeout)}
		case <-tick.C:
		}
	}
}

func winStartTypeName(info winServiceInfo) string {
	switch info.StartType {
	case mgr.StartAutomatic:
		if info.DelayedAutoStart {
			return "automatic (delayed)"
		}
		return "automatic"
	case mgr.StartManual:
		return "manual"
	case mgr.StartDisabled:
		return "disabled"
	}
	return fmt.Sprintf("%d", info.StartType)
}

// scmServices is the real winServiceManager. It opens the Service Control Manager with SC_MANAGER_CONNECT only: mgr.Connect asks for SC_MANAGER_ALL_ACCESS, which a standard user does not have, and the precheck runs unelevated.
type scmServices struct{}

func (scmServices) Query(name string) (winServiceInfo, error) {
	var info winServiceInfo
	err := withService(name, windows.SERVICE_QUERY_STATUS|windows.SERVICE_QUERY_CONFIG, func(s *mgr.Service) error {
		status, err := s.Query()
		if err != nil {
			return fmt.Errorf("query %s status: %w", name, err)
		}
		cfg, err := s.Config()
		if err != nil {
			return fmt.Errorf("query %s configuration: %w", name, err)
		}
		info = winServiceInfo{State: status.State, StartType: cfg.StartType, DelayedAutoStart: cfg.DelayedAutoStart, Win32ExitCode: status.Win32ExitCode}
		return nil
	})
	return info, err
}

func (scmServices) Start(name string) error {
	return withService(name, windows.SERVICE_START, func(s *mgr.Service) error {
		if err := s.Start(); err != nil {
			return fmt.Errorf("start %s: %w", name, err)
		}
		return nil
	})
}

// withService opens service name with access and calls fn with it.
func withService(name string, access uint32, fn func(*mgr.Service) error) error {
	scm, err := windows.OpenSCManager(nil, nil, windows.SC_MANAGER_CONNECT)
	if err != nil {
		return fmt.Errorf("open the service control manager: %w", err)
	}
	defer windows.CloseServiceHandle(scm)
	namePtr, err := windows.UTF16PtrFromString(name)
	if err != nil {
		return err
	}
	h, err := windows.OpenService(scm, namePtr, access)
	if errors.Is(err, windows.ERROR_SERVICE_DOES_NOT_EXIST) {
		return fmt.Errorf("%s: %w", name, errServiceMissing)
	}
	if err != nil {
		return fmt.Errorf("open service %s: %w", name, err)
	}
	s := &mgr.Service{Name: name, Handle: h}
	defer s.Close()
	return fn(s)
}
