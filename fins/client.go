package fins

import (
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	gofins "github.com/xiaotushaoxia/fins"
)

// Client represents a connection to an Omron PLC via FINS protocol.
type Client struct {
	mu        sync.Mutex
	client    *gofins.UDPClient
	address   string
	port      int
	localAddr gofins.UDPAddress
	plcAddr   gofins.UDPAddress
	network   byte
	node      byte
	unit      byte
	srcNode   byte // Source node number (local node)
	connected bool
	timeout   time.Duration
	debug     bool // Enable packet logging
}

// Option is a functional option for configuring the client.
type Option func(*Client)

// WithNetwork sets the FINS network number.
func WithNetwork(network byte) Option {
	return func(c *Client) {
		c.network = network
	}
}

// WithNode sets the FINS node number.
func WithNode(node byte) Option {
	return func(c *Client) {
		c.node = node
	}
}

// WithUnit sets the FINS unit number (CPU unit).
func WithUnit(unit byte) Option {
	return func(c *Client) {
		c.unit = unit
	}
}

// WithTimeout sets the response timeout.
func WithTimeout(timeout time.Duration) Option {
	return func(c *Client) {
		c.timeout = timeout
	}
}

// WithPort sets the FINS port (default 9600).
func WithPort(port int) Option {
	return func(c *Client) {
		c.port = port
	}
}

// WithSourceNode sets the source (local) node number for FINS communication.
// In FINS/UDP, this is typically the last octet of the local IP address.
// If not set, it will be auto-detected from the local IP.
func WithSourceNode(node byte) Option {
	return func(c *Client) {
		c.srcNode = node
	}
}

// WithDebug enables packet logging for debugging.
func WithDebug(enabled bool) Option {
	return func(c *Client) {
		c.debug = enabled
	}
}

// Connect establishes a connection to an Omron PLC via FINS/UDP.
func Connect(address string, opts ...Option) (*Client, error) {
	c := &Client{
		address: address,
		port:    9600, // Default FINS port
		network: 0,
		node:    0,
		unit:    0,
		srcNode: 0, // Will be auto-detected if not set
		timeout: 2000 * time.Millisecond, // 2 second timeout for reliable communication
	}

	for _, opt := range opts {
		opt(c)
	}

	// Auto-detect source node from local IP if not explicitly configured
	if c.srcNode == 0 {
		c.srcNode = detectLocalNode(address)
	}

	// Create addresses
	// Local address: use the detected/configured source node
	// The source node is important for FINS - many PLCs require it to match the IP address
	c.localAddr = gofins.NewUDPAddress("0.0.0.0", 0, c.network, c.srcNode, 0)

	// PLC address
	c.plcAddr = gofins.NewUDPAddress(c.address, c.port, c.network, c.node, c.unit)

	// Create client
	client, err := gofins.NewUDPClient(c.localAddr, c.plcAddr)
	if err != nil {
		return nil, fmt.Errorf("failed to create FINS client: %w", err)
	}

	// Configure client
	client.SetTimeoutMs(uint(c.timeout.Milliseconds()))

	// Enable packet logging if debug mode is on
	if c.debug {
		client.SetShowPacket(true)
	}

	c.client = client
	c.connected = true

	return c, nil
}

// detectLocalNode attempts to determine the appropriate source node number
// by looking at the local network interface that would route to the PLC.
// In FINS/UDP, the source node is typically the last octet of the local IP.
func detectLocalNode(plcAddress string) byte {
	// Try to establish a UDP connection to determine the local IP
	conn, err := net.Dial("udp", plcAddress+":9600")
	if err != nil {
		// If we can't determine, use 0 (some PLCs accept this)
		return 0
	}
	defer conn.Close()

	localAddr := conn.LocalAddr().(*net.UDPAddr)
	ip := localAddr.IP.To4()
	if ip == nil {
		return 0
	}

	// Return the last octet as the node number
	return ip[3]
}

