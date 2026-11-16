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
# The root package runs the headless screen tests under go test; the 3,000-row long-list one skips itself under the race detector (it alone took up to 25 minutes on a loaded machine) and runs here once without it.
go test -race -timeout 25m ./...
go test -count=1 -run '^TestScreensSystemJunkLongList$' .

echo "check: golangci-lint (darwin, windows)"
# Runs that overlap (CI's runners on one Mac, several checkouts on one machine) share golangci-lint's lock, which fails a second instance outright; --allow-serial-runners has it wait its turn instead.
golangci-lint run --allow-serial-runners ./...
GOOS=windows golangci-lint run --allow-serial-runners ./...

echo "check: nsgo test (headless)"
go tool nsgo test ./internal/...

echo "check: nsgo check"
go tool nsgo check -target macos_arm64,macos_x64,windows_x64,windows_arm64

echo "check: ok"
