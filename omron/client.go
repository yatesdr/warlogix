// Package omron provides unified Omron PLC communication.
// Supports FINS/UDP, FINS/TCP, and EIP/CIP protocols for CS/CJ and NJ/NX series PLCs.
package omron

import (
	"encoding/binary"
	"fmt"
	"strings"
	"sync"
	"time"

	"warlogix/cip"
	"warlogix/eip"
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

	switch c.transport {
	case TransportFINS:
		// Try TCP first (more reliable), fall back to UDP if TCP fails
		return c.connectFINSWithFallback()
	case TransportFINSUDP:
		return c.connectFINSUDP()
	case TransportFINSTCP:
		return c.connectFINSTCP()
	case TransportEIP:
		return c.connectEIP()
	default:
		return nil, fmt.Errorf("unsupported transport: %s", c.transport)
	}
}

// connectFINSWithFallback tries TCP first, then falls back to UDP if TCP fails.
func (c *Client) connectFINSWithFallback() (*Client, error) {
	// Try TCP first
	tcpErr := c.tryConnectFINSTCP()
	if tcpErr == nil {
		c.transport = TransportFINSTCP
		return c, nil
	}

	// TCP failed, try UDP
	udpErr := c.tryConnectFINSUDP()
	if udpErr == nil {
		c.transport = TransportFINSUDP
		return c, nil
	}

	// Both failed, return the TCP error (usually more informative)
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

	c.eipClient = eip.NewEipClientWithPort(c.address, uint16(port))
	if err := c.eipClient.SetTimeout(c.timeout); err != nil {
		return nil, fmt.Errorf("failed to set timeout: %w", err)
	}

	if err := c.eipClient.Connect(); err != nil {
		return nil, fmt.Errorf("failed to connect: %w", err)
	}

	c.connected = true
	return c, nil
}

// Close closes the connection.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.connected = false

	if c.fins != nil {
		err := c.fins.close()
		c.fins = nil
		return err
	}

	if c.eipClient != nil {
		err := c.eipClient.Disconnect()
		c.eipClient = nil
		return err
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
		if err := c.tryConnectFINSTCP(); err == nil {
			c.transport = TransportFINSTCP
			return nil
		}
		if err := c.tryConnectFINSUDP(); err != nil {
			return err
		}
		c.transport = TransportFINSUDP

	case TransportFINSUDP:
		t := newUDPTransport()
		t.timeout = c.timeout
		t.debug = c.debug
		if err := t.connect(c.address, c.port, c.network, c.node, c.unit, c.srcNode); err != nil {
			return err
		}
		c.fins = t
		c.connected = true

	case TransportFINSTCP:
		t := newTCPTransport()
		t.timeout = c.timeout
		t.debug = c.debug
		if err := t.connect(c.address, c.port, c.network, c.node, c.unit, c.srcNode); err != nil {
			return err
		}
		c.fins = t
		c.connected = true

	case TransportEIP:
		port := c.port
		if port == defaultFINSPort {
			port = 44818
		}
		c.eipClient = eip.NewEipClientWithPort(c.address, uint16(port))
		c.eipClient.SetTimeout(c.timeout)
		if err := c.eipClient.Connect(); err != nil {
			return err
		}
		c.connected = true
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
func (c *Client) Read(addresses ...string) ([]*TagValue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected")
	}

	// EIP uses symbolic addressing
	if c.eipClient != nil {
		return c.readEIP(addresses)
	}

	// FINS uses memory addressing
	return c.readFINS(addresses)
}

