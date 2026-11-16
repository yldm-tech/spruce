package sysinfo

// ring keeps the last cap values pushed, oldest first.
type ring[T any] struct {
	buf   []T
	start int // index of the oldest value once full
	full  bool
}

func newRing[T any](capacity int) *ring[T] {
	return &ring[T]{buf: make([]T, 0, capacity)}
}

// push adds v, dropping the oldest value when full.
func (r *ring[T]) push(v T) {
	if !r.full {
		r.buf = append(r.buf, v)
		r.full = len(r.buf) == cap(r.buf)
		return
	}
	r.buf[r.start] = v
	r.start = (r.start + 1) % len(r.buf)
}

// values returns a copy of the values, oldest first.
func (r *ring[T]) values() []T {
	out := make([]T, 0, len(r.buf))
	out = append(out, r.buf[r.start:]...)
	return append(out, r.buf[:r.start]...)
}

// last returns the newest value.
func (r *ring[T]) last() (T, bool) {
	if len(r.buf) == 0 {
		var zero T
		return zero, false
	}
	return r.buf[(r.start+len(r.buf)-1)%len(r.buf)], true
}

func (r *ring[T]) len() int { return len(r.buf) }

// reset forgets every value.
func (r *ring[T]) reset() {
	clear(r.buf)
	r.buf = r.buf[:0]
	r.start = 0
	r.full = false
}
