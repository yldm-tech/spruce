package maintenance

import (
	"context"
	"log/slog"

	"spruce/apps/spruce/internal/engine/execx"
	"spruce/apps/spruce/internal/engine/taskresult"
)

// Icon is a semantic icon key that the frontend maps to an SVG glyph. Adding a key is a contract change: the frontend must draw it.
type Icon string

// Icon keys. The first six are the macOS cards of the design; the rest are available to the Windows catalog.
const (
	IconNetwork    Icon = "network"     // globe: DNS cache
	IconSearch     Icon = "search"      // magnifier: Spotlight / Windows Search index
	IconPreview    Icon = "preview"     // eye: QuickLook / thumbnail cache
	IconIcons      Icon = "icons"       // framed square: icon cache
	IconOpenWith   Icon = "open-with"   // arrow: Launch Services / file associations
	IconDatabase   Icon = "database"    // cylinder: app databases
	IconCleanup    Icon = "cleanup"     // broom: temporary files, component store cleanup
	IconRepair     Icon = "repair"      // wrench: system file repair
	IconDisk       Icon = "disk"        // drive: disk optimisation / TRIM
	IconUpdate     Icon = "update"      // circular arrows: update caches
	IconFont       Icon = "font"        // letter: font cache
	IconStore      Icon = "store"       // bag: app store cache
	IconNetworkFix Icon = "network-fix" // plug: network stack reset
)

// Task describes one maintenance card. It is static: the same values on every call for a given platform and build.
type Task struct {
	// ID is stable across releases; see the package documentation.
	ID string `json:"id"`
	// Title is the card title in Simplified Chinese, e.g. "刷新 DNS 缓存".
	Title string `json:"title"`
	// Description is the card's secondary line, e.g. "网页打不开、域名解析异常时有用".
	Description string `json:"description"`
	// Hint is an optional prerequisite shown in the accent colour instead of Description, e.g. "需要先退出 Safari 和邮件". Empty when there is none.
	Hint string `json:"hint,omitempty"`
	// Icon picks the card's glyph.
	Icon Icon `json:"icon"`
	// DefaultOn is whether the card's switch starts on.
	DefaultOn bool `json:"defaultOn"`
	// NeedsAdmin is whether Run needs administrator rights and so runs in the elevated batch.
	NeedsAdmin bool `json:"needsAdmin"`
	// EstimatedSeconds is a typical run time, summed by the page for "预计用时".
	EstimatedSeconds int `json:"estimatedSeconds"`
}

// TaskImpl couples a Task with its behaviour. Platform files build these in PlatformTasks.
type TaskImpl struct {
	Task
	// Precheck, when set, runs in the app process before any task runs. It returns nil to let the task run, or the final Result to stop it (usually Skipped or Unavailable).
	Precheck func(ctx context.Context, env Env) *taskresult.Result
	// Run does the work and returns the Result. For NeedsAdmin tasks it runs inside the elevated child. The Service overwrites TaskID and Duration.
	Run func(ctx context.Context, env Env) taskresult.Result
}

// Env is what a task may use.
type Env struct {
	// Runner runs external programs.
	Runner execx.Runner
	// Permissions answers permission questions such as Full Disk Access. Nil inside the elevated child.
	Permissions PermissionChecker
	// HomeDir is the user's home directory. Empty inside the elevated child.
	HomeDir string
	// Elevated is true inside the elevated child.
	Elevated bool
	// Log receives diagnostics; never nil once passed through NewService or ElevatedHandler.
	Log *slog.Logger
}

// PermissionState is whether the app holds a system permission.
type PermissionState string

const (
	// PermissionGranted means the app has the permission.
	PermissionGranted PermissionState = "granted"
	// PermissionDenied means the user has not given it; the app can guide them to System Settings.
	PermissionDenied PermissionState = "denied"
	// PermissionUnknown means the system cannot tell.
	PermissionUnknown PermissionState = "unknown"
	// PermissionNotRequired means the platform has no such permission (Full Disk Access on Windows), so whatever it guards is allowed.
	PermissionNotRequired PermissionState = "notRequired"
)

// PermissionChecker reports system permissions to tasks. internal/app implements it with internal/host/fulldisk, so the engine does not import any UI framework; tests use a fake.
type PermissionChecker interface {
	// FullDiskAccess reports whether the app may read other apps' data under ~/Library (macOS). The "databases" task skips with a hint when it is not PermissionGranted or PermissionNotRequired.
	FullDiskAccess() (PermissionState, error)
}
