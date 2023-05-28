package vara

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strings"

	"github.com/la5nta/wl2k-go/transport"
)

// Implementations for various wl2k-go/transport interfaces.

func (m *Modem) DialURL(url *transport.URL) (net.Conn, error) {
	return m.DialURLContext(context.Background(), url)
}

// DialURLContext dials varafm/varahf URLs with cancellation support.
//
// If the context is cancelled while dialing, the connection may be closed gracefully before returning an error.
// Use Abort() for immediate cancellation of a dial operation.
func (m *Modem) DialURLContext(ctx context.Context, url *transport.URL) (net.Conn, error) {
	if url.Scheme != m.scheme {
		return nil, transport.ErrUnsupportedScheme
	}
	if m.closed {
		return nil, errors.New("modem closed")
	}

	// TODO: Handle race condition here. Should prevent concurrent dialing.
	if m.connectedState != disconnected {
		return nil, errors.New("modem busy")
	}

	// Set temporary bandwidth from the URL
	// This is reset on disconnect by handleCmd.
	if err := m.setBandwidth(url.Params.Get("bw")); err != nil {
		return nil, err
	}

	// TODO: Why? What does this do?
	if m.scheme == "varahf" {
		// VaraHF only - Winlink or P2P?
		p2p := url.Params.Get("p2p") == "true"
		if p2p {
			if err := m.writeCmd("P2P SESSION"); err != nil {
				return nil, err
			}
		} else {
			if err := m.writeCmd("WINLINK SESSION"); err != nil {
				return nil, err
			}
		}
	}

	// Start connecting
	m.connectedState = connecting
	m.cmds.Publish(connecting) // TODO: Can we get rid of this?
	cmds, cancel := m.cmds.Subscribe(connected, disconnected)
	defer cancel()
	if err := m.writeCmd(fmt.Sprintf("CONNECT %s %s", m.myCall, url.Target)); err != nil {
		return nil, err
	}

	// Handle context cancellation
	// VARA does not always accept DISCONNECT while dialing, so we might end up returning
	// a connection even after DISCONNECT is sent.
	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-ctx.Done():
			debugPrint("context cancellation - sending disconnect command...")
			m.writeCmd("DISCONNECT")
		case <-done:
			debugPrint("dial completed - context cancellation no longer possible")
		}
	}()

	// Block until connected state is updated
	switch cmd := <-cmds; {
	case strings.HasPrefix(cmd, connected):
		// TODO: What if this coincidentally was an inbound connection, or a connection dialed concurrently by another goroutine?
		//         Should the newState include remote address?
		//         Or maybe the complete command string instead of this enum?
		// Hand the VARA data TCP port to the client code
		return m.newConn(url.Target), nil
	case ctx.Err() != nil:
		// DISCONNECTED after context cancellation.
		return nil, ctx.Err()
	default:
		// DISCONNECTED for some other reason. Most likely a timeout.
		return nil, errors.New("connect timeout")
	}
}

// Disconnect gracefully closes any active connection, blocking until the link is disconnected.
//
// If the modem is not connected, this is a no-op.
func (m *Modem) Disconnect() error {
	ack, cancel := m.cmds.Subscribe(disconnected)
	defer cancel()
	if m.connectedState == disconnected {
		return nil
	}
	if err := m.writeCmd("DISCONNECT"); err != nil {
		return err
	}
	<-ack
	return nil
}

// Abort disconnects the link immediately.
func (m *Modem) Abort() error {
	err := m.writeCmd("ABORT")
	// VARA does not send a DISCONNECTED state change after ABORT if it's
	// already in the process of disconnecting, so we have to fake it.
	m.cmds.Publish(disconnected)
	m.handleDisconnected()
	return err
}

func (m *Modem) setBandwidth(bw string) error {
	if bw == "" {
		return nil
	}
	if !contains(bandwidths, bw) {
		return fmt.Errorf("bandwidth %s not supported", bw)
	}
	return m.writeCmd("BW" + bw)
}

func contains(c []string, s string) bool {
	for _, e := range c {
		if e == s {
			return true
		}
	}
	return false
}

// Busy returns true if the channel is not clear.
func (m *Modem) Busy() bool {
	return m.busy
}

// SetPTT injects the PTTController (probably hooked to a transceiver) that should be controlled by
// the modem.
//
// If nil, the PTT request from the TNC is ignored. VOX may still work.
func (m *Modem) SetPTT(ptt transport.PTTController) {
	m.rig = ptt
}
