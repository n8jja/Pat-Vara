package vara

import (
	"net"
	"time"
)

// Implementation for the net.Conn interface.

// Read reads data from the connection.go.
// Read can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetReadDeadline.
func (m *Modem) Read(b []byte) (n int, err error) {
	return 0, notImplemented
}

// Write writes data to the connection.go.
// Write can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetWriteDeadline.
func (m *Modem) Write(b []byte) (n int, err error) {
	return 0, notImplemented
}

// Close closes the connection.go.
// Any blocked Read or Write operations will be unblocked and return errors.
func (m *Modem) Close() error {
	return notImplemented
}

// LocalAddr returns the local network address.
func (m *Modem) LocalAddr() net.Addr {
	return nil
}

// RemoteAddr returns the remote network address.
func (m *Modem) RemoteAddr() net.Addr {
	return nil
}

// SetDeadline sets the read and write deadlines associated
// with the connection.go. It is equivalent to calling both
// SetReadDeadline and SetWriteDeadline.
func (m *Modem) SetDeadline(t time.Time) error {
	return notImplemented
}

// SetReadDeadline sets the deadline for future Read calls
// and any currently-blocked Read call.
// A zero value for t means Read will not time out.
func (m *Modem) SetReadDeadline(t time.Time) error {
	return notImplemented
}

// SetWriteDeadline sets the deadline for future Write calls
// and any currently-blocked Write call.
// Even if write times out, it may return n > 0, indicating that
// some of the data was successfully written.
// A zero value for t means Write will not time out.
func (m *Modem) SetWriteDeadline(t time.Time) error {
	return notImplemented
}
