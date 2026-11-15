# Spruce

Spruce is a desktop app for macOS and Windows that tells you how much space you can safely free, cleans up junk, uninstalls apps, shows what fills the disk and how it grows, runs maintenance tasks and watches the system's health. It is written in Go on [NSGo](https://github.com/nsgo-dev/nsgo); the UI copy is Simplified Chinese.

## What it does

- **概览 (overview).** There is no scan button. Spruce checks the three read-only sources (系统垃圾, 开发者, 安装包) when the window opens and in the background, and the overview shows the conclusion at once (「可以放心释放 18.7 GB」), when it last checked, and 重新检查. The last result is cached in the data folder and shown on the next launch while a fresh check runs; a cached result is never cleaned from, the buttons wait for the new check.
- **Recommendation cards.** Each card is one action with a reason and a consequence (e.g. Xcode 编译缓存, 闲置项目的构建产物, 下载里的安装包), sorted by size times safety, with one button and the items behind an expander. Items Spruce does not select by default (caches of running apps, recent installers, projects with uncommitted changes) are counted under 需要你判断. 系统垃圾, 开发者 and 安装包 are drill-down views of the same result, reached from 按类别查看, a card's 在…中查看全部 and 操作历史; they have a 「‹ 概览」 link back and no scan of their own.
- **Background checks.** At most every 6 hours (the first one 2 minutes after launch), only on AC power, not in Low Power Mode, not when the Mac is hot or busy (over 40 % CPU), never while a clean runs, and never anything that asks for a permission: folders that prompt (Desktop, Documents, Downloads, iCloud Drive, other volumes) are read in the background only after a check you started has read them, and the overview names the ones it skipped with 检查这些位置. 设置 → 通用 → 在后台定期检查 turns them off.
- **Menu bar (macOS).** A menu bar icon shows free space, switches to an alert icon when space is low, and offers 打开 Spruce, 立即检查 (read-only), the automatic rules and 退出 Spruce. Closing the window keeps Spruce in the menu bar and hides its Dock icon; 设置 → 通用 → 关闭窗口后继续在菜单栏运行 turns that off. Quit from the menu or with Cmd+Q. Started at login with that setting on, Spruce stays in the menu bar without opening its window.
- **Low-disk notification.** 设置 → 通用 → 空间不足时提醒我 (off by default, threshold 20 GB, 1 to 500) posts a notification when the system volume drops below the threshold: once per crossing, again only after a further 5 GB and at most once a day. Pressing it opens the overview (空间透视 when there is nothing to clean). The notification permission is asked for only when you turn on an alert, a rule or the weekly summary.
- **Undo (macOS).** Moves to the Trash happen at once and show 「已移到废纸篓 3.1 GB · 撤销」; 撤销 puts the items back. 操作历史 offers 放回原处 for moves of the last 30 days that are still in the Trash. Nothing is overwritten: an item whose place is taken, whose folder is gone or which was replaced in the Trash stays where it is and the record says why. Permanent deletions, commands (such as `brew cleanup` or `go clean -cache`) and emptying the Trash always ask first and cannot be undone.
- **Automatic cleaning (macOS).** 设置 → 自动清理 has four rules, all off by default: 旧的应用缓存 (unchanged for 30 days, weekly), 旧安装包 (.dmg and .pkg in Downloads older than 7 days whose app is installed, daily), 闲置项目的构建产物 (projects idle for 90 days and clean in Git, weekly) and 未完成的下载 (unchanged for 7 days, daily). Rules run after a background check, only move items to the Trash (never a command, never a permanent deletion), never take an item you put back, and each run is a 自动清理 record in 操作历史 that can be put back. 每周汇总 (on by default) sends 「本周自动清理了 {size}」 on Mondays.
- **Space over time.** Once a day, after a background check, Spruce records the sizes of the main locations (Library caches and app data, Downloads, Desktop, Documents, media folders, the Trash, developer caches, project folders); other apps' containers are read only with Full Disk Access. 空间透视 shows 最近变化 over 24 hours, 7 days or 30 days, and the overview the top three, e.g. 「~/Library/Caches/com.apple.Safari 从周二到现在涨了 6 GB」.
- **Projects.** 开发者 → 项目 lists each project with its last activity, the size of its build output (node_modules, target, .next and so on) and its Git state. A project with uncommitted changes, or with tracked files inside its build output, is never selected and never cleaned (Spruce runs `git status` again right before deleting); when Git is missing or fails the state is 无法确定 and the project is not selected by default.
- **Command line.** The same executable takes commands, see below.

## Command line

Run the executable with a command: `Spruce.app/Contents/MacOS/Spruce <command>` on macOS, `Spruce.exe <command>` on Windows. Everything is read-only except `clean --yes` and `restore --yes`.

| Command | What it does |
|---|---|
| `scan [--source system-junk\|dev-junk\|installers]... [--json]` | Checks and lists the categories with sizes and item counts. |
| `recommendations [--json]` (or `list`) | Checks and lists the recommendation cards: ID, title, size, action and reason. |
| `clean [<recommendation-id>...] [--category <source:category>]... [--source <id>]... [--older-than 12h\|30d\|2w] [--yes \| --dry-run] [--json]` | Checks, selects and prints the plan; deletes only with `--yes`. Without IDs it takes every recommendation that runs no command; a command runs only when its recommendation is named. It goes through the same engine checks as the app (scanned roots, exclusions, the re-check before each item, held items, the Git re-check) and is recorded in 操作历史. |
| `projects [--json]` | Checks the project folders and lists the projects with their build output and Git state. |
| `history [--limit 20] [--json]` | Lists recent records and whether they can be put back. |
| `restore <run-id> [--yes] [--json]` | macOS only: lists what a record moved to the Trash that can still go back; puts it back only with `--yes`. |
| `help [<command>]`, `version` | Help and version. |

