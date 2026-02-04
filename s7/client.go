package s7

import (
	"encoding/binary"
	"fmt"
	"math"
	"strings"
	"sync"
	"time"

	"github.com/robinson/gos7"
)

// Client is a high-level wrapper for S7 PLC communication.
type Client struct {
	handler   *gos7.TCPClientHandler
	client    gos7.Client
	address   string
	rack      int
	slot      int
	connected bool
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
// Default is rack 0, slot 0 for S7-1200/1500 (most common modern PLCs).
// For S7-300/400, use rack 0, slot 2 (or the slot where the CPU is placed).
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
	// Default to slot 0 for S7-1200/1500 (most common modern PLCs)
	// S7-300/400 users should explicitly set slot 2 or appropriate slot
	cfg := &options{
		rack:    0,
		slot:    0,
		timeout: 10 * time.Second,
	}
	for _, opt := range opts {
		opt(cfg)
	}

	// Create TCP handler
	handler := gos7.NewTCPClientHandler(address, cfg.rack, cfg.slot)
	handler.Timeout = cfg.timeout
	handler.IdleTimeout = cfg.timeout

	// Connect
	if err := handler.Connect(); err != nil {
		return nil, fmt.Errorf("Connect: %w", err)
	}

	// Create client
	client := gos7.NewClient(handler)

	return &Client{
		handler:   handler,
		client:    client,
		address:   address,
		rack:      cfg.rack,
		slot:      cfg.slot,
		connected: true,
	}, nil
}

// Close releases all resources associated with the client.
func (c *Client) Close() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
	if c.handler != nil {
		c.handler.Close()
	}
}

// IsConnected returns true if the client is connected.
func (c *Client) IsConnected() bool {
	if c == nil {
		return false
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.connected
}

// SetDisconnected marks the client as disconnected.
// This is called when a read/write error indicates the connection is lost.
func (c *Client) SetDisconnected() {
	if c == nil {
		return
	}
	c.mu.Lock()
	defer c.mu.Unlock()
	c.connected = false
}

// Reconnect attempts to re-establish the connection.
// Returns nil if already connected, otherwise attempts reconnection.
func (c *Client) Reconnect() error {
	if c == nil {
		return fmt.Errorf("nil client")
	}

	c.mu.Lock()
	if c.connected {
		c.mu.Unlock()
		return nil
	}

	// Close existing handler if any
	if c.handler != nil {
		c.handler.Close()
	}

	address := c.address
	rack := c.rack
	slot := c.slot
	c.mu.Unlock()

	// Create new handler
	handler := gos7.NewTCPClientHandler(address, rack, slot)
	handler.Timeout = 10 * time.Second
	handler.IdleTimeout = 10 * time.Second

	if err := handler.Connect(); err != nil {
		return fmt.Errorf("reconnect failed: %w", err)
	}

	client := gos7.NewClient(handler)

	c.mu.Lock()
	c.handler = handler
	c.client = client
	c.connected = true
	c.mu.Unlock()

	return nil
}

// ConnectionMode returns a human-readable string describing the connection mode.
func (c *Client) ConnectionMode() string {
	if c == nil {
		return "Not connected"
	}
	c.mu.Lock()
	connected := c.connected
	rack := c.rack
	slot := c.slot
	c.mu.Unlock()
	if connected {
		return fmt.Sprintf("S7 Connected (Rack %d, Slot %d)", rack, slot)
	}
	return "Disconnected"
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
	if c == nil || c.client == nil {
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
	buf := make([]byte, addr.Size)

	var err error
	switch addr.Area {
	case AreaDB:
		err = c.client.AGReadDB(addr.DBNumber, addr.Offset, addr.Size, buf)
	case AreaI:
		err = c.client.AGReadEB(addr.Offset, addr.Size, buf)
	case AreaQ:
		err = c.client.AGReadAB(addr.Offset, addr.Size, buf)
	case AreaM:
		err = c.client.AGReadMB(addr.Offset, addr.Size, buf)
	case AreaT:
		err = c.client.AGReadTM(addr.Offset, addr.Size, buf)
	case AreaC:
		err = c.client.AGReadCT(addr.Offset, addr.Size, buf)
	default:
		return nil, fmt.Errorf("unsupported area: %v", addr.Area)
	}

	if err != nil {
		// Check if this is a connection error that indicates the link is dead
		if isConnectionError(err) {
			c.connected = false
		}
		return nil, err
	}

	return buf, nil
}

// isConnectionError checks if an error indicates the TCP connection is broken.
func isConnectionError(err error) bool {
	if err == nil {
		return false
	}
	errStr := strings.ToLower(err.Error())
	// Common connection-related error patterns
	return strings.Contains(errStr, "connection") ||
		strings.Contains(errStr, "broken pipe") ||
		strings.Contains(errStr, "reset by peer") ||
		strings.Contains(errStr, "eof") ||
		strings.Contains(errStr, "timeout") ||
		strings.Contains(errStr, "refused") ||
		strings.Contains(errStr, "closed") ||
		strings.Contains(errStr, "nil")
}

// Write writes a value to an S7 address.
// The value type is inferred and converted appropriately.
func (c *Client) Write(address string, value interface{}) error {
	if c == nil || c.client == nil {
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
	var err error
	switch addr.Area {
	case AreaDB:
		err = c.client.AGWriteDB(addr.DBNumber, addr.Offset, len(data), data)
	case AreaI:
		err = c.client.AGWriteEB(addr.Offset, len(data), data)
	case AreaQ:
		err = c.client.AGWriteAB(addr.Offset, len(data), data)
	case AreaM:
		err = c.client.AGWriteMB(addr.Offset, len(data), data)
	case AreaT:
		err = c.client.AGWriteTM(addr.Offset, len(data), data)
	case AreaC:
		err = c.client.AGWriteCT(addr.Offset, len(data), data)
	default:
		return fmt.Errorf("unsupported area: %v", addr.Area)
	}
	return err
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
func (c *Client) GetCPUInfo() (*CPUInfo, error) {
	if c == nil || c.client == nil {
		return nil, fmt.Errorf("GetCPUInfo: nil client")
	}

	c.mu.Lock()
	defer c.mu.Unlock()

	info, err := c.client.GetCPUInfo()
	if err != nil {
		return nil, err
	}

	return &CPUInfo{
		ModuleTypeName: info.ModuleTypeName,
		SerialNumber:   info.SerialNumber,
		ASName:         info.ASName,
		Copyright:      info.Copyright,
		ModuleName:     info.ModuleName,
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
