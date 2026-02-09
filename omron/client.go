// Package omron provides unified Omron PLC communication.
// Supports FINS/UDP, FINS/TCP, and EIP/CIP protocols for CS/CJ and NJ/NX series PLCs.
package omron

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	"warlink/cip"
	"warlink/eip"
	"warlink/logging"
)

// CIP service codes for Read/Write Tag operations.
const (
	svcReadTag  byte = 0x4C // Read Tag Service
	svcWriteTag byte = 0x4D // Write Tag Service
)

// finsTransport is the interface for FINS transports (UDP and TCP).
type finsTransport interface {
	connect(address string, port int, network, node, unit, srcNode byte) error
	close() error
	isConnected() bool
	setDisconnected()
	sendCommand(command uint16, data []byte) ([]byte, error)
	readWords(area byte, address uint16, count uint16) ([]uint16, error)
	writeWords(area byte, address uint16, words []uint16) error
	readBits(area byte, address uint16, bitOffset byte, count uint16) ([]bool, error)
	writeBits(area byte, address uint16, bitOffset byte, bits []bool) error
	readCPUStatus() (*CPUStatus, error)
	readCycleTime() (*CycleTime, error)
	connectionMode(address string, port int) string
	getSourceNode() byte
	setDebug(enabled bool)
}

// Client represents a connection to an Omron PLC.
type Client struct {
	mu        sync.Mutex
	transport Transport
	address   string
	port      int
	network   byte
	node      byte
	unit      byte
	srcNode   byte
	timeout   time.Duration
	debug     bool
	connected bool

	// FINS transport (for UDP/TCP)
	fins finsTransport

	// EIP transport (for NJ/NX)
	eipClient *eip.EipClient
	cipConn   *cip.Connection
	connSize  uint16 // Connection size for connected messaging
}

// Option is a functional option for configuring the client.
type Option func(*Client)

// WithTransport sets the communication transport.
func WithTransport(t Transport) Option {
	return func(c *Client) {
		c.transport = t
	}
}

// WithPort sets the port number.
func WithPort(port int) Option {
	return func(c *Client) {
		c.port = port
	}
}

// WithNetwork sets the FINS network number.
func WithNetwork(network byte) Option {
	return func(c *Client) {
		c.network = network
	}
}

// WithNode sets the FINS destination node number.
func WithNode(node byte) Option {
	return func(c *Client) {
		c.node = node
	}
}

// WithUnit sets the FINS unit number.
func WithUnit(unit byte) Option {
	return func(c *Client) {
		c.unit = unit
	}
}

// WithSourceNode sets the source node number.
func WithSourceNode(node byte) Option {
	return func(c *Client) {
		c.srcNode = node
	}
}

// WithTimeout sets the timeout duration.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// WithDebug enables debug logging.
func WithDebug(enabled bool) Option {
	return func(c *Client) {
		c.debug = enabled
	}
}

// Connect establishes a connection to an Omron PLC.
func Connect(address string, opts ...Option) (*Client, error) {
	c := &Client{
		address:   address,
		transport: TransportFINS, // Default to FINS (auto TCP/UDP)
		port:      defaultFINSPort,
		timeout:   5 * time.Second,
	}

	for _, opt := range opts {
		opt(c)
	}

	logging.DebugLog("Omron", "Connect to %s transport=%s port=%d network=%d node=%d unit=%d srcNode=%d timeout=%v",
		address, c.transport, c.port, c.network, c.node, c.unit, c.srcNode, c.timeout)

	switch c.transport {
	case TransportFINS:
		// Try TCP first (more reliable), fall back to UDP if TCP fails
		logging.DebugLog("Omron", "Auto transport mode: will try TCP first, then UDP")
		return c.connectFINSWithFallback()
	case TransportFINSUDP:
		return c.connectFINSUDP()
	case TransportFINSTCP:
		return c.connectFINSTCP()
	case TransportEIP:
		return c.connectEIP()
	default:
		logging.DebugLog("Omron", "Unsupported transport: %s", c.transport)
		return nil, fmt.Errorf("unsupported transport: %s", c.transport)
	}
}

// connectFINSWithFallback tries TCP first, then falls back to UDP if TCP fails.
func (c *Client) connectFINSWithFallback() (*Client, error) {
	// Try TCP first
	logging.DebugLog("Omron", "Attempting FINS/TCP connection to %s:%d", c.address, c.port)
	tcpErr := c.tryConnectFINSTCP()
	if tcpErr == nil {
		c.transport = TransportFINSTCP
		logging.DebugLog("Omron", "FINS/TCP connection successful")
		return c, nil
	}
	logging.DebugLog("Omron", "FINS/TCP failed: %v - falling back to UDP", tcpErr)

	// TCP failed, try UDP
	logging.DebugLog("Omron", "Attempting FINS/UDP connection to %s:%d", c.address, c.port)
	udpErr := c.tryConnectFINSUDP()
	if udpErr == nil {
		c.transport = TransportFINSUDP
		logging.DebugLog("Omron", "FINS/UDP connection successful")
		return c, nil
	}
	logging.DebugLog("Omron", "FINS/UDP also failed: %v", udpErr)

	// Both failed, return the TCP error (usually more informative)
	logging.DebugLog("Omron", "All FINS connection attempts failed")
	return nil, fmt.Errorf("FINS connection failed (TCP: %v, UDP: %v)", tcpErr, udpErr)
}