// Close closes the connection to the PLC.
func (c *Client) Close() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
	c.connected = false
	return nil
}

// IsConnected returns true if the client is connected.
func (c *Client) IsConnected() bool {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected && c.client != nil
}

// SetDebug enables or disables packet logging for debugging.
// When enabled, FINS packets are logged to stdout.
func (c *Client) SetDebug(enabled bool) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.debug = enabled
	if c.client != nil {
		c.client.SetShowPacket(enabled)
	}
}

// GetSourceNode returns the source node number used for this connection.
func (c *Client) GetSourceNode() byte {
	return c.srcNode
}

// SetDisconnected marks the client as disconnected.
func (c *Client) SetDisconnected() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
}

// Reconnect attempts to re-establish the connection.
func (c *Client) Reconnect() error {
	c.mu.Lock()
	defer c.mu.Unlock()

	// Close existing connection if any
	if c.client != nil {
		c.client.Close()
		c.client = nil
	}
	c.connected = false

	// Create new client
	client, err := gofins.NewUDPClient(c.localAddr, c.plcAddr)
	if err != nil {
		return fmt.Errorf("failed to reconnect FINS client: %w", err)
	}

	// Configure client
	client.SetTimeoutMs(uint(c.timeout.Milliseconds()))

	// Enable packet logging if debug mode is on
	if c.debug {
		client.SetShowPacket(true)
	}

	c.client = client
	c.connected = true

	return nil
}

// ConnectionMode returns a human-readable description of the connection.
func (c *Client) ConnectionMode() string {
	if c == nil {
		return "Not connected"
	}
	// Show source node for debugging FINS connection issues
	return fmt.Sprintf("FINS/UDP %s:%d (DST NET:%d NODE:%d UNIT:%d, SRC NODE:%d)",
		c.address, c.port, c.network, c.node, c.unit, c.srcNode)
}

// DeviceInfo holds information about the connected PLC.
type DeviceInfo struct {
	Model      string
	Version    string
	CPUType    string
	MemorySize int
}

// GetDeviceInfo retrieves device information from the PLC.
// Note: FINS doesn't have a standard device info command, so this returns basic info.
func (c *Client) GetDeviceInfo() (*DeviceInfo, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	// FINS doesn't have a standard device info command
	// Return basic connection info
	return &DeviceInfo{
		Model:   "Omron PLC",
		Version: "FINS",
		CPUType: fmt.Sprintf("Node %d", c.node),
	}, nil
}

// Read reads multiple tags from the PLC.
func (c *Client) Read(addresses ...string) ([]*TagValue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	results := make([]*TagValue, len(addresses))

	for i, addr := range addresses {
		tv := &TagValue{
			Name:  addr,
			Count: 1,
		}

		// Parse the address
		parsed, err := ParseAddress(addr)
		if err != nil {
			tv.Error = err
			results[i] = tv
			continue
		}

		tv.DataType = parsed.TypeCode
		tv.Count = parsed.Count

		// Read based on type
		var data []byte
		if parsed.TypeCode == TypeBool {
			// Read bits
			bits, err := c.client.ReadBits(BitAreaFromWordArea(parsed.MemoryArea), parsed.Address, parsed.BitOffset, uint16(parsed.Count))
			if err != nil {
				if isConnectionError(err) {
					c.connected = false
				}
				tv.Error = err
				results[i] = tv
				continue
			}
			// Convert bits to bytes (2 bytes per bool for consistency with word reads)
			data = make([]byte, len(bits)*2)
			for j, b := range bits {
				if b {
					data[j*2] = 0
					data[j*2+1] = 1
				}
			}
		} else {
			// Read words
			wordCount := (TypeSize(parsed.TypeCode) * parsed.Count) / 2
			if wordCount < 1 {
				wordCount = 1
			}
			words, err := c.client.ReadWords(parsed.MemoryArea, parsed.Address, uint16(wordCount))
			if err != nil {
				if isConnectionError(err) {
					c.connected = false
				}
				tv.Error = err
				results[i] = tv
				continue
			}
			// Convert words to bytes (big-endian)
			data = make([]byte, len(words)*2)
			for j, w := range words {
				data[j*2] = byte(w >> 8)
				data[j*2+1] = byte(w)
			}
		}

		tv.Bytes = data
		if parsed.Count > 1 {
			tv.DataType = MakeArrayType(parsed.TypeCode)
		}
		results[i] = tv
	}

	return results, nil
}

