package s7

import (
	"encoding/binary"
	"fmt"
	"math"
	"sync"
	"time"
)

// Client is a high-level wrapper for S7 PLC communication.
type Client struct {
	transport *transport
	address   string
	rack      int
	slot      int
	pduRef    uint16
	mu        sync.Mutex
}

// options holds configuration options for Connect.
type options struct {
	rack    int
	slot    int
	timeout time.Duration
}

// Option is a functional option for Connect.
type Option func(*options)

// WithRackSlot configures the rack and slot numbers for the PLC.
// Default is rack 0, slot 2 (common for S7-300/400 where CPU is in slot 2).
// For S7-1200/1500, use rack 0, slot 0 (CPU is onboard).
func WithRackSlot(rack, slot int) Option {
	return func(o *options) {
		o.rack = rack
		o.slot = slot
	}
}

// WithTimeout configures the connection timeout.
func WithTimeout(d time.Duration) Option {
	return func(o *options) {
		o.timeout = d
	}
}

// Connect establishes a connection to an S7 PLC at the given address.
func Connect(address string, opts ...Option) (*Client, error) {
	// Apply options
	// Default to slot 2 for S7-300/400 (CPU typically in slot 2)
	// S7-1200/1500 users should explicitly set slot 0
	cfg := &options{
		rack:    0,
		slot:    2,
		timeout: 10 * time.Second,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	t := newTransport()
	t.timeout = cfg.timeout

	if err := t.connect(address, cfg.rack, cfg.slot); err != nil {
		return nil, fmt.Errorf("Connect: %w", err)
	}

	return &Client{
		transport: t,
		address:   address,
		rack:      cfg.rack,
		slot:      cfg.slot,
		pduRef:    0,
	}, nil
}

// Close releases all resources associated with the client.
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.transport != nil {
		c.transport.close()
	}
}

// IsConnected returns true if the client is connected.
func (c *Client) IsConnected() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.transport != nil && c.transport.isConnected()
}

// SetDisconnected marks the client as disconnected.
// This is called when a read/write error indicates the connection is lost.
func (c *Client) SetDisconnected() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	if c.transport != nil {
		c.transport.mu.Lock()
		c.transport.connected = false
		c.transport.mu.Unlock()
	}
}

// Reconnect attempts to re-establish the connection.
// Returns nil if already connected, otherwise attempts reconnection.
func (c *Client) Reconnect() error {
	if c == nil {
		return fmt.Errorf("nil client")
	}

	c.mu.Lock()
	if c.transport != nil && c.transport.isConnected() {
		c.mu.Unlock()
		return nil
	}

	// Close existing transport if any
	if c.transport != nil {
		c.transport.close()
	}

	address := c.address
	rack := c.rack
	slot := c.slot
	c.mu.Unlock()

	// Create new transport
	t := newTransport()
	t.timeout = 10 * time.Second

	if err := t.connect(address, rack, slot); err != nil {
		return fmt.Errorf("reconnect failed: %w", err)
	}

	c.mu.Lock()
	c.transport = t
	c.mu.Unlock()

	return nil
}

// ConnectionMode returns a human-readable string describing the connection mode.
func (c *Client) ConnectionMode() string {
	if c == nil {
		return "Not connected"
	}
	c.mu.Lock()
	connected := c.transport != nil && c.transport.isConnected()
	rack := c.rack
	slot := c.slot
	c.mu.Unlock()
	if connected {
		return fmt.Sprintf("S7 Connected (Rack %d, Slot %d)", rack, slot)
	}
	return "Disconnected"
}

// nextPDURef returns the next PDU reference number.
func (c *Client) nextPDURef() uint16 {
	c.pduRef++
	if c.pduRef == 0 {
		c.pduRef = 1
	}
	return c.pduRef
}

// TagRequest represents a tag to read with optional type hint.
type TagRequest struct {
	Address  string // S7 address (e.g., "DB1.0" or "DB1.DBD0")
	TypeHint string // Optional type name (e.g., "DINT") - used when address doesn't specify type
}

// Read reads one or more addresses by their S7 address strings.
// Each address in the result includes its own error status (nil if successful).
func (c *Client) Read(addresses ...string) ([]*TagValue, error) {
	// Convert to TagRequests with no type hints
	requests := make([]TagRequest, len(addresses))
	for i, addr := range addresses {
		requests[i] = TagRequest{Address: addr}
	}
	return c.ReadWithTypes(requests)
}

