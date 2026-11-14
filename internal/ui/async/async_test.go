package async

import (
	"context"
	"errors"
	"runtime"
	"slices"
	"sync"
	"testing"
	"time"

	"github.com/nsgo-dev/nsgo/core"
)

// host is a graph with a post queue the test drains on its own goroutine, which stands for the engine's, and a manual clock.
type host struct {
	root  *core.Scope
	clock *core.ManualClock
	mu    sync.Mutex
	posts []func()
}

func newHost(t *testing.T) *host {
	h := &host{clock: core.NewManualClock(time.Date(2026, 11, 14, 9, 0, 0, 0, time.UTC))}
	h.root = core.NewRoot(core.RootOptions{
		Post: func(fn func()) {
			h.mu.Lock()
			h.posts = append(h.posts, fn)
			h.mu.Unlock()
		},
		Clock:  h.clock,
		Report: func(err error) { t.Errorf("graph failed: %v", err) },
	})
	t.Cleanup(h.root.Dispose)
	return h
}

// drain runs the posted functions, and those they post, until none is left.
func (h *host) drain() {
	for {
		h.mu.Lock()
		posts := h.posts
		h.posts = nil
		h.mu.Unlock()
		if len(posts) == 0 {
			return
		}
		for _, fn := range posts {
			fn()
		}
	}
}

// drainUntil drains until cond holds or a second passes, for work on other goroutines.
func (h *host) drainUntil(t *testing.T, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !cond() {
		if time.Now().After(deadline) {
			t.Fatal("condition not reached")
		}
		h.drain()
		time.Sleep(time.Millisecond)
	}
}

// advance moves the clock and runs what it posted.
func (h *host) advance(d time.Duration) {
	h.drain()
	h.clock.Advance(d)
	h.drain()
}

func TestDoPostsTheResult(t *testing.T) {
	h := newHost(t)
	var got int
	var gotErr error
	done := false
	Do(h.root, t.Context(), func(context.Context) (int, error) { return 42, nil }, func(v int, err error) {
		got, gotErr, done = v, err, true
	})
	h.drainUntil(t, func() bool { return done })
	if got != 42 || gotErr != nil {
		t.Errorf("done(%d, %v)", got, gotErr)
	}
}

func TestDoDropsTheResultOnceDisposed(t *testing.T) {
	h := newHost(t)
	child := h.root.Child()
	release := make(chan struct{})
	finished := make(chan struct{})
	called := false
	Do(child, t.Context(), func(context.Context) (string, error) {
		<-release
		defer close(finished)
		return "", errors.New("late")
	}, func(string, error) { called = true })
	child.Dispose()
	close(release)
	<-finished
	time.Sleep(10 * time.Millisecond)
	h.drain()
	if called {
		t.Error("done ran after its scope was disposed")
	}
}

func TestFeedThrottlesAndDeliversTheLast(t *testing.T) {
	h := newHost(t)
	var got []int
	f := NewFeed(h.root, 100*time.Millisecond, func(v int) { got = append(got, v) })
	f.Send(1)
	h.drain()
	if !slices.Equal(got, []int{1}) {
		t.Fatalf("first value not delivered at once: %v", got)
	}
	for v := 2; v <= 5; v++ {
		f.Send(v)
	}
	h.drain()
	if !slices.Equal(got, []int{1}) {
		t.Fatalf("values inside the window delivered early: %v", got)
	}
	h.advance(100 * time.Millisecond)
	if !slices.Equal(got, []int{1, 5}) {
		t.Fatalf("after the window got %v, want the latest", got)
	}
	// Nothing new: the window closes, and the next value goes at once.
	h.advance(100 * time.Millisecond)
	f.Send(6)
	h.drain()
	if !slices.Equal(got, []int{1, 5, 6}) {
		t.Fatalf("got %v", got)
	}
}