// Write writes a value to an address on the PLC.
func (c *Client) Write(address string, value interface{}) error {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.client == nil {
		return fmt.Errorf("not connected")
	}

	// Parse the address
	parsed, err := ParseAddress(address)
	if err != nil {
		return err
	}

	// Encode the value
	data, err := EncodeValue(value, parsed.TypeCode)
	if err != nil {
		return err
	}

	// Write based on type
	if parsed.TypeCode == TypeBool {
		// Write bit
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
		bits := []bool{bitVal}
		err = c.client.WriteBits(BitAreaFromWordArea(parsed.MemoryArea), parsed.Address, parsed.BitOffset, bits)
	} else {
		// Write words
		// Convert bytes to words
		words := make([]uint16, (len(data)+1)/2)
		for i := 0; i < len(words); i++ {
			idx := i * 2
			if idx+1 < len(data) {
				words[i] = uint16(data[idx])<<8 | uint16(data[idx+1])
			} else if idx < len(data) {
				words[i] = uint16(data[idx]) << 8
			}
		}
		err = c.client.WriteWords(parsed.MemoryArea, parsed.Address, words)
	}

	if err != nil && isConnectionError(err) {
		c.connected = false
	}

	return err
}

// ParsedAddress holds the parsed components of a FINS address.
type ParsedAddress struct {
	MemoryArea byte
	Address    uint16
	BitOffset  byte
	TypeCode   uint16
	Count      int
}

// Address parsing regex patterns.
var (
	// Pattern: DM100, CIO50, HR10, WR5, AR20
	wordAddrPattern = regexp.MustCompile(`^([A-Z]+)(\d+)(?:\[(\d+)\])?$`)
	// Pattern: DM100.5, CIO50.0 (bit access)
	bitAddrPattern = regexp.MustCompile(`^([A-Z]+)(\d+)\.(\d+)(?:\[(\d+)\])?$`)
)

// ParseAddress parses a FINS address string into its components.
// Supports formats:
//   - DM100 - Word address
//   - DM100[10] - Array of 10 words
//   - DM100.5 - Bit address (bit 5 of DM100)
//   - CIO0, HR10, WR5, AR20 - Other memory areas
func ParseAddress(addr string) (*ParsedAddress, error) {
	addr = strings.ToUpper(strings.TrimSpace(addr))

	// Try bit address pattern first
	if matches := bitAddrPattern.FindStringSubmatch(addr); matches != nil {
		areaName := matches[1]
		wordAddr, _ := strconv.ParseUint(matches[2], 10, 16)
		bitOffset, _ := strconv.ParseUint(matches[3], 10, 8)
		count := 1
		if matches[4] != "" {
			count, _ = strconv.Atoi(matches[4])
		}

		area, ok := MemoryAreaFromName(areaName)
		if !ok {
			return nil, fmt.Errorf("unknown memory area: %s", areaName)
		}

		if bitOffset > 15 {
			return nil, fmt.Errorf("bit offset must be 0-15, got %d", bitOffset)
		}

		return &ParsedAddress{
			MemoryArea: area,
			Address:    uint16(wordAddr),
			BitOffset:  byte(bitOffset),
			TypeCode:   TypeBool,
			Count:      count,
		}, nil
	}

	// Try word address pattern
	if matches := wordAddrPattern.FindStringSubmatch(addr); matches != nil {
		areaName := matches[1]
		wordAddr, _ := strconv.ParseUint(matches[2], 10, 16)
		count := 1
		if matches[3] != "" {
			count, _ = strconv.Atoi(matches[3])
		}

		area, ok := MemoryAreaFromName(areaName)
		if !ok {
			return nil, fmt.Errorf("unknown memory area: %s", areaName)
		}

		return &ParsedAddress{
			MemoryArea: area,
			Address:    uint16(wordAddr),
			BitOffset:  0,
			TypeCode:   TypeWord, // Default to WORD, can be overridden with type hint
			Count:      count,
		}, nil
	}

	return nil, fmt.Errorf("invalid FINS address format: %s", addr)
}

