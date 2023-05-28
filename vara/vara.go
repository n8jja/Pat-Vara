package vara

import (
	"errors"
	"fmt"
	"log"
	"net"
	"strings"
	"sync"
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
	scheme         string
	myCall         string
	config         ModemConfig
	bandwidth      string
	cmdConn        *net.TCPConn
	dataConn       *net.TCPConn
	busy           bool
	cmds           pubSub
	inboundConns   chan *conn
	connectedState string
	rig            transport.PTTController

	bufferCount *bufferCount
	closeOnce   sync.Once
	closed      bool
}

const (
	connected    = "CONNECTED"
	disconnected = "DISCONNECTED"
	connecting   = "CONNECTING"
)

var bandwidths = []string{"500", "2300", "2750"}

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
		scheme:         scheme,
		myCall:         myCall,
		config:         config,
		busy:           false,
		cmds:           newPubSub(),
		inboundConns:   make(chan *conn),
		connectedState: disconnected,
		bufferCount:    newBufferCount(),
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

// SetBandwidth sets the default bandwidth for outbound and inbound connections.
func (m *Modem) SetBandwidth(bandwidth string) error {
	if err := m.setBandwidth(bandwidth); err != nil {
		return err
	}
	// Save this so we can revert on disconnect in case it's changed via connect uri parameter
	m.bandwidth = bandwidth
	return nil
}

// Idle returns true if the modem is not in a connecting or connected state.
func (m *Modem) Idle() bool {
	return m.connectedState == disconnected
}

// Close closes the RF and then the TCP connections to the VARA modem. Blocks until finished.
func (m *Modem) Close() error {
	m.closeOnce.Do(func() {
		m.closed = true
		defer func() {
			m.cmds.Close()
			close(m.inboundConns)
			m.dataConn.Close()
			m.cmdConn.Close()
		}()

		// Disconnect if connected
		connectChange, cancel := m.cmds.Subscribe(disconnected, connecting, connected)
		defer cancel()
		if m.connectedState != disconnected {
			// Send DISCONNECT command
			if err := m.writeCmd("DISCONNECT"); err != nil {
				// We have already lost connection with the modem, just publish that the state is disconnected and return.
				m.cmds.Publish(disconnected)
				m.handleDisconnected()
				return
			}
			select {
			case res := <-connectChange:
				if res != disconnected {
					log.Println("Disconnect failed, aborting!")
					m.Abort()
				}
			case <-time.After(time.Second * 60):
				m.Abort()
			}
		}

		// Make sure to stop TX (should have already happened, but this is a backup)
		m.sendPTT(false)
	})
	return nil
}

func (m *Modem) connectTCP(name string, port int) (*net.TCPConn, error) {
	debugPrint("Connecting %s", name)
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
	debugPrint("disonnected %s", name)
	return nil
}

// wrapper around m.cmdConn.Write
func (m *Modem) writeCmd(cmd string) error {
	debugPrint3("writing cmd: %v", cmd)
	m.cmdConn.SetWriteDeadline(time.Now().Add(time.Second * 5))
	_, err := m.cmdConn.Write([]byte(cmd + "\r"))
	if err != nil {
		m.closed = true
		debugPrint3("writeCmd err: %v", err)
	}
	return err
}

// goroutine listening for incoming commands
func (m *Modem) cmdListen() {
	defer m.Close()
	buf := make([]byte, 1<<16)
	for !m.closed {
		// VARA spec says it sends IAMALIVE every 60 seconds, so if we have not heard anything
		// for 2 minutes, assume we have lost connection and close the modem.
		m.cmdConn.SetReadDeadline(time.Now().Add(2 * time.Minute))
		l, err := m.cmdConn.Read(buf)
		if err != nil {
			if m.connectedState != disconnected {
				log.Println("VARA modem disconnected unexpectedly!")
			}
			debugPrint("cmdListen err: %v", err)
			m.cmdConn.Close() // Make sure any attempts to write to the connection fails hard.
			return
		}
		cmds := strings.Split(string(buf[:l]), "\r")
		for _, c := range cmds {
			if c == "" {
				continue
			}
			m.handleCmd(c)
			m.cmds.Publish(c)
		}
	}
}

// handleCmd handles one command coming from the VARA modem. It returns true if listening should
// continue or false if listening should stop.
func (m *Modem) handleCmd(c string) {
	debugPrint("got cmd: %v", c)
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
	case "LINK UNREGISTERED", "LINK REGISTERED":
		// nothing to do
	case "DISCONNECTED":
		m.handleDisconnected()
	default:
		if strings.HasPrefix(c, "BUFFER ") {
			m.bufferCount.set(parseBuffer(c))
			break
		}
		if strings.HasPrefix(c, "CONNECTED ") {
			m.connectedState = connected
			m.handleConnected(c)
			break
		}
		if strings.HasPrefix(c, "REGISTERED") {
			parts := strings.Split(c, " ")
			if len(parts) > 1 {
				log.Printf("VARA full speed available, registered to %s", parts[1])
			}
			break
		}
		if strings.HasPrefix(c, "VERSION") {
			break
		}
		log.Printf("got a vara command I wasn't expecting: %v", c)
	}
}

func (m *Modem) handleDisconnected() {
	m.connectedState = disconnected
	m.bufferCount.reset()       // reset buffer count in case we had outstanding frames
	m.setBandwidth(m.bandwidth) // reset bandwidth to default in case it was changed
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
		// Handled by DialURL through pubsub.
	case dst == m.myCall:
		select {
		case m.inboundConns <- m.newConn(src):
		default:
			debugPrint("no one is calling Accept() at this time. dropping connection from %s", src)
			m.writeCmd("DISCONNECT")
		}
	default:
		panic(fmt.Sprintf("unhandled CONNECTED cmd: %q", cmd))
	}
}

func (m *Modem) Ping() bool {
	return !m.closed
}

func (m *Modem) Version() (string, error) {
	// TODO
	return "v1", nil
}