// tryConnectFINSTCP attempts a FINS/TCP connection without returning the client.
func (c *Client) tryConnectFINSTCP() error {
	t := newTCPTransport()
	t.timeout = c.timeout
	t.debug = c.debug

	if err := t.connect(c.address, c.port, c.network, c.node, c.unit, c.srcNode); err != nil {
		return err
	}

	c.fins = t
	c.srcNode = t.getSourceNode()
	c.connected = true
	return nil
}

// tryConnectFINSUDP attempts a FINS/UDP connection without returning the client.
func (c *Client) tryConnectFINSUDP() error {
	t := newUDPTransport()
	t.timeout = c.timeout
	t.debug = c.debug

	if err := t.connect(c.address, c.port, c.network, c.node, c.unit, c.srcNode); err != nil {
		return err
	}

	c.fins = t
	c.srcNode = t.getSourceNode()
	c.connected = true
	return nil
}

// connectFINSUDP establishes a FINS/UDP connection.
func (c *Client) connectFINSUDP() (*Client, error) {
	t := newUDPTransport()
	t.timeout = c.timeout
	t.debug = c.debug

	if err := t.connect(c.address, c.port, c.network, c.node, c.unit, c.srcNode); err != nil {
		return nil, err
	}

	c.fins = t
	c.srcNode = t.getSourceNode()
	c.connected = true
	return c, nil
}

// connectFINSTCP establishes a FINS/TCP connection.
func (c *Client) connectFINSTCP() (*Client, error) {
	t := newTCPTransport()
	t.timeout = c.timeout
	t.debug = c.debug

	if err := t.connect(c.address, c.port, c.network, c.node, c.unit, c.srcNode); err != nil {
		return nil, err
	}

	c.fins = t
	c.srcNode = t.getSourceNode()
	c.connected = true
	return c, nil
}

// connectEIP establishes an EIP/CIP connection.
func (c *Client) connectEIP() (*Client, error) {
	port := c.port
	if port == defaultFINSPort {
		port = 44818 // Standard EIP port
	}

	logging.DebugLog("Omron", "Attempting EIP/CIP connection to %s:%d", c.address, port)

	c.eipClient = eip.NewEipClientWithPort(c.address, uint16(port))
	if err := c.eipClient.SetTimeout(c.timeout); err != nil {
		logging.DebugLog("Omron", "EIP set timeout failed: %v", err)
		return nil, fmt.Errorf("failed to set timeout: %w", err)
	}

	if err := c.eipClient.Connect(); err != nil {
		logging.DebugLog("Omron", "EIP connect failed: %v", err)
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	c.connected = true
	logging.DebugLog("Omron", "EIP/CIP connection established to %s:%d", c.address, port)
	return c, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	logging.DebugLog("Omron", "Closing connection to %s (transport=%s)", c.address, c.transport)

	c.connected = false

	// Close CIP connected messaging first
	if c.cipConn != nil {
		c.closeConnection()
	}

	if c.fins != nil {
		err := c.fins.close()
		c.fins = nil
		if err != nil {
			logging.DebugLog("Omron", "FINS close error: %v", err)
		}
		return err
	}

	if c.eipClient != nil {
		err := c.eipClient.Disconnect()
		c.eipClient = nil
		if err != nil {
			logging.DebugLog("Omron", "EIP disconnect error: %v", err)
		}
		return err
	}

	return nil
}

// OpenConnection establishes a CIP connection using Forward Open.
// This enables more efficient connected messaging for EIP transport.
// Optional - if not called, unconnected messaging is used (still batched).
func (c *Client) OpenConnection() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.eipClient == nil {
		return fmt.Errorf("OpenConnection: EIP transport not active")
	}
	if c.cipConn != nil {
		return fmt.Errorf("OpenConnection: connection already open")
	}

	logging.DebugLog("Omron", "Opening CIP connection to %s", c.address)

	// Try large connection size first, then fall back to small
	sizes := []uint16{4002, 504}

	var lastErr error
	for _, size := range sizes {
		err := c.tryForwardOpen(size)
		if err == nil {
			logging.DebugLog("Omron", "CIP connection established with size %d", size)
			return nil
		}
		lastErr = err
		logging.DebugLog("Omron", "Forward Open with size %d failed: %v", size, err)
	}

	return fmt.Errorf("OpenConnection: all connection sizes failed: %w", lastErr)
}

