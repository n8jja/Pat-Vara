package vara

import (
	"net"
	"time"
)

// Implementation for the net.Conn interface.

const network = "vara"

// Read reads data from the connection.
// Read can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetReadDeadline.
func (m *Modem) Read(b []byte) (n int, err error) { return m.dataConn.Read(b) }

// Write writes data to the connection.
// Write can be made to time out and return an error after a fixed
// time limit; see SetDeadline and SetWriteDeadline.
func (m *Modem) Write(b []byte) (n int, err error) { return m.dataConn.Write(b) }

// Close closes the connection.
// Any blocked Read or Write operations will be unblocked and return errors.
func (m *Modem) Close() error {
	if err := m.writeCmd("ABORT"); err != nil {
		return err
	}
	if m.rig != nil {
		_ = m.rig.SetPTT(false)
	}

	d := m.dataConn
	m.dataConn = nil
	if err := d.Close(); err != nil {
		return err
	}

	c := m.cmdConn
	m.cmdConn = nil
	if err := c.Close(); err != nil {
		return err
	}

	return nil
}

// LocalAddr returns the local network address.
func (m *Modem) LocalAddr() net.Addr { return Addr{m.myCall} }

// RemoteAddr returns the remote network address.
func (m *Modem) RemoteAddr() net.Addr { return Addr{m.toCall} }

// SetDeadline sets the read and write deadlines associated
// with the connection. It is equivalent to calling both
// SetReadDeadline and SetWriteDeadline.
func (m *Modem) SetDeadline(t time.Time) error {
	err := m.SetReadDeadline(t)
	if err != nil {
		return err
	}
	return m.SetWriteDeadline(t)
}

// SetReadDeadline sets the deadline for future Read calls
// and any currently-blocked Read call.
// A zero value for t means Read will not time out.
func (m *Modem) SetReadDeadline(t time.Time) error {
	return errNotImplemented
}

// SetWriteDeadline sets the deadline for future Write calls
// and any currently-blocked Write call.
// Even if write times out, it may return n > 0, indicating that
// some of the data was successfully written.
// A zero value for t means Write will not time out.
func (m *Modem) SetWriteDeadline(t time.Time) error {
	return errNotImplemented
}

type Addr struct{ string }

func (a Addr) Network() string { return network }
func (a Addr) String() string  { return a.string }
