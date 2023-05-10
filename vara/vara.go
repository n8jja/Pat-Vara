package vara

import (
	"errors"
	"fmt"
	"io"
	"log"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/imdario/mergo"
	"github.com/la5nta/wl2k-go/transport"
)

const network = "vara"

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
	scheme        string
	myCall        string
	config        ModemConfig
	cmdConn       *net.TCPConn
	dataConn      *net.TCPConn
	busy          bool
	connectChange chan connectedState // TODO: Should support multiple "subscribers"
	inboundConns  chan *conn
	lastState     connectedState
	rig           transport.PTTController

	bufferCount *bufferCount
}

type connectedState int

const (
	connected connectedState = iota
	disconnected
	connecting
)

var (
	bandwidths = []string{"500", "2300", "2750"}
	debug      bool
)

func init() {
	debug = os.Getenv("VARA_DEBUG") != ""
}

func Bandwidths() []string {
	return bandwidths
}

// NewModem initializes configuration for a new VARA modem client stub.
func NewModem(scheme string, myCall string, config ModemConfig) (*Modem, error) {
	// Back-fill empty config values with defaults
	if err := mergo.Merge(&config, defaultConfig); err != nil {
		return nil, err
	}
	m := &Modem{
		scheme:        scheme,
		myCall:        myCall,
		config:        config,
		busy:          false,
		connectChange: make(chan connectedState, 1),
		inboundConns:  make(chan *conn),
		lastState:     disconnected,
		bufferCount:   newBufferCount(),
	}
	if err := m.start(); err != nil {
		return nil, err
	}
	return m, nil
}

// Start establishes TCP connections with the VARA modem program. This must be called before
// sending commands to the modem.
func (m *Modem) start() error {
	// Open command port TCP connection
	var err error
	m.cmdConn, err = m.connectTCP("command", m.config.CmdPort)
	if err != nil {
		return err
	}

	// Open the data port TCP connection
	m.dataConn, err = m.connectTCP("data", m.config.DataPort)
	if err != nil {
		return err
	}

	// Select public
	if err := m.writeCmd(fmt.Sprintf("PUBLIC ON")); err != nil {
		return err
	}
	// CWID enable
	if m.scheme == "varahf" {
		if err := m.writeCmd(fmt.Sprintf("CWID ON")); err != nil {
			return err
		}
	}
	// Set compression
	if err := m.writeCmd(fmt.Sprintf("COMPRESSION TEXT")); err != nil {
		return err
	}
	// Set MYCALL
	if err := m.writeCmd(fmt.Sprintf("MYCALL %s", m.myCall)); err != nil {
		return err
	}
	// Listen off
	if err := m.writeCmd(fmt.Sprintf("LISTEN OFF")); err != nil {
		return err
	}

	// Start listening for incoming VARA commands
	go m.cmdListen()
	return nil
}

// Idle returns true if the modem is not in a connecting or connected state.
func (m *Modem) Idle() bool {
	return m.lastState == disconnected
}

// Close closes the RF and then the TCP connections to the VARA modem. Blocks until finished.
func (m *Modem) Close() error {
	if m.cmdConn == nil {
		// Modem already closed.
		return nil
	}
	defer func() {
		if m.dataConn != nil {
			m.dataConn.Close()
			m.dataConn = nil
		}
		if m.cmdConn != nil {
			m.cmdConn.Close()
			m.cmdConn = nil
		}
		close(m.connectChange)
	}()

	// Block until VARA modem acks disconnect
	if m.lastState != disconnected {
		// Send DISCONNECT command
		if m.cmdConn != nil {
			if err := m.writeCmd("DISCONNECT"); err != nil {
				return err
			}
		}

		select {
		case res, ok := <-m.connectChange:
			if !ok {
				// Modem closed.
				return nil
			}
			if res != disconnected {
				log.Println("Disconnect failed, aborting!")
				if err := m.writeCmd("ABORT"); err != nil {
					return err
				}
			}
		case <-time.After(time.Second * 60):
			if m.cmdConn == nil {
				// Modem already closed.
				return nil
			}
			if err := m.writeCmd("ABORT"); err != nil {
				return err
			}
		}
	}

	// Make sure to stop TX (should have already happened, but this is a backup)
	if m.rig != nil {
		_ = m.rig.SetPTT(false)
	}

	// Clear up internal state
	m.busy = false
	return nil
}