// tryForwardOpen attempts Forward Open with the specified connection size.
// Forward Open establishes a CIP connection for connected messaging.
// Connection path: Port 1 (backplane), Slot 0, to Message Router (class 2, instance 1)
func (c *Client) tryForwardOpen(connectionSize uint16) error {
	logging.DebugLog("Omron", "tryForwardOpen: connectionSize=%d", connectionSize)

	// Build Forward Open config
	cfg := cip.DefaultForwardOpenConfig()
	// Connection path: Port 1 (0x01), Link 0 (0x00), Class 2 (0x20,0x02), Instance 1 (0x24,0x01)
	cfg.ConnectionPath = []byte{0x01, 0x00, 0x20, 0x02, 0x24, 0x01}
	cfg.OTConnectionSize = connectionSize
	cfg.TOConnectionSize = connectionSize

	logging.DebugLog("Omron", "ForwardOpen config: path=%X OT_Size=%d TO_Size=%d",
		cfg.ConnectionPath, cfg.OTConnectionSize, cfg.TOConnectionSize)

	// Use standard Forward Open (0x54) for sizes ≤511, Large (0x5B) for >511
	var reqData []byte
	var connSerial uint16
	var err error
	if connectionSize <= 511 {
		logging.DebugLog("Omron", "Using standard Forward Open (0x54)")
		reqData, connSerial, err = cip.BuildForwardOpenRequestSmall(cfg)
	} else {
		logging.DebugLog("Omron", "Using Large Forward Open (0x5B)")
		reqData, connSerial, err = cip.BuildForwardOpenRequest(cfg)
	}
	if err != nil {
		logging.DebugLog("Omron", "Failed to build Forward Open request: %v", err)
		return fmt.Errorf("build Forward Open: %w", err)
	}

	logging.DebugLog("Omron", "ForwardOpen request: connSerial=%d reqLen=%d", connSerial, len(reqData))
	logging.DebugTX("eip", reqData)

	// Send via unconnected messaging
	cpf := &eip.EipCommonPacket{
		Items: []eip.EipCommonPacketItem{
			{TypeId: eip.CpfAddressNullId, Length: 0, Data: nil},
			{TypeId: eip.CpfUnconnectedMessageId, Length: uint16(len(reqData)), Data: reqData},
		},
	}

	resp, err := c.eipClient.SendRRData(*cpf)
	if err != nil {
		logging.DebugLog("Omron", "ForwardOpen SendRRData failed: %v", err)
		return fmt.Errorf("SendRRData failed: %w", err)
	}

	if len(resp.Items) < 2 {
		logging.DebugLog("Omron", "ForwardOpen response missing CPF items: got %d", len(resp.Items))
		return fmt.Errorf("expected 2 CPF items, got %d", len(resp.Items))
	}

	cipResp := resp.Items[1].Data
	logging.DebugRX("eip", cipResp)

	if len(cipResp) < 4 {
		logging.DebugLog("Omron", "ForwardOpen CIP response too short: %d bytes", len(cipResp))
		return fmt.Errorf("response too short: %d bytes", len(cipResp))
	}

	// Check CIP response status
	replyService := cipResp[0]
	status := cipResp[2]
	addlStatusSize := cipResp[3]

	logging.DebugLog("Omron", "ForwardOpen response: service=0x%02X status=0x%02X addlStatusSize=%d",
		replyService, status, addlStatusSize)

	if status != 0x00 {
		extStatus := uint16(0)
		if addlStatusSize >= 1 && len(cipResp) >= 6 {
			extStatus = binary.LittleEndian.Uint16(cipResp[4:6])
		}
		statusMsg := forwardOpenErrorMessage(status, extStatus)
		logging.DebugLog("Omron", "ForwardOpen failed: status=0x%02X extStatus=0x%04X (%s)",
			status, extStatus, statusMsg)
		return fmt.Errorf("Forward Open failed: status=0x%02X, extStatus=0x%04X (%s)", status, extStatus, statusMsg)
	}

	// Parse Forward Open response
	dataStart := 4 + int(addlStatusSize)*2
	if dataStart >= len(cipResp) {
		logging.DebugLog("Omron", "ForwardOpen response missing connection data")
		return fmt.Errorf("response missing data")
	}

	foResp, err := cip.ParseForwardOpenResponse(cipResp[dataStart:])
	if err != nil {
		logging.DebugLog("Omron", "Failed to parse ForwardOpen response: %v", err)
		return err
	}

	// Store the connection
	c.cipConn = &cip.Connection{
		OTConnID:     foResp.OTConnectionID,
		TOConnID:     foResp.TOConnectionID,
		SerialNumber: connSerial,
		VendorID:     cfg.VendorID,
		OrigSerial:   cfg.OriginatorSerial,
	}
	c.connSize = connectionSize

	logging.DebugLog("Omron", "ForwardOpen success: OT_ConnID=0x%08X TO_ConnID=0x%08X",
		foResp.OTConnectionID, foResp.TOConnectionID)

	return nil
}

