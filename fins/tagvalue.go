package fins

import "fmt"

// TagInfo holds metadata about a configured tag/address.
// This structure is designed to be compatible with the warlogix plcman package.
type TagInfo struct {
	Name       string // User-defined name or address (e.g., "DM100", "CIO0.0")
	MemoryArea byte   // Memory area code (e.g., MemoryAreaDMWord)
	Address    uint16 // Word address within the memory area
	BitOffset  byte   // Bit offset (0-15) for bit-level access
	TypeCode   uint16 // Data type code
	Count      int    // Number of elements (1 for scalar)
	Comment    string // Optional description
}

// IsReadable returns true if the tag can be read.
// All FINS tags are readable unless there's a specific restriction.
func (t *TagInfo) IsReadable() bool {
	return true
}

// IsWritable returns true if the tag can be written.
// Most FINS memory areas are writable.
func (t *TagInfo) IsWritable() bool {
	// All standard memory areas are writable
	return true
}

// IsPrimitive returns true if the type is a primitive (not a struct).
func (t *TagInfo) IsPrimitive() bool {
	switch t.TypeCode {
	case TypeBool, TypeByte, TypeSByte, TypeWord, TypeInt16,
		TypeDWord, TypeInt32, TypeLWord, TypeInt64,
		TypeReal, TypeLReal, TypeString:
		return true
	default:
		return false
	}
}

// String returns a string representation of the tag info.
func (t *TagInfo) String() string {
	if t == nil {
		return "<nil>"
	}
	return fmt.Sprintf("%s (%s.%d, %s)", t.Name, MemoryAreaName(t.MemoryArea), t.Address, TypeName(t.TypeCode))
}

// AddressString returns the formatted address string (e.g., "DM100", "CIO0.5").
func (t *TagInfo) AddressString() string {
	if t == nil {
		return ""
	}
	areaName := MemoryAreaName(t.MemoryArea)

	// For bit areas, include the bit offset
	if t.TypeCode == TypeBool {
		return fmt.Sprintf("%s%d.%d", areaName, t.Address, t.BitOffset)
	}

	// For word areas
	return fmt.Sprintf("%s%d", areaName, t.Address)
}
