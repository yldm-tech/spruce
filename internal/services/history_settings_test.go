package services

import (
	"bytes"
	"context"
	"errors"
	"log/slog"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/history"
	"github.com/yldm-tech/spruce/internal/engine/settings"
)

func TestHistoryAppendedEventName(t *testing.T) {
	if HistoryAppended.Name() != "history:appended" {
		t.Errorf("event name %q", HistoryAppended.Name())
	}
}

func TestForwardRunsSendsEveryRecordedRun(t *testing.T) {
	store, err := history.Open(filepath.Join(t.TempDir(), "history.jsonl"))
	if err != nil {
		t.Fatal(err)
	}
	ch, unsubscribe := store.(history.Watcher).Subscribe()
	sent := make(chan history.Run, 4)
	ctx, cancel := context.WithCancel(t.Context())
	done := make(chan struct{})
	go func() {
		forwardRuns(ctx, ch, func(r history.Run) { sent <- r })
		close(done)
	}()
	// The maintenance and cleanup services append through this same store, so every recorded run reaches the page.
	start := time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC)
	if err := store.Append(history.Run{ID: "run-1", Kind: history.KindSystemJunk, StartedAt: start, EndedAt: start.Add(time.Second), Summary: &history.Totals{Freed: 1 << 20, Items: 3}}); err != nil {
		t.Fatal(err)
	}
	var got history.Run
	select {
	case got = <-sent:
	case <-time.After(5 * time.Second):
		t.Fatal("run not forwarded")
	}
	if got.Kind != history.KindSystemJunk || got.Summary == nil || got.Summary.Items != 3 {
		t.Errorf("forwarded %+v", got)
	}
	cancel()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("forwardRuns did not stop with its context")
	}

	// A closed subscription ends it as well.
	done = make(chan struct{})
	go func() {
		forwardRuns(t.Context(), ch, func(history.Run) {})
		close(done)
	}()
	unsubscribe()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("forwardRuns did not stop when its channel closed")
	}
}

func TestBroadcastHistoryNeedsAWatcher(t *testing.T) {
	var buf bytes.Buffer
	done := make(chan struct{})
	go func() {
		BroadcastHistory(t.Context(), &fakeStore{}, slog.New(slog.NewTextHandler(&buf, nil)))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("BroadcastHistory waited on a store that announces nothing")
	}
	if !strings.Contains(buf.String(), "does not announce") {
		t.Errorf("log %q", buf.String())
	}
}

func TestSettingsLimitsAreThePackageRanges(t *testing.T) {
	s := NewSettings(newSettingsStore(t), &fakeLogin{}, slog.New(slog.DiscardHandler))
	l := s.Limits()
	if l.CPUThreshold != (SettingRange{settings.MinCPUThreshold, settings.MaxCPUThreshold}) ||
		l.SustainMinutes != (SettingRange{settings.MinSustainMinutes, settings.MaxSustainMinutes}) ||
		l.DevArtifactMinAgeDays != (SettingRange{settings.MinDevArtifactMinAge, settings.MaxDevArtifactMinAge}) ||
		l.LargeFileMinMB != (SettingRange{settings.MinLargeFileMB, settings.MaxLargeFileMB}) ||
		l.RetentionDays != (SettingRange{settings.MinRetentionDays, settings.MaxRetentionDays}) ||
		l.MaxPaths != settings.MaxPaths {
		t.Errorf("Limits = %+v", l)
	}
	// Every range is what Update clamps to: values outside come back at its ends.
	v := s.Get()
	v.Health.CPUThreshold, v.Health.SustainMinutes = l.CPUThreshold.Max+1, l.SustainMinutes.Min-1
	v.Cleanup.DevArtifactMinAgeDays, v.Cleanup.LargeFileMinMB = l.DevArtifactMinAgeDays.Min-1, l.LargeFileMinMB.Max+1
	v.History.RetentionDays = l.RetentionDays.Min - 1
	got, err := s.Update(v)
	if err != nil {
		t.Fatal(err)
	}
	if got.Health.CPUThreshold != l.CPUThreshold.Max || got.Health.SustainMinutes != l.SustainMinutes.Min ||
		got.Cleanup.DevArtifactMinAgeDays != l.DevArtifactMinAgeDays.Min || got.Cleanup.LargeFileMinMB != l.LargeFileMinMB.Max ||
		got.History.RetentionDays != l.RetentionDays.Min {
		t.Errorf("clamped to %+v, limits %+v", got, l)
	}
}

func TestSettingsChooseFolder(t *testing.T) {
	s := NewSettings(newSettingsStore(t), &fakeLogin{}, slog.New(slog.DiscardHandler))
	var titles []string
	answer, answerErr := "/Users/me/Projects", error(nil)
	s.pickFolder = func(_ context.Context, title string) (string, error) {
		titles = append(titles, title)
		return answer, answerErr
	}
	if p, err := s.ChooseFolder(t.Context(), "选择要排除的文件夹"); err != nil || p != "/Users/me/Projects" {
		t.Errorf("ChooseFolder = %q, %v", p, err)
	}
	answer = ""
	if p, err := s.ChooseFolder(t.Context(), "x"); err != nil || p != "" {
		t.Errorf("cancelled ChooseFolder = %q, %v", p, err)
	}
	answerErr = errors.New("no window")
	if _, err := s.ChooseFolder(t.Context(), "x"); err == nil || err.Error() != "无法打开选择文件夹的窗口" {
		t.Errorf("failed ChooseFolder: %v", err)
	}
	if len(titles) != 3 || titles[0] != "选择要排除的文件夹" {
		t.Errorf("titles %q", titles)
	}
	// Choosing does not change the settings by itself.
	if st := s.Get(); len(st.Cleanup.Exclusions) != 0 {
		t.Errorf("settings changed: %+v", st.Cleanup)
	}
}

func TestSettingsAbout(t *testing.T) {
	s := NewSettings(newSettingsStore(t), &fakeLogin{}, slog.New(slog.DiscardHandler))
	dir := t.TempDir()
	s.version = func() string { return "1.2.3" }
	s.dataDir = func() (string, error) { return dir, nil }
	if a, err := s.About(); err != nil || a != (AppAbout{Version: "1.2.3", DataDir: dir}) {
		t.Errorf("About = %+v, %v", a, err)
	}
	s.dataDir = func() (string, error) { return "", errors.New("no config dir") }
	if _, err := s.About(); err == nil || err.Error() != "无法读取 Spruce 的数据文件夹" {
		t.Errorf("About without a data folder: %v", err)
	}
}
