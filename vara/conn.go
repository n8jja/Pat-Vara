package vara

import (
	"fmt"
	"io"
	"net"
	"time"
)

// Wrapper for the data port connection we hand to clients. Implements net.Conn.
type varaDataConn struct {
	// the remote station's callsign
	toCall string
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
	// TODO: Handle race. What if this is an old connection? Should we have a stateful conn?
	if v.modem.lastState != connected {
		return nil
	}

	v.modem.writeCmd(fmt.Sprintf("DISCONNECT"))
	// TODO: Timeout if modem doesn't respond?
	<-v.modem.connectChange
	return nil
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
	return Addr{v.toCall}
}

func (v *varaDataConn) Read(b []byte) (n int, err error) {
	if v.modem.lastState != connected {
		return 0, io.EOF
	}

	type res struct {
		n   int
		err error
	}
	ready := make(chan res, 1)
	go func() {
		defer close(ready)
		v.TCPConn.SetReadDeadline(time.Time{}) // Disable read deadline
		n, err = v.TCPConn.Read(b)
		ready <- res{n, err}
	}()
	select {
	case res := <-ready:
		return res.n, res.err
	case <-v.modem.connectChange:
		// Set a read deadline to ensure the Read call is cancelled.
		v.TCPConn.SetReadDeadline(time.Now())
		return 0, io.EOF
	}
}

func (v *varaDataConn) Write(b []byte) (int, error) {
	if v.modem.lastState != connected {
		return 0, io.EOF // TODO: Different error? "use of closed network connection"
	}

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
	case <-v.modem.connectChange:
		return 0, io.EOF // TODO: Different error? "use of closed network connection"
	case <-time.After(time.Minute):
		return n, fmt.Errorf("write queue timeout")
	}
	return n, err
}

// TxBufferLen implements the transport.TxBuffer interface.
// It returns the current number of bytes in the TX buffer queue or in transit to the modem.
func (v *varaDataConn) TxBufferLen() int { return v.modem.bufferCount.get() }