// forwardOpenErrorMessage returns a human-readable message for Forward Open errors.
func forwardOpenErrorMessage(status byte, extStatus uint16) string {
	// Common Forward Open extended status codes
	switch extStatus {
	case 0x0100:
		return "connection in use or duplicate Forward Open"
	case 0x0103:
		return "transport class and trigger combination not supported"
	case 0x0106:
		return "ownership conflict"
	case 0x0107:
		return "target connection not found"
	case 0x0108:
		return "invalid network connection parameter"
	case 0x0109:
		return "invalid connection size"
	case 0x0110:
		return "target not configured for RPI"
	case 0x0111:
		return "RPI not supported"
	case 0x0113:
		return "out of connections"
	case 0x0114:
		return "vendor ID or product code mismatch"
	case 0x0115:
		return "product type mismatch"
	case 0x0116:
		return "revision mismatch"
	case 0x0117:
		return "invalid produced or consumed application path"
	case 0x0118:
		return "invalid or inconsistent configuration application path"
	case 0x0119:
		return "non-listen only connection not opened"
	case 0x011A:
		return "target object out of connections"
	case 0x0203:
		return "connection timed out"
	case 0x0204:
		return "unconnected send timed out"
	case 0x0205:
		return "parameter error in unconnected send"
	case 0x0206:
		return "message too large for unconnected send"
	case 0x0301:
		return "no buffer memory available"
	case 0x0302:
		return "network bandwidth not available"
	case 0x0303:
		return "no screeners available"
	case 0x0305:
		return "signature mismatch"
	case 0x0311:
		return "port not available"
	case 0x0312:
		return "link address not valid"
	case 0x0315:
		return "invalid segment in connection path"
	case 0x0316:
		return "error in Forward Close service"
	case 0x0317:
		return "scheduling not specified"
	case 0x0318:
		return "link address to self not valid"
	case 0x0319:
		return "secondary resources not available"
	case 0x031A:
		return "rack connection already established"
	case 0x031B:
		return "module connection already established"
	case 0x031C:
		return "miscellaneous"
	case 0x031D:
		return "redundant connection mismatch"
	case 0x031E:
		return "no more user-configurable link consumer resources"
	case 0x0800:
		return "network link offline"
	case 0x0810:
		return "no target application data available"
	case 0x0811:
		return "no originator application data available"
	case 0x0812:
		return "node address changed since connection established"
	default:
		return "unknown error"
	}
}

// CloseConnection tears down the CIP connection using Forward Close.
func (c *Client) CloseConnection() error {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.closeConnection()
}

// closeConnection internal method (must hold lock).
func (c *Client) closeConnection() error {
	if c.cipConn == nil {
		return nil
	}

	logging.DebugLog("Omron", "Closing CIP connection")

	// Build Forward Close request
	connPath := []byte{0x01, 0x00, 0x20, 0x02, 0x24, 0x01}
	reqData, err := cip.BuildForwardCloseRequest(c.cipConn, connPath)
	if err != nil {
		c.cipConn = nil
		return err
	}

	// Send (best-effort, ignore errors)
	cpf := &eip.EipCommonPacket{
		Items: []eip.EipCommonPacketItem{
			{TypeId: eip.CpfAddressNullId, Length: 0, Data: nil},
			{TypeId: eip.CpfUnconnectedMessageId, Length: uint16(len(reqData)), Data: reqData},
		},
	}
	_, _ = c.eipClient.SendRRData(*cpf)

	c.cipConn = nil
	c.connSize = 0
	return nil
}

// Keepalive sends a NOP to keep the CIP connection alive.
// Call periodically when using connected messaging.
func (c *Client) Keepalive() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.cipConn == nil {
		return nil // Not using connected messaging
	}

	// Send Identity object NOP
	reqData := []byte{
		0x17,       // Service code (NOP)
		0x02,       // Path size (2 words)
		0x20, 0x01, // Class segment: class 1 (Identity)
		0x24, 0x01, // Instance segment: instance 1
	}

	connData := c.cipConn.WrapConnected(reqData)
	cpf := c.buildConnectedCpf(connData)

	resp, err := c.eipClient.SendUnitDataTransaction(*cpf)
	if err != nil {
		return fmt.Errorf("Keepalive: %w", err)
	}

	if len(resp.Items) < 2 {
		return fmt.Errorf("Keepalive: expected 2 CPF items")
	}

	return nil
}

// IsConnected returns true if connected.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.fins != nil {
		return c.fins.isConnected()
	}
	if c.eipClient != nil {
		return c.connected && c.eipClient.IsConnected()
	}
	return false
}

// SetDisconnected marks the client as disconnected.
func (c *Client) SetDisconnected() {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	if c.fins != nil {
		c.fins.setDisconnected()
	}
}

