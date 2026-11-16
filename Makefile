.PHONY: test dev build check

# The NSGo target of this machine, such as macos_arm64 or windows_x64, which make build builds; TARGET=windows_x64 or a list (TARGET=macos_arm64,macos_x64) builds others.
TARGET ?= $(subst darwin,macos,$(shell go env GOOS))_$(subst amd64,x64,$(shell go env GOARCH))

# The repository root is a Go workspace (go.work), not a module, so ./... matches nothing here: every target names the app's folder.
test:
	go test ./apps/spruce/...

dev:
	go tool nsgo dev apps/spruce

build:
	go tool nsgo build -target $(TARGET) apps/spruce

# Every acceptance check CI runs: gofmt, vet and build for each target, the third-party notices, the race tests, golangci-lint, the headless nsgo tests and nsgo check (apps/spruce/scripts/check.sh says what it needs).
check:
	apps/spruce/scripts/check.sh
