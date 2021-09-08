package vara

import (
	"errors"
	"fmt"
	"log"
	"net"
	"os"
	"strings"

	"github.com/imdario/mergo"
	"github.com/la5nta/wl2k-go/transport"
)

var errNotImplemented = errors.New("not implemented")

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
	myCall    string
	config    ModemConfig
	cmdConn   *net.TCPConn
	dataConn  *net.TCPConn
	toCall    string
	busy      bool
	connected chan rune
	rig       transport.PTTController
}

var debug bool

func init() {
	debug = os.Getenv("VARA_DEBUG") != ""
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
func (m *Modem) start() error {
	var err error
	m.cmdConn, err = m.reconnect("command", m.config.CmdPort)
	if err != nil {
		return err
	}
	m.connected = make(chan rune, 1)
	go m.cmdListen()

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

func (m *Modem) writeCmd(cmd string) error {
	debugPrint(fmt.Sprintf("writing cmd: %v", cmd))
	_, err := m.cmdConn.Write([]byte(cmd + "\r"))
	return err
}

func (m *Modem) cmdListen() {
	var buf = make([]byte, 1<<16)
	for {
		if m.cmdConn == nil {
			// probably disconnected
			return
		}
		l, err := m.cmdConn.Read(buf)
		if err != nil {
			debugPrint(fmt.Sprintf("cmdListen err: %v", err))
			continue
		}
		cmds := strings.Split(string(buf[:l]), "\r")
		for _, c := range cmds {
			if c == "" {
				continue
			}
			debugPrint(fmt.Sprintf("got cmd: %v", c))
			switch c {
			case "PTT ON":
				if m.rig != nil {
					_ = m.rig.SetPTT(true)
				}
			case "PTT OFF":
				if m.rig != nil {
					_ = m.rig.SetPTT(false)
				}
			case "BUSY ON":
				m.busy = true
			case "BUSY OFF":
				m.busy = false
			case "OK":
				// nothing to do
			case "IAMALIVE":
				// nothing to do
			case "DISCONNECTED":
				m.connected <- 'd'
				return
			default:
				if strings.HasPrefix(c, "CONNECTED") {
					m.connected <- 'c'
					break
				}
				if strings.HasPrefix(c, "BUFFER") {
					// nothing to do
					break
				}
				log.Printf("got a vara command I wasn't expecting: %v", c)
			}
		}
	}
}

func debugPrint(msg string) {
	if debug {
		log.Print(msg)
	}
}
