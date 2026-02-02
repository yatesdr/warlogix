package logix

import (
	"encoding/binary"
	"fmt"
	"log"
	"math"
)

// Client is a high-level wrapper that manages connection lifecycle
// and provides simplified methods for common PLC operations.
type Client struct {
	plc *PLC // Low-level access preserved
}

// options holds configuration options for Connect.
type options struct {
	slot            byte
	routePath       []byte
	skipForwardOpen bool
}

// Option is a functional option for Connect.
type Option func(*options)

// WithSlot configures the CPU slot for ControlLogix systems.
// This sets up backplane routing to the specified slot.
func WithSlot(slot byte) Option {
	return func(o *options) {
		o.slot = slot
		o.routePath = nil // Slot routing overrides custom route path
	}
}

// WithRoutePath configures explicit routing for the PLC.
// Use this when connecting through a gateway or communication module.
func WithRoutePath(path []byte) Option {
	return func(o *options) {
		o.routePath = path
	}
}

// WithoutConnection skips the Forward Open and uses unconnected messaging only.
// Useful when connected messaging is not supported or not desired.
func WithoutConnection() Option {
	return func(o *options) {
		o.skipForwardOpen = true
	}
}

// Connect establishes a connection to a Logix PLC at the given address.
// It attempts to establish a CIP connection (Forward Open) for efficient messaging.
// If Forward Open fails, it falls back to unconnected messaging with a warning.
func Connect(address string, opts ...Option) (*Client, error) {
	// Apply options
	cfg := &options{}
	for _, opt := range opts {
		opt(cfg)
	}

	// Create low-level PLC connection
	plc, err := NewPLC(address)
	if err != nil {
		return nil, fmt.Errorf("Connect: %w", err)
	}

	// Configure routing
	if cfg.routePath != nil {
		plc.SetRoutePath(cfg.routePath)
	} else if cfg.slot > 0 {
		plc.SetSlotRouting(cfg.slot)
	}

	// Attempt Forward Open for connected messaging
	if !cfg.skipForwardOpen {
		err = plc.OpenConnection()
		if err != nil {
			log.Printf("Warning: Forward Open failed, using unconnected messaging: %v", err)
		}
	}

	return &Client{plc: &plc}, nil
}

// Close releases all resources associated with the client.
func (c *Client) Close() {
	if c == nil || c.plc == nil {
		return
	}
	c.plc.Close()
}

// PLC returns the underlying low-level PLC for advanced operations.
func (c *Client) PLC() *PLC {
	return c.plc
}

// IsConnected returns true if a CIP connection is established.
func (c *Client) IsConnected() bool {
	return c.plc != nil && c.plc.IsConnected()
}

// ConnectionInfo returns information about the current connection.
// Returns connected (CIP connection active), size (negotiated connection size in bytes).
// If not using connected messaging, size is 0.
func (c *Client) ConnectionInfo() (connected bool, size uint16) {
	if c == nil || c.plc == nil {
		return false, 0
	}
	return c.plc.IsConnected(), c.plc.connSize
}

// ConnectionMode returns a human-readable string describing the connection mode.
func (c *Client) ConnectionMode() string {
	if c == nil || c.plc == nil {
		return "Not connected"
	}
	if c.plc.IsConnected() {
		if c.plc.connSize == ConnectionSizeLarge {
			return "Connected (Large Forward Open, 4002 bytes)"
		}
		return "Connected (Standard Forward Open, 504 bytes)"
	}
	return "Unconnected messaging"
}

// Programs returns the list of program names in the PLC.
// Returns names like "MainProgram", "SafetyProgram", etc. (without "Program:" prefix).
func (c *Client) Programs() ([]string, error) {
	if c == nil || c.plc == nil {
		return nil, fmt.Errorf("Programs: nil client")
	}

	fullNames, err := c.plc.ListPrograms()
	if err != nil {
		return nil, fmt.Errorf("Programs: %w", err)
	}

	// Strip "Program:" prefix for cleaner API
	programs := make([]string, len(fullNames))
	for i, name := range fullNames {
		if len(name) > 8 && name[:8] == "Program:" {
			programs[i] = name[8:]
		} else {
			programs[i] = name
		}
	}

	return programs, nil
}

