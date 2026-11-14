package app

import (
	"context"
	"log/slog"
	"regexp"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/maintenance"
	"github.com/yldm-tech/spruce/internal/engine/taskresult"
)

func TestSummarize(t *testing.T) {
	start := time.Date(2026, 11, 14, 1, 0, 0, 0, time.UTC)
	s := summarize("id", start, start.Add(time.Minute), []taskresult.Result{
		{TaskID: "a", Outcome: taskresult.Applied},
		{TaskID: "b", Outcome: taskresult.Applied},
		{TaskID: "c", Outcome: taskresult.Skipped},
		{TaskID: "d", Outcome: taskresult.Failed},
		{TaskID: "e", Outcome: taskresult.Unchanged},
		{TaskID: "f", Outcome: taskresult.Unavailable},
	})
	if s.Applied != 2 || s.Skipped != 1 || s.Failed != 1 || s.Unchanged != 1 || s.Unavailable != 1 || len(s.Results) != 6 {
		t.Errorf("summary %+v", s)
	}
}

func TestNewRunID(t *testing.T) {
	id := newRunID(time.Date(2026, 11, 14, 1, 4, 12, 0, time.FixedZone("JST", 9*3600)))
	if !regexp.MustCompile(`^20261113T160412Z-[0-9a-f]{8}$`).MatchString(id) {
		t.Errorf("id %q", id)
	}
}

func TestRunErrorsAreChinese(t *testing.T) {
	release := make(chan struct{})
	entered := make(chan struct{})
	slow := maintenance.TaskImpl{
		Task: maintenance.Task{ID: "slow", Title: "slow", Icon: maintenance.IconNetwork},
		Run: func(context.Context, maintenance.Env) taskresult.Result {
			close(entered)
			<-release
			return taskresult.Result{Outcome: taskresult.Applied, Message: "好"}
		},
	}
	svc, err := maintenance.NewService([]maintenance.TaskImpl{slow}, maintenance.Env{}, nil)
	if err != nil {
		t.Fatal(err)
	}
	m := NewMaintenance(svc, &fakeStore{}, slog.New(slog.DiscardHandler))

	done := make(chan error)
	go func() {
		_, err := m.Run(t.Context(), []string{"slow"}, nil)
		done <- err
	}()
	<-entered
	cases := map[string][]string{
		"已有维护正在进行，请等它结束后再试":    {"slow"},
		"维护项目已更新，请重新打开维护页面后再试": {"renamed-task"},
		"没有选择任何维护项目":           nil,
	}
	for want, ids := range cases {
		if _, err := m.Run(t.Context(), ids, nil); err == nil || err.Error() != want {
			t.Errorf("Run(%q) error %v, want %q", ids, err, want)
		}
	}
	close(release)
	if err := <-done; err != nil {
		t.Fatal(err)
	}
}
