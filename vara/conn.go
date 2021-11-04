package vara

import (
	"net"
	"sync"
)

// Wrapper for the data port connection we hand to clients.
type varaDataConn struct {
	// the underlying TCP conn we're wrapping (type embedding)
	net.TCPConn
	// the parent modem hosting this connection
	modem Modem
	// Locking mechanism
	mu sync.Mutex
	// Listener for VARA Comm
	net.Listener
	// Buffer
	buffer int
}

// Close closes the connection.
// Any blocked Read or Write operations will be unblocked and return errors.
func (v *varaDataConn) Close() error {
	// If client wants to close the data stream, close down RF and TCP as well
	return v.modem.Close()
}

// TxBufferLen returns the current buffer length
func (v *varaDataConn) TxBufferLen() int {
	v.mu.Lock()

	defer v.mu.Unlock()

	return v.buffer
}

// UpdateBuffer updates the buffer.
func (v *varaDataConn) UpdateBuffer (b int){
	if v == nil {
		return
	}

	v.mu.Lock()

	defer v.mu.Unlock()
	v.buffer = b
}
