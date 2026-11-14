package main

import (
	"testing"

	"github.com/nsgo-dev/nsgo/nsgotest"
)

// TestMain hands the main thread to the platform under go tool nsgo test -target; headless it only runs the tests.
func TestMain(m *testing.M) { nsgotest.Main(m) }
