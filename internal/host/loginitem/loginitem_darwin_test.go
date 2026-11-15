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
	// open passes what follows --args to the app, so a login start is told apart.
	for _, want := range []string{"<string>ai.yldm.spruce</string>", "<string>/usr/bin/open</string>", "<string>/Applications/Spruce &amp; Co.app</string>",
		"<string>--args</string>\n\t\t<string>--login</string>\n\t</array>", "<key>RunAtLoad</key>"} {
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

func TestRefreshAddsTheLoginArgument(t *testing.T) {
	home := t.TempDir()
	path := filepath.Join(home, "Library", "LaunchAgents", "ai.yldm.spruce.plist")

	// Starting at login is off: nothing is written.
	if err := refreshAgent(path, "ai.yldm.spruce", "/Applications/Spruce.app"); err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(path); !os.IsNotExist(err) {
		t.Fatalf("refresh wrote an agent that was not there: %v", err)
	}

	// An earlier build's agent, without the argument, is rewritten for the app where it is now.
	old := `<plist version="1.0"><dict><key>Label</key><string>ai.yldm.spruce</string><key>ProgramArguments</key><array>` +
		`<string>/usr/bin/open</string><string>-a</string><string>/Applications/Old/Spruce.app</string></array></dict></plist>`
	if err := os.MkdirAll(filepath.Dir(path), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(old), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := refreshAgent(path, "ai.yldm.spruce", "/Applications/Spruce.app"); err != nil {
		t.Fatal(err)
	}
	data, err := os.ReadFile(path)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(data), "<string>--login</string>") || !strings.Contains(string(data), "<string>/Applications/Spruce.app</string>") {
		t.Fatalf("agent not rewritten:\n%s", data)
	}

	// One that passes it already is left alone.
	marked := strings.Replace(string(data), "<key>RunAtLoad</key>", "<key>KeepAlive</key><false/><key>RunAtLoad</key>", 1)
	if err := os.WriteFile(path, []byte(marked), 0o644); err != nil {
		t.Fatal(err)
	}
	if err := refreshAgent(path, "ai.yldm.spruce", "/Applications/Spruce.app"); err != nil {
		t.Fatal(err)
	}
	if again, _ := os.ReadFile(path); string(again) != marked {
		t.Errorf("an agent with the argument was rewritten:\n%s", again)
	}
}

func TestWatchLaunchOutsideAnApp(t *testing.T) {
	// The test binary never finishes launching as an app: the observer is set up and the answer stays unknown, which the shell waits a second for and then shows the window.
	WatchLaunch()
	WatchLaunch()
	if atLogin, known := LaunchedAtLogin(); atLogin || known {
		t.Errorf("LaunchedAtLogin() = %v, %v before the app finished launching", atLogin, known)
	}
}
