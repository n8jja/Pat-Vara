package vara

import (
	"errors"
	"fmt"
	"net"
)

// Implementation for the net.Listener interface.
// (Close method is implemented in connection.go.)

// Accept waits for and returns the next connection to the listener.
func (m *Modem) Accept() (net.Conn, error) {
	conn, err := m.tncConnect()
	if err != nil {
		return conn, err
	}

	err = m.tncSetup()
	if err != nil {
		return nil, err
	}

	if err := m.writeCmd("LISTEN ON"); err != nil {
		return nil, err
	}

	// Block until connected
	if <-m.connectChange != connected {
		m.dataConn = nil
		return nil, errors.New("connection failed")
	}

	debugPrint(fmt.Sprintf("connected to %s", m.toCall))

	// Hand the VARA data TCP port to the client code
	return &varaDataConn{*m.dataConn, *m}, nil
}

// Addr returns the listener's network address.
func (m *Modem) Addr() net.Addr {
	return Addr{m.myCall}
}

type Addr struct{ string }

func (a Addr) Network() string { return network }
func (a Addr) String() string  { return a.string }
