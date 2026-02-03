package ads

import "fmt"

// TagValue holds the result of a tag read operation.
// This structure is designed to be compatible with the warlogix plcman package.
type TagValue struct {
	Name     string // Symbol name
	DataType uint16 // ADS type code
	Bytes    []byte // Raw value bytes (little-endian)
	Error    error  // Per-tag error (nil if successful)
}

// GoValue returns the decoded Go value from the raw bytes.
func (t *TagValue) GoValue() interface{} {
	if t == nil || t.Error != nil || len(t.Bytes) == 0 {
		return nil
	}
	return DecodeValue(t.DataType, t.Bytes)
}

// TypeName returns the human-readable type name.
func (t *TagValue) TypeName() string {
	if t == nil {
		return ""
	}
	return TypeName(t.DataType)
}

// String returns a string representation of the tag value.
func (t *TagValue) String() string {
	if t == nil {
		return "<nil>"
	}
	if t.Error != nil {
		return fmt.Sprintf("%s: error: %v", t.Name, t.Error)
	}
	return fmt.Sprintf("%s (%s) = %v", t.Name, t.TypeName(), t.GoValue())
}

// TagInfo holds metadata about a discovered symbol.
// This structure is designed to be compatible with the warlogix plcman package.
type TagInfo struct {
	Name       string // Full symbol name (e.g., "MAIN.Temperature")
	TypeCode   uint16 // ADS type code
	TypeName   string // Type name from TwinCAT (e.g., "REAL", "FB_MyBlock")
	Size       uint32 // Size in bytes
	Comment    string // Symbol comment/description
	IndexGroup uint32 // Index group for direct access
	IndexOffset uint32 // Index offset for direct access
	Flags      uint32 // Symbol flags
}

// IsReadable returns true if the symbol can be read.
// Most symbols are readable unless they have specific access restrictions.
func (t *TagInfo) IsReadable() bool {
	// Check flags for read access
	// TwinCAT symbol flags: bit 0 = persistent, bit 1 = bit value, etc.
	// For now, assume all discovered symbols are readable
	return true
}

// IsWritable returns true if the symbol can be written.
// This checks the symbol flags for write access.
func (t *TagInfo) IsWritable() bool {
	// Check flags for write access
	// In TwinCAT, most variables are writable unless marked as CONSTANT
	// Flag bit 4 (0x10) typically indicates read-only
	return (t.Flags & 0x10) == 0
}

// IsPrimitive returns true if the type is a primitive (not a struct/FB).
func (t *TagInfo) IsPrimitive() bool {
	switch t.TypeCode {
	case TypeBool, TypeByte, TypeSByte, TypeWord, TypeInt16,
		TypeDWord, TypeInt32, TypeLWord, TypeInt64,
		TypeReal, TypeLReal, TypeString, TypeWString,
		TypeTime, TypeDate, TypeTimeOfDay, TypeDateTime:
		return true
	default:
		// Check if size matches a primitive
		return t.Size <= 8
	}
}

// SymbolFlags contains bit flags for symbol attributes.
const (
	SymFlagPersistent uint32 = 0x0001 // Persistent variable
	SymFlagBitValue   uint32 = 0x0002 // Bit value (part of larger type)
	SymFlagReserved   uint32 = 0x0004 // Reserved
	SymFlagReference  uint32 = 0x0008 // Reference to another variable
	SymFlagReadOnly   uint32 = 0x0010 // Read-only (CONSTANT)
	SymFlagStaticVar  uint32 = 0x0020 // Static variable
	SymFlagInput      uint32 = 0x0040 // Input variable
	SymFlagOutput     uint32 = 0x0080 // Output variable
	SymFlagInOut      uint32 = 0x0100 // InOut variable
)
