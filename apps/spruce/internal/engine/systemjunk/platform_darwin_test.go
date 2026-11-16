package systemjunk

import (
	"errors"
	"testing"

	"spruce/apps/spruce/internal/engine/execx/execxtest"
)

func TestRunningAppsFromLSAppInfo(t *testing.T) {
	f := execxtest.New()
	f.On("/usr/bin/lsappinfo", "list").Return(` 1) "Google Chrome" ASN:0x0-0x1001:
    bundleID="com.google.Chrome"
    bundle path="/Applications/Google Chrome.app"
 2) "universalaccessd" ASN:0x0-0xb00b:
    bundleID=[ NULL ]
`, 0)
	r, err := runningApps(t.Context(), f)
	if err != nil {
		t.Fatal(err)
	}
	if !r["com.google.chrome"] || !r["google chrome"] || !r["universalaccessd"] || len(r) != 3 {
		t.Errorf("running %v", r)
	}
	f.On("/usr/bin/lsappinfo", "list").Fail(errors.New("no lsappinfo"))
	if _, err := runningApps(t.Context(), f); err == nil {
		t.Error("no error")
	}
}
