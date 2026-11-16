# Changelog

What changed in each version of Spruce, newest first. Each section is also the notes of the GitHub release of that version.

## 0.1.1 - 2026-11-16

### Fixes

- Name the licences of the linked code exactly and ship their texts

## 0.1.0 - 2026-11-16

### Features

- Spruce scaffold and maintenance page for macOS and Windows
- Add fsx and trash packages for the cleaner pages
- Add the shared cleanup, settings and history foundation for the new pages
- Add the shared page components, page shells and routes for every sidebar entry
- Announce recorded runs and add settings limits, folder picker and about info
- Add the 系统垃圾 engine for macOS and Windows
- Build the 操作历史 and 设置 pages
- Add the sysinfo sampler and Sysinfo service behind the 健康监测 dashboard
- Add the space engine and bound Space service for 空间透视
- Add the 安装包 engine and register both cleaners
- Build the 系统垃圾 and 安装包 pages on the shared cleaner flow
- Scan developer tool caches and project build output
- Build the 空间透视 page
- Build the 开发者垃圾 page
- Build the 健康监测 dashboard page
- Add the uninstall engine and its bound service
- Build the 应用卸载 page
- Combine the 系统垃圾, 开发者垃圾 and 安装包 scanners into one cleaner
- Build the 智能扫描 page and open the app on it
- Add the async bridge, formatting, navigation, icons and the kit design system on NSGo
- Add the stores, the shared components, the shell, the 维护 page and main on NSGo
- Port the selection, clean plan and clean summary logic with their tests
- Port the labels and the squarified treemap layout to Go
- Build the 操作历史 page on NSGo
- Add the cleaner flow, its views and the 系统垃圾 page
- Build the 健康监测 page on NSGo with its cards, sparklines and process lists
- Port the 设置 page to NSGo with debounced, sequenced saves
- Build the 空间透视 page on NSGo with its treemap, entry lists and move to the Trash
- Add the 系统垃圾 screenshots and keep empty idle lines out of the layout
- Fill the height between header and action bar and add the 应用卸载 screenshots
- Build the 安装包 page on the shared cleaner flow
- Port the 开发者垃圾 page onto the shared cleaner flow
- Build the 智能扫描 page on NSGo with its hero, status cards, per-source scan and grouped results
- Split 设置 into tabs of grouped rows
- Spruce v2: conclusions on open, recommendation cards, undo, menu bar, automatic rules, growth and developer tools
- Sort the app list by size by default
- Motion across the app, and a working bar that shows it works

### Fixes

- Dim entries of later milestones so they read as unavailable
- Keep the lsappinfo parser macOS-only and move the shell header checks into the page tests
- Bound the disk list, satisfy the linters and drop the replaced shell from the shell test
- Let a short window scroll instead of sliding the map under the action bar
- Cap the CPU alert threshold at 400% so the default sits inside the slider
- Keep the system card headline on one line by moving the build number to its own row
- Restart the icon agent and the Dock with killall, since launchctl kickstart is refused under SIP
- Draw Inter and PingFang weights on macOS and match the design's sidebar strip
- Match the design's 65-point rows
- Let the page background shrink to the window so a long page scrolls
- Scroll a page taller than the window inside the main area and check the cells' text shadow
- Draw the project folders at Inter's normal line height, as the design
- Draw the settings slider as the old app's native range input
- Keep a busy confirmation on screen when Escape is pressed
- Stop keeping a timer cleanup per throttle window
- Read Full Disk Access and the free space again as the pages open
- Name the unreadable places above the empty state too
- Keep a page that cannot be narrower than the window inside it
- Refuse a folder item that holds an exclusion added after the scan
- Run automatic rules under the current settings and keep stopped or asked checks in order
- Read free space from the startup disk alone
- Never retry a move an earlier restore tried, and check before restore --yes
- Journal permanent deletions so an interrupted clean's history counts them
- See commits in a monorepo or worktree, and edits outside git, as project activity
- Report unknown commands, refuse root, survive a hangup and record interrupted cleans
- Watch the user's folders where Windows keeps them
- Keep a card's clean and its toast alive when the page goes
- Keep the page while an overlay holds the sidebar when the menu bar or a notification opens the window
- Stop a drill-down clean with its view and show its toast from the app's scope
- Say what deletions really do and finish the move from 扫描 to 检查
- Leave out 按类别查看 when no source found anything
- Ask about the Trash once, with its live figures
- Line up a maintenance record's statuses in one grid and stop repeating them
- Keep a drill-down view's action bar above the toast at the end of a long list
- Build the maintenance task grid from plain cells and set its status on the title's line
- Ask with the danger tone whenever the Trash is emptied, and count the overlay as the question does
- Keep a card's running clean, its progress and 停止 in the app's store so 概览 shows them after coming back
- Journal cleans on Windows too, so an interrupted clean there gets its history run
- Name 最近变化's days in the user's time zone, whatever the clock's wall time carries
- Keep the scan and the results' place while another page shows
