package vara

import (
	"errors"
	"fmt"
	"net"
	"strconv"

	"github.com/la5nta/wl2k-go/transport"
)

// Implementations for various wl2k-go/transport interfaces.

func (m *Modem) DialURL(url *transport.URL) (net.Conn, error) {
	if url.Scheme != network {
		return nil, transport.ErrUnsupportedScheme
	}
	if m.cmdConn == nil {
		if err := m.start(); err != nil {
			return nil, err
		}
	}

	err := m.setBandwidth(url)
	if err != nil {
		return nil, err
	}

	m.toCall = url.Target
	err = m.writeCmd(fmt.Sprintf("CONNECT %s %s", m.myCall, m.toCall))
	if err != nil {
		return nil, err
	}

	if <-m.connected != 'c' {
		_ = m.Close()
		return nil, errors.New("connection failed")
	}

	return m, nil
}

func (m *Modem) setBandwidth(url *transport.URL) error {
	var bandwidth int
	bw := url.Params.Get("bw")
	if bw != "" {
		var err error
		bandwidth, err = strconv.Atoi(bw)
		if err != nil {
			return fmt.Errorf("parsing bw: %w", err)
		}
	}
	if bandwidth != 500 && bandwidth != 2750 {
		bandwidth = 2300
	}
	return m.writeCmd(fmt.Sprintf("BW%d", bandwidth))
}

// Busy returns true if the channel is not clear.
func (m *Modem) Busy() bool {
	return m.busy
}

// SetPTT Sets the PTT (probably a transceiver) that should be controlled by the TNC.
//
// If nil, the PTT request from the TNC is ignored.
func (m *Modem) SetPTT(ptt transport.PTTController) {
	m.rig = ptt
}