// Reconnect attempts to re-establish the connection.
func (c *Client) Reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	logging.DebugLog("Omron", "Reconnecting to %s (transport=%s)", c.address, c.transport)

	// Close existing
	if c.fins != nil {
		c.fins.close()
		c.fins = nil
	}
	if c.eipClient != nil {
		c.eipClient.Disconnect()
		c.eipClient = nil
	}
	c.connected = false

	// Reconnect based on transport
	switch c.transport {
	case TransportFINS:
		// Try TCP first, then UDP
		logging.DebugLog("Omron", "Reconnect: trying FINS/TCP first")
		if err := c.tryConnectFINSTCP(); err == nil {
			c.transport = TransportFINSTCP
			logging.DebugLog("Omron", "Reconnect: FINS/TCP successful")
			return nil
		}
		logging.DebugLog("Omron", "Reconnect: FINS/TCP failed, trying UDP")
		if err := c.tryConnectFINSUDP(); err != nil {
			logging.DebugLog("Omron", "Reconnect: FINS/UDP also failed: %v", err)
			return err
		}
		c.transport = TransportFINSUDP
		logging.DebugLog("Omron", "Reconnect: FINS/UDP successful")

	case TransportFINSUDP:
		t := newUDPTransport()
		t.timeout = c.timeout
		t.debug = c.debug
		if err := t.connect(c.address, c.port, c.network, c.node, c.unit, c.srcNode); err != nil {
			logging.DebugLog("Omron", "Reconnect FINS/UDP failed: %v", err)
			return err
		}
		c.fins = t
		c.connected = true
		logging.DebugLog("Omron", "Reconnect FINS/UDP successful")

	case TransportFINSTCP:
		t := newTCPTransport()
		t.timeout = c.timeout
		t.debug = c.debug
		if err := t.connect(c.address, c.port, c.network, c.node, c.unit, c.srcNode); err != nil {
			logging.DebugLog("Omron", "Reconnect FINS/TCP failed: %v", err)
			return err
		}
		c.fins = t
		c.connected = true
		logging.DebugLog("Omron", "Reconnect FINS/TCP successful")

	case TransportEIP:
		port := c.port
		if port == defaultFINSPort {
			port = 44818
		}
		c.eipClient = eip.NewEipClientWithPort(c.address, uint16(port))
		c.eipClient.SetTimeout(c.timeout)
		if err := c.eipClient.Connect(); err != nil {
			logging.DebugLog("Omron", "Reconnect EIP failed: %v", err)
			return err
		}
		c.connected = true
		logging.DebugLog("Omron", "Reconnect EIP successful")
	}

	return nil
}

// ConnectionMode returns a human-readable description of the connection.
func (c *Client) ConnectionMode() string {
	if c == nil {
		return "Not connected"
	}

	switch c.transport {
	case TransportFINSUDP, TransportFINSTCP:
		if c.fins != nil {
			return c.fins.connectionMode(c.address, c.port)
		}
	case TransportEIP:
		mode := "Unconnected"
		if c.cipConn != nil {
			mode = "Connected"
		}
		return fmt.Sprintf("EIP/CIP %s %s:%d", mode, c.address, c.port)
	}

	return fmt.Sprintf("%s %s:%d", c.transport, c.address, c.port)
}

// SetDebug enables or disables debug logging.
func (c *Client) SetDebug(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.debug = enabled
	if c.fins != nil {
		c.fins.setDebug(enabled)
	}
}

// GetSourceNode returns the source node number.
func (c *Client) GetSourceNode() byte {
	if c.fins != nil {
		return c.fins.getSourceNode()
	}
	return c.srcNode
}

// GetDeviceInfo returns device information.
func (c *Client) GetDeviceInfo() (*DeviceInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected")
	}

	// For EIP, use identity object
	if c.eipClient != nil {
		return c.getEIPDeviceInfo()
	}

	// For FINS, return basic info (no standard device info command)
	return &DeviceInfo{
		Model:   "Omron PLC",
		Version: string(c.transport),
		CPUType: fmt.Sprintf("Node %d", c.node),
	}, nil
}

// getEIPDeviceInfo retrieves device info via CIP Identity Object.
func (c *Client) getEIPDeviceInfo() (*DeviceInfo, error) {
	path, _ := cip.EPath().Class(0x01).Instance(0x01).Build()
	req := cip.Request{
		Service: cip.SvcGetAttributesAll,
		Path:    path,
	}

	respData, err := c.sendCIPRequest(req)
	if err != nil {
		return nil, err
	}

	if len(respData) < 20 {
		return &DeviceInfo{
			Model: "Omron NJ/NX PLC",
		}, nil
	}

	info := &DeviceInfo{
		VendorID:     binary.LittleEndian.Uint16(respData[0:2]),
		ProductCode:  binary.LittleEndian.Uint16(respData[4:6]),
		SerialNumber: binary.LittleEndian.Uint32(respData[8:12]),
	}

	if len(respData) >= 8 {
		info.Version = fmt.Sprintf("%d.%d", respData[6], respData[7])
	}

	if len(respData) > 12 {
		nameOffset := 12
		if nameOffset < len(respData) {
			nameLen := int(respData[nameOffset])
			if nameOffset+1+nameLen <= len(respData) {
				info.Model = string(respData[nameOffset+1 : nameOffset+1+nameLen])
			}
		}
	}

	if info.Model == "" {
		info.Model = "Omron NJ/NX PLC"
	}

	return info, nil
}

