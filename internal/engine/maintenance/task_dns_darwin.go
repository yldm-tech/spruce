package maintenance

import (
	"bufio"
	"context"
	"slices"
	"strconv"
	"strings"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// mDNSResponderPlist is the SIP-protected launchd plist of the DNS resolver. The service label is read from it rather than found by process name, so that nothing but Apple's own service can be signalled.
const mDNSResponderPlist = "/System/Library/LaunchDaemons/com.apple.mDNSResponder.plist"

// mDNSResponderLabels are the labels the plist is known to carry; macOS 26 and later use the ".reloaded" one. Any other value stops the task before it changes anything.
var mDNSResponderLabels = []string{"com.apple.mDNSResponder", "com.apple.mDNSResponder.reloaded"}

// notPrivilegedToSignal is what launchctl prints when macOS refuses to signal a service even for root (macOS 27 does this for mDNSResponder).
const notPrivilegedToSignal = "Not privileged to signal service"

// vpnInterfacePrefixes are interface names that carry a VPN tunnel when they own the default route.
var vpnInterfacePrefixes = []string{"utun", "ipsec", "ppp"}

const dnsCommandTimeout = 15 * time.Second

// dnsTask flushes the directory service cache and makes mDNSResponder drop its cache.
//
// It is skipped while a VPN is connected, or when the VPN state cannot be determined: SIGHUP makes mDNSResponder reload its configuration, which VPN clients that watch DNS settings (WireGuard and similar Network Extension tunnels) treat as a network change and reconnect.
//
// macOS 27 refuses launchctl kill on mDNSResponder for every caller, root included. Only after that exact refusal does the task retry once as the service's own account (sudo -u _mdnsresponder killall -u _mdnsresponder -HUP mDNSResponder): the kernel lets that account signal only its own processes and -u restricts the match to them, so no same-named process of another user can be reached. A root killall by name is never used. When the retry is refused too, the task is Skipped, since the directory cache was flushed and the OS withheld the rest.
func dnsTask() TaskImpl {
	return TaskImpl{
		Task: Task{
			ID:               "dns",
			Title:            "刷新 DNS 缓存",
			Description:      "网页打不开、域名解析异常时有用",
			Icon:             IconNetwork,
			DefaultOn:        true,
			NeedsAdmin:       true,
			EstimatedSeconds: 3,
		},
		Precheck: precheckDNS,
		Run:      runDNS,
	}
}

// vpnState is the result of the VPN probe.
type vpnState int

const (
	vpnNone vpnState = iota
	vpnActive
	vpnUnknown
)

func precheckDNS(ctx context.Context, env Env) *taskresult.Result {
	state, detail := detectVPN(ctx, env.Runner)
	switch state {
	case vpnActive:
		return &taskresult.Result{Outcome: taskresult.Skipped, Message: "VPN 已连接，为避免断线已跳过；断开 VPN 后再运行一次", Detail: detail}
	case vpnUnknown:
		return &taskresult.Result{Outcome: taskresult.Skipped, Message: "无法确认 VPN 是否已连接，为避免断线已跳过", Detail: detail}
	}
	return nil
}

// detectVPN looks for a connected VPN with two narrow signals, since macOS creates utun interfaces for many features that are not VPNs (iCloud Private Relay, Continuity, AirDrop): a service that scutil --nc list reports as Connected (system and Network Extension VPN configurations), or a tunnel interface owning the default route (full-tunnel third-party VPNs). A split-tunnel VPN that is not a system configuration and does not own the default route is not detected.
func detectVPN(ctx context.Context, runner execx.Runner) (vpnState, string) {
	nc := execx.Command{Name: pathScutil, Args: []string{"--nc", "list"}, Env: cLocale, Timeout: dnsCommandTimeout}
	out, err := runner.Run(ctx, nc)
	if err != nil {
		return vpnUnknown, err.Error()
	}
	if name, ok := connectedNetworkService(string(out.Stdout)); ok {
		return vpnActive, "scutil --nc list: connected: " + name
	}

	route := execx.Command{Name: pathRoute, Args: []string{"-n", "get", "default"}, Env: cLocale, Timeout: dnsCommandTimeout}
	out, err = runner.Run(ctx, route)
	if err != nil {
		// Without a default route (offline) no full-tunnel VPN can be routing traffic: a known "none", not an unknown state.
		if strings.Contains(combinedOutput(out), "not in table") {
			return vpnNone, ""
		}
		return vpnUnknown, err.Error()
	}
	iface := defaultRouteInterface(string(out.Stdout))
	if iface == "" {
		return vpnUnknown, "route -n get default: no interface in output"
	}
	if slices.ContainsFunc(vpnInterfacePrefixes, func(p string) bool { return strings.HasPrefix(iface, p) }) {
		return vpnActive, "default route via " + iface
	}
	return vpnNone, ""
}

// connectedNetworkService finds a line of scutil --nc list whose status is "(Connected)", e.g. `* (Connected)      7C1A… VPN (com.wireguard.macos) "Office" [VPN:com.wireguard.macos]`, and returns the line.
func connectedNetworkService(out string) (string, bool) {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		line := strings.TrimSpace(sc.Text())
		status := strings.TrimSpace(strings.TrimPrefix(line, "*"))
		if strings.HasPrefix(status, "(Connected)") {
			return line, true
		}
	}
	return "", false
}

