package vara

import (
	"net"
	"testing"

	"github.com/la5nta/wl2k-go/transport"
)

func TestInterfaces(t *testing.T) {
	modem, _ := NewModem("varafm", "N0CALL", ModemConfig{})

	// Ensure modem implements the necessary interfaces
	// (https://github.com/la5nta/pat/wiki/Adding-transports)
	var _ transport.Dialer = modem
	var _ net.Conn = &conn{}

	// Ensure modem implements optional interfaces with extended functionality
	var _ net.Listener = &listener{}
	var _ transport.BusyChannelChecker = modem
	var _ transport.Flusher = &conn{}
	var _ transport.TxBuffer = &conn{}
}

func TestBandwidths(t *testing.T) {
	bw := Bandwidths()
	if !contains(bw, "500") || !contains(bw, "2300") || !contains(bw, "2750") {
		t.Fail()
	}
}