// ControllerTags returns all controller-scope tags (excluding program entries and system tags).
func (c *Client) ControllerTags() ([]TagInfo, error) {
	if c == nil || c.plc == nil {
		return nil, fmt.Errorf("ControllerTags: nil client")
	}

	allTags, err := c.plc.ListTags()
	if err != nil {
		return nil, fmt.Errorf("ControllerTags: %w", err)
	}

	// Filter to only readable data tags at controller scope
	var dataTags []TagInfo
	for _, t := range allTags {
		if t.IsReadable() {
			dataTags = append(dataTags, t)
		}
	}

	return dataTags, nil
}

// ProgramTags returns all tags within a specific program.
// programName can be just the name (e.g., "MainProgram") or full form ("Program:MainProgram").
func (c *Client) ProgramTags(program string) ([]TagInfo, error) {
	if c == nil || c.plc == nil {
		return nil, fmt.Errorf("ProgramTags: nil client")
	}

	tags, err := c.plc.ListProgramTags(program)
	if err != nil {
		return nil, fmt.Errorf("ProgramTags: %w", err)
	}

	// Filter to only readable data tags
	var dataTags []TagInfo
	for _, t := range tags {
		if t.IsReadable() {
			dataTags = append(dataTags, t)
		}
	}

	return dataTags, nil
}

// AllTags returns all readable tags (controller-scope and program-scope).
// This excludes program entries, routines, and system tags.
func (c *Client) AllTags() ([]TagInfo, error) {
	if c == nil || c.plc == nil {
		return nil, fmt.Errorf("AllTags: nil client")
	}

	tags, err := c.plc.ListDataTags()
	if err != nil {
		return nil, fmt.Errorf("AllTags: %w", err)
	}

	return tags, nil
}

// Read reads one or more tags by name and returns their values.
// Each tag in the result includes its own error status (nil if successful).
// The method returns an error only for transport-level failures.
func (c *Client) Read(tagNames ...string) ([]*TagValue, error) {
	if c == nil || c.plc == nil {
		return nil, fmt.Errorf("Read: nil client")
	}
	if len(tagNames) == 0 {
		return nil, nil
	}

	// Determine batch size based on connection mode
	batchSize := 5 // Conservative for unconnected messaging
	if c.plc.IsConnected() {
		batchSize = 50
	}

	results := make([]*TagValue, 0, len(tagNames))

	// Process in batches
	for i := 0; i < len(tagNames); i += batchSize {
		end := i + batchSize
		if end > len(tagNames) {
			end = len(tagNames)
		}
		batch := tagNames[i:end]

		tags, err := c.plc.ReadMultiple(batch)
		if err != nil {
			// Transport-level failure - mark all tags in batch as failed
			for _, name := range batch {
				results = append(results, &TagValue{
					Name:  name,
					Error: err,
				})
			}
			continue
		}

		// Convert results
		for j, tag := range tags {
			if tag == nil {
				results = append(results, &TagValue{
					Name:  batch[j],
					Error: fmt.Errorf("tag read failed"),
				})
			} else {
				results = append(results, &TagValue{
					Name:     tag.Name,
					DataType: tag.DataType,
					Bytes:    tag.Bytes,
					Error:    nil,
				})
			}
		}
	}

	return results, nil
}

// ReadAll discovers and reads all readable tags from the PLC.
// This is a convenience method that combines AllTags() and Read().
func (c *Client) ReadAll() ([]*TagValue, error) {
	if c == nil || c.plc == nil {
		return nil, fmt.Errorf("ReadAll: nil client")
	}

	tags, err := c.AllTags()
	if err != nil {
		return nil, fmt.Errorf("ReadAll: %w", err)
	}

	tagNames := make([]string, len(tags))
	for i, t := range tags {
		tagNames[i] = t.Name
	}

	return c.Read(tagNames...)
}

