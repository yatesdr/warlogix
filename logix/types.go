package logix

import (
	"fmt"
	"strings"
)

// Logix CIP data type codes.
// These are returned in the DataType field of Tag after a ReadTag operation.
// The caller uses these to interpret the raw bytes.
const (
	TypeBOOL  uint16 = 0x00C1 // 1 byte (0 or 1)
	TypeSINT  uint16 = 0x00C2 // 1 byte signed
	TypeINT   uint16 = 0x00C3 // 2 bytes signed
	TypeDINT  uint16 = 0x00C4 // 4 bytes signed
	TypeLINT  uint16 = 0x00C5 // 8 bytes signed
	TypeUSINT uint16 = 0x00C6 // 1 byte unsigned
	TypeUINT  uint16 = 0x00C7 // 2 bytes unsigned
	TypeUDINT uint16 = 0x00C8 // 4 bytes unsigned
	TypeULINT uint16 = 0x00C9 // 8 bytes unsigned
	TypeREAL  uint16 = 0x00CA // 4 bytes IEEE 754 float
	TypeLREAL uint16 = 0x00CB // 8 bytes IEEE 754 double

	// String types
	TypeSTRING      uint16 = 0x00D0 // Logix STRING (82 bytes: 4-byte len + 82 chars)
	TypeShortSTRING uint16 = 0x00DA // Short string

	// Bit string types (arrays of bits)
	TypeBitString8  uint16 = 0x00D1 // 8 bits
	TypeBitString16 uint16 = 0x00D2 // 16 bits
	TypeBitString32 uint16 = 0x00D3 // 32 bits

	// Structure flag - when bit 15 is set, this is a structure/UDT.
	// The lower bits contain the template instance ID.
	TypeStructureMask uint16 = 0x8000

	// Array flag - when bit 13 is set, this is an array.
	// Combined with other type info.
	TypeArrayMask uint16 = 0x2000

	// System structure flag
	TypeSystemMask uint16 = 0x1000
)

// TypeSize returns the byte size of atomic types.
// Returns 0 for structures, arrays, or unknown types.
func TypeSize(dataType uint16) int {
	// Mask off array/structure flags for base type
	baseType := dataType & 0x0FFF

	switch baseType {
	case TypeBOOL, TypeSINT, TypeUSINT:
		return 1
	case TypeINT, TypeUINT:
		return 2
	case TypeDINT, TypeUDINT, TypeREAL:
		return 4
	case TypeLINT, TypeULINT, TypeLREAL:
		return 8
	default:
		return 0
	}
}

// TemplateID extracts the template instance ID from a structure type code.
// Returns 0 if the type is not a structure.
func TemplateID(dataType uint16) uint16 {
	if !IsStructure(dataType) {
		return 0
	}
	return dataType & 0x0FFF
}

// IsStructure returns true if the data type represents a structure/UDT.
func IsStructure(dataType uint16) bool {
	return (dataType & TypeStructureMask) != 0
}

// IsArray returns true if the data type represents an array.
func IsArray(dataType uint16) bool {
	return (dataType & TypeArrayMask) != 0
}

// TypeName returns a human-readable name for the data type.
func TypeName(dataType uint16) string {
	if IsStructure(dataType) {
		templateID := TemplateID(dataType)
		name := fmt.Sprintf("STRUCT(%d)", templateID)
		if IsArray(dataType) {
			return name + "[]"
		}
		return name
	}

	baseType := dataType & 0x0FFF
	var name string

	switch baseType {
	case TypeBOOL:
		name = "BOOL"
	case TypeSINT:
		name = "SINT"
	case TypeINT:
		name = "INT"
	case TypeDINT:
		name = "DINT"
	case TypeLINT:
		name = "LINT"
	case TypeUSINT:
		name = "USINT"
	case TypeUINT:
		name = "UINT"
	case TypeUDINT:
		name = "UDINT"
	case TypeULINT:
		name = "ULINT"
	case TypeREAL:
		name = "REAL"
	case TypeLREAL:
		name = "LREAL"
	case TypeSTRING:
		name = "STRING"
	case TypeShortSTRING:
		name = "SHORT_STRING"
	default:
		name = "UNKNOWN"
	}

	if IsArray(dataType) {
		return name + "[]"
	}
	return name
}

// TypeCodeFromName returns the type code for a given type name.
// Returns (typeCode, true) if found, (0, false) otherwise.
func TypeCodeFromName(name string) (uint16, bool) {
	switch strings.ToUpper(name) {
	case "BOOL":
		return TypeBOOL, true
	case "SINT":
		return TypeSINT, true
	case "INT":
		return TypeINT, true
	case "DINT":
		return TypeDINT, true
	case "LINT":
		return TypeLINT, true
	case "USINT":
		return TypeUSINT, true
	case "UINT":
		return TypeUINT, true
	case "UDINT":
		return TypeUDINT, true
	case "ULINT":
		return TypeULINT, true
	case "REAL":
		return TypeREAL, true
	case "LREAL":
		return TypeLREAL, true
	case "STRING":
		return TypeSTRING, true
	default:
		return 0, false
	}
}

// SupportedTypeNames returns a list of supported type names for manual tag entry.
func SupportedTypeNames() []string {
	return []string{"BOOL", "SINT", "INT", "DINT", "LINT", "USINT", "UINT", "UDINT", "ULINT", "REAL", "LREAL", "STRING"}
}