// readFINS reads FINS addresses.
func (c *Client) readFINS(addresses []string) ([]*TagValue, error) {
	results := make([]*TagValue, len(addresses))

	for i, addr := range addresses {
		tv := &TagValue{
			Name:      addr,
			Count:     1,
			bigEndian: true, // FINS is big-endian
		}

		parsed, err := ParseAddress(addr)
		if err != nil {
			tv.Error = err
			results[i] = tv
			continue
		}

		tv.DataType = parsed.TypeCode
		tv.Count = parsed.Count

		if parsed.TypeCode == TypeBool {
			bits, err := c.fins.readBits(BitAreaFromWordArea(parsed.MemoryArea), parsed.Address, parsed.BitOffset, uint16(parsed.Count))
			if err != nil {
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
		} else {
			wordCount := (TypeSize(parsed.TypeCode) * parsed.Count) / 2
			if wordCount < 1 {
				wordCount = 1
			}
			words, err := c.fins.readWords(parsed.MemoryArea, parsed.Address, uint16(wordCount))
			if err != nil {
				tv.Error = err
				results[i] = tv
				continue
			}
			data := make([]byte, len(words)*2)
			for j, w := range words {
				binary.BigEndian.PutUint16(data[j*2:j*2+2], w)
			}
			tv.Bytes = data
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

	for i, tagName := range tagNames {
		tv := &TagValue{
			Name:      tagName,
			Count:     1,
			bigEndian: false, // CIP is little-endian
		}

		path, err := cip.EPath().Symbol(tagName).Build()
		if err != nil {
			tv.Error = fmt.Errorf("invalid tag path: %w", err)
			results[i] = tv
			continue
		}

		reqData := binary.LittleEndian.AppendUint16(nil, 1) // Element count
		req := cip.Request{
			Service: svcReadTag,
			Path:    path,
			Data:    reqData,
		}

		respData, err := c.sendCIPRequest(req)
		if err != nil {
			tv.Error = err
			results[i] = tv
			continue
		}

		// Parse response - first 2 bytes are data type
		if len(respData) < 2 {
			tv.Error = fmt.Errorf("response too short")
			results[i] = tv
			continue
		}

		tv.DataType = binary.LittleEndian.Uint16(respData[0:2])
		if len(respData) > 2 {
			tv.Bytes = respData[2:]
		}
		results[i] = tv
	}

	return results, nil
}

// sendCIPRequest sends a CIP request via EIP.
func (c *Client) sendCIPRequest(req cip.Request) ([]byte, error) {
	reqData := req.Marshal()

	cpf := eip.EipCommonPacket{
		Items: []eip.EipCommonPacketItem{
			{TypeId: eip.CpfAddressNullId, Length: 0, Data: nil},
			{TypeId: eip.CpfUnconnectedMessageId, Length: uint16(len(reqData)), Data: reqData},
		},
	}

	resp, err := c.eipClient.SendRRData(cpf)
	if err != nil {
		return nil, err
	}

	if len(resp.Items) < 2 {
		return nil, fmt.Errorf("invalid CIP response")
	}

	return resp.Items[1].Data, nil
}

// Write writes a value to an address.
func (c *Client) Write(address string, value interface{}) error {
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
	return c.writeFINS(address, value)
}

// writeFINS writes to a FINS address.
func (c *Client) writeFINS(address string, value interface{}) error {
	parsed, err := ParseAddress(address)
	if err != nil {
		return err
	}

	data, err := EncodeValue(value, parsed.TypeCode, true)
	if err != nil {
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
			return fmt.Errorf("cannot convert %T to BOOL", value)
		}
		return c.fins.writeBits(BitAreaFromWordArea(parsed.MemoryArea), parsed.Address, parsed.BitOffset, []bool{bitVal})
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
	return c.fins.writeWords(parsed.MemoryArea, parsed.Address, words)
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
func (c *Client) ReadWithTypes(requests []TagRequest) ([]*TagValue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected {
		return nil, fmt.Errorf("not connected")
	}

	// EIP doesn't need type hints (types are embedded)
	if c.eipClient != nil {
		addrs := make([]string, len(requests))
		for i, req := range requests {
			addrs[i] = req.Address
		}
		return c.readEIP(addrs)
	}

	// FINS with type hints
	results := make([]*TagValue, len(requests))

	for i, req := range requests {
		tv := &TagValue{
			Name:      req.Address,
			Count:     1,
			bigEndian: true,
		}

		parsed, err := ParseAddressWithType(req.Address, req.TypeHint)
		if err != nil {
			tv.Error = err
			results[i] = tv
			continue
		}

		tv.DataType = parsed.TypeCode
		tv.Count = parsed.Count

		if parsed.TypeCode == TypeBool {
			bits, err := c.fins.readBits(BitAreaFromWordArea(parsed.MemoryArea), parsed.Address, parsed.BitOffset, uint16(parsed.Count))
			if err != nil {
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
		} else {
			elemSize := TypeSize(parsed.TypeCode)
			if elemSize == 0 {
				elemSize = 2
			}
			wordCount := (elemSize*parsed.Count + 1) / 2
			if wordCount < 1 {
				wordCount = 1
			}

			words, err := c.fins.readWords(parsed.MemoryArea, parsed.Address, uint16(wordCount))
			if err != nil {
				tv.Error = err
				results[i] = tv
				continue
			}
			data := make([]byte, len(words)*2)
			for j, w := range words {
				binary.BigEndian.PutUint16(data[j*2:j*2+2], w)
			}
			tv.Bytes = data
		}

		if parsed.Count > 1 {
			tv.DataType = MakeArrayType(parsed.TypeCode)
		}
		results[i] = tv
	}

	return results, nil
}

// AllTags discovers all tags (EIP only).
func (c *Client) AllTags() ([]TagInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.eipClient == nil {
		return nil, fmt.Errorf("tag discovery only supported for EIP transport")
	}

	var tags []TagInfo
	instanceID := uint32(0)

	for {
		instanceID++

		path, _ := cip.EPath().Class16(0x6B).Instance32(instanceID).Build()
		req := cip.Request{
			Service: cip.SvcGetAttributesAll,
			Path:    path,
		}

		respData, err := c.sendCIPRequest(req)
		if err != nil {
			break
		}

		if len(respData) < 4 {
			break
		}

		tag := c.parseSymbolInstance(respData, instanceID)
		if tag.Name != "" {
			tags = append(tags, tag)
		}

		if instanceID > 10000 {
			break
		}
	}

	return tags, nil
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