// defaultRouteInterface returns the value of the "interface:" line of route -n get default.
func defaultRouteInterface(out string) string {
	sc := bufio.NewScanner(strings.NewReader(out))
	for sc.Scan() {
		key, value, ok := strings.Cut(sc.Text(), ":")
		if ok && strings.TrimSpace(key) == "interface" {
			return strings.TrimSpace(value)
		}
	}
	return ""
}

// runDNS runs in the elevated child.
func runDNS(ctx context.Context, env Env) taskresult.Result {
	labelCmd := execx.Command{Name: pathPlutil, Args: []string{"-extract", "Label", "raw", "-o", "-", mDNSResponderPlist}, Timeout: dnsCommandTimeout}
	out, err := env.Runner.Run(ctx, labelCmd)
	if err != nil {
		return commandFailure("无法确认系统的 DNS 服务，未做改动", labelCmd, err)
	}
	label := strings.TrimSpace(string(out.Stdout))
	if !slices.Contains(mDNSResponderLabels, label) {
		return taskresult.Result{Outcome: taskresult.Failed, Message: "无法确认系统的 DNS 服务，未做改动", Detail: "unexpected mDNSResponder label " + strconv.Quote(label)}
	}

	flush := execx.Command{Name: pathDscacheutil, Args: []string{"-flushcache"}, Timeout: dnsCommandTimeout}
	if _, err := env.Runner.Run(ctx, flush); err != nil {
		return commandFailure("刷新 DNS 缓存失败", flush, err)
	}

	signal := execx.Command{Name: pathLaunchctl, Args: []string{"kill", "SIGHUP", "system/" + label}, Env: cLocale, Timeout: dnsCommandTimeout}
	out, err = env.Runner.Run(ctx, signal)
	if err == nil {
		return taskresult.Result{Outcome: taskresult.Applied, Message: "已刷新 DNS 缓存"}
	}
	if !strings.Contains(combinedOutput(out), notPrivilegedToSignal) {
		return commandFailure("刷新 DNS 缓存失败", signal, err)
	}

	retry := execx.Command{Name: pathSudo, Args: []string{"-n", "-u", "_mdnsresponder", pathKillall, "-u", "_mdnsresponder", "-HUP", "mDNSResponder"}, Timeout: dnsCommandTimeout}
	if _, retryErr := env.Runner.Run(ctx, retry); retryErr != nil {
		return taskresult.Result{
			Outcome: taskresult.Skipped,
			Message: "已清空目录服务缓存，但 macOS 不允许重置 DNS 解析服务；重启电脑后完全生效",
			Detail:  joinDetail(err.Error(), retryErr.Error()),
		}
	}
	return taskresult.Result{Outcome: taskresult.Applied, Message: "已刷新 DNS 缓存", Detail: "launchctl refused (" + notPrivilegedToSignal + "); signalled as _mdnsresponder"}
}
