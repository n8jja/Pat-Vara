package vara

import (
	"net"
	"testing"

	"github.com/la5nta/wl2k-go/transport"
)

func TestInterfaces(t *testing.T) {
	var modem, _ = NewModem("N0CALL", ModemConfig{})

	// Ensure modem implements the necessary interfaces
	// (https://github.com/la5nta/pat/wiki/Adding-transports)
	var _ net.Conn = modem
	var _ transport.Dialer = modem

	// Ensure modem implements optional interfaces with extended functionality
	var _ net.Listener = modem
	var _ transport.BusyChannelChecker = modem
}