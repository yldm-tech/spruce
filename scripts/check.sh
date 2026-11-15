#!/usr/bin/env bash
# check.sh runs the acceptance checks every part of the NSGo migration must pass (DESIGN.md section 6): gofmt, vet and build for macOS and Windows on arm64 and amd64 without cgo, the tests with the race detector, golangci-lint for macOS and Windows, the headless nsgo tests and nsgo check. Run it from anywhere; it stops at the first failure with its exit code.
set -euo pipefail
cd "$(dirname "$0")/.."

unformatted=$(gofmt -l $(git ls-files '*.go'))
if [ -n "$unformatted" ]; then
  echo "check: gofmt would change:" >&2
  echo "$unformatted" >&2
  exit 1
fi

for t in darwin/arm64 darwin/amd64 windows/amd64 windows/arm64; do
  echo "check: vet and build $t"
  GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go vet ./...
  GOOS=${t%/*} GOARCH=${t#*/} CGO_ENABLED=0 go build -o /dev/null .
done

echo "check: go test -race"
# The root package runs the headless screen tests, and the long-list one alone takes about 9 minutes with the race detector on a loaded machine, so the default 10 minute limit is too tight.
go test -race -timeout 25m ./...

echo "check: golangci-lint (darwin, windows)"
golangci-lint run ./...
GOOS=windows golangci-lint run ./...

echo "check: nsgo test (headless)"
go tool nsgo test ./internal/...

echo "check: nsgo check"
go tool nsgo check -target macos_arm64,macos_x64,windows_x64,windows_arm64

echo "check: ok"
