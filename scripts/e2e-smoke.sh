#!/usr/bin/env bash
# e2e-smoke.sh: launches the e2e build of Spruce on macOS, checks that the 维护 page renders the task catalog, saves a screenshot of the idle page and quits the app. It never presses 运行, so no maintenance task runs.
#
#   scripts/e2e-smoke.sh [--build] [screenshot.png]
#
# --build runs `bun run build:e2e` first; without it the script builds only when the e2e app is missing. The screenshot defaults to ~/screenshots/spruce/e2e-smoke/idle.png. The app is driven through mygo's automation plugin (e2e_automation.go, linked only with -tags e2e) and the mygo-automation CLI; install the CLI from the module directory with `go install github.com/egoist/mygo/plugins/automation/cmd/mygo-automation`.
set -euo pipefail

root=$(cd "$(dirname "$0")/.." && pwd)
build=0
shot="$HOME/screenshots/spruce/e2e-smoke/idle.png"
for arg in "$@"; do
  case $arg in
    --build) build=1 ;;
    -*) echo "usage: $0 [--build] [screenshot.png]" >&2; exit 2 ;;
    *) shot=$arg ;;
  esac
done

if [ "$(uname -s)" != Darwin ]; then
  echo "e2e-smoke: macOS only" >&2
  exit 2
fi
command -v mygo-automation >/dev/null || { echo "e2e-smoke: mygo-automation not found; run: go install github.com/egoist/mygo/plugins/automation/cmd/mygo-automation" >&2; exit 2; }

case $(uname -m) in
  arm64) arch=arm64 ;;
  x86_64) arch=amd64 ;;
  *) echo "e2e-smoke: unsupported architecture $(uname -m)" >&2; exit 2 ;;
esac
app="$root/build/darwin-$arch/Spruce.app/Contents/MacOS/Spruce"
if [ "$build" = 1 ] || [ ! -x "$app" ]; then
  (cd "$root" && bun run build:e2e)
fi

# The app holds a single-instance lock: a second copy would hand over to the running one and exit, and the smoke test would then drive the wrong app.
if pgrep -x Spruce >/dev/null; then
  echo "e2e-smoke: Spruce is already running; quit it first" >&2
  exit 1
fi

tmp=$(mktemp -d "${TMPDIR:-/tmp}/spruce-smoke.XXXXXX")
sock="$tmp/app.sock"
export MYGO_AUTOMATION_SESSION="spruce-smoke-$$"
app_pid=""
cleanup() {
  mygo-automation close >/dev/null 2>&1 || true
  if [ -n "$app_pid" ] && kill -0 "$app_pid" 2>/dev/null; then
    kill "$app_pid" 2>/dev/null || true
    wait "$app_pid" 2>/dev/null || true
  fi
  rm -rf "$tmp"
}
trap cleanup EXIT INT TERM

MYGO_AUTOMATION="$sock" "$app" >"$tmp/app.log" 2>&1 &
app_pid=$!

for _ in $(seq 1 60); do
  [ -S "$sock" ] && break
  if ! kill -0 "$app_pid" 2>/dev/null; then
    echo "e2e-smoke: the app exited before it listened; its log:" >&2
    cat "$tmp/app.log" >&2
    exit 1
  fi
  sleep 0.5
done
[ -S "$sock" ] || { echo "e2e-smoke: no automation socket after 30 s" >&2; cat "$tmp/app.log" >&2; exit 1; }

mygo-automation open --socket "$sock"
mygo-automation wait --timeout 15s

# Waits in the page until the catalog has rendered (or 15 s pass), then reports what the page shows. Stalled CSS transitions are finished so the screenshot shows final colours: macOS does not tick animations of a window that is out of sight.
report=$(mygo-automation eval --json "$(cat <<'JS'
const deadline = Date.now() + 15000;
while (Date.now() < deadline && document.querySelectorAll('.app-main [role=switch]').length === 0) {
  await new Promise((r) => setTimeout(r, 100));
}
document.getAnimations().forEach((a) => a.finish());
const switches = Array.from(document.querySelectorAll('.app-main [role=switch]'));
const run = Array.from(document.querySelectorAll('.app-main button')).find((b) => b.textContent.trim() === '运行');
return {
  path: location.pathname,
  heading: document.querySelector('.app-main h1')?.textContent ?? '',
  cards: switches.map((s) => ({ title: document.getElementById(s.getAttribute('aria-labelledby'))?.textContent ?? '', on: s.checked })),
  runButton: Boolean(run),
  runEnabled: Boolean(run && !run.disabled),
};
JS
)")
echo "$report"

# Checks the report with the JavaScript runtime bun already provides, so the script needs no jq.
REPORT="$report" bun -e '
const r = JSON.parse(process.env.REPORT);
const fail = (m) => { console.error("e2e-smoke: " + m); process.exit(1); };
if (r.path !== "/maintenance") fail("expected /maintenance, got " + r.path);
if (r.heading !== "维护") fail("expected the 维护 heading, got " + JSON.stringify(r.heading));
if (r.cards.length === 0) fail("the catalog rendered no cards");
if (r.cards.some((c) => !c.title)) fail("a card has no title");
if (!r.runButton) fail("no 运行 button");
if (r.cards.some((c) => c.on) !== r.runEnabled) fail("运行 is enabled=" + r.runEnabled + " while " + r.cards.filter((c) => c.on).length + " cards are on");
console.log("e2e-smoke: " + r.cards.length + " cards: " + r.cards.map((c) => c.title + (c.on ? " (on)" : "")).join(", "));
'

mkdir -p "$(dirname "$shot")"
mygo-automation screenshot "$shot"
echo "e2e-smoke: ok"
