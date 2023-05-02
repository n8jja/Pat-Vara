package vara

import "sync"

// bufferCount is a thread-safe int for keeping the buffer count state.
type bufferCount struct {
	ch chan int // Channel for receiving BUFFER updates from the modem.

	m sync.RWMutex
	n int
}

func newBufferCount() *bufferCount { return &bufferCount{ch: make(chan int)} }

// get returns the current buffer count.
func (m *bufferCount) get() int {
	m.m.RLock()
	defer m.m.RUnlock()
	return m.n
}

// set sets the current buffer count.
func (m *bufferCount) set(n int) {
	m.m.Lock()
	m.n = n
	m.m.Unlock()
	select {
	case m.ch <- n:
	default:
	}
}

// notifyQueued subscribes to BUFFER updates sent from the modem.
//
// The returned channel is buffered, allowing the receiver to defer reading
// from the channel without missing out on the next BUFFER value sent from the
// modem.
func (m *bufferCount) notifyQueued() <-chan int {
	nextUpdate := make(chan int, 1)
	go func() {
		defer close(nextUpdate)
		for n := range m.ch {
			nextUpdate <- n
			return
		}
	}()
	return nextUpdate
}
