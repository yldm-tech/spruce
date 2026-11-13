package maintenance

import (
	"context"
	"fmt"
	"io/fs"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

const (
	ncListNoVPN = `Available network connection services in the current set (*=enabled):
* (Disconnected)   5B6C1E2A-0000-4000-8000-000000000001 VPN (com.wireguard.macos) "Office"            [VPN:com.wireguard.macos]
`
	ncListVPN = `Available network connection services in the current set (*=enabled):
* (Connected)      5B6C1E2A-0000-4000-8000-000000000001 VPN (com.wireguard.macos) "Office"            [VPN:com.wireguard.macos]
`
	routeEn0 = `   route to: default
destination: default
       mask: default
    gateway: 192.168.1.1
  interface: en0
      flags: <UP,GATEWAY,DONE,STATIC,PRCLONING,GLOBAL>
`
	routeUtun = `   route to: default
destination: default
       mask: default
  interface: utun4
      flags: <UP,DONE,STATIC,PRCLONING,GLOBAL>
`
)

func onScutil(f *execxtest.Fake) *execxtest.Expectation {
	return f.On(pathScutil, "--nc", "list")
}

func onRoute(f *execxtest.Fake) *execxtest.Expectation {
	return f.On(pathRoute, "-n", "get", "default")
}

func TestDNSPrecheckVPN(t *testing.T) {
	tests := []struct {
		name    string
		script  func(f *execxtest.Fake)
		want    taskresult.Outcome // "" means the precheck lets the task run
		message string
	}{
		{"no vpn", func(f *execxtest.Fake) {
			onScutil(f).Return(ncListNoVPN, 0)
			onRoute(f).Return(routeEn0, 0)
		}, "", ""},
		{"offline", func(f *execxtest.Fake) {
			onScutil(f).Return(ncListNoVPN, 0)
			onRoute(f).ReturnOutput(execx.Output{Stderr: []byte("route: writing to routing socket: not in table\n"), ExitCode: 1})
		}, "", ""},
		{"system vpn connected", func(f *execxtest.Fake) {
			onScutil(f).Return(ncListVPN, 0)
		}, taskresult.Skipped, "VPN 已连接"},
		{"full tunnel owns the default route", func(f *execxtest.Fake) {
			onScutil(f).Return(ncListNoVPN, 0)
			onRoute(f).Return(routeUtun, 0)
		}, taskresult.Skipped, "VPN 已连接"},
		{"scutil fails", func(f *execxtest.Fake) {
			onScutil(f).Return("", 1)
		}, taskresult.Skipped, "无法确认 VPN"},
		{"route fails otherwise", func(f *execxtest.Fake) {
			onScutil(f).Return(ncListNoVPN, 0)
			onRoute(f).ReturnOutput(execx.Output{Stderr: []byte("route: permission denied\n"), ExitCode: 1})
		}, taskresult.Skipped, "无法确认 VPN"},
		{"route prints no interface", func(f *execxtest.Fake) {
			onScutil(f).Return(ncListNoVPN, 0)
			onRoute(f).Return("   route to: default\n", 0)
		}, taskresult.Skipped, "无法确认 VPN"},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			f := execxtest.New()
			tt.script(f)
			res := dnsTask().Precheck(t.Context(), macEnv(f))
			if tt.want == "" {
				if res != nil {
					t.Fatalf("precheck stopped the task: %+v", *res)
				}
				return
			}
			if res == nil {
				t.Fatal("precheck let the task run")
			}
			assertOutcome(t, *res, tt.want, tt.message)
		})
	}
}

func TestDNSPrecheckUsesCLocale(t *testing.T) {
	f := execxtest.New()
	onScutil(f).Return(ncListNoVPN, 0)
	onRoute(f).Return(routeEn0, 0)
	dnsTask().Precheck(t.Context(), macEnv(f))
	for _, c := range f.Calls() {
		if len(c.Env) != 1 || c.Env[0] != "LC_ALL=C" {
			t.Errorf("%s runs with env %v, want LC_ALL=C", c.String(), c.Env)
		}
	}
}

func onLabel(f *execxtest.Fake) *execxtest.Expectation {
	return f.On(pathPlutil, "-extract", "Label", "raw", "-o", "-", mDNSResponderPlist)
}

var mdnsRetry = []string{"-n", "-u", "_mdnsresponder", pathKillall, "-u", "_mdnsresponder", "-HUP", "mDNSResponder"}

