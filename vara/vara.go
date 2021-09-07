package vara

import (
	"errors"
	"fmt"
	"net"

	"github.com/imdario/mergo"
)

var notImplemented = errors.New("not implemented")

// ModemConfig defines configuration options for connecting with the VARA modem program.
type ModemConfig struct {
	// Host on the network which is hosting VARA; defaults to `localhost`
	Host string
	// CmdPort is the TCP port on which to reach VARA; defaults to 8300
	CmdPort int
	// DataPort is the TCP port on which to exchange over-the-air payloads with VARA;
	// defaults to 8301
	DataPort int
}

var defaultConfig = ModemConfig{
	Host:     "localhost",
	CmdPort:  8300,
	DataPort: 8301,
}

type Modem struct {
	myCall   string
	config   ModemConfig
	cmdConn  *net.TCPConn
	dataConn *net.TCPConn
}

// NewModem initializes configuration for a new VARA modem client stub.
func NewModem(myCall string, config ModemConfig) (*Modem, error) {
	// Back-fill empty config values with defaults
	if err := mergo.Merge(&config, defaultConfig); err != nil {
		return nil, err
	}
	return &Modem{myCall: myCall, config: config}, nil
}

// Start establishes TCP connections with the VARA modem program. This must be called before
// sending commands to the modem.
func (m *Modem) Start() error {
	var err error
	m.cmdConn, err = m.reconnect("command", m.config.CmdPort)
	if err != nil {
		return err
	}

	m.dataConn, err = m.reconnect("data", m.config.DataPort)
	if err != nil {
		return err
	}
	return nil
}

func (m *Modem) reconnect(name string, port int) (*net.TCPConn, error) {
	cmdAddr, err := net.ResolveTCPAddr("tcp", fmt.Sprintf("%s:%d", m.config.Host, port))
	if err != nil {
		return nil, fmt.Errorf("couldn't resolve VARA %s address: %w", name, err)
	}
	conn, err := net.DialTCP("tcp", nil, cmdAddr)
	if err != nil {
		return nil, fmt.Errorf("couldn't connect to VARA %s port: %w", name, err)
	}
	return conn, nil
}
