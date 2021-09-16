package vara

import (
	"net"
)

// Wrapper for the data port connection we hand to clients.
type varaDataConn struct {
	// the underlying TCP conn we're wrapping (type embedding)
	net.TCPConn
	// the parent modem hosting this connection
	modem Modem
}

// Close closes the connection.
// Any blocked Read or Write operations will be unblocked and return errors.
func (v *varaDataConn) Close() error {
	// If client wants to close the data stream, close down RF and TCP as well
	return v.modem.Close()
}
