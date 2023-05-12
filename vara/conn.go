package vara

import (
	"fmt"
	"io"
	"net"
	"sync"
	"time"
)

// Wrapper for the data port connection we hand to clients. Implements net.Conn.
type conn struct {
	*Modem
	closeOnce  sync.Once
	remoteCall string
}

// SetDeadline sets the read and write deadlines associated with the connection.
func (v *conn) SetDeadline(t time.Time) error { return v.dataConn.SetDeadline(t) }

// SetWriteDeadline sets the write deadline associated with the connection.
func (v *conn) SetWriteDeadline(t time.Time) error { return v.dataConn.SetWriteDeadline(t) }

// SetReadDeadline sets the read deadline associated with the connection.
func (v *conn) SetReadDeadline(t time.Time) error { return v.dataConn.SetReadDeadline(t) }

// LocalAddr returns the local network address.
func (v *conn) LocalAddr() net.Addr { return Addr{v.myCall} }

// RemoteAddr returns the remote network address.
func (v *conn) RemoteAddr() net.Addr { return Addr{v.remoteCall} }

// Close closes the connection.
//
// Any blocked Read or Write operations will be unblocked and return errors.
func (v *conn) Close() error {
	v.closeOnce.Do(func() {
		connectChange, cancel := v.connectChange.Subscribe()
		defer cancel()
		if v.lastState != connected {
			return
		}
		v.writeCmd(fmt.Sprintf("DISCONNECT"))
		<-connectChange
	})
	return nil
}

func (v *conn) Read(b []byte) (n int, err error) {
	connectChange, cancel := v.connectChange.Subscribe()
	defer cancel()
	if v.lastState != connected {
		return 0, io.EOF
	}

	type res struct {
		n   int
		err error
	}
	ready := make(chan res, 1)
	go func() {
		defer close(ready)
		v.dataConn.SetReadDeadline(time.Time{}) // Disable read deadline
		n, err = v.dataConn.Read(b)
		ready <- res{n, err}
	}()
	select {
	case res := <-ready:
		return res.n, res.err
	case <-connectChange:
		// Set a read deadline to ensure the Read call is cancelled.
		v.dataConn.SetReadDeadline(time.Now())
		return 0, io.EOF
	}
}

func (v *conn) Write(b []byte) (int, error) {
	connectChange, cancel := v.connectChange.Subscribe()
	defer cancel()
	if v.lastState != connected {
		return 0, io.EOF // TODO: Different error? "use of closed network connection"
	}

	queued := v.bufferCount.notifyQueued()
	n, err := v.dataConn.Write(b)
	// Block until the modem confirms that data has been added to the
	// transmit buffer queue. This is needed to ensure TxBufferLen are
	// able to report the correct number of bytes, as well as making the
	// Write call behave more or less synchronous with regards to the
	// transmitted data (rate).
	select {
	case <-queued:
		return n, err
	case <-connectChange:
		return 0, io.EOF // TODO: Different error? "use of closed network connection"
	case <-time.After(time.Minute):
		return n, fmt.Errorf("write queue timeout")
	}
	return n, err
}

// TxBufferLen implements the transport.TxBuffer interface.
// It returns the current number of bytes in the TX buffer queue or in transit to the modem.
func (v *conn) TxBufferLen() int { return v.bufferCount.get() }
