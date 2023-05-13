package vara

import (
	"context"
	"errors"
	"fmt"
	"net"

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

	// TODO: Handle race condition here. Should prevent concurrent dialing.
	if m.lastState != disconnected {
		return nil, errors.New("modem busy")
	}

	// Set bandwidth from the URL
	if err := m.setBandwidth(url); err != nil {
		return nil, err
	}

	// TODO: Why? What does this do?
	if m.scheme == "varahf" {
		// VaraHF only - Winlink or P2P?
		p2p := url.Params.Get("p2p") == "true"
		if p2p {
			if err := m.writeCmd(fmt.Sprintf("P2P SESSION")); err != nil {
				return nil, err
			}
		} else {
			if err := m.writeCmd(fmt.Sprintf("WINLINK SESSION")); err != nil {
				return nil, err
			}
		}
	}

	// Start connecting
	m.lastState = connecting
	m.connectChange.Publish(connecting)
	connectChange, cancel := m.connectChange.Subscribe()
	defer cancel()
	if err := m.writeCmd(fmt.Sprintf("CONNECT %s %s", m.myCall, url.Target)); err != nil {
		return nil, err
	}

	// Block until connected or context cancellation
	select {
	case <-ctx.Done():
		m.writeCmd("DISCONNECT")
		<-connectChange
		return nil, ctx.Err()
	case newState := <-connectChange:
		if newState != connected {
			return nil, errors.New("connection failed")
		}
		// Hand the VARA data TCP port to the client code
		// TODO: What if this coincidentally was an inbound connection, or a connection dialed concurrently by another goroutine?
		//         Should the newState include remote address?
		//         Or maybe the complete command string instead of this enum?
		return &conn{Modem: m, remoteCall: url.Target}, nil
	}
}

// Abort disconnects the link immediately.
func (m *Modem) Abort() error { return m.writeCmd(fmt.Sprintf("ABORT")) }

func (m *Modem) setBandwidth(url *transport.URL) error {
	bw := url.Params.Get("bw")
	if bw == "" {
		return nil
	}
	if !contains(bandwidths, bw) {
		return errors.New(fmt.Sprintf("bandwidth %s not supported", bw))
	}
	return m.writeCmd(fmt.Sprintf("BW%s", bw))
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
