package schedule_test

import (
	"context"
	"errors"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"github.com/yldm-tech/spruce/internal/engine/schedule"
	"github.com/yldm-tech/spruce/internal/host/instance"
)

const wait = 5 * time.Second

// realLock is instance.LockFile on a clean.lock in a temporary folder, and a second opener standing in for another Spruce process.
func realLock(t *testing.T) (schedule.LockFunc, func() bool) {
	t.Helper()
	path := filepath.Join(t.TempDir(), "clean.lock")
	lock := func() (func() error, bool, error) { return instance.LockFile(path) }
	other := func() bool {
		release, held, err := instance.LockFile(path)
		if err != nil {
			t.Fatal(err)
		}
		if held {
			_ = release()
		}
		return held
	}
	return lock, other
}

func done(ctx context.Context) bool {
	select {
	case <-ctx.Done():
		return true
	case <-time.After(wait):
		return false
	}
}

func TestCleanCancelsBackgroundAndWaitsForIt(t *testing.T) {
	lock, otherCanLock := realLock(t)
	a := schedule.NewActivity(lock)
	run, endRun, err := a.BeginBackground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if otherCanLock() {
		t.Fatal("another process could take the lock while the background run holds it")
	}
	returned := make(chan struct{})
	go func() {
		<-run.Done()
		time.Sleep(20 * time.Millisecond) // the run finishes what it was doing
		close(returned)
		endRun()
	}()
	clean, endClean, err := a.BeginClean(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	select {
	case <-returned:
	default:
		t.Fatal("BeginClean returned before the background run ended")
	}
	if !a.Cleaning() || clean.Err() != nil {
		t.Errorf("cleaning %v, ctx %v", a.Cleaning(), clean.Err())
	}
	if otherCanLock() {
		t.Error("another process could take the lock during the clean")
	}
	// Background work waits for the clean.
	if _, _, err := a.BeginBackground(t.Context()); !errors.Is(err, schedule.ErrBusy) {
		t.Errorf("background during a clean: %v", err)
	}
	endClean()
	endClean()
	if a.Cleaning() || !otherCanLock() {
		t.Error("the clean's end did not release")
	}
	if _, end, err := a.BeginBackground(t.Context()); err != nil {
		t.Errorf("background after the clean: %v", err)
	} else {
		end()
	}
}

func TestBeginCleanGivesUpWhenItsContextEnds(t *testing.T) {
	a := schedule.NewActivity(nil)
	_, endRun, err := a.BeginBackground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer endRun()
	ctx, cancel := context.WithTimeout(t.Context(), 50*time.Millisecond)
	defer cancel()
	// The run never ends: the clean waits until its own context ends.
	if _, _, err := a.BeginClean(ctx); !errors.Is(err, context.DeadlineExceeded) {
		t.Errorf("BeginClean = %v", err)
	}
	if a.Cleaning() {
		t.Error("a clean that never began still counts")
	}
}

// TestAutoCleanRunsInsideTheBackgroundSlot is the regression test for the self-lock: the rule's clean neither cancels its own run nor takes the lock a second time.
func TestAutoCleanRunsInsideTheBackgroundSlot(t *testing.T) {
	lock, otherCanLock := realLock(t)
	a := schedule.NewActivity(lock)
	run, endRun, err := a.BeginBackground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	auto, endAuto, err := a.BeginAutoClean(run)
	if err != nil {
		t.Fatalf("BeginAutoClean: %v", err)
	}
	if !schedule.IsAuto(auto) || schedule.IsAuto(run) || schedule.IsAuto(t.Context()) {
		t.Error("IsAuto marks the wrong contexts")
	}
	if run.Err() != nil || auto.Err() != nil || !a.Cleaning() || otherCanLock() {
		t.Fatalf("run %v auto %v cleaning %v", run.Err(), auto.Err(), a.Cleaning())
	}
	// A context of its own is not the slot.
	if _, _, err := a.BeginAutoClean(t.Context()); !errors.Is(err, schedule.ErrNotAuto) {
		t.Errorf("BeginAutoClean outside the slot: %v", err)
	}

	// The user's clean wins: it cancels the run, and the rule stops after its current item.
	var wg sync.WaitGroup
	wg.Go(func() {
		<-auto.Done()
		endAuto()
		endRun()
	})
	_, endClean, err := a.BeginClean(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	wg.Wait()
	if auto.Err() == nil || run.Err() == nil {
		t.Error("the user's clean did not cancel the rule")
	}
	if _, _, err := a.BeginAutoClean(run); err == nil {
		t.Error("an automatic clean began in a cancelled run")
	}
	endClean()
	if a.Cleaning() || !otherCanLock() {
		t.Error("not released")
	}
}

func TestAnotherProcessHoldsTheLock(t *testing.T) {
	path := filepath.Join(t.TempDir(), "clean.lock")
	release, held, err := instance.LockFile(path)
	if err != nil || !held {
		t.Fatal(held, err)
	}
	a := schedule.NewActivity(func() (func() error, bool, error) { return instance.LockFile(path) })
	if _, _, err := a.BeginClean(t.Context()); !errors.Is(err, schedule.ErrBusyElsewhere) {
		t.Errorf("BeginClean = %v", err)
	}
	if _, _, err := a.BeginBackground(t.Context()); !errors.Is(err, schedule.ErrBusyElsewhere) {
		t.Errorf("BeginBackground = %v", err)
	}
	if a.Cleaning() {
		t.Error("a refused clean still counts")
	}
	_ = release()
	_, end, err := a.BeginClean(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	end()

	broken := schedule.NewActivity(func() (func() error, bool, error) { return nil, false, errors.New("permission denied") })
	if _, _, err := broken.BeginClean(t.Context()); !errors.Is(err, schedule.ErrLock) {
		t.Errorf("BeginClean with a broken lock = %v", err)
	}
	if _, _, err := broken.BeginBackground(t.Context()); !errors.Is(err, schedule.ErrLock) {
		t.Errorf("BeginBackground with a broken lock = %v", err)
	}
}

func TestOverlappingCleansShareTheLock(t *testing.T) {
	lock, otherCanLock := realLock(t)
	a := schedule.NewActivity(lock)
	_, end1, err := a.BeginClean(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	_, end2, err := a.BeginClean(t.Context())
	if err != nil {
		t.Fatalf("a second clean of this process: %v", err)
	}
	end1()
	if otherCanLock() || !a.Cleaning() {
		t.Error("the lock went with the first of two cleans")
	}
	end2()
	if !otherCanLock() || a.Cleaning() {
		t.Error("the lock stayed after the last clean")
	}
}

func TestShutdownAndWait(t *testing.T) {
	a := schedule.NewActivity(nil)
	clean, endClean, err := a.BeginClean(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	if a.Wait(10 * time.Millisecond) {
		t.Error("Wait returned true while a clean runs")
	}
	a.Shutdown()
	if !done(clean) {
		t.Fatal("Shutdown did not end the clean's context")
	}
	go func() {
		time.Sleep(20 * time.Millisecond)
		endClean()
	}()
	if !a.Wait(wait) {
		t.Error("Wait did not see the last clean end")
	}
	if _, _, err := a.BeginClean(t.Context()); err == nil {
		t.Error("a clean began after Shutdown")
	}
	if _, _, err := a.BeginBackground(t.Context()); err == nil {
		t.Error("background work began after Shutdown")
	}
	if !schedule.NewActivity(nil).Wait(time.Millisecond) {
		t.Error("Wait without cleans")
	}
}

func TestShutdownEndsTheBackgroundRun(t *testing.T) {
	a := schedule.NewActivity(nil)
	run, end, err := a.BeginBackground(t.Context())
	if err != nil {
		t.Fatal(err)
	}
	defer end()
	a.Shutdown()
	if !done(run) {
		t.Error("Shutdown did not end the background run")
	}
}