// ReadWithTypes reads addresses with optional type hints.
// Type hints are used for simple addresses (DB1.0) that don't specify the data type.
func (c *Client) ReadWithTypes(requests []TagRequest) ([]*TagValue, error) {
	if c == nil || c.transport == nil {
		return nil, fmt.Errorf("Read: nil client")
	}
	if len(requests) == 0 {
		return nil, nil
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	results := make([]*TagValue, 0, len(requests))

	for _, req := range requests {
		addr, err := ParseAddress(req.Address)
		if err != nil {
			results = append(results, &TagValue{
				Name:  req.Address,
				Error: err,
			})
			continue
		}

		// If address didn't specify type/size, use the type hint
		if addr.Size == 0 && req.TypeHint != "" {
			typeCode, ok := TypeCodeFromName(req.TypeHint)
			if ok {
				addr.DataType = typeCode
				addr.Size = TypeSize(typeCode)
				// Handle variable-length types
				if addr.Size == 0 {
					switch BaseType(typeCode) {
					case TypeString:
						// S7 STRING: 1 byte max len + 1 byte actual len + up to 254 chars = 256 bytes max
						addr.Size = 256
					case TypeWString:
						// S7 WSTRING: 2 bytes max len + 2 bytes actual len + up to 508 bytes (254 UTF-16 chars)
						addr.Size = 512
					}
				}
			}
		}

		// Default to DINT if still no size
		if addr.Size == 0 {
			addr.DataType = TypeDInt
			addr.Size = 4
		}

		// Default Count to 1 if not set
		if addr.Count < 1 {
			addr.Count = 1
		}

		// For arrays, read Count * element size bytes
		totalSize := addr.Size * addr.Count
		readAddr := &Address{
			Area:     addr.Area,
			DBNumber: addr.DBNumber,
			Offset:   addr.Offset,
			BitNum:   addr.BitNum,
			DataType: addr.DataType,
			Size:     totalSize,
			Count:    addr.Count,
		}

		data, err := c.readAddress(readAddr)
		if err != nil {
			results = append(results, &TagValue{
				Name:  req.Address,
				Error: err,
			})
			continue
		}

		results = append(results, &TagValue{
			Name:     req.Address,
			DataType: addr.DataType,
			Bytes:    data,
			BitNum:   addr.BitNum,
			Count:    addr.Count,
			Error:    nil,
		})
	}

	return results, nil
}

// readAddress reads data from a specific S7 address.
func (c *Client) readAddress(addr *Address) ([]byte, error) {
	// Build read request
	request := buildReadRequest([]*Address{addr}, c.nextPDURef())

	// Send and receive
	response, err := c.transport.sendReceive(request)
	if err != nil {
		return nil, err
	}

	// Parse response
	results, errors := parseReadResponse(response, 1)
	if errors[0] != nil {
		return nil, errors[0]
	}

	return results[0], nil
}

// Write writes a value to an S7 address.
// The value type is inferred and converted appropriately.
func (c *Client) Write(address string, value interface{}) error {
	if c == nil || c.transport == nil {
		return fmt.Errorf("Write: nil client")
	}

	addr, err := ParseAddress(address)
	if err != nil {
		return fmt.Errorf("Write: %w", err)
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	data, err := c.encodeValue(addr, value)
	if err != nil {
		return err
	}

	return c.writeAddress(addr, data)
}

// writeAddress writes data to a specific S7 address.
func (c *Client) writeAddress(addr *Address, data []byte) error {
	// Build write request
	request := buildWriteRequest(addr, data, c.nextPDURef())

	// Send and receive
	response, err := c.transport.sendReceive(request)
	if err != nil {
		return err
	}

	// Parse response
	return parseWriteResponse(response)
}

// encodeValue converts a Go value to bytes for the given address type.
func (c *Client) encodeValue(addr *Address, value interface{}) ([]byte, error) {
	// For bit writes, we need to do read-modify-write
	if addr.BitNum >= 0 {
		return c.encodeBitValue(addr, value)
	}

	switch addr.DataType {
	case TypeBool:
		return encodeBool(value)
	case TypeByte, TypeSInt:
		return encodeByte(value)
	case TypeWord:
		return encodeWord(value)
	case TypeInt:
		return encodeInt(value)
	case TypeDWord:
		return encodeDWord(value)
	case TypeDInt:
		return encodeDInt(value)
	case TypeReal:
		return encodeReal(value)
	case TypeLReal:
		return encodeLReal(value)
	case TypeLInt:
		return encodeLInt(value)
	case TypeULInt:
		return encodeULInt(value)
	default:
		return nil, fmt.Errorf("unsupported data type: %s", TypeName(addr.DataType))
	}
}

// encodeBitValue handles read-modify-write for individual bits.
func (c *Client) encodeBitValue(addr *Address, value interface{}) ([]byte, error) {
	// Read current byte
	data, err := c.readAddress(&Address{
		Area:     addr.Area,
		DBNumber: addr.DBNumber,
		Offset:   addr.Offset,
		BitNum:   -1,
		DataType: TypeByte,
		Size:     1,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to read byte for bit write: %w", err)
	}

	// Modify the bit
	var boolVal bool
	switch v := value.(type) {
	case bool:
		boolVal = v
	case int:
		boolVal = v != 0
	case int64:
		boolVal = v != 0
	default:
		return nil, fmt.Errorf("cannot convert %T to bool", value)
	}

	if boolVal {
		data[0] |= (1 << addr.BitNum)
	} else {
		data[0] &^= (1 << addr.BitNum)
	}

	return data, nil
}

func encodeBool(value interface{}) ([]byte, error) {
	var v bool
	switch val := value.(type) {
	case bool:
		v = val
	case int:
		v = val != 0
	case int64:
		v = val != 0
	default:
		return nil, fmt.Errorf("cannot convert %T to bool", value)
	}
	if v {
		return []byte{1}, nil
	}
	return []byte{0}, nil
}

func encodeByte(value interface{}) ([]byte, error) {
	switch v := value.(type) {
	case uint8:
		return []byte{v}, nil
	case int8:
		return []byte{byte(v)}, nil
	case int:
		return []byte{byte(v)}, nil
	case int64:
		return []byte{byte(v)}, nil
	case uint64:
		return []byte{byte(v)}, nil
	default:
		return nil, fmt.Errorf("cannot convert %T to byte", value)
	}
}

func encodeWord(value interface{}) ([]byte, error) {
	buf := make([]byte, 2)
	switch v := value.(type) {
	case uint16:
		binary.BigEndian.PutUint16(buf, v)
	case int16:
		binary.BigEndian.PutUint16(buf, uint16(v))
	case int:
		binary.BigEndian.PutUint16(buf, uint16(v))
	case int64:
		binary.BigEndian.PutUint16(buf, uint16(v))
	case uint64:
		binary.BigEndian.PutUint16(buf, uint16(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to word", value)
	}
	return buf, nil
}

func encodeInt(value interface{}) ([]byte, error) {
	buf := make([]byte, 2)
	switch v := value.(type) {
	case int16:
		binary.BigEndian.PutUint16(buf, uint16(v))
	case int:
		binary.BigEndian.PutUint16(buf, uint16(v))
	case int64:
		binary.BigEndian.PutUint16(buf, uint16(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to int", value)
	}
	return buf, nil
}

func encodeDWord(value interface{}) ([]byte, error) {
	buf := make([]byte, 4)
	switch v := value.(type) {
	case uint32:
		binary.BigEndian.PutUint32(buf, v)
	case int32:
		binary.BigEndian.PutUint32(buf, uint32(v))
	case int:
		binary.BigEndian.PutUint32(buf, uint32(v))
	case int64:
		binary.BigEndian.PutUint32(buf, uint32(v))
	case uint64:
		binary.BigEndian.PutUint32(buf, uint32(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to dword", value)
	}
	return buf, nil
}

func encodeDInt(value interface{}) ([]byte, error) {
	buf := make([]byte, 4)
	switch v := value.(type) {
	case int32:
		binary.BigEndian.PutUint32(buf, uint32(v))
	case int:
		binary.BigEndian.PutUint32(buf, uint32(v))
	case int64:
		binary.BigEndian.PutUint32(buf, uint32(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to dint", value)
	}
	return buf, nil
}

func encodeReal(value interface{}) ([]byte, error) {
	buf := make([]byte, 4)
	switch v := value.(type) {
	case float32:
		binary.BigEndian.PutUint32(buf, math.Float32bits(v))
	case float64:
		binary.BigEndian.PutUint32(buf, math.Float32bits(float32(v)))
	default:
		return nil, fmt.Errorf("cannot convert %T to real", value)
	}
	return buf, nil
}

func encodeLReal(value interface{}) ([]byte, error) {
	buf := make([]byte, 8)
	switch v := value.(type) {
	case float64:
		binary.BigEndian.PutUint64(buf, math.Float64bits(v))
	case float32:
		binary.BigEndian.PutUint64(buf, math.Float64bits(float64(v)))
	default:
		return nil, fmt.Errorf("cannot convert %T to lreal", value)
	}
	return buf, nil
}

func encodeLInt(value interface{}) ([]byte, error) {
	buf := make([]byte, 8)
	switch v := value.(type) {
	case int64:
		binary.BigEndian.PutUint64(buf, uint64(v))
	case int:
		binary.BigEndian.PutUint64(buf, uint64(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to lint", value)
	}
	return buf, nil
}

func encodeULInt(value interface{}) ([]byte, error) {
	buf := make([]byte, 8)
	switch v := value.(type) {
	case uint64:
		binary.BigEndian.PutUint64(buf, v)
	case int64:
		binary.BigEndian.PutUint64(buf, uint64(v))
	case int:
		binary.BigEndian.PutUint64(buf, uint64(v))
	default:
		return nil, fmt.Errorf("cannot convert %T to ulint", value)
	}
	return buf, nil
}

// GetCPUInfo returns information about the connected CPU.
// Note: CPU info retrieval is not yet implemented in native protocol.
func (c *Client) GetCPUInfo() (*CPUInfo, error) {
	if c == nil || c.transport == nil {
		return nil, fmt.Errorf("GetCPUInfo: nil client")
	}

	// CPU info retrieval requires UserData PDU type which is more complex
	// Return a placeholder for now
	return &CPUInfo{
		ModuleTypeName: "S7 PLC",
		SerialNumber:   "",
		ASName:         "",
		Copyright:      "",
		ModuleName:     "",
	}, nil
}

// CPUInfo contains information about the S7 CPU.
type CPUInfo struct {
	ModuleTypeName string
	SerialNumber   string
	ASName         string
	Copyright      string
	ModuleName     string
}
