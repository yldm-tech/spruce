package maintenance

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"testing"

	"github.com/yldm-tech/spruce/internal/engine/execx"
	"github.com/yldm-tech/spruce/internal/engine/execx/execxtest"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

// iconCacheFixture creates <tmp>/C with an iconservices cache, a Dock icon cache and an unrelated cache, plus a file outside C that a symlink in the cache points to.
func iconCacheFixture(t *testing.T) (cacheDir, outside string) {
	t.Helper()
	base := t.TempDir()
	cacheDir = filepath.Join(base, "C")
	icons := filepath.Join(cacheDir, iconServicesCacheName)
	outside = filepath.Join(base, "outside.txt")
	mustMkdir(t, filepath.Join(icons, "sub"))
	mustWrite(t, filepath.Join(icons, "store.index"))
	mustWrite(t, filepath.Join(icons, "sub", "blob"))
	mustWrite(t, filepath.Join(cacheDir, dockIconCacheName))
	mustWrite(t, filepath.Join(cacheDir, "com.apple.other", "keep"))
	mustWrite(t, outside)
	if err := os.Symlink(outside, filepath.Join(icons, "link")); err != nil {
		t.Fatal(err)
	}
	return cacheDir, outside
}

func mustMkdir(t *testing.T, dir string) {
	t.Helper()
	if err := os.MkdirAll(dir, 0o700); err != nil {
		t.Fatal(err)
	}
}

func mustWrite(t *testing.T, path string) {
	t.Helper()
	mustMkdir(t, filepath.Dir(path))
	if err := os.WriteFile(path, []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
}

func onKickstart(f *execxtest.Fake, label string) *execxtest.Expectation {
	return f.On(pathLaunchctl, "kickstart", "-k", "gui/"+strconv.Itoa(os.Getuid())+"/"+label)
}

func TestIconsClearsUserCachesAndRestartsAgents(t *testing.T) {
	cacheDir, outside := iconCacheFixture(t)
	f := execxtest.New()
	f.On(pathGetconf, "DARWIN_USER_CACHE_DIR").Return(cacheDir+"/\n", 0)
	onKickstart(f, iconServicesAgentLabel)
	onKickstart(f, dockAgentLabel)

	res := runIcons(t.Context(), macEnv(f))
	assertOutcome(t, res, taskresult.Applied, "图标缓存")

	entries, err := os.ReadDir(filepath.Join(cacheDir, iconServicesCacheName))
	if err != nil {
		t.Fatalf("the iconservices directory itself was removed: %v", err)
	}
	if len(entries) != 0 {
		t.Errorf("iconservices cache still holds %v", entries)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, dockIconCacheName)); !os.IsNotExist(err) {
		t.Errorf("Dock icon cache not removed: %v", err)
	}
	for _, keep := range []string{filepath.Join(cacheDir, "com.apple.other", "keep"), outside} {
		if _, err := os.Stat(keep); err != nil {
			t.Errorf("%s was removed: %v", keep, err)
		}
	}
	f.AssertCalled(t, pathLaunchctl, "kickstart", "-k", "gui/"+strconv.Itoa(os.Getuid())+"/"+iconServicesAgentLabel)
	f.AssertCalled(t, pathLaunchctl, "kickstart", "-k", "gui/"+strconv.Itoa(os.Getuid())+"/"+dockAgentLabel)
	f.AssertNotCalled(t, pathKillall)
}

func TestIconsIsIdempotentWithoutCaches(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "C")
	mustMkdir(t, cacheDir)
	f := execxtest.New()
	f.On(pathGetconf, "DARWIN_USER_CACHE_DIR").Return(cacheDir+"/\n", 0)
	onKickstart(f, iconServicesAgentLabel)
	onKickstart(f, dockAgentLabel)
	assertOutcome(t, runIcons(t.Context(), macEnv(f)), taskresult.Applied)
}

func TestIconsRestartFailureAsksForLogout(t *testing.T) {
	cacheDir, _ := iconCacheFixture(t)
	f := execxtest.New()
	f.On(pathGetconf, "DARWIN_USER_CACHE_DIR").Return(cacheDir+"/\n", 0)
	onKickstart(f, iconServicesAgentLabel).Return("", 1)
	onKickstart(f, dockAgentLabel)
	res := runIcons(t.Context(), macEnv(f))
	assertOutcome(t, res, taskresult.Applied, "注销并重新登录后生效")
	if !res.Partial {
		t.Errorf("result %+v, want partial so the done card shows the logout step", res)
	}
}

func TestIconsCancelledAfterClearingReportsTheChange(t *testing.T) {
	cacheDir, _ := iconCacheFixture(t)
	ctx, cancel := context.WithCancel(t.Context())
	f := execxtest.New()
	f.On(pathGetconf, "DARWIN_USER_CACHE_DIR").Return(cacheDir+"/\n", 0)
	onKickstart(f, iconServicesAgentLabel).Do(func(context.Context, execx.Command) (execx.Output, error) {
		cancel()
		return execx.Output{ExitCode: -1}, context.Canceled
	})
	res := runIcons(ctx, macEnv(f))
	assertOutcome(t, res, taskresult.Applied, "已清除图标缓存")
	if !res.Partial {
		t.Errorf("result %+v, want partial", res)
	}
	if entries, _ := os.ReadDir(filepath.Join(cacheDir, iconServicesCacheName)); len(entries) != 0 {
		t.Errorf("cache entries left: %v", entries)
	}
	if f.Called(pathLaunchctl, "kickstart", "-k", "gui/"+strconv.Itoa(os.Getuid())+"/"+dockAgentLabel) {
		t.Error("restarted the Dock after the cancel")
	}
}

func TestIconsCancelledWithNothingRemovedIsInterrupted(t *testing.T) {
	cacheDir := filepath.Join(t.TempDir(), "C")
	mustMkdir(t, cacheDir)
	ctx, cancel := context.WithCancel(t.Context())
	f := execxtest.New()
	f.On(pathGetconf, "DARWIN_USER_CACHE_DIR").Return(cacheDir+"/\n", 0)
	onKickstart(f, iconServicesAgentLabel).Do(func(context.Context, execx.Command) (execx.Output, error) {
		cancel()
		return execx.Output{ExitCode: -1}, context.Canceled
	})
	assertOutcome(t, runIcons(ctx, macEnv(f)), taskresult.Skipped, "未执行")
}

func TestIconsRejectsSuspiciousCacheDirectory(t *testing.T) {
	cacheDir, _ := iconCacheFixture(t)
	link := filepath.Join(t.TempDir(), "C")
	if err := os.Symlink(cacheDir, link); err != nil {
		t.Fatal(err)
	}
	for _, out := range []string{"", "relative/C", filepath.Dir(cacheDir), link, filepath.Join(t.TempDir(), "missing", "C")} {
		f := execxtest.New()
		f.On(pathGetconf, "DARWIN_USER_CACHE_DIR").Return(out+"\n", 0)
		res := runIcons(t.Context(), macEnv(f))
		assertOutcome(t, res, taskresult.Failed, "找不到图标缓存")
		f.AssertNotCalled(t, pathLaunchctl)
	}
	if _, err := os.Stat(filepath.Join(cacheDir, iconServicesCacheName, "store.index")); err != nil {
		t.Errorf("cache removed through a rejected directory: %v", err)
	}
}
