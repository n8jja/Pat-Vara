package vara

import (
	"strconv"
	"strings"
	"sync"
)

// bufferCount is a thread-safe int for keeping the buffer count state.
type bufferCount struct {
	mu sync.RWMutex
	n  int
}

func newBufferCount() *bufferCount { return &bufferCount{} }

// reset resets the buffer count to zero.
func (m *bufferCount) reset() { m.mu.Lock(); m.n = 0; m.mu.Unlock() }

func (m *bufferCount) set(n int) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n = n
}

// get returns the current buffer count.
func (m *bufferCount) get() int {
	m.mu.RLock()
	defer m.mu.RUnlock()
	return m.n
}

// incr increments the current buffer count, retuning the new (calculated) value.
func (m *bufferCount) incr(n int) int {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.n += n
	return m.n
}

func parseBuffer(s string) int {
	n, _ := strconv.Atoi(strings.TrimPrefix(s, "BUFFER "))
	return n
}
