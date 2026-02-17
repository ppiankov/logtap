package forward

import "sync"

// Batch holds a failed push attempt for later retry.
type Batch struct {
	Labels map[string]string
	Lines  []TimestampedLine
	Size   int // estimated byte size
}

// Buffer is a bounded FIFO queue that drops oldest entries when full.
type Buffer struct {
	mu      sync.Mutex
	batches []Batch
	size    int
	cap     int
	drops   int64
}

// NewBuffer creates a buffer with the given byte capacity.
func NewBuffer(maxBytes int) *Buffer {
	return &Buffer{cap: maxBytes}
}

// Add appends a batch, evicting oldest entries if over capacity.
func (b *Buffer) Add(batch Batch) {
	b.mu.Lock()
	defer b.mu.Unlock()

	// evict oldest until there is room
	for b.size+batch.Size > b.cap && len(b.batches) > 0 {
		b.size -= b.batches[0].Size
		b.batches = b.batches[1:]
		b.drops++
	}

	b.batches = append(b.batches, batch)
	b.size += batch.Size
}

// Drain returns all buffered batches and clears the buffer.
func (b *Buffer) Drain() []Batch {
	b.mu.Lock()
	defer b.mu.Unlock()

	if len(b.batches) == 0 {
		return nil
	}

	out := b.batches
	b.batches = nil
	b.size = 0
	return out
}

// Size returns the current total byte usage.
func (b *Buffer) Size() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.size
}

// Len returns the number of buffered batches.
func (b *Buffer) Len() int {
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.batches)
}

// Drops returns the total number of batches dropped due to overflow.
func (b *Buffer) Drops() int64 {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.drops
}

// EstimateBatchSize returns a rough byte estimate for a batch.
func EstimateBatchSize(labels map[string]string, lines []TimestampedLine) int {
	size := 0
	for k, v := range labels {
		size += len(k) + len(v)
	}
	for _, l := range lines {
		size += len(l.Line) + 24 // 24 bytes for timestamp overhead
	}
	return size
}