// ParseAddressWithType parses a FINS address and applies the type hint.
func ParseAddressWithType(addr string, typeHint string) (*ParsedAddress, error) {
	parsed, err := ParseAddress(addr)
	if err != nil {
		return nil, err
	}

	// Apply type hint if provided
	if typeHint != "" {
		if tc, ok := TypeCodeFromName(typeHint); ok {
			parsed.TypeCode = tc
		}
	}

	return parsed, nil
}

// ValidateAddress checks if an address string is a valid FINS address.
// Returns nil if valid, or an error describing the problem.
func ValidateAddress(addr string) error {
	_, err := ParseAddress(addr)
	return err
}

// isConnectionError checks if an error indicates the connection is broken.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	return strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "refused") ||
		strings.Contains(errStr, "unreachable") ||
		strings.Contains(errStr, "closed")
}

// TagRequest represents a request to read a tag with optional type hint.
type TagRequest struct {
	Address  string
	TypeHint string
}

// ReadWithTypes reads multiple tags using type hints for proper interpretation.
func (c *Client) ReadWithTypes(requests []TagRequest) ([]*TagValue, error) {
	c.mu.Lock()
	defer c.mu.Unlock()

	if !c.connected || c.client == nil {
		return nil, fmt.Errorf("not connected")
	}

	results := make([]*TagValue, len(requests))

	for i, req := range requests {
		tv := &TagValue{
			Name:  req.Address,
			Count: 1,
		}

		// Parse the address with type hint
		parsed, err := ParseAddressWithType(req.Address, req.TypeHint)
		if err != nil {
			tv.Error = err
			results[i] = tv
			continue
		}

		tv.DataType = parsed.TypeCode
		tv.Count = parsed.Count

		// Read based on type
		var data []byte
		if parsed.TypeCode == TypeBool {
			// Read bits
			bits, err := c.client.ReadBits(BitAreaFromWordArea(parsed.MemoryArea), parsed.Address, parsed.BitOffset, uint16(parsed.Count))
			if err != nil {
				if isConnectionError(err) {
					c.connected = false
				}
				tv.Error = err
				results[i] = tv
				continue
			}
			// Convert bits to bytes (2 bytes per bool for consistency)
			data = make([]byte, len(bits)*2)
			for j, b := range bits {
				if b {
					data[j*2] = 0
					data[j*2+1] = 1
				}
			}
		} else {
			// Calculate word count based on type size
			elemSize := TypeSize(parsed.TypeCode)
			if elemSize == 0 {
				elemSize = 2 // Default to word size
			}
			wordCount := (elemSize * parsed.Count + 1) / 2
			if wordCount < 1 {
				wordCount = 1
			}

			words, err := c.client.ReadWords(parsed.MemoryArea, parsed.Address, uint16(wordCount))
			if err != nil {
				if isConnectionError(err) {
					c.connected = false
				}
				tv.Error = err
				results[i] = tv
				continue
			}
			// Convert words to bytes (big-endian)
			data = make([]byte, len(words)*2)
			for j, w := range words {
				data[j*2] = byte(w >> 8)
				data[j*2+1] = byte(w)
			}
		}

		tv.Bytes = data
		if parsed.Count > 1 {
			tv.DataType = MakeArrayType(parsed.TypeCode)
		}
		results[i] = tv
	}

	return results, nil
}
