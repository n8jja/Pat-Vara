package vara

import (
	"errors"
	"fmt"
	"log"
	"net"
	"sync"
)

var ErrListenerClosed = errors.New("listener closed")

type listener struct {
	*Modem

	closeOnce sync.Once
	done      chan struct{}
}

// Accept waits for and returns the next connection to the listener.
func (m *Modem) Listen() (net.Listener, error) {
	if m.closed {
		return nil, errors.New("modem closed")
	}
	if err := m.writeCmd(fmt.Sprintf("LISTEN ON")); err != nil {
		return nil, err
	}
	return &listener{Modem: m, done: make(chan struct{})}, nil
}

// Accept waits for and returns the next inbound connection.
func (ln *listener) Accept() (net.Conn, error) {
	log.Println("Accept()")
	select {
	case conn, ok := <-ln.inboundConns:
		log.Println("Accept()  got", conn, ok)
		if !ok {
			return nil, ErrListenerClosed
		}
		return conn, nil
	case <-ln.done:
		return nil, ErrListenerClosed
	}
}

// Addr returns the listener's network address.
func (ln *listener) Addr() net.Addr { return Addr{ln.myCall} }

// Close closes the listener, any blocked Accept operations will be unblocked.
func (ln *listener) Close() error {
	var err error
	ln.closeOnce.Do(func() {
		err = ln.writeCmd("LISTEN OFF")
		if err == nil {
			close(ln.done)
		}
	})
	return err
}
