package vara

import (
	"net"

	"github.com/la5nta/wl2k-go/transport"
)

// Implementations for various wl2k-go/transport interfaces.

func (m *Modem) DialURL(url *transport.URL) (net.Conn, error) {
	return nil, notImplemented
}

// TxBufferLen returns the number of bytes in the out buffer queue.
func (m *Modem) TxBufferLen() int {
	return 0
}

// Flush flushes the transmit buffers of the underlying modem.
func (m *Modem) Flush() error {
	return notImplemented
}

// Busy returns true if the channel is not clear.
func (m *Modem) Busy() bool {
	return true
}

func (m *Modem) SetPTT(on bool) error {
	return notImplemented
}

// SetRobust enables/disables robust mode.
func (m *Modem) SetRobust(r bool) error {
	return notImplemented
}
