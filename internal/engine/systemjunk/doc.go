// Package systemjunk is the engine of the 系统垃圾 page: it finds user-level junk (application and browser caches, logs, crash and error reports, temporary files, interrupted downloads, the Trash) and deletes what the user picked. It implements cleanup.Scanner; wire.go registers it under app.CleanerSystemJunk. It does not import any UI framework.
//
// # Locations
//
// macOS (darwinSpecs):
//
//   - 应用缓存: each child of ~/Library/Caches, except Apple's account, iCloud, keychain and similar caches (appleDenylist), the developer tool caches 开发者垃圾 offers (internal/engine/devcaches), the browser caches below and Spruce's own. A cache whose app is running goes to 正在运行的应用的缓存, which starts unchecked. Deleted for good.
//   - 浏览器缓存: Cache, Code Cache and GPUCache of every Chrome, Edge, Brave, Arc and Opera profile (and the shared GPU shader caches), Firefox's cache2 and Safari's cache folder when it is readable. Never cookies, history, logins or the profiles themselves. A running browser's caches go to 正在运行的浏览器的缓存, which starts unchecked. Deleted for good.
//   - 日志: each child of ~/Library/Logs but DiagnosticReports; 崩溃报告: each child of ~/Library/Logs/DiagnosticReports. Deleted for good.
//   - 未完成的下载: .crdownload, .part, .download and .partial entries directly in Downloads, untouched for a day. Moved to the Trash.
//   - 废纸篓: its size (trash.Bin.Size); cleaning it empties it (trash.Bin.Empty). It starts unchecked; without Full Disk Access it is listed with an explanation and nothing to select.
//
// Windows (windowsSpecs): 临时文件 (children of %TEMP%, %TMP% and %LOCALAPPDATA%\Temp untouched for a day), 应用缓存 (Internet temporary files and GPU shader caches, appCachesWindows), 浏览器缓存 (Chrome, Edge, Brave, Opera and Firefox), 错误报告 (WER ReportArchive and ReportQueue, and CrashDumps), 未完成的下载 and 回收站, with the same rules.
//
// Both platforms' location lists are plain functions of Env, compiled everywhere, so the tests exercise the Windows layout on macOS with temporary folders.
//
// # Safety
//
// Every item is a direct child (or a named cache folder) of a category root, listed with os.ReadDir and os.Lstat, so symlinks are never followed; symlinks themselves are not offered. Items under the user's exclusions, or containing an excluded path, are left out. Each item is checked against fsx.CheckRemovable (or fsx.CheckTrashable) at scan time, and the cleanup.Executor deletes only what the last scan returned, through fsx.SafeRemove inside the category's roots. Nothing needs administrator rights.
//
// Cleaning the Trash together with items that go to the Trash empties it first, so this clean's own items stay recoverable.
//
// When the running apps cannot be listed, 应用缓存 and 浏览器缓存 carry the warning 「无法确认哪些应用正在运行」: the page still offers them, but automatic rules skip a category with a warning. Env.MayRead, the background read gate, is asked before each folder is listed (after the exclusions); a folder it refuses, on macOS the Downloads folder until a foreground read has entered it, is skipped silently.
package systemjunk
