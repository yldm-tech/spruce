package loginitem

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestLaunchAgent(t *testing.T) {
	path := filepath.Join(t.TempDir(), "LaunchAgents", "ai.yldm.spruce.plist")
	if err := setLaunchAgent(path, "ai.yldm.spruce", "/Applications/Spruce & Co.app", true); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	for _, want := range []string{"<string>ai.yldm.spruce</string>", "<string>/usr/bin/open</string>", "<string>/Applications/Spruce &amp; Co.app</string>", "<key>RunAtLoad</key>"} {
		if !strings.Contains(string(data), want) {
			t.Errorf("plist lacks %q:\n%s", want, data)
		}
	}
	if err := setLaunchAgent(path, "ai.yldm.spruce", "", false); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Errorf("agent still there: %v", err)
	}
	// Removing an agent that is not there is fine.
	if err := setLaunchAgent(path, "ai.yldm.spruce", "", false); err != nil {
		t.Errorf("second removal: %v", err)
	}
}

func TestOutsideABundle(t *testing.T) {
	// The test binary is not in an app bundle: nothing is registered and it reports off.
	it := New("ai.yldm.spruce.test")
	if it.OpenAtLogin() {
		t.Error("OpenAtLogin is on outside a bundle")
	}
	if err := it.SetOpenAtLogin(true); err == nil {
		t.Error("SetOpenAtLogin succeeded outside a bundle")
	}
}