// Read reads multiple tags from the PLC.
// Uses optimized batching for high throughput.
func (c *Client) Read(addresses ...string) ([]*TagValue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected")
	}

	// EIP uses symbolic addressing with MSP batching
	if c.eipClient != nil {
		return c.readEIPBatched(addresses)
	}

	// FINS uses memory addressing with contiguous grouping
	return c.readFINSBatched(addresses)
}

// readFINS reads FINS addresses.
func (c *Client) readFINS(addresses []string) ([]*TagValue, error) {
	results := make([]*TagValue, len(addresses))
	logging.DebugLog("Omron", "Read %d FINS addresses", len(addresses))

	for i, addr := range addresses {
		tv := &TagValue{
			Name:      addr,
			Count:     1,
			bigEndian: true, // FINS is big-endian
		}

		parsed, err := ParseAddress(addr)
		if err != nil {
			logging.DebugLog("Omron", "Address parse error for %q: %v", addr, err)
			tv.Error = err
			results[i] = tv
			continue
		}

		tv.DataType = parsed.TypeCode
		tv.Count = parsed.Count

		areaName := AreaName(parsed.MemoryArea)
		logging.DebugLog("Omron", "Read %q: area=%s(0x%02X) addr=%d bit=%d type=%s count=%d",
			addr, areaName, parsed.MemoryArea, parsed.Address, parsed.BitOffset,
			TypeName(parsed.TypeCode), parsed.Count)

		if parsed.TypeCode == TypeBool {
			bitArea := BitAreaFromWordArea(parsed.MemoryArea)
			logging.DebugLog("Omron", "Reading %d bits from area 0x%02X address %d.%d",
				parsed.Count, bitArea, parsed.Address, parsed.BitOffset)
			bits, err := c.fins.readBits(bitArea, parsed.Address, parsed.BitOffset, uint16(parsed.Count))
			if err != nil {
				logging.DebugLog("Omron", "Bit read error for %q: %v", addr, err)
				tv.Error = err
				results[i] = tv
				continue
			}
			data := make([]byte, len(bits)*2)
			for j, b := range bits {
				if b {
					data[j*2+1] = 1
				}
			}
			tv.Bytes = data
			logging.DebugLog("Omron", "Read %q: got %d bits", addr, len(bits))
		} else {
			wordCount := (TypeSize(parsed.TypeCode) * parsed.Count) / 2
			if wordCount < 1 {
				wordCount = 1
			}
			logging.DebugLog("Omron", "Reading %d words from area 0x%02X address %d",
				wordCount, parsed.MemoryArea, parsed.Address)
			words, err := c.fins.readWords(parsed.MemoryArea, parsed.Address, uint16(wordCount))
			if err != nil {
				logging.DebugLog("Omron", "Word read error for %q: %v", addr, err)
				tv.Error = err
				results[i] = tv
				continue
			}
			data := make([]byte, len(words)*2)
			for j, w := range words {
				binary.BigEndian.PutUint16(data[j*2:j*2+2], w)
			}
			tv.Bytes = data
			logging.DebugLog("Omron", "Read %q: got %d words (%d bytes)", addr, len(words), len(data))
		}

		if parsed.Count > 1 {
			tv.DataType = MakeArrayType(parsed.TypeCode)
		}
		results[i] = tv
	}

	return results, nil
}

// readEIP reads symbolic tags via CIP.
func (c *Client) readEIP(tagNames []string) ([]*TagValue, error) {
	results := make([]*TagValue, len(tagNames))
	logging.DebugLog("Omron", "Read %d EIP/CIP tags", len(tagNames))

	for i, tagName := range tagNames {
		tv := &TagValue{
			Name:      tagName,
			Count:     1,
			bigEndian: false, // CIP is little-endian
		}

		path, err := cip.EPath().Symbol(tagName).Build()
		if err != nil {
			logging.DebugLog("Omron", "EIP tag path error for %q: %v", tagName, err)
			tv.Error = fmt.Errorf("invalid tag path: %w", err)
			results[i] = tv
			continue
		}

		logging.DebugLog("Omron", "Reading EIP tag %q", tagName)

		reqData := binary.LittleEndian.AppendUint16(nil, 1) // Element count
		req := cip.Request{
			Service: svcReadTag,
			Path:    path,
			Data:    reqData,
		}

		respData, err := c.sendCIPRequest(req)
		if err != nil {
			logging.DebugLog("Omron", "EIP read error for %q: %v", tagName, err)
			tv.Error = err
			results[i] = tv
			continue
		}

		// Parse response - first 2 bytes are data type
		if len(respData) < 2 {
			logging.DebugLog("Omron", "EIP response too short for %q: %d bytes", tagName, len(respData))
			tv.Error = fmt.Errorf("response too short")
			results[i] = tv
			continue
		}

		tv.DataType = binary.LittleEndian.Uint16(respData[0:2])
		if len(respData) > 2 {
			tv.Bytes = respData[2:]
		}
		logging.DebugLog("Omron", "EIP read %q: type=0x%04X (%s) %d bytes",
			tagName, tv.DataType, TypeName(tv.DataType), len(tv.Bytes))
		results[i] = tv
	}

	return results, nil
}

