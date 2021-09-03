package vara

import "net"

// Implementation for the net.Listener interface.
// (Close method is implemented in connection.go.)

// Accept waits for and returns the next connection to the listener.
func (m *Modem) Accept() (net.Conn, error) {
	return nil, notImplemented
}

// Addr returns the listener's network address.
func (m *Modem) Addr() net.Addr {
	return nil
}
