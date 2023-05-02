package vara

import "sync"

// bufferCount is a thread-safe int for keeping the buffer count state.
type bufferCount struct {
	m sync.RWMutex
	n int
}

func newBufferCount() *bufferCount { return &bufferCount{} }

func (m *bufferCount) get() int {
	m.m.Lock()
	defer m.m.Unlock()
	return m.n
}

func (m *bufferCount) set(n int) {
	m.m.RLock()
	m.n = n
	m.m.RUnlock()
}
