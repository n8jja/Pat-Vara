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
	remoteCall string
	closeOnce  sync.Once
	closing    bool
}

func (m *Modem) newConn(remoteCall string) *conn {
	m.dataConn.SetDeadline(time.Time{}) // Reset any previous deadlines
	return &conn{
		Modem:      m,
		remoteCall: remoteCall,
	}
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
		if v.closed {
			return
		}
		v.closing = true
		connectChange, cancel := v.connectChange.Subscribe()
		defer cancel()
		if v.lastState == disconnected {
			return
		}
		v.writeCmd("DISCONNECT")
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
	if v.closing && v.lastState == connected {
		// VARA keeps accepting data after a DISCONNECT command has been, adding it to the TX buffer queue.
		// Since VARA keeps the connection open until the TX buffer is empty, we need to make sure we don't
		// keep feeding the buffer after we've sent the DISCONNECT command.
		// To do this, we block until the disconnect is complete.
		<-connectChange
	}
	if v.lastState != connected {
		return 0, io.EOF
	}

	queued, done := v.bufferCount.notifyQueued()
	defer done()
	n, err := v.dataConn.Write(b)
	if err != nil {
		return n, err
	}
	// Block until the modem confirms that data has been added to the
	// transmit buffer queue. This is needed to ensure TxBufferLen are
	// able to report the correct number of bytes, as well as making the
	// Write call behave more or less synchronous with regards to the
	// transmitted data (rate).
	t := time.NewTimer(10 * time.Minute)
	defer t.Stop()
	select {
	case <-queued:
		return n, nil
	case <-connectChange:
		return 0, io.EOF
	case <-t.C:
		// Modem didn't ACK the write. This is most likely due to a
		// app<->tnc bug, but might also be due to stalled connection.
		//
		// This was previously a one minute timeout, but increased because
		// it seems newer versions of VARA HF is no longer guaranteed to
		// send BUFFER when data is added to the tx buffer, only when the
		// remote end ACKs (despite the spec saying otherwise).
		return n, fmt.Errorf("write queue timeout")
	}
}

// TxBufferLen implements the transport.TxBuffer interface.
// It returns the current number of bytes in the TX buffer queue or in transit to the modem.
func (v *conn) TxBufferLen() int { return v.bufferCount.get() }