// Write writes a value to a tag. The value type is inferred and converted appropriately.
// Supported value types: bool, int/int8/int16/int32/int64, uint/uint8/uint16/uint32/uint64,
// float32/float64, string.
func (c *Client) Write(tagName string, value interface{}) error {
	if c == nil || c.plc == nil {
		return fmt.Errorf("Write: nil client")
	}

	var dataType uint16
	var data []byte

	switch v := value.(type) {
	case bool:
		dataType = TypeBOOL
		if v {
			data = []byte{1}
		} else {
			data = []byte{0}
		}

	case int8:
		dataType = TypeSINT
		data = []byte{byte(v)}

	case int16:
		dataType = TypeINT
		data = binary.LittleEndian.AppendUint16(nil, uint16(v))

	case int32:
		dataType = TypeDINT
		data = binary.LittleEndian.AppendUint32(nil, uint32(v))

	case int64:
		dataType = TypeLINT
		data = binary.LittleEndian.AppendUint64(nil, uint64(v))

	case int:
		// Default int to DINT (most common)
		dataType = TypeDINT
		data = binary.LittleEndian.AppendUint32(nil, uint32(v))

	case uint8:
		dataType = TypeUSINT
		data = []byte{v}

	case uint16:
		dataType = TypeUINT
		data = binary.LittleEndian.AppendUint16(nil, v)

	case uint32:
		dataType = TypeUDINT
		data = binary.LittleEndian.AppendUint32(nil, v)

	case uint64:
		dataType = TypeULINT
		data = binary.LittleEndian.AppendUint64(nil, v)

	case uint:
		// Default uint to UDINT
		dataType = TypeUDINT
		data = binary.LittleEndian.AppendUint32(nil, uint32(v))

	case float32:
		dataType = TypeREAL
		data = binary.LittleEndian.AppendUint32(nil, math.Float32bits(v))

	case float64:
		dataType = TypeLREAL
		data = binary.LittleEndian.AppendUint64(nil, math.Float64bits(v))

	case string:
		// Write as Logix STRING (4-byte length prefix + data)
		dataType = TypeSTRING
		strBytes := []byte(v)
		data = binary.LittleEndian.AppendUint32(nil, uint32(len(strBytes)))
		data = append(data, strBytes...)

	default:
		return fmt.Errorf("Write: unsupported value type %T", value)
	}

	return c.plc.WriteTag(tagName, dataType, data)
}

// WriteBool writes a boolean value to a tag.
func (c *Client) WriteBool(tagName string, val bool) error {
	if c == nil || c.plc == nil {
		return fmt.Errorf("WriteBool: nil client")
	}
	data := []byte{0}
	if val {
		data[0] = 1
	}
	return c.plc.WriteTag(tagName, TypeBOOL, data)
}

// WriteInt writes an integer value to a tag.
// Writes as DINT (32-bit signed integer).
func (c *Client) WriteInt(tagName string, val int64) error {
	if c == nil || c.plc == nil {
		return fmt.Errorf("WriteInt: nil client")
	}
	data := binary.LittleEndian.AppendUint32(nil, uint32(val))
	return c.plc.WriteTag(tagName, TypeDINT, data)
}

// WriteFloat writes a floating-point value to a tag.
// Writes as REAL (32-bit float).
func (c *Client) WriteFloat(tagName string, val float64) error {
	if c == nil || c.plc == nil {
		return fmt.Errorf("WriteFloat: nil client")
	}
	data := binary.LittleEndian.AppendUint32(nil, math.Float32bits(float32(val)))
	return c.plc.WriteTag(tagName, TypeREAL, data)
}

// WriteString writes a string value to a tag.
// Writes as Logix STRING (4-byte length prefix + character data).
func (c *Client) WriteString(tagName string, val string) error {
	if c == nil || c.plc == nil {
		return fmt.Errorf("WriteString: nil client")
	}
	strBytes := []byte(val)
	data := binary.LittleEndian.AppendUint32(nil, uint32(len(strBytes)))
	data = append(data, strBytes...)
	return c.plc.WriteTag(tagName, TypeSTRING, data)
}