func (m *Modem) connectTCP(name string, port int) (*net.TCPConn, error) {
	debugPrint(fmt.Sprintf("Connecting %s", name))
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

func disconnectTCP(name string, port *net.TCPConn) *net.TCPConn {
	if port == nil {
		return nil
	}
	_ = port.Close()
	debugPrint(fmt.Sprintf("disonnected %s", name))
	return nil
}

// wrapper around m.cmdConn.Write
func (m *Modem) writeCmd(cmd string) error {
	debugPrint(fmt.Sprintf("writing cmd: %v", cmd))
	_, err := m.cmdConn.Write([]byte(cmd + "\r"))
	return err
}

// goroutine listening for incoming commands
func (m *Modem) cmdListen() {
	buf := make([]byte, 1<<16)
	for {
		if m.cmdConn == nil {
			// probably disconnected
			return
		}
		l, err := m.cmdConn.Read(buf)
		if err != nil {
			debugPrint(fmt.Sprintf("cmdListen err: %v", err))
			if errors.Is(err, io.EOF) {
				// VARA program killed?
				return
			}
			continue
		}
		cmds := strings.Split(string(buf[:l]), "\r")
		for _, c := range cmds {
			if c == "" {
				continue
			}
			if !m.handleCmd(c) {
				return
			}
		}
	}
}

// handleCmd handles one command coming from the VARA modem. It returns true if listening should
// continue or false if listening should stop.
func (m *Modem) handleCmd(c string) bool {
	debugPrint(fmt.Sprintf("got cmd: %v", c))
	switch c {
	case "PTT ON":
		// VARA wants to start TX; send that to the PTTController
		m.sendPTT(true)
	case "PTT OFF":
		// VARA wants to stop TX; send that to the PTTController
		m.sendPTT(false)
	case "BUSY ON":
		m.busy = true
	case "BUSY OFF":
		m.busy = false
	case "OK":
		// nothing to do
	case "IAMALIVE":
		// nothing to do
	case "PENDING":
		// nothing to do
	case "CANCELPENDING":
		// nothing to do
	case "DISCONNECTED":
		m.handleDisconnect()
	default:
		if strings.HasPrefix(c, "CONNECTED ") {
			m.handleConnected(c)
			break
		}
		if strings.HasPrefix(c, "BUFFER") {
			parts := strings.Split(c, " ")
			if len(parts) != 2 {
				// nothing to do
				break
			}
			n, err := strconv.Atoi(parts[1])
			if err != nil {
				// not a valid int. nothing to do.
				break
			}
			m.bufferCount.set(n)
			break
		}
		if strings.HasPrefix(c, "REGISTERED") {
			parts := strings.Split(c, " ")
			if len(parts) > 1 {
				log.Printf("VARA full speed available, registered to %s", parts[1])
			}
			break
		}
		log.Printf("got a vara command I wasn't expecting: %v", c)
	}
	return true
}

func (m *Modem) sendPTT(on bool) {
	if m.rig != nil {
		_ = m.rig.SetPTT(on)
	}
}

func (m *Modem) handleConnected(cmd string) {
	parts := strings.Split(cmd, " ")
	if len(parts) < 3 {
		panic(fmt.Sprintf("unexpected CONNECTED command: %q", cmd))
	}
	switch src, dst := parts[1], parts[2]; {
	case src == m.myCall:
		m.lastState = connected
		m.connectChange <- connected
	case dst == m.myCall:
		m.lastState = connected
		select {
		case m.inboundConns <- &conn{Modem: m, remoteCall: src}:
		default:
			debugPrint(fmt.Sprintf("no one is calling Accept() at this time. dropping connection from %s", src))
			m.writeCmd("DISCONNECT")
		}
	default:
		panic(fmt.Sprintf("unhandled CONNECTED cmd: %q", cmd))
	}
}

func (m *Modem) handleDisconnect() {
	m.lastState = disconnected
	m.connectChange <- disconnected
}

func (m *Modem) Ping() bool {
	// TODO
	return true
}

func (m *Modem) Version() (string, error) {
	// TODO
	return "v1", nil
}

// If env var VARA_DEBUG exists, log more stuff
func debugPrint(msg string) {
	if debug {
		log.Printf("[VARA] %s", msg)
	}
}
