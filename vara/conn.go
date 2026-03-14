package vara

import (
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
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
	cmds, cancel := v.cmds.Subscribe("DISCONNECTED", "BUFFER")
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
			switch {
			case !ok:
				return ErrModemClosed
			case cmd == "DISCONNECTED":
				return io.EOF
			default:
				if !timeout.Stop() {
					<-timeout.C
				}
				timeout.Reset(time.Minute)
				count = parseBuffer(cmd)
			}
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
		debugPrint("Closing connection...")
		// Reset buffer size for the next connexion
		bufferMaxSize = -1
		if v.Modem.closed {
			err = ErrModemClosed
			return
		}
		defer func() {
			// Discard any remaining data
			v.dataConn.SetReadDeadline(time.Now().Add(time.Second))
			n, _ := io.Copy(io.Discard, v.dataConn)
			debugPrint("close: discarded %d bytes of remaining data", n)
		}()
		v.closing = true
		connectChange, cancel := v.cmds.Subscribe("DISCONNECTED")
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
		case _, ok := <-connectChange:
			if !ok {
				err = ErrModemClosed
				return
			}
			// This is the happy path. Connection was gracefully closed.
			err = nil
			return
		case <-time.After(60 * time.Second):
			debugPrint("disconnect timeout - aborting connection")
			v.Abort()
			err = fmt.Errorf("disconnect timeout - connection aborted")
			return
		}
	})
	return err
}

func (v *conn) Read(b []byte) (n int, err error) {
	connectChange, cancel := v.cmds.Subscribe("DISCONNECTED")
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
	case _, ok := <-connectChange:
		debugPrint("read: disconnected while reading")
		if !ok {
			return 0, ErrModemClosed
		}
		// Workaround for race condition between cmd and data conn.
		// The data was of course sent before the DISCONNECT, but they are received
		// out of order since they're sent from the modem on independent streams.
		select {
		case res := <-ready:
			debugPrint("read: got data (%d bytes) after disconnect (err: %v)", res.n, res.err)
			if res.err != nil {
				return res.n, io.EOF
			}
			return res.n, nil
		case <-time.After(2 * time.Second):
			debugPrint("read: timeout waiting for data after disconnect")
			// Set a read deadline to ensure the above Read call is cancelled after we return.
			v.dataConn.SetReadDeadline(time.Now())
			return 0, io.EOF
		}
	}
}

var bufferMaxSize = -1

func (v *conn) Write(b []byte) (int, error) {
	cmds, cancel := v.cmds.Subscribe("DISCONNECTED", "BUFFER", "BITRATE", "SN")
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

	// THE MAGIC NUMBER DID NOT WORK FOR ME
	// When the link quality is high enough the vara modem estimate the air speed based on the SNR of the previous
	// transmission and the data in it's buffer.
	// If the amount of data is lower than a full air frame at the maximum possible speed the Vara modem will do two things :
	// - It will adapt the max air speed to transmit the content of  the buffer
	// - It will also suppose that there is no more data to transmit for the moment and send a break signal to it's peer
	//
	// Then a full link estimation is done for the next frame
	// This end to a very sub-optimum speed
	//
	// Due to the long interleaving FEC used by the modem the buffer must be filled with at least enough data for the next
	// air frame, as the modem did not send BUFFER signal during the TX and need to have enough data to calculate
	// the frame with the FEC prior to start TX
	// This is an attemp to overcome this issue using the last TX speed and the SNR to estimate the air speed
	// before the start of the playload transmission and size the buffer accordingly
	// The buffer is sized to store 2 air frames around 12 seconds

	const magicNumber = 7
	const airTime = 12

	// start with magic number as first estimation
	if bufferMaxSize == -1 {
		bufferMaxSize = magicNumber * len(b)
	}

	// Adapt the buufer size to store 2 frames at max speed
	if (v.maxBitRate*airTime)/8 > bufferMaxSize {
		bufferMaxSize = (v.maxBitRate * airTime) / 8
		debugPrint("Buffer size adapted for high speed to %d", bufferMaxSize)
	}

	bufferTimeout := time.NewTimer(time.Minute)
	defer bufferTimeout.Stop()
	bufferCount := v.bufferCount.get()
	for bufferCount >= bufferMaxSize && !v.closing {

		debugPrint("write: buffer full (%d >= %d)", bufferCount, bufferMaxSize)
		select {
		case cmd, ok := <-cmds:
			switch {
			case !ok:
				return 0, ErrModemClosed
			case cmd == "DISCONNECTED":
				debugPrint("write: state changed while waiting for buffer space")
				return 0, io.EOF
			case strings.HasPrefix(cmd, "BITRATE"):
				// We have a new bit rate, resize the buffer if needed
				debugPrint("BITRATE  %s", cmd)
				if (v.maxBitRate*airTime)/8 > bufferMaxSize {
					bufferMaxSize = (v.maxBitRate * airTime) / 8
					debugPrint("Buffer size adapted for high speed to %d", bufferMaxSize)
				}
			case strings.HasPrefix(cmd, "SN"):
				// We have a new SN, resize the buffer if needed
				debugPrint("SN  %s", cmd)
				if (v.maxBitRate*airTime)/8 > bufferMaxSize {
					bufferMaxSize = (v.maxBitRate * airTime) / 8
					debugPrint("Buffer size adapted for high speed to %d", bufferMaxSize)
				}
			default:
				bufferCount = parseBuffer(cmd)
				if !bufferTimeout.Stop() {
					<-bufferTimeout.C
				}
				bufferTimeout.Reset(time.Minute)
			}
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
			if cmd != "DISCONNECTED" {
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