// sendCIPRequest sends a CIP request via EIP.
// Uses unconnected messaging (SendRRData) for individual requests.
func (c *Client) sendCIPRequest(req cip.Request) ([]byte, error) {
	reqData := req.Marshal()

	logging.DebugLog("Omron", "sendCIPRequest: service=0x%02X path=%X dataLen=%d",
		req.Service, req.Path, len(req.Data))
	logging.DebugTX("eip", reqData)

	cpf := eip.EipCommonPacket{
		Items: []eip.EipCommonPacketItem{
			{TypeId: eip.CpfAddressNullId, Length: 0, Data: nil},
			{TypeId: eip.CpfUnconnectedMessageId, Length: uint16(len(reqData)), Data: reqData},
		},
	}

	resp, err := c.eipClient.SendRRData(cpf)
	if err != nil {
		logging.DebugLog("Omron", "sendCIPRequest SendRRData error: %v", err)
		return nil, err
	}

	if len(resp.Items) < 2 {
		logging.DebugLog("Omron", "sendCIPRequest: invalid response, expected 2 CPF items, got %d", len(resp.Items))
		return nil, fmt.Errorf("invalid CIP response: expected 2 CPF items, got %d", len(resp.Items))
	}

	respData := resp.Items[1].Data
	logging.DebugRX("eip", respData)

	// Log CIP response header if available
	if len(respData) >= 4 {
		replyService := respData[0]
		status := respData[2]
		logging.DebugLog("Omron", "sendCIPRequest response: service=0x%02X status=0x%02X dataLen=%d",
			replyService, status, len(respData))
	}

	return respData, nil
}

// Write writes a value to an address.
func (c *Client) Write(address string, value interface{}) error {
	return c.WriteWithType(address, value, "")
}

// WriteWithType writes a value to an address with an explicit type hint.
// The typeHint overrides the default type inferred from the address.
func (c *Client) WriteWithType(address string, value interface{}, typeHint string) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return fmt.Errorf("not connected")
	}

	// EIP uses symbolic addressing
	if c.eipClient != nil {
		return c.writeEIP(address, value)
	}

	// FINS uses memory addressing
	return c.writeFINS(address, value, typeHint)
}

// writeFINS writes to a FINS address.
func (c *Client) writeFINS(address string, value interface{}, typeHint string) error {
	parsed, err := ParseAddressWithType(address, typeHint)
	if err != nil {
		logging.DebugLog("Omron", "Write address parse error for %q: %v", address, err)
		return err
	}

	areaName := AreaName(parsed.MemoryArea)
	logging.DebugLog("Omron", "Write %q: area=%s(0x%02X) addr=%d type=%s value=%v",
		address, areaName, parsed.MemoryArea, parsed.Address, TypeName(parsed.TypeCode), value)

	data, err := EncodeValue(value, parsed.TypeCode, true)
	if err != nil {
		logging.DebugLog("Omron", "Write encode error for %q: %v", address, err)
		return err
	}

	if parsed.TypeCode == TypeBool {
		var bitVal bool
		switch v := value.(type) {
		case bool:
			bitVal = v
		case int:
			bitVal = v != 0
		case int32:
			bitVal = v != 0
		case int64:
			bitVal = v != 0
		default:
			logging.DebugLog("Omron", "Write type conversion error: cannot convert %T to BOOL", value)
			return fmt.Errorf("cannot convert %T to BOOL", value)
		}
		bitArea := BitAreaFromWordArea(parsed.MemoryArea)
		logging.DebugLog("Omron", "Writing bit to area 0x%02X address %d.%d value=%v",
			bitArea, parsed.Address, parsed.BitOffset, bitVal)
		err := c.fins.writeBits(bitArea, parsed.Address, parsed.BitOffset, []bool{bitVal})
		if err != nil {
			logging.DebugLog("Omron", "Write bit error for %q: %v", address, err)
		}
		return err
	}

	words := make([]uint16, (len(data)+1)/2)
	for i := 0; i < len(words); i++ {
		idx := i * 2
		if idx+1 < len(data) {
			words[i] = binary.BigEndian.Uint16(data[idx : idx+2])
		} else if idx < len(data) {
			words[i] = uint16(data[idx]) << 8
		}
	}
	logging.DebugLog("Omron", "Writing %d words to area 0x%02X address %d",
		len(words), parsed.MemoryArea, parsed.Address)
	err = c.fins.writeWords(parsed.MemoryArea, parsed.Address, words)
	if err != nil {
		logging.DebugLog("Omron", "Write words error for %q: %v", address, err)
	}
	return err
}

