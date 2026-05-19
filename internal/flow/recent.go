package flow

import "sync"

// RecentBuffer is a fixed-capacity ring buffer of the most recent flow records.
// Writers (ingest pipeline) push; readers (dashboard API) take a Snapshot.
// The buffer overwrites oldest records once full.
type RecentBuffer struct {
	mu   sync.Mutex
	ring []Record
	head int
	full bool
}

// NewRecentBuffer returns a buffer that retains the last `capacity` records.
// capacity < 1 is clamped to 1.
func NewRecentBuffer(capacity int) *RecentBuffer {
	if capacity < 1 {
		capacity = 1
	}
	return &RecentBuffer{ring: make([]Record, capacity)}
}

// Push appends r, evicting the oldest record if the buffer is full.
// Safe for concurrent callers; the ingest pipeline currently has one writer.
func (b *RecentBuffer) Push(r Record) {
	b.mu.Lock()
	b.ring[b.head] = r
	b.head++
	if b.head >= len(b.ring) {
		b.head = 0
		b.full = true
	}
	b.mu.Unlock()
}

// Snapshot returns up to n records newest-first.
// n <= 0 or n larger than the buffer returns every record currently retained.
func (b *RecentBuffer) Snapshot(n int) []Record {
	b.mu.Lock()
	defer b.mu.Unlock()
	size := b.head
	if b.full {
		size = len(b.ring)
	}
	if n <= 0 || n > size {
		n = size
	}
	if n == 0 {
		return []Record{}
	}
	out := make([]Record, n)
	pos := b.head - 1
	if pos < 0 {
		pos = len(b.ring) - 1
	}
	for i := 0; i < n; i++ {
		out[i] = b.ring[pos]
		pos--
		if pos < 0 {
			pos = len(b.ring) - 1
		}
	}
	return out
}

// Cap returns the configured capacity.
func (b *RecentBuffer) Cap() int { return len(b.ring) }
