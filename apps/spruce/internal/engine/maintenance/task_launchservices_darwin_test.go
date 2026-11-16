package maintenance

import (
	"os"
	"path/filepath"
	"slices"
	"testing"

	"spruce/apps/spruce/internal/engine/execx/execxtest"
	"spruce/apps/spruce/internal/engine/taskresult"
)

func writeExecutable(t *testing.T, path string, mode os.FileMode) {
	t.Helper()
	if err := os.WriteFile(path, []byte("#!/bin/sh\n"), mode); err != nil {
		t.Fatal(err)
	}
}

func TestLaunchServicesRunsOnlyGarbageCollection(t *testing.T) {
	dir := t.TempDir()
	missing := filepath.Join(dir, "missing")
	notExec := filepath.Join(dir, "not-executable")
	lsregister := filepath.Join(dir, "lsregister")
	writeExecutable(t, notExec, 0o644)
	writeExecutable(t, lsregister, 0o755)

	f := execxtest.New()
	f.On(lsregister, "-gc")
	res := launchServicesTask([]string{missing, notExec, lsregister}).Run(t.Context(), macEnv(f))
	assertOutcome(t, res, taskresult.Applied, "打开方式")

	calls := f.Calls()
	if len(calls) != 1 || calls[0].Name != lsregister || !slices.Equal(calls[0].Args, []string{"-gc"}) {
		t.Fatalf("ran %v, want exactly %s -gc", calls, lsregister)
	}
}

func TestLaunchServicesUnavailableAndFailed(t *testing.T) {
	f := execxtest.New()
	res := launchServicesTask([]string{filepath.Join(t.TempDir(), "missing")}).Run(t.Context(), macEnv(f))
	assertOutcome(t, res, taskresult.Unavailable)
	if len(f.Calls()) != 0 {
		t.Errorf("ran %v without lsregister", f.Calls())
	}

	lsregister := filepath.Join(t.TempDir(), "lsregister")
	writeExecutable(t, lsregister, 0o755)
	f = execxtest.New()
	f.On(lsregister, "-gc").Return("", 1)
	assertOutcome(t, launchServicesTask([]string{lsregister}).Run(t.Context(), macEnv(f)), taskresult.Failed, "失败")
}

func TestLaunchServicesDefaultCandidatesUseTheFramework(t *testing.T) {
	for _, p := range lsregisterCandidates {
		if !filepath.IsAbs(p) || filepath.Base(p) != "lsregister" {
			t.Errorf("candidate %q is not an absolute lsregister path", p)
		}
	}
}