// writeEIP writes to a CIP symbolic tag.
func (c *Client) writeEIP(tagName string, value interface{}) error {
	// First read to get the data type
	path, err := cip.EPath().Symbol(tagName).Build()
	if err != nil {
		return fmt.Errorf("invalid tag path: %w", err)
	}

	// Read tag to determine type
	readReq := cip.Request{
		Service: svcReadTag,
		Path:    path,
		Data:    binary.LittleEndian.AppendUint16(nil, 1),
	}

	respData, err := c.sendCIPRequest(readReq)
	if err != nil {
		return fmt.Errorf("failed to read tag type: %w", err)
	}

	if len(respData) < 2 {
		return fmt.Errorf("response too short")
	}

	dataType := binary.LittleEndian.Uint16(respData[0:2])

	// Encode value
	encodedData, err := EncodeValue(value, dataType, false)
	if err != nil {
		return fmt.Errorf("failed to encode value: %w", err)
	}

	// Build write request
	writeData := make([]byte, 4+len(encodedData))
	binary.LittleEndian.PutUint16(writeData[0:2], dataType)
	binary.LittleEndian.PutUint16(writeData[2:4], 1)
	copy(writeData[4:], encodedData)

	writeReq := cip.Request{
		Service: svcWriteTag,
		Path:    path,
		Data:    writeData,
	}

	_, err = c.sendCIPRequest(writeReq)
	return err
}

// ReadWithTypes reads multiple tags using type hints.
// Uses optimized batching for high throughput.
func (c *Client) ReadWithTypes(requests []TagRequest) ([]*TagValue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected")
	}

	// EIP doesn't need type hints (types are embedded) - use batched read
	if c.eipClient != nil {
		addrs := make([]string, len(requests))
		for i, req := range requests {
			addrs[i] = req.Address
		}
		return c.readEIPBatched(addrs)
	}

	// FINS with type hints - use batched read with type application
	return c.readFINSWithTypesBatched(requests)
}

// readFINSWithTypesBatched reads FINS addresses with type hints using batching.
func (c *Client) readFINSWithTypesBatched(requests []TagRequest) ([]*TagValue, error) {
	// Use batched read with type hints applied during parsing
	return c.readFINSBatchedWithTypes(requests)
}

// AllTags discovers all tags (EIP only).
// Uses efficient CIP pagination with Get Instance Attribute List (0x55).
func (c *Client) AllTags() ([]TagInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.eipClient == nil {
		return nil, fmt.Errorf("tag discovery only supported for EIP transport")
	}

	// Try efficient pagination first
	tags, err := c.allTagsEIP()
	if err == nil && len(tags) > 0 {
		return tags, nil
	}

	// Fall back to legacy instance-by-instance discovery
	// Some older PLCs may not support Get Instance Attribute List
	return c.allTagsEIPFallback()
}

// parseSymbolInstance parses a Symbol Object instance response.
func (c *Client) parseSymbolInstance(data []byte, instance uint32) TagInfo {
	tag := TagInfo{Instance: instance}

	if len(data) < 8 {
		return tag
	}

	// Skip CIP response header
	offset := 0
	if data[0]&0x80 != 0 {
		extStatusSize := int(data[3]) * 2
		offset = 4 + extStatusSize
	}

	if offset >= len(data) {
		return tag
	}

	remaining := data[offset:]

	// Name (length + chars)
	if len(remaining) > 2 {
		nameLen := int(remaining[0])
		if nameLen > 0 && nameLen+1 <= len(remaining) {
			tag.Name = string(remaining[1 : 1+nameLen])
			remaining = remaining[1+nameLen:]
		}
	}

	// Type code
	if len(remaining) >= 2 {
		tag.TypeCode = binary.LittleEndian.Uint16(remaining[0:2])
		remaining = remaining[2:]
	}

	// Dimensions
	if len(remaining) >= 4 {
		dimCount := binary.LittleEndian.Uint32(remaining[0:4])
		remaining = remaining[4:]
		for i := uint32(0); i < dimCount && len(remaining) >= 4; i++ {
			dim := binary.LittleEndian.Uint32(remaining[0:4])
			tag.Dimensions = append(tag.Dimensions, dim)
			remaining = remaining[4:]
		}
	}

	return tag
}

// ReadCPUStatus reads the CPU status (FINS only).
func (c *Client) ReadCPUStatus() (*CPUStatus, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.fins == nil {
		return nil, fmt.Errorf("CPU status only supported for FINS transport")
	}

	return c.fins.readCPUStatus()
}

// ReadCycleTime reads the CPU cycle time (FINS only).
func (c *Client) ReadCycleTime() (*CycleTime, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.fins == nil {
		return nil, fmt.Errorf("cycle time only supported for FINS transport")
	}

	return c.fins.readCycleTime()
}

// isConnectionError checks if an error indicates a connection problem.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "refused") ||
		strings.Contains(errStr, "unreachable") ||
		strings.Contains(errStr, "closed") ||
		strings.Contains(errStr, "reset")
}