With `--json`, standard output is one JSON object (`{"version":1,"command":...}`, or `{"version":1,"error":...}`); logs go to standard error. Exit codes: 0 success (including nothing to do and dry runs), 1 failure, 2 usage error, 3 partly done (some items failed or were held), 4 busy (Spruce is cleaning, in the app or another command), 130 interrupted.

The command line never takes the app's single-instance lock or talks to a running app; the app and the command line share `clean.lock` in the data folder, so they never clean at the same time. On Windows the executable is a GUI program, so `cmd.exe` does not wait for it: use `start /wait Spruce.exe scan` or run it from PowerShell (`.\Spruce.exe scan | Out-Host`).

## Windows

The pinned NSGo has no tray icon or extra windows on Windows, and moving to the Recycle Bin can ask whether to delete an item for good when it does not fit, so Windows keeps the earlier behaviour where an unattended or reversible flow cannot be guaranteed:

- no tray icon; closing the window quits Spruce, and the close-to-tray setting is not shown; at login the window opens as usual;
- background checks and the low-disk notification run only while Spruce is open, and a notification press works only then;
- moves to the Recycle Bin are confirmed first and have no 撤销 or 放回原处 (restore it from the Recycle Bin); `spruce restore` is not available;
- no automatic rules or weekly summary (the 自动清理 tab says so);
- recommendation cards, drill-down views, projects, 最近变化 and the command line work as on macOS.

## Data folder

Spruce keeps its data in `~/Library/Application Support/Spruce` on macOS and `%AppData%\Spruce` on Windows. Files are 0600 in 0700 folders. Settings, the cached result, snapshots and ledger entries are replaced atomically (a temporary file and a rename); `history.jsonl` and the undo journals are appended line by line, and a partial last line from a crash is skipped. A damaged file or one of another version is ignored and replaced on the next save.

| File | What it holds | Limit |
|---|---|---|
| `settings.json` | Settings (version 2; a version 1 file gets the new defaults) | |
| `history.jsonl` | 操作历史, one JSON line per run | 500 runs |
| `overview-cache.json` | The last check result, for display only (never used to delete) | 8 MB, shown for 7 days |
| `undo/<run>.json`, `undo/<run>.partial`, `undo/restored.json` | What each run moved to the Trash, the journal of a clean in progress, and the IDs of items put back (so rules leave them alone) | 200 entries, 32 MB, 30 days |
| `growth/<time>.json` | Daily size snapshots | 40 files, 8 MB, 90 days |
| `background.json` | When each automatic rule last ran, the last weekly summary, the low-disk alert state | small |
| `folder-access.json` | The prompting folders a check you started has read | small |
| `clean.lock` | The lock the app and the command line share while cleaning or checking | |
| `instance.lock`, `instance.sock` | The single-instance lock and the socket a second launch uses to bring the running copy to the front | |
| `window.json`, `app-icons/` | The window frame and cached app icons | |

清空历史 in 操作历史 also deletes the undo ledger (not `restored.json`), the cached result and the snapshots.

## Requirements

- Go 1.27.3 (the version in `go.mod`). The `nsgo` tool is a Go tool dependency, so `go tool nsgo` needs nothing else installed.
- golangci-lint v2, built with a Go at least as new as the one in `go.mod`, for `scripts/check.sh`.
- macOS for the macOS app and the window tests; Windows executables cross-build from any platform (`CGO_ENABLED=0`).

## Development

| Task | Command |
|---|---|
| Run the app, rebuilding on change | `go tool nsgo dev` |
| Build the macOS app and dmg | `go tool nsgo build -target macos_arm64` (or `macos_x64`) |
| Build the Windows exe and installer script | `go tool nsgo build -target windows_x64` (or `windows_arm64`) |
| Headless UI tests | `go tool nsgo test ./internal/...` |
| Smoke test and screenshots in a real window | `go tool nsgo test -target macos` (on Windows: `-target windows`) |
| Only some screens | `go tool nsgo test -target macos -run 'TestScreensSpace'` |
| All checks (gofmt, vet and build for every target, race tests, lint, nsgo test, nsgo check) | `scripts/check.sh` |

Build output goes to `build/`, and nsgo's caches, test artefacts and screenshots to `.nsgo/` (screenshots under `.nsgo/test/screenshots/<target>`); both are ignored by git.

`nsgo dev` and `nsgo test -target macos` sign the app ad hoc and start it from the terminal, so anything that depends on the app's identity (Full Disk Access, the login item, elevated maintenance tasks) has to be checked in a bundle made by `nsgo build`. Release signing is described in `nsgo.yaml`.

## Layout

- `main.go`, `wire.go`: start-up, the single-instance check and the engine wiring.
- `internal/engine/`: scanning, cleaning, uninstalling, maintenance, history, settings and system information, with no UI dependency; `advice` (recommendation cards), `rules` (automatic rules), `schedule` (background conditions, the clean lock), `scancache`, `undo` (the undo ledger), `growth` (snapshots) and `gitstate`.
- `internal/app/`: the controllers the UI calls, between the engine and the pages; `overview.go` is the overview controller that runs the background loop.
- `internal/cli/`: the command line.
- `internal/host/`: the data folder, single instance, login item, Full Disk Access and the background read gate (`folderaccess`).
- `internal/ui/`: the shell (window, menu bar, notifications), the pages (`internal/ui/pages/<page>`), the design system (`kit`), the shared drill-down flow (`cleanflow`) and the app-wide stores.
- `screens_<page>_test.go`: window tests that put each page into its main states and take screenshots.
