//go:build e2e

package main

// The automation plugin lets scripts/e2e-smoke.sh and other test clients drive the running app through a Unix socket: list windows, evaluate scripts in the page, send input and capture the window as PNG. It listens only when MYGO_AUTOMATION names the socket path, and this file is compiled only with -tags e2e, so release builds never contain it.
import _ "github.com/egoist/mygo/plugins/automation"
