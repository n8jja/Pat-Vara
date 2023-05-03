package vara

import (
	"fmt"
	"net"
	"time"
)

// Wrapper for the data port connection we hand to clients. Implements net.Conn.
type varaDataConn struct {
	// the underlying TCP conn we're wrapping (type embedding)
	net.TCPConn
	// the parent modem hosting this connection
	modem Modem
}

// Close closes the connection.
// Any blocked Read or Write operations will be unblocked and return errors.
//
// "Overrides" net.Conn.Close.
func (v *varaDataConn) Close() error {
	// If client wants to close the data stream, close down RF and TCP as well
	return v.modem.Close()
}

// LocalAddr returns the local network address.
//
// "Overrides" net.Conn.LocalAddr.
func (v *varaDataConn) LocalAddr() net.Addr {
	return Addr{v.modem.myCall}
}

// RemoteAddr returns the remote network address.
//
// "Overrides" net.Conn.RemoteAddr.
func (v *varaDataConn) RemoteAddr() net.Addr {
	return Addr{v.modem.toCall}
}

func (v *varaDataConn) Write(b []byte) (int, error) {
	queued := v.modem.bufferCount.notifyQueued()
	n, err := v.TCPConn.Write(b)
	// Block until the modem confirms that data has been added to the
	// transmit buffer queue. This is needed to ensure TxBufferLen are
	// able to report the correct number of bytes, as well as making the
	// Write call behave more or less synchronous with regards to the
	// transmitted data (rate).
	select {
	case <-queued:
		return n, err
	case <-time.After(time.Minute):
		return n, fmt.Errorf("write queue timeout")
	}
	return n, err
}

// TxBufferLen implements the transport.TxBuffer interface.
// It returns the current number of bytes in the TX buffer queue or in transit to the modem.
func (v *varaDataConn) TxBufferLen() int { return v.modem.bufferCount.get() }
