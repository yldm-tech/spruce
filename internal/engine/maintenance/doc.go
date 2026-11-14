// Package maintenance describes the maintenance tasks of the 维护 page and runs a selection of them. It does not import mygo: the services layer adapts it to the UI.
//
// # What lives where
//
//   - task.go: Task, Icon, TaskImpl, Env, PermissionChecker. These are the types the platform catalogs fill in.
//   - service.go: Service, which validates a catalog, validates a selection (unknown IDs are rejected with ErrUnknownTask), runs prechecks, runs every NeedsAdmin task in ONE elevated batch through elevate.Elevator (one system prompt per run), runs the other tasks in the app process, reports progress through a callback, and returns one taskresult.Result per selected task. ElevatedHandler is the same catalog's entry point inside the elevated child.
//   - catalog_darwin.go (macOS) and catalog_windows.go (Windows), plus the task implementations in further files with the same build suffix (e.g. task_dns_darwin.go).
//
// # Platform contract
//
// Each platform's catalog file declares exactly:
//
//	// PlatformTasks returns this platform's maintenance tasks in display order.
//	func PlatformTasks() []TaskImpl
//
// main.go passes PlatformTasks() to NewService and to ElevatedHandler; nothing in this package calls it, so the Service and its tests stay platform-neutral.
//
// # Task IDs
//
// macOS uses "dns", "spotlight", "quicklook", "icons", "launchservices" and "databases", in that display order, with DefaultOn true, false, true, true, true, false as designed. Windows uses "dns", "search", "thumbnails", "icons" and "associations". IDs must satisfy elevate.ValidTaskID and stay stable: they are stored in the history and sent to the elevated child.
//
// # Rules for task implementations
//
//   - Run external programs only through Env.Runner (an execx.Runner), never os/exec directly, so tests use execxtest.Fake.
//   - Return a Result with a Simplified Chinese Message and put commands, exit codes and stderr in Detail. The Service fills TaskID and Duration, so Run need not.
//   - A NeedsAdmin task's Run executes inside the elevated child (Env.Elevated is true, Env.HomeDir and Env.Permissions are empty): it must not touch the user's home directory there. Split work that needs both into an admin task and a user task, or do the user part in Precheck.
//   - Precheck runs in the app process before anything else and returns a non-nil Result to stop the task (Skipped for a prerequisite the user can fix, such as Safari being open or Full Disk Access missing; Unavailable when the system lacks the tool).
//   - macOS "launchservices" must only garbage-collect registrations of apps that no longer exist (lsregister -gc). Never rebuild the domain-wide database (lsregister -r -domain …): that drops Network Extension VPN registrations and triggers Apple Intelligence re-indexing.
package maintenance
