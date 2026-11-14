package scancache

import "time"

// SetNow replaces the clock Load compares SavedAt with.
func (s *Store) SetNow(now func() time.Time) { s.now = now }
