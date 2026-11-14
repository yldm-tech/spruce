# Spruce

Spruce is a desktop app for macOS and Windows that cleans up junk, uninstalls apps, shows what fills the disk, runs maintenance tasks and watches the system's health. It is written in Go on [NSGo](https://github.com/nsgo-dev/nsgo); the UI copy is Simplified Chinese.

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
- `internal/engine/`: scanning, cleaning, uninstalling, maintenance, history, settings and system information, with no UI dependency.
- `internal/app/`: the controllers the UI calls, between the engine and the pages.
- `internal/host/`: the data folder, single instance, login item and Full Disk Access.
- `internal/ui/`: the shell, the pages (`internal/ui/pages/<page>`), the design system (`kit`), the shared cleaner flow (`cleanflow`) and the app-wide stores.
- `screens_<page>_test.go`: window tests that put each page into its main states and take screenshots.