func TestDNSRunFlushesAndSignals(t *testing.T) {
	for _, label := range mDNSResponderLabels {
		t.Run(label, func(t *testing.T) {
			f := execxtest.New()
			onLabel(f).Return(label+"\n", 0)
			f.On(pathDscacheutil, "-flushcache")
			f.On(pathLaunchctl, "kill", "SIGHUP", "system/"+label)
			res := runDNS(t.Context(), macEnv(f))
			assertOutcome(t, res, taskresult.Applied, "已刷新 DNS 缓存")
			f.AssertCalled(t, pathDscacheutil, "-flushcache")
			f.AssertCalled(t, pathLaunchctl, "kill", "SIGHUP", "system/"+label)
			f.AssertNotCalled(t, pathSudo)
			f.AssertNotCalled(t, pathKillall)
		})
	}
}

func TestDNSRunRetriesAsServiceAccountWhenRefused(t *testing.T) {
	refused := execx.Output{Stderr: []byte("Not privileged to signal service.\n"), ExitCode: 1}

	t.Run("retry works", func(t *testing.T) {
		f := execxtest.New()
		onLabel(f).Return("com.apple.mDNSResponder.reloaded\n", 0)
		f.On(pathDscacheutil, "-flushcache")
		f.On(pathLaunchctl, "kill", "SIGHUP", "system/com.apple.mDNSResponder.reloaded").ReturnOutput(refused)
		f.On(pathSudo, mdnsRetry...)
		res := runDNS(t.Context(), macEnv(f))
		assertOutcome(t, res, taskresult.Applied, "已刷新 DNS 缓存")
		f.AssertCalled(t, pathSudo, mdnsRetry...)
	})

	t.Run("retry refused too", func(t *testing.T) {
		f := execxtest.New()
		onLabel(f).Return("com.apple.mDNSResponder.reloaded\n", 0)
		f.On(pathDscacheutil, "-flushcache")
		f.On(pathLaunchctl, "kill", "SIGHUP", "system/com.apple.mDNSResponder.reloaded").ReturnOutput(refused)
		f.On(pathSudo, mdnsRetry...).Return("", 1)
		res := runDNS(t.Context(), macEnv(f))
		assertOutcome(t, res, taskresult.Skipped, "macOS 不允许")
	})

	t.Run("other launchctl failure is not retried", func(t *testing.T) {
		f := execxtest.New()
		onLabel(f).Return("com.apple.mDNSResponder.reloaded\n", 0)
		f.On(pathDscacheutil, "-flushcache")
		f.On(pathLaunchctl, "kill", "SIGHUP", "system/com.apple.mDNSResponder.reloaded").ReturnOutput(execx.Output{Stderr: []byte("Could not find service\n"), ExitCode: 113})
		res := runDNS(t.Context(), macEnv(f))
		assertOutcome(t, res, taskresult.Failed)
		f.AssertNotCalled(t, pathSudo)
	})
}

func TestDNSRunRefusesUnknownLabel(t *testing.T) {
	f := execxtest.New()
	onLabel(f).Return("com.example.evil\n", 0)
	res := runDNS(t.Context(), macEnv(f))
	assertOutcome(t, res, taskresult.Failed, "未做改动")
	f.AssertNotCalled(t, pathDscacheutil)
	f.AssertNotCalled(t, pathLaunchctl)
}

func TestDNSRunFailures(t *testing.T) {
	t.Run("plutil missing", func(t *testing.T) {
		f := execxtest.New()
		onLabel(f).Fail(&fs.PathError{Op: "fork/exec", Path: pathPlutil, Err: fs.ErrNotExist})
		assertOutcome(t, runDNS(t.Context(), macEnv(f)), taskresult.Unavailable)
	})
	t.Run("flush fails", func(t *testing.T) {
		f := execxtest.New()
		onLabel(f).Return("com.apple.mDNSResponder\n", 0)
		f.On(pathDscacheutil, "-flushcache").Return("", 1)
		assertOutcome(t, runDNS(t.Context(), macEnv(f)), taskresult.Failed, "刷新 DNS 缓存失败")
		f.AssertNotCalled(t, pathLaunchctl)
	})
	t.Run("cancelled", func(t *testing.T) {
		f := execxtest.New()
		onLabel(f).Fail(fmt.Errorf("%s: %w", pathPlutil, context.Canceled))
		assertOutcome(t, runDNS(t.Context(), macEnv(f)), taskresult.Skipped, "中断")
	})
}

func TestConnectedNetworkService(t *testing.T) {
	if _, ok := connectedNetworkService(ncListNoVPN); ok {
		t.Error("a disconnected service reads as connected")
	}
	if _, ok := connectedNetworkService(ncListVPN); !ok {
		t.Error("a connected service is not found")
	}
	if _, ok := connectedNetworkService("  (Connected)      ID PPP --> L2TP \"x\"\n"); !ok {
		t.Error("a connected service that is not enabled in the set is not found")
	}
}
