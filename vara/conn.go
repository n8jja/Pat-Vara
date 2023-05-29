package vara

import (
	"errors"
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

	lastWrite time.Time
	closeOnce sync.Once
	closing   bool
}

func (m *Modem) newConn(remoteCall string) *conn {
	m.dataConn.SetDeadline(time.Time{}) // Reset any previous deadlines
	return &conn{
		Modem:      m,
		remoteCall: remoteCall,
	}
}

// Flush blocks until the modem's TX buffer is empty.
func (v *conn) Flush() error {
	debugPrint("Flushing...")
	defer debugPrint("Flushed")
	cmds, cancel := v.cmds.Subscribe(disconnected, "BUFFER")
	defer cancel()
	if v.closing {
		return nil
	}

	timeout := time.NewTimer(time.Minute)
	defer timeout.Stop()

	count := v.bufferCount.get()
	for count > 0 {
		select {
		case cmd, ok := <-cmds:
			if !ok || cmd == disconnected {
				return io.EOF
			}
			if !timeout.Stop() {
				<-timeout.C
			}
			timeout.Reset(time.Minute)
			count = parseBuffer(cmd)
		case <-timeout.C:
			return errors.New("flush: buffer timeout")
		}
	}
	return nil
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
	var err error
	v.closeOnce.Do(func() {
		if v.Modem.closed {
			return
		}
		defer func() {
			// Discard any remaining data
			v.dataConn.SetReadDeadline(time.Now().Add(time.Second))
			n, _ := io.Copy(io.Discard, v.dataConn)
			debugPrint("close: discarded %d bytes of remaining data", n)
		}()
		v.closing = true
		connectChange, cancel := v.cmds.Subscribe(disconnected)
		defer cancel()
		if v.connectedState == disconnected {
			// Connection is already closed.
			return
		}

		// Workaround for race condition between write and close
		// (since cmd and data are not synchronized being on separate TCP sockets):
		// VARA promise that DISCONNECT will flush the TX buffer before closing the connection, but we
		// need to make sure the last data written have reached the modem before calling DISCONNECT.
		if dur := time.Since(v.lastWrite); dur < 2*time.Second {
			<-time.After(2*time.Second - dur)
		}

		v.writeCmd("DISCONNECT")
		select {
		case <-connectChange:
			// This is the happy path. Connection was gracefully closed.
			err = nil
		case <-time.After(60 * time.Second):
			debugPrint("disconnect timeout - aborting connection")
			v.Abort()
			err = fmt.Errorf("disconnect timeout - connection aborted")
		}
	})
	return err
}

func (v *conn) Read(b []byte) (n int, err error) {
	connectChange, cancel := v.cmds.Subscribe(disconnected)
	defer cancel()
	if v.connectedState != connected {
		debugPrint("read: not connected")
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
		if err != nil {
			debugPrint("read error: %v", err)
		}
		ready <- res{n, err}
	}()
	select {
	case res := <-ready:
		// We got data. Return it :)
		return res.n, res.err
	case <-connectChange:
		debugPrint("read: disconnected while reading")
		// Workaround for race condition between cmd and data conn.
		// The data was of course sent before the DISCONNECT, but they are received
		// out of order since they're sent from the modem on independent streams.
		select {
		case res := <-ready:
			debugPrint("read: got data after disconnect")
			return res.n, res.err
		case <-time.After(2 * time.Second):
			debugPrint("read: timeout waiting for data after disconnect")
			return 0, io.EOF
		}
		// Set a read deadline to ensure the Read call is cancelled.
		v.dataConn.SetReadDeadline(time.Now())
		return 0, io.EOF
	}
}

func (v *conn) Write(b []byte) (int, error) {
	cmds, cancel := v.cmds.Subscribe(disconnected, "BUFFER")
	defer cancel()
	if v.connectedState != connected {
		return 0, io.EOF
	}

	// Throttle to match the transmitted data rate by blocking if the tx buffer size is getting much bigger
	// than the payloads being sent.
	//
	// Yes, a magic number. We don't know the actual on-air packet length and/or max outstanding frames of
	// the mode in use. We also don't know how often the modem sends BUFFER updates. If the number is too
	// small, we end up causing unnecessary IDLE time. Too large and we end up with non-blocking writes and
	// a very large TX buffer causing Close() to block for a very long time. This magic number seem to work
	// well enough for both VARA FM and VARA HF.
	const magicNumber = 7

	bufferTimeout := time.NewTimer(time.Minute)
	defer bufferTimeout.Stop()
	bufferCount := v.bufferCount.get()
	for bufferCount >= magicNumber*len(b) && !v.closing {
		debugPrint("write: buffer full (%d >= %d)", bufferCount, magicNumber*len(b))
		select {
		case cmd, ok := <-cmds:
			if !ok || cmd == disconnected {
				debugPrint("write: state changed while waiting for buffer space")
				return 0, io.EOF
			}
			bufferCount = parseBuffer(cmd)
			if !bufferTimeout.Stop() {
				<-bufferTimeout.C
			}
			bufferTimeout.Reset(time.Minute)
		case <-bufferTimeout.C:
			// This is most likely due to a app<->tnc bug, but might also be due
			// to stalled connection.
			return 0, fmt.Errorf("write: buffer timeout")
		}
	}

	// VARA keeps accepting data after a DISCONNECT command has been sent, adding it to the TX buffer queue.
	// Since VARA keeps the connection open until the TX buffer is empty, we need to make sure we don't
	// keep feeding the buffer after we've sent the DISCONNECT command.
	// To do this, we block until the disconnect is complete.
	if v.closing && v.connectedState == connected {
		debugPrint("write: waiting for disconnect to complete...")
		for cmd := range cmds {
			if cmd != disconnected {
				continue
			}
			break
		}
		debugPrint("write: disconnect complete")
		return 0, io.EOF
	}

	// Modem is ready to receive more data :-)
	debugPrint("write: sending %d bytes", len(b))
	v.bufferCount.incr(len(b))
	v.lastWrite = time.Now()
	return v.dataConn.Write(b)
}

// TxBufferLen implements the transport.TxBuffer interface.
// It returns the current number of bytes in the TX buffer queue or in transit to the modem.
func (v *conn) TxBufferLen() int { return v.bufferCount.get() }