func TestFeedFlushGoesBeforeTheResult(t *testing.T) {
	h := newHost(t)
	var events []string
	f := NewFeed(h.root, time.Second, func(v string) { events = append(events, "progress "+v) })
	f.Send("a")
	h.drain()
	f.Send("b")
	f.Flush()
	h.root.Post(func() { events = append(events, "result") })
	h.drain()
	if !slices.Equal(events, []string{"progress a", "progress b", "result"}) {
		t.Fatalf("events %v", events)
	}
	// The window's tick has nothing left to deliver.
	h.advance(time.Second)
	if len(events) != 3 {
		t.Fatalf("events after the tick %v", events)
	}
}

func TestFeedStopsWithItsScope(t *testing.T) {
	h := newHost(t)
	child := h.root.Child()
	var got []int
	f := NewFeed(child, 100*time.Millisecond, func(v int) { got = append(got, v) })
	f.Send(1)
	h.drain()
	f.Send(2)
	child.Dispose()
	h.advance(time.Second)
	f.Send(3)
	f.Flush()
	h.advance(time.Second)
	if !slices.Equal(got, []int{1}) {
		t.Fatalf("delivered after disposal: %v", got)
	}
}

func TestFeedFromManyGoroutines(t *testing.T) {
	h := newHost(t)
	last := -1
	f := NewFeed(h.root, 10*time.Millisecond, func(v int) { last = v })
	var wg sync.WaitGroup
	for g := range 8 {
		wg.Go(func() {
			for i := range 100 {
				f.Send(g*1000 + i)
			}
		})
	}
	wg.Wait()
	f.Send(-7)
	h.advance(10 * time.Millisecond)
	h.advance(10 * time.Millisecond)
	if last != -7 {
		t.Errorf("last delivered %d, want the last sent", last)
	}
}

func TestBatchKeepsEveryValue(t *testing.T) {
	h := newHost(t)
	var batches [][]string
	b := NewBatch(h.root, 120*time.Millisecond, func(vs []string) { batches = append(batches, vs) })
	b.Send("a")
	h.drain()
	b.Send("b")
	b.Send("c")
	h.drain()
	if len(batches) != 1 {
		t.Fatalf("batches %v", batches)
	}
	h.advance(120 * time.Millisecond)
	b.Send("d")
	b.Flush()
	h.drain()
	want := [][]string{{"a"}, {"b", "c"}, {"d"}}
	if !slices.EqualFunc(batches, want, slices.Equal) {
		t.Fatalf("batches %v, want %v", batches, want)
	}
}

func TestSubscribeForwardsUntilTheEnd(t *testing.T) {
	h := newHost(t)
	ch := make(chan int)
	unsubscribed := make(chan struct{})
	ctx, cancel := context.WithCancel(t.Context())
	var got []int
	Subscribe(h.root, ctx, ch, func() { close(unsubscribed) }, func(v int) { got = append(got, v) })
	ch <- 1
	ch <- 2
	h.drainUntil(t, func() bool { return len(got) == 2 })
	cancel()
	select {
	case <-unsubscribed:
	case <-time.After(5 * time.Second):
		t.Fatal("unsubscribe not called")
	}

	// A closed channel ends it too.
	ch2 := make(chan int)
	done := make(chan struct{})
	Subscribe(h.root, t.Context(), ch2, func() { close(done) }, func(int) {})
	close(ch2)
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("closed channel did not end the subscription")
	}
}

// heapInUse is the live heap after a full collection.
func heapInUse() uint64 {
	var m runtime.MemStats
	runtime.GC()
	runtime.GC()
	runtime.ReadMemStats(&m)
	return m.HeapAlloc
}

// A feed left open for hours (健康监测's samples) opens a window per value; a window that has ended must leave nothing behind in its scope.
func TestFeedKeepsNoMemoryPerWindow(t *testing.T) {
	h := newHost(t)
	f := NewFeed(h.root, 10*time.Millisecond, func(int) {})
	windows := func(n int) {
		for i := range n {
			f.Send(i)
			h.drain()
			// Nothing else comes: the window ends empty and the next value opens another.
			h.advance(10 * time.Millisecond)
		}
	}
	windows(200)
	before := heapInUse()
	windows(20000)
	after := heapInUse()
	if after > before && after-before > 1<<20 {
		t.Errorf("20000 windows kept %d bytes", after-before)
	}
}
